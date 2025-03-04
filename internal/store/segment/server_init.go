// Copyright 2023 Linkall Inc.
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

//go:generate mockgen -source=server.go -destination=mock_server.go -package=segment
package segment

import (
	// standard libraries.
	"context"
	"path/filepath"
	"time"

	// first-party libraries.
	"github.com/vanus-labs/vanus/observability/log"
	ctrlpb "github.com/vanus-labs/vanus/proto/pkg/controller"
	metapb "github.com/vanus-labs/vanus/proto/pkg/meta"

	// this project.
	"github.com/vanus-labs/vanus/internal/primitive"
	"github.com/vanus-labs/vanus/internal/primitive/vanus"
	"github.com/vanus-labs/vanus/internal/store/block"
	"github.com/vanus-labs/vanus/internal/store/block/raw"
	"github.com/vanus-labs/vanus/internal/store/config"
	"github.com/vanus-labs/vanus/internal/store/meta"
	raft "github.com/vanus-labs/vanus/internal/store/raft/block"
	"github.com/vanus-labs/vanus/internal/store/vsb"
)

func (s *server) Initialize(ctx context.Context) error {
	// TODO(james.yin): how to organize block engine?
	if err := s.loadVSBEngine(ctx, s.cfg.VSB); err != nil {
		return err
	}

	if err := s.initRaftEngine(ctx, s.cfg.Raft); err != nil {
		return err
	}

	// Recover replicas.
	if err := s.recover(ctx); err != nil {
		return err
	}

	// Fetch block information in volume from controller, and make state up to date.
	if err := s.reconcileBlocks(ctx); err != nil {
		return err
	}

	s.state = primitive.ServerStateStarted

	if !s.isDebugMode {
		// Register to controller.
		if err := s.registerSelf(ctx); err != nil {
			return err
		}
	} else {
		log.Info(ctx, "the segment server debug mode enabled", nil)
		s.id = vanus.NewTestID()
		if err := s.Start(ctx); err != nil {
			return err
		}
		s.state = primitive.ServerStateRunning
	}

	return nil
}

func (s *server) loadVSBEngine(ctx context.Context, cfg config.VSB) error {
	dir := filepath.Join(s.cfg.Volume.Dir, "block")
	opts := append([]vsb.Option{
		vsb.WithArchivedListener(block.ArchivedCallback(s.onBlockArchived)),
	}, cfg.Options()...)
	return vsb.Initialize(dir, opts...)
}

func (s *server) initRaftEngine(ctx context.Context, cfg config.Raft) error {
	// TODO(james.yin): move metaStore and offsetStore to raftEngine?

	metaStore, err := meta.RecoverSyncStore(ctx, filepath.Join(s.volumeDir, "meta"), s.cfg.MetaStore.Options()...)
	if err != nil {
		return err
	}

	offsetStore, err := meta.RecoverAsyncStore(ctx, filepath.Join(s.volumeDir, "offset"), s.cfg.OffsetStore.Options()...)
	if err != nil {
		return err
	}

	opts := append([]raft.Option{
		raft.WithStateStore(metaStore),
		raft.WithHintStore(offsetStore),
		raft.WithLeaderChangedListener(s.onLeaderChanged),
		raft.WithEntryAppendedListener(s.onEntryAppended),
	}, cfg.Options()...)
	s.raftEngine.Init(filepath.Join(s.volumeDir, "raft"), s.localAddr, opts...)

	return nil
}

// recover recovers replicas.
func (s *server) recover(ctx context.Context) error {
	vsbEngine, _ := raw.ResolveEngine(raw.VSB)
	raws, err := vsbEngine.Recover(ctx)
	if err != nil {
		return err
	}

	appenders, err := s.raftEngine.Recover(ctx, raws)
	if err != nil {
		return err
	}

	// Recover replicas.
	for id, r := range raws {
		a, ok := appenders[id]
		if !ok {
			log.Warning(ctx, "Not found raft appender for block, maybe it has been deleted or not created.",
				map[string]interface{}{
					"block_id": id,
				})
			// TODO(james.yin): remain this block?
			if err = r.Delete(ctx); err != nil {
				log.Error(ctx, "Failed to delete block", map[string]interface{}{
					"block_id":   id,
					log.KeyError: err,
				})
			}
			continue
		}

		s.replicas.Store(id, &replica{
			id:       id,
			idStr:    id.String(),
			raw:      r,
			appender: a,
		})
	}

	return nil
}

func (s *server) reconcileBlocks(ctx context.Context) error {
	// TODO(james.yin): Fetch block information in volume from controller, and make state up to date.
	return nil
}

func (s *server) registerSelf(ctx context.Context) error {
	// TODO(james.yin): pass information of blocks.
	start := time.Now()
	log.Info(ctx, "connecting to controller", nil)
	if err := s.ctrl.WaitForControllerReady(false); err != nil {
		return err
	}
	res, err := s.cc.RegisterSegmentServer(ctx, &ctrlpb.RegisterSegmentServerRequest{
		Address:  s.localAddr,
		VolumeId: s.volumeID,
		Capacity: s.cfg.Volume.Capacity,
	})
	if err != nil {
		return err
	}
	log.Info(ctx, "connected to controller", map[string]interface{}{
		"used": time.Since(start),
	})
	s.id = vanus.NewIDFromUint64(res.ServerId)

	// FIXME(james.yin): some blocks may not be bound to segment.

	// No block in the volume of this server.
	if len(res.Segments) == 0 {
		return nil
	}

	s.reconcileSegments(ctx, res.Segments)

	return nil
}

func (s *server) reconcileSegments(ctx context.Context, segments map[uint64]*metapb.Segment) {
	for _, segment := range segments {
		if len(segment.Replicas) == 0 {
			continue
		}
		var myID vanus.ID
		for blockID, block := range segment.Replicas {
			// Don't use address to compare.
			if block.VolumeID == s.volumeID {
				if myID != 0 {
					// FIXME(james.yin): multiple blocks of same segment in this server.
					log.Warning(ctx, "Multiple blocks of the same segment in this server.", map[string]interface{}{
						"block_id":   blockID,
						"other":      myID,
						"segment_id": segment.Id,
						"volume_id":  s.volumeID,
					})
				}
				myID = vanus.NewIDFromUint64(blockID)
			}
		}
		if myID == 0 {
			// TODO(james.yin): no my block
			log.Warning(ctx, "No block of the specific segment in this server.", map[string]interface{}{
				"segmentID": segment.Id,
				"volumeID":  s.volumeID,
			})
			continue
		}
		s.registerReplicas(ctx, segment)
	}
}

func (s *server) registerReplicas(ctx context.Context, segment *metapb.Segment) {
	for blockID, block := range segment.Replicas {
		if block.Endpoint == "" {
			if block.VolumeID == s.volumeID {
				block.Endpoint = s.localAddr
			} else {
				log.Info(ctx, "Block is offline.", map[string]interface{}{
					"block_id":    blockID,
					"segment_id":  segment.Id,
					"eventlog_id": segment.EventlogId,
					"volume_id":   block.VolumeID,
				})
				continue
			}
		}
		_ = s.raftEngine.RegisterNodeRecord(blockID, block.Endpoint)
	}
}
