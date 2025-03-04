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
	// standard libraries.
	"context"
	"flag"
	"fmt"
	"net"
	"os"

	// first-party libraries.
	"github.com/vanus-labs/vanus/observability"
	"github.com/vanus-labs/vanus/observability/log"
	"github.com/vanus-labs/vanus/observability/metrics"
	"github.com/vanus-labs/vanus/pkg/util/signal"

	// this project.
	"github.com/vanus-labs/vanus/internal/primitive/vanus"
	"github.com/vanus-labs/vanus/internal/store"
	"github.com/vanus-labs/vanus/internal/store/block/raw"
	"github.com/vanus-labs/vanus/internal/store/segment"
)

var configPath = flag.String("config", "./config/store.yaml", "store config file path")

func main() {
	flag.Parse()

	cfg, err := store.InitConfig(*configPath)
	if err != nil {
		log.Error(context.Background(), "Initialize store config failed.", map[string]interface{}{
			log.KeyError: err,
		})
		os.Exit(-1)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		log.Error(context.Background(), "Listen tcp port failed.", map[string]interface{}{
			log.KeyError: err,
			"port":       cfg.Port,
		})
		os.Exit(-1)
	}

	ctx := signal.SetupSignalContext()
	cfg.Observability.T.ServerName = "Vanus Store"
	_ = observability.Initialize(ctx, cfg.Observability, metrics.GetSegmentServerMetrics)
	srv := segment.NewServer(*cfg)

	if err = srv.Initialize(ctx); err != nil {
		log.Error(ctx, "The SegmentServer has initialized failed.", map[string]interface{}{
			log.KeyError: err,
		})
		os.Exit(-2)
	}

	log.Info(ctx, "The SegmentServer ready to work.", map[string]interface{}{
		"listen_ip":   cfg.IP,
		"listen_port": cfg.Port,
	})

	if err = vanus.InitSnowflake(ctx, cfg.ControllerAddresses,
		vanus.NewNode(vanus.StoreService, cfg.Volume.ID)); err != nil {
		log.Error(context.Background(), "init id generator failed", map[string]interface{}{
			log.KeyError: err,
			"port":       cfg.Port,
		})
		os.Exit(-3)
	}
	defer vanus.DestroySnowflake()

	go func() {
		if err = srv.Serve(listener); err != nil {
			log.Error(ctx, "The SegmentServer occurred an error.", map[string]interface{}{
				log.KeyError: err,
			})
			return
		}
	}()

	select {
	case <-ctx.Done():
		log.Info(ctx, "received system signal, preparing exit", nil)
	}
	raw.CloseAllEngine()
	log.Info(ctx, "The SegmentServer has been shutdown.", nil)
}
