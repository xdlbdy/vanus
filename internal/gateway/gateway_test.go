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
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	ce "github.com/cloudevents/sdk-go/v2"
	cehttp "github.com/cloudevents/sdk-go/v2/protocol/http"
	. "github.com/golang/mock/gomock"
	. "github.com/prashantv/gostub"
	. "github.com/smartystreets/goconvey/convey"

	"github.com/vanus-labs/vanus/client"
	"github.com/vanus-labs/vanus/client/pkg/api"
	"github.com/vanus-labs/vanus/internal/primitive"
	"github.com/vanus-labs/vanus/internal/primitive/vanus"
)

func TestGateway_NewGateway(t *testing.T) {
	Convey("test new gateway ", t, func() {
		c := Config{
			Port:           8080,
			ControllerAddr: []string{"127.0.0.1"},
		}
		ceGa := NewGateway(c)
		So(ceGa.config.Port, ShouldEqual, 8080)
		So(ceGa.config.ControllerAddr[0], ShouldEqual, "127.0.0.1")
	})
}

func TestGateway_StartReceive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ga := &ceGateway{
		config: Config{
			Port: 18080,
		},
	}
	Convey("test start receive ", t, func() {
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()
		err := ga.startCloudEventsReceiver(ctx)
		So(err, ShouldBeNil)
	})
}

func TestGateway_receive(t *testing.T) {
	ctx := context.Background()
	ga := &ceGateway{}
	Convey("test receive failure1 ", t, func() {
		e := ce.NewEvent()
		reqData := &cehttp.RequestData{
			URL: &url.URL{
				Opaque: "/test",
			},
		}
		stub := StubFunc(&requestDataFromContext, reqData)
		defer stub.Reset()
		_, ret := ga.receive(ctx, e)
		So(ret, ShouldBeError)
	})

	Convey("test receive failure2", t, func() {
		e := ce.NewEvent()
		reqData := &cehttp.RequestData{
			URL: &url.URL{
				Opaque: "/gateway/test",
			},
		}
		e.SetExtension(primitive.XVanusDeliveryTime, "2006-01-02T15:04:05")
		stub := StubFunc(&requestDataFromContext, reqData)
		defer stub.Reset()
		_, ret := ga.receive(ctx, e)
		So(ret, ShouldBeError)
	})

	// Convey("test receive failure3", t, func() {
	// 	e := ce.NewEvent()
	// 	reqData := &cehttp.RequestData{
	// 		URL: &url.URL{
	// 			Opaque: "/gateway/test",
	// 		},
	// 	}
	// 	e.SetExtension(xceVanusDeliveryTime, "2006-01-02T15:04:05Z")
	// 	stub := StubFunc(&requestDataFromContext, reqData)
	// 	defer stub.Reset()
	// 	ga.config = Config{
	// 		ControllerAddr: []string{"127.0.0.1"},
	// 	}
	// 	ret := ga.receive(ctx, e)
	// 	So(ret, ShouldBeError)
	// })
}

func TestGateway_getEventbusFromPath(t *testing.T) {
	Convey("test get eventbus from path return nil ", t, func() {
		reqData := &cehttp.RequestData{
			URL: &url.URL{
				Opaque: "/test",
			},
		}
		_, err := getEventbusFromPath(reqData)
		So(err.Error(), ShouldEqual, "invalid eventbus id")
	})
	Convey("test get eventbus from path return path ", t, func() {
		vid := vanus.NewTestID()
		reqData := &cehttp.RequestData{
			URL: &url.URL{
				Opaque: fmt.Sprintf("/gateway/%s", vid),
			},
		}
		id, err := getEventbusFromPath(reqData)
		So(err, ShouldBeNil)
		So(id, ShouldEqual, vid)
	})
}

func TestGateway_EventID(t *testing.T) {
	ctrl := NewController(t)
	defer ctrl.Finish()
	var (
		busID       = vanus.NewTestID()
		controllers = []string{"127.0.0.1:2048"}
		port        = 8087
	)

	mockClient := client.NewMockClient(ctrl)
	mockEventbus := api.NewMockEventbus(ctrl)
	mockBusWriter := api.NewMockBusWriter(ctrl)
	mockClient.EXPECT().Eventbus(Any(), Any()).AnyTimes().Return(mockEventbus)
	mockEventbus.EXPECT().Writer().AnyTimes().Return(mockBusWriter)
	mockBusWriter.EXPECT().Append(Any(), Any()).AnyTimes().Return([]string{"AABBCC"}, nil)

	cfg := Config{
		Port:           port,
		ControllerAddr: controllers,
	}
	ga := NewGateway(cfg)

	ga.proxySrv.SetClient(mockClient)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ga.startCloudEventsReceiver(ctx)

	time.Sleep(50 * time.Millisecond)

	Convey("test put event and receive response event", t, func() {
		p, err := ce.NewHTTP()
		So(err, ShouldBeNil)
		c, err := ce.NewClient(p, ce.WithTimeNow(), ce.WithUUIDs())
		So(err, ShouldBeNil)

		event := ce.NewEvent()
		event.SetID("example-event")
		event.SetSource("example/uri")
		event.SetType("example.type")
		_ = event.SetData(ce.ApplicationJSON, map[string]string{"hello": "world"})

		ctx := ce.ContextWithTarget(context.Background(),
			fmt.Sprintf("http://127.0.0.1:%d/gateway/%s", cfg.GetCloudEventReceiverPort(), busID))
		resEvent, res := c.Request(ctx, event)
		So(ce.IsACK(res), ShouldBeTrue)
		var httpResult *cehttp.Result
		ce.ResultAs(res, &httpResult)
		So(httpResult, ShouldNotBeNil)

		So(resEvent, ShouldBeNil)
	})
}
