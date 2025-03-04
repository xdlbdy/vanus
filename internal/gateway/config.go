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

package gateway

import (
	"google.golang.org/grpc/credentials/insecure"

	"github.com/vanus-labs/vanus/observability"

	"github.com/vanus-labs/vanus/internal/gateway/proxy"
	"github.com/vanus-labs/vanus/internal/primitive"
)

type Config struct {
	Port                 int                  `yaml:"port"`
	SinkPort             int                  `yaml:"sink_port"`
	Observability        observability.Config `yaml:"observability"`
	ControllerAddr       []string             `yaml:"controllers"`
	GRPCReflectionEnable bool                 `yaml:"grpc_reflection_enable"`
}

func (c Config) GetProxyConfig() proxy.Config {
	return proxy.Config{
		Endpoints:              c.ControllerAddr,
		SinkPort:               c.SinkPort,
		ProxyPort:              c.Port,
		CloudEventReceiverPort: c.GetCloudEventReceiverPort(),
		GRPCReflectionEnable:   c.GRPCReflectionEnable,
		Credentials:            insecure.NewCredentials(),
	}
}

func (c Config) GetCloudEventReceiverPort() int {
	return c.Port + 1
}

func InitConfig(filename string) (*Config, error) {
	c := new(Config)
	err := primitive.LoadConfig(filename, c)
	if err != nil {
		return nil, err
	}
	return c, nil
}
