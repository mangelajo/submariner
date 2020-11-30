package nbctl

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/log"
	"k8s.io/klog"

	"github.com/submariner-io/submariner/pkg/util"
)

type NbCtl struct {
	clientKey        string
	clientCert       string
	ca               string
	connectionString string
}

func New(db, clientkey, clientcert, ca string) *NbCtl {
	return &NbCtl{
		connectionString: db,
		clientKey:        clientkey,
		clientCert:       clientcert,
		ca:               ca,
	}
}

func (n *NbCtl) nbctl(parameters ...string) (output string, err error) {
	allParameters := []string{
		fmt.Sprintf("--db=%s", n.connectionString),
		"-c", n.clientCert,
		"-p", n.clientKey,
		"-C", n.ca,
	}

	allParameters = append(allParameters, parameters...)

	cmd := exec.Command("/usr/bin/ovn-nbctl", allParameters...)

	klog.V(log.DEBUG).Infof("ovn-nbctl %v", allParameters)

	out, err := cmd.CombinedOutput()

	strOut := string(out)

	if err != nil {
		klog.Errorf("error running ovn-nbctl %+v, output:\n%s", err, strOut)
		return strOut, err
	}

	return strOut, err
}

func (n *NbCtl) SetGatewayChassis(lrp, chassis string, prio int) error {
	_, err := n.nbctl("lrp-set-gateway-chassis", lrp, chassis, strconv.Itoa(prio))
	return err
}

func (n *NbCtl) DelGatewayChassis(lrp, chassis string, prio int) error {
	_, err := n.nbctl("lrp-del-gateway-chassis", lrp, chassis, strconv.Itoa(prio))
	return err
}

func (n *NbCtl) GetGatewayChassis(lrp, chassis string) (string, error) {
	output, err := n.nbctl("lrp-get-gateway-chassis", lrp, chassis)
	return output, err
}

func (n *NbCtl) LrPolicyAdd(logicalRouter string, prio int, filter string, actions ...string) error {
	allParameters := []string{"lr-policy-add", logicalRouter, strconv.Itoa(prio), filter}
	allParameters = append(allParameters, actions...)
	_, err := n.nbctl(allParameters...)

	return err
}
func (n *NbCtl) LrPolicyDel(logicalRouter string, prio int, filter string) error {
	_, err := n.nbctl("lr-policy-del", logicalRouter, strconv.Itoa(prio), filter)
	return err
}

func (n *NbCtl) LrPolicyGetSubnets(logicalRouter, rerouteIp string) (*util.StringSet, error) {
	output, err := n.nbctl("lr-policy-list", logicalRouter)
	if err != nil {
		return nil, errors.Wrapf(err, "error getting existing routing policies for router %q", logicalRouter)
	}

	return parseLrPolicyGetOutput(output, rerouteIp), nil
}

func parseLrPolicyGetOutput(output, rerouteIp string) *util.StringSet {
	// Example output:
	// $ ovn-nbctl lr-policy-list submariner_router
	// Routing Policies
	//        10                                 ip.src==1.1.1.1/32         reroute              169.254.34.1
	//        10                                 ip.src==1.1.1.2/32         reroute              169.254.34.1
	//
	subnets := util.NewStringSet()
	//TODO: make this regex more generic in a global variable, so we avoid re-compiling the regex on each call
	r := regexp.MustCompile("ip4\\.dst == ([0-9\\.]+/[0-9]+)[\\s\\t]+reroute[\\s\\t]+" + rerouteIp)
	for _, match := range r.FindAllStringSubmatch(output, -1) {
		if len(match) == 2 {
			subnets.Add(match[1])
		}
	}

	return subnets
}