package cable

import (
	"fmt"

	"github.com/submariner-io/submariner/pkg/cable/ipsec"
	"github.com/submariner-io/submariner/pkg/types"
)

const (
	IPsec      = "ipsec"
	WireGuard  = "wireguard"
	StrongSwan = "strongswan"
	LibreSwan  = "libreswan"
	Vxlan      = "vxlan"

	DriverImpl = "driver"
)

// Driver is used by the ipsec engine to actually connect the tunnels.
type Driver interface {

	// Init initializes the driver with any state it needs.
	Init() error

	// GetActiveConnections returns an array of all the active connections for the given cluster.
	GetActiveConnections(clusterID string) ([]string, error)

	// ConnectToEndpoint establishes a connection to the given endpoint and returns a string
	// representation of the IP address of the target endpoint.
	ConnectToEndpoint(endpoint types.SubmarinerEndpoint) (string, error)

	// DisconnectFromEndpoint disconnects from the connection to the given endpoint.
	DisconnectFromEndpoint(endpoint types.SubmarinerEndpoint) error
}

func NewDriver(localSubnets []string, localEndpoint types.SubmarinerEndpoint) (Driver, error) {
	switch localEndpoint.Spec.Backend {
	case IPsec:
		driver := StrongSwan
		if localEndpoint.Spec.BackendConfig != nil {
			if d, ok := localEndpoint.Spec.BackendConfig[DriverImpl]; ok {
				driver = d
			}
		}
		switch driver {
		case StrongSwan:
			return ipsec.NewStrongSwan(localSubnets, localEndpoint)
		case LibreSwan:
			// TODO add LibreSwan support
		}
		return nil, fmt.Errorf("Unsupported %s driver for %s", driver, IPsec)
	case WireGuard:
		// TODO add WireGuard support
	}
	// TODO define ERROR
	return nil, fmt.Errorf("Unsupported backend type - %s", localEndpoint.Spec.Backend)
}
