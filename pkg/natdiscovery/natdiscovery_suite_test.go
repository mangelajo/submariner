/*
© 2021 Red Hat, Inc. and others.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package natdiscovery

import (
	"net"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"
	"k8s.io/klog"

	submarinerv1 "github.com/submariner-io/submariner/pkg/apis/submariner.io/v1"
	natproto "github.com/submariner-io/submariner/pkg/natdiscovery/proto"
	"github.com/submariner-io/submariner/pkg/types"
)

func init() {
	klog.InitFlags(nil)
}

func TestNATDiscovery(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NAT Discovery Suite")
}

const (
	testLocalEndpointName = "cluster-a-ep-1"
	testLocalClusterID    = "cluster-a"
	testLocalPublicIP     = "10.1.1.1"
	testLocalPrivateIP    = "2.2.2.2"

	testRemoteEndpointName        = "cluster-b-ep-1"
	testRemoteUnknownEndpointName = "cluster-b-ep-unknown"
	testRemoteClusterID           = "cluster-b"
	testRemotePublicIP            = "10.3.3.3"
	testRemotePrivateIP           = "4.4.4.4"
)

var (
	testLocalNATPort  int32 = 1234
	testRemoteNATPort int32 = 4321
)

func parseProtocolRequest(buf []byte) *natproto.SubmarinerNatDiscoveryRequest {
	msg := natproto.SubmarinerNatDiscoveryMessage{}
	if err := proto.Unmarshal(buf, &msg); err != nil {
		klog.Errorf("error unmarshaling message received on UDP port %d: %s", natproto.DefaultPort, err)
	}

	request := msg.GetRequest()
	Expect(request).NotTo(BeNil())

	return request
}

func parseProtocolResponse(buf []byte) *natproto.SubmarinerNatDiscoveryResponse {
	msg := natproto.SubmarinerNatDiscoveryMessage{}
	if err := proto.Unmarshal(buf, &msg); err != nil {
		klog.Errorf("error unmarshaling message received on UDP port %d: %s", natproto.DefaultPort, err)
	}

	response := msg.GetResponse()
	Expect(response).NotTo(BeNil())

	return response
}

func createTestListener(endpoint *types.SubmarinerEndpoint) (*natDiscovery, chan []byte) {
	listener, err := newNatDiscovery(endpoint)
	readyChannel := make(chan *NATEndpointInfo, 100)
	listener.SetReadyChannel(readyChannel)
	Expect(err).NotTo(HaveOccurred())

	udpSentChannel := make(chan []byte, 10)
	listener.serverUDPWrite = func(b []byte, addr *net.UDPAddr) (int, error) {
		udpSentChannel <- b
		return len(b), nil
	}

	return listener, udpSentChannel
}
func createTestLocalEndpoint() types.SubmarinerEndpoint {
	return types.SubmarinerEndpoint{
		Spec: submarinerv1.EndpointSpec{
			CableName:        testLocalEndpointName,
			ClusterID:        testLocalClusterID,
			PublicIP:         testLocalPublicIP,
			PrivateIP:        testLocalPrivateIP,
			NATDiscoveryPort: &testLocalNATPort,
		},
	}
}

func createTestRemoteEndpoint() types.SubmarinerEndpoint {
	return types.SubmarinerEndpoint{
		Spec: submarinerv1.EndpointSpec{
			CableName:        testRemoteEndpointName,
			ClusterID:        testRemoteClusterID,
			PublicIP:         testRemotePublicIP,
			PrivateIP:        testRemotePrivateIP,
			NATDiscoveryPort: &testRemoteNATPort,
		},
	}
}

func createTestUnknownRemoteEndpoint() types.SubmarinerEndpoint {
	return types.SubmarinerEndpoint{
		Spec: submarinerv1.EndpointSpec{
			CableName:        testRemoteUnknownEndpointName,
			ClusterID:        testRemoteClusterID,
			PublicIP:         testRemotePublicIP,
			PrivateIP:        testRemotePrivateIP,
			NATDiscoveryPort: &testRemoteNATPort,
		},
	}
}
