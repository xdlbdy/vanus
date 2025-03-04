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

package command

var (
	// for vsctl event.
	id                string
	dataFormat        string
	eventSource       string
	eventType         string
	eventData         string
	eventDeliveryTime string
	eventDelayTime    string
	dataFile          string
	printDataTemplate bool
	offset            int64
	number            int16
	detail            bool
	eventID           string
	eventCreateTime   string

	// for both of eventbus and subscription.
	namespace           string
	eventbus            string
	eventlogID          uint64
	eventlogNum         int32
	sink                string
	filters             string
	transformer         string
	rateLimit           int32
	from                string
	subscriptionIDStr   string
	description         string
	subscriptionName    string
	disableSubscription bool

	orderedPushEvent     bool
	orderedPushEventStr  string
	disableDeadLetter    bool
	disableDeadLetterStr string

	subProtocol        string
	sinkCredentialType string
	sinkCredential     string
	deliveryTimeout    int32
	maxRetryAttempts   int32
	offsetTimestamp    uint64

	showSegment bool
	showBlock   bool

	// for cluster
	clusterConfigFile   string
	clusterVersion      string
	showInstallableList bool
	showUpgradeableList bool
	controllerReplicas  int32
	storeReplicas       int32
	triggerReplicas     int32

	// for connector
	connectorConfigFile string
	kind                string
	name                string
	ctype               string
	annotations         string
	connectorVersion    string
	showConnectors      bool

	startOffset uint64
	endOffset   uint64
)

const (
	AWSCredentialType    = "aws"
	GCloudCredentialType = "gcloud"
)
