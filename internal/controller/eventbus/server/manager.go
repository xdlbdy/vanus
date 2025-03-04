// Copyright 2022 Linkall Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:generate mockgen -source=manager.go -destination=mock_manager.go -package=server
package server

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/vanus-labs/vanus/observability/log"
	"github.com/vanus-labs/vanus/pkg/errors"
	segpb "github.com/vanus-labs/vanus/proto/pkg/segment"

	"github.com/vanus-labs/vanus/internal/primitive/vanus"
)

type Manager interface {
	AddServer(ctx context.Context, srv Server) error
	RemoveServer(ctx context.Context, srv Server) error
	GetServerByAddress(addr string) Server
	GetServerByServerID(id vanus.ID) Server
	Run(ctx context.Context) error
	Stop(ctx context.Context)
	CanCreateEventbus(ctx context.Context, replicaNum int) bool
}

const (
	serverStateRunning = "running"
)

func NewServerManager() Manager {
	return &segmentServerManager{
		ticker:                   time.NewTicker(time.Second),
		segmentServerCredentials: insecure.NewCredentials(),
	}
}

type segmentServerManager struct {
	segmentServerCredentials credentials.TransportCredentials
	mutex                    sync.Mutex
	// map[string]Server
	segmentServerMapByIP sync.Map
	// map[vanus.ID]Server
	segmentServerMapByID sync.Map
	cancelCtx            context.Context
	cancel               func()
	ticker               *time.Ticker
	onlineServerNumber   int64
}

func (mgr *segmentServerManager) AddServer(ctx context.Context, srv Server) error {
	if srv == nil {
		return nil
	}
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()

	v, exist := mgr.segmentServerMapByIP.Load(srv.Address())
	if exist {
		srvOld, _ := v.(Server)
		if srv.ID().Equals(srvOld.ID()) {
			return nil
		}
		return errors.ErrSegmentServerHasBeenAdded
	}
	mgr.segmentServerMapByIP.Store(srv.Address(), srv)
	mgr.segmentServerMapByID.Store(srv.ID().Key(), srv)
	atomic.AddInt64(&mgr.onlineServerNumber, 1)
	log.Info(ctx, "the segment server added", map[string]interface{}{
		"server_id": srv.ID(),
		"addr":      srv.Address(),
		"online":    atomic.LoadInt64(&mgr.onlineServerNumber),
	})
	return nil
}

func (mgr *segmentServerManager) RemoveServer(ctx context.Context, srv Server) error {
	if srv == nil {
		return nil
	}
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()
	mgr.segmentServerMapByIP.Delete(srv.Address())
	mgr.segmentServerMapByID.Delete(srv.ID().Key())
	atomic.AddInt64(&mgr.onlineServerNumber, -1)
	log.Info(ctx, "the segment server was removed", map[string]interface{}{
		"server_id": srv.ID(),
		"addr":      srv.Address(),
		"online":    atomic.LoadInt64(&mgr.onlineServerNumber),
	})
	return nil
}

func (mgr *segmentServerManager) GetServerByAddress(addr string) Server {
	v, exist := mgr.segmentServerMapByIP.Load(addr)
	if exist {
		return v.(Server)
	}
	return nil
}

func (mgr *segmentServerManager) GetServerByServerID(id vanus.ID) Server {
	v, exist := mgr.segmentServerMapByID.Load(id.Key())
	if exist {
		return v.(Server)
	}
	return nil
}

func (mgr *segmentServerManager) Run(ctx context.Context) error {
	newCtx := context.Background()
	mgr.cancelCtx, mgr.cancel = context.WithCancel(newCtx)
	go func() {
		for {
			select {
			case <-mgr.cancelCtx.Done():
				return
			case <-mgr.ticker.C:
				mgr.segmentServerMapByIP.Range(func(key, value interface{}) bool {
					srv, ok := value.(Server)
					if !ok {
						mgr.segmentServerMapByIP.Delete(key)
					}
					if !srv.IsActive(ctx) {
						mgr.segmentServerMapByIP.Delete(srv.Address())
						mgr.segmentServerMapByID.Delete(srv.ID().Key())
						log.Info(newCtx, "the server isn't active", map[string]interface{}{
							"id":      srv.ID(),
							"address": srv.Address(),
							"up_time": srv.Uptime(),
						})
					}
					return true
				})
			}
		}
	}()
	return nil
}

func (mgr *segmentServerManager) Stop(ctx context.Context) {
	mgr.cancel()
	mgr.segmentServerMapByIP.Range(func(key, value interface{}) bool {
		srv, ok := value.(Server)
		if !ok {
			mgr.segmentServerMapByIP.Delete(key)
		}

		err := srv.(*segmentServer).grpcConn.Close()
		if err != nil {
			log.Warning(ctx, "close grpc connection error", map[string]interface{}{
				"id":         srv.ID(),
				"address":    srv.Address(),
				"up_time":    srv.Uptime(),
				log.KeyError: err,
			})
		} else {
			log.Info(ctx, "the connection to server was closed", map[string]interface{}{
				"id":      srv.ID(),
				"address": srv.Address(),
				"up_time": srv.Uptime(),
			})
		}

		return true
	})
}

func (mgr *segmentServerManager) CanCreateEventbus(ctx context.Context, replicaNum int) bool {
	activeNum := 0
	mgr.segmentServerMapByID.Range(func(_, value any) bool {
		s, _ := value.(Server)
		if s.IsActive(ctx) {
			activeNum++
		}
		return true
	})
	return activeNum >= replicaNum
}

type Server interface {
	RemoteStart(ctx context.Context) error
	RemoteStop(ctx context.Context)
	GetClient() segpb.SegmentServerClient
	ID() vanus.ID
	Address() string
	Close() error
	Polish()
	IsActive(ctx context.Context) bool
	Uptime() time.Time
}

type segmentServer struct {
	id                vanus.ID
	addr              string
	grpcConn          *grpc.ClientConn
	client            segpb.SegmentServerClient
	uptime            time.Time
	lastHeartbeatTime time.Time
}

type Getter func(id vanus.ID, addr string) (Server, error)

var (
	getter Getter = newSegmentServerWithID
	mutex  sync.Mutex
)

func NewSegmentServerWithID(id vanus.ID, addr string) (Server, error) {
	return getter(id, addr)
}

func MockServerGetter(gt Getter) {
	mutex.Lock()
	defer mutex.Unlock()
	getter = gt
}

func MockReset() {
	mutex.Lock()
	defer mutex.Unlock()
	getter = newSegmentServerWithID
}

func newSegmentServerWithID(id vanus.ID, addr string) (Server, error) {
	srv, err := NewSegmentServer(addr)
	if err != nil {
		return nil, err
	}
	srv.(*segmentServer).id = id
	return srv, nil
}

func NewSegmentServer(addr string) (Server, error) {
	id, err := vanus.NewID()
	if err != nil {
		return nil, err
	}
	srv := &segmentServer{
		id:                id,
		addr:              addr,
		uptime:            time.Now(),
		lastHeartbeatTime: time.Now(),
	}
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}
	srv.grpcConn = conn
	srv.client = segpb.NewSegmentServerClient(conn)
	return srv, nil
}

func (ss *segmentServer) RemoteStart(ctx context.Context) error {
	_, err := ss.client.Start(ctx, &segpb.StartSegmentServerRequest{
		ServerId: ss.id.Uint64(),
	})
	if err != nil {
		log.Warning(ctx, "start server failed", map[string]interface{}{
			log.KeyError: err,
			"server_id":  ss.id,
		})
		return err
	}
	return nil
}

func (ss *segmentServer) RemoteStop(ctx context.Context) {
	_, err := ss.client.Stop(ctx, &segpb.StopSegmentServerRequest{})
	if err != nil {
		log.Warning(ctx, "stop server failed", map[string]interface{}{
			log.KeyError: err,
			"server_id":  ss.id,
		})
	}
}

func (ss *segmentServer) GetClient() segpb.SegmentServerClient {
	return ss.client
}

func (ss *segmentServer) ID() vanus.ID {
	return ss.id
}

func (ss *segmentServer) Address() string {
	return ss.addr
}

func (ss *segmentServer) Close() error {
	return ss.grpcConn.Close()
}

func (ss *segmentServer) Polish() {
	ss.lastHeartbeatTime = time.Now()
}

func (ss *segmentServer) IsActive(ctx context.Context) bool {
	res, err := ss.client.Status(ctx, &emptypb.Empty{})
	if err != nil {
		log.Warning(ctx, "ping segment server failed", map[string]interface{}{
			log.KeyError: err,
			"address":    ss.addr,
		})
		return false
	}

	// maximum heartbeat interval is 1 minute
	// return time.Now().Sub(ss.lastHeartbeatTime) > time.Minute
	// TODO optimize here
	return res.Status == serverStateRunning
}

func (ss *segmentServer) Uptime() time.Time {
	return ss.uptime
}
