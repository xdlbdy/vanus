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

package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"sync"

	"google.golang.org/grpc"

	"github.com/vanus-labs/vanus/observability"
	"github.com/vanus-labs/vanus/observability/log"
	"github.com/vanus-labs/vanus/observability/metrics"
	"github.com/vanus-labs/vanus/pkg/util/signal"
	pbtrigger "github.com/vanus-labs/vanus/proto/pkg/trigger"

	"github.com/vanus-labs/vanus/internal/primitive"
	"github.com/vanus-labs/vanus/internal/trigger"
)

var configPath = flag.String("config", "./config/trigger.yaml", "trigger worker config file path")

func main() {
	flag.Parse()

	cfg, err := trigger.InitConfig(*configPath)
	if err != nil {
		log.Error(context.Background(), "init config error", map[string]interface{}{
			log.KeyError: err,
		})
		os.Exit(-1)
	}
	listen, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		log.Error(context.Background(), "failed to listen", map[string]interface{}{
			log.KeyError: err,
		})
		os.Exit(-1)
	}
	ctx := signal.SetupSignalContext()
	_ = observability.Initialize(ctx, cfg.Observability, metrics.GetTriggerMetrics)
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	srv := trigger.NewTriggerServer(*cfg)
	pbtrigger.RegisterTriggerWorkerServer(grpcServer, srv)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info(ctx, "the grpc server ready to work", nil)
		err = grpcServer.Serve(listen)
		if err != nil {
			log.Error(ctx, "grpc server occurred an error", map[string]interface{}{
				log.KeyError: err,
			})
		}
	}()
	init := srv.(primitive.Initializer)
	if err = init.Initialize(ctx); err != nil {
		log.Error(ctx, "the trigger worker has initialized failed", map[string]interface{}{
			log.KeyError: err,
		})
		os.Exit(1)
	}
	<-ctx.Done()
	closer := srv.(primitive.Closer)
	closer.Close(ctx)
	grpcServer.GracefulStop()
	wg.Wait()
	log.Info(ctx, "trigger worker stopped", nil)
}
