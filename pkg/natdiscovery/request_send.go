/*
© 2021 Red Hat, Inc. and others

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

	"github.com/google/gopacket/routing"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"k8s.io/klog"

	natproto "github.com/submariner-io/submariner/pkg/natdiscovery/proto"
)

func (nd *natDiscovery) sendCheckRequestByRemoteID(id string) error {
	remoteNAT := nd.remoteEndpoints[id]
	return nd.sendCheckRequest(remoteNAT)
}

func (nd *natDiscovery) sendCheckRequest(remoteNAT *remoteEndpointNAT) error {
	var errPrivate, errPublic error
	var reqID uint64
	if remoteNAT.endpoint.Spec.PrivateIP != "" {
		reqID, errPrivate = nd.sendCheckRequestToTargetIP(remoteNAT, remoteNAT.endpoint.Spec.PrivateIP)
		if errPrivate == nil {
			remoteNAT.lastPrivateIPRequestID = reqID
		}
	}

	if remoteNAT.endpoint.Spec.PublicIP != "" {
		reqID, errPublic = nd.sendCheckRequestToTargetIP(remoteNAT, remoteNAT.endpoint.Spec.PublicIP)
		if errPublic == nil {
			remoteNAT.lastPublicIPRequestID = reqID
		}
	}

	if errPrivate != nil && errPublic != nil {
		return errors.Errorf("error while trying to discover both public & private IPs of endpoint %q, [%s, %s]",
			remoteNAT.endpoint.Spec.CableName, errPublic, errPrivate)
	}

	if errPrivate != nil {
		return errors.Wrapf(errPrivate, "error while trying to NAT-discover private IP of endpoint %q",
			remoteNAT.endpoint.Spec.CableName)
	}

	if errPublic != nil {
		return errors.Wrapf(errPublic, "error while trying to NAT-discover public IP of endpoint %q",
			remoteNAT.endpoint.Spec.CableName)
	}

	return nil
}

func (nd *natDiscovery) sendCheckRequestToTargetIP(remoteNAT *remoteEndpointNAT, targetIP string) (uint64, error) {
	targetPort, err := extractNATDiscoveryPort(remoteNAT.endpoint)

	if err != nil {
		return 0, err
	}

	sourceIP, err := nd.findSrcIP(targetIP)
	if err != nil {
		klog.Warningf("unable to determine source IP while preparing NAT discovery request to endpoint %q: %s",
			remoteNAT.endpoint.Spec.CableName, err)
	}

	nd.requestCounter++

	request := &natproto.SubmarinerNatDiscoveryRequest{
		RequestNumber: nd.requestCounter,
		Sender: &natproto.EndpointDetails{
			EndpointId: nd.localEndpoint.Spec.CableName,
			ClusterId:  nd.localEndpoint.Spec.ClusterID,
		},
		Receiver: &natproto.EndpointDetails{
			EndpointId: remoteNAT.endpoint.Spec.CableName,
			ClusterId:  remoteNAT.endpoint.Spec.ClusterID,
		},
		UsingSrc: &natproto.IPPortPair{
			IP:   sourceIP,
			Port: uint32(nd.serverPort),
		},
		UsingDst: &natproto.IPPortPair{
			IP:   targetIP,
			Port: uint32(targetPort),
		},
	}

	msgRequest := &natproto.SubmarinerNatDiscoveryMessage_Request{
		Request: request,
	}

	message := natproto.SubmarinerNatDiscoveryMessage{
		Version: natproto.Version,
		Message: msgRequest,
	}

	buf, err := proto.Marshal(&message)
	if err != nil {
		return request.RequestNumber, errors.Wrapf(err, "error marshaling request %#v", request)
	}

	addr := net.UDPAddr{
		IP:   net.ParseIP(targetIP),
		Port: int(targetPort),
	}

	if length, err := nd.serverUDPWrite(buf, &addr); err != nil {
		return request.RequestNumber, errors.Wrapf(err, "error sending request packet %#v", request)
	} else if length != len(buf) {
		return request.RequestNumber, errors.Errorf("the sent UDP packet was smaller than requested, sent=%d, expected=%d", length,
			len(buf))
	}

	remoteNAT.checkSent()

	return request.RequestNumber, nil
}

func findPreferredSourceIP(destinationIP string) (string, error) {
	var ip net.IP
	if ip = net.ParseIP(destinationIP); ip == nil {
		return "", errors.Errorf("error parsing destination IP %q while trying to figure out preferred source IP", destinationIP)
	}

	router, err := routing.New()
	if err != nil {
		return "", errors.Wrap(err, "error while creating gopacket routing object")
	}

	_, _, preferredSourceIP, err := router.Route(ip)
	if err != nil {
		return "", errors.Wrapf(err, "error finding src IP in route to IP %q", ip.String())
	}

	return preferredSourceIP.String(), nil
}
