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
package libreswan

import (
	v1 "github.com/submariner-io/submariner/pkg/apis/submariner.io/v1"
	"github.com/submariner-io/submariner/pkg/types"

	"k8s.io/klog"
)

type operationMode int

const (
	operationModeBidirectional operationMode = iota
	operationModeServer
	operationModeClient
)

var operationModeName = map[operationMode]string{
	operationModeBidirectional: "bi-directional",
	operationModeServer:        "server",
	operationModeClient:        "client",
}

func (i *libreswan) calculateOperationMode(remoteEndpoint *types.SubmarinerEndpoint) operationMode {
	defaultValue := false
	leftPreferred, err := i.localEndpoint.Spec.GetBackendBool(v1.PreferredServerConfig, &defaultValue)
	if err != nil {
		klog.Errorf("Error parsing local endpoint config: %s", err)
	}

	rightPreferred, err := remoteEndpoint.Spec.GetBackendBool(v1.PreferredServerConfig, nil)
	if err != nil {
		klog.Errorf("Error parsing remote endpoint config %q: %s", remoteEndpoint.Spec.CableName, err)
	}

	if rightPreferred == nil || !*leftPreferred && !*rightPreferred {
		return operationModeBidirectional
	}

	if *leftPreferred && !*rightPreferred {
		return operationModeServer
	} else if *rightPreferred && !*leftPreferred {
		return operationModeClient
	}

	// At this point both would like to be server, so we decide based on the cable name
	if i.localEndpoint.Spec.CableName > remoteEndpoint.Spec.CableName {
		return operationModeServer
	}

	return operationModeClient
}
