# Submariner

Submariner is a tool built to connect overlay networks of different Kubernetes clusters. While most testing is performed against Kubernetes clusters that have enabled Flannel/Canal, Submariner should be compatible with any CNI-compatible cluster network provider, as it utilizes off-the-shelf components such as strongSwan/Charon to establish IPsec tunnels between each Kubernetes cluster.

Note that Submariner is in the <strong>pre-alpha</strong> stage, and should not be used for production purposes. While we welcome usage/experimentation with it, it is quite possible that you could run into severe bugs with it, and as such this is why it has this labeled status.

# Architecture

The basic architecture diagram for Submariner is as follows:

![submariner architecture](https://raw.githubusercontent.com/rancher/submariner/master/docs/img/architecture.png)

Submariner consists of a few components that work and operate off of Kubernetes Custom Resource Definitions (CRDs). The Submariner CRDs are `clusters.submariner.io` and `endpoints.submariner.io`. 

The two primary Submariner components within connected clusters are:

- submariner (Deployment)
- submariner-route-agent (DaemonSet)

The submariner pods are run on the gateway nodes, and will perform leader election between them to elect an active IPsec endpoint. On gateway nodes that are idle, the `submariner-route-agent` will configure routes to enable full connectivity to remote clusters from non-leader gateway and worker nodes. `submariner-route-agent` as a component is run on every node, and is aware of the current leader. When run on the gateway node that is the leader, it will simply sit idle awaiting leader loss.

Upon startup, the `submariner` pod that is elected leader will perform a reconciliation process that ensures it is the sole endpoint for this cluster. This is part of the reason why it is important to have unique cluster IDs between clusters, as two clusters with the same ID will reconcile each other out of existence.

Upon failure, another Submariner pod (on one of the other gateway hosts) will gain leadership and perform reconciliation to ensure it is the active leader. When done, the remote clusters will reconcile the IPsec endpoint to the new endpoint, and connection will be re-established. In the interim, the `submariner-route-agent` pods will update the route tables on each node to point towards the new endpoint host.

Submariner uses a central broker to facilitate the exchange of information and sync CRD's between clusters. The `datastoresyncer` runs as a controller within the leader-elected `submariner` pod, and is responsible for performing a two-way synchronization between the datastore and local cluster of Submariner CRDs. The `datastoresyncer` will only push CRD data to the central broker for the local cluster (based on cluster ID), and will sync all data from the broker the local cluster when the data does not match the local cluster (to prevent circular loops)

#### submariner

submariner (compiled as the binary `submariner-engine`) has a few controllers built into it that establish state. It is responsible for running/interfacing with Charon to establish IPsec tunnels, as well as updating local cluster information into the central broker to share information between clusters. 

submariner-engine runs and utilizes leader election to establish an active gateway node, which is used to facilitate IPsec tunnel connections to remote clusters. It also manipulates the IPtables rules on each node to enable a. forwarding of traffic and b. SNAT for local node traffic.

#### submariner-route-agent

The submariner-route-agent runs as a DaemonSet on all Kubernetes nodes, and ensures route rules to allow all pods/nodes to communicate through the elected gateway node for remote cluster networks. It will ensure state and react on CRD changes, which means that it is able to remove/add routes as leader election occurs.

### Network Path

The network path of Submariner varies depending on the origin/destination of the IP traffic. In all cases, traffic between two clusters will transit between the leader elected (in each cluster) gateway nodes, through `ip xfrm` rules. Each gateway node has a running Charon daemon which will perform IPsec keying and policy management. 

When the source pod is on a worker node that is not the elected gateway node, the traffic destined for the remote cluster will transit through the kernel routing rules table to the cluster-local gateway node, which will perform source network address translation (SNAT) to the remote network. This allows for much more efficient traffic selectors to be configured, as well as more predictable routing paths. Once the traffic reaches the destination gateway node, it is routed one of two ways, depending on the destination CIDR. If the destination CIDR is a pod network, the traffic is routed however the CNI-compatible network routes traffic destined for pod IPs. If the destination CIDR is a service network, then traffic is routed through the facility configured via `kube-proxy` on the destination gateway node.

# Prerequisites

Submariner has a few requirements in order to get started:

- At least 3 Kubernetes clusters, one of which is designated to serve as the central broker that is accessible by all of your connected clusters; this can be one of your connected clusters, but comes with the limitation that the cluster is required to be up in order to facilitate interconnectivity/negotiation
- Different cluster/service CIDR's (as well as different kubernetes DNS suffixes) between clusters. This is to prevent traffic selector/policy/routing conflicts.
- Direct IP connectivity between instances through the internet (or on the same network if not running Submariner over the internet). Submariner supports 1:1 NAT setups, but has a few caveats/provider specific configuration instructions in this configuration.
- Knowledge of each cluster's network configuration
- Helm version that supports crd-install hook (v2.12.1+)

An example of three clusters configured to use with Submariner would look like the following:

| Cluster Name | Provider | Cluster CIDR | Service CIDR | DNS Suffix |
|:-------------|:---------|:-------------|:-------------|:-----------|
| broker       | AWS      | 10.42.0.0/16 | 10.43.0.0/16 | cluster.local |
| west         | vSphere  | 10.0.0.0/16  | 10.1.0.0/16  | west.local |
| east         | AWS      | 10.98.0.0/16 | 10.99.0.0/16 | east.local |

# Installation

## Setup

Submariner utilizes the following tools for installation:

- `kubectl`
- `helm`
- `base64`
- `cat`
- `tr`
- `fold`
- `head`

These instructions assume you have a combined kube config file with at least three contexts that correspond to the respective clusters. Thus, you should be able to perform commands like

```
kubectl config use-context broker
kubectl config use-context west
kubectl config use-context east
```

Submariner utilizes Helm as a package management tool. 

Before you start, you should add the `submariner-latest` chart repository to deploy the Submariner helm charts.

```
helm repo add submariner-latest https://releases.rancher.com/submariner-charts/latest
```

## Broker Installation/Setup

The broker is the component that Submariner utilizes to exchange metadata information between clusters for connection information. This should only be installed once on your central broker cluster. Currently, the broker is implemented by utilizing the Kubernetes API, but is modular and will be enhanced in the future to bring support for other interfaces. The broker can be installed by using a helm chart.

First, you should switch into the context for the broker cluster
```
kubectl config use-context <BROKER_CONTEXT>
```

If you have not yet initialized Tiller on the cluster, you can do so with the following commands:

```
kubectl -n kube-system create serviceaccount tiller

kubectl create clusterrolebinding tiller \
  --clusterrole=cluster-admin \
  --serviceaccount=kube-system:tiller

helm init --service-account tiller
```

Wait for Tiller to initialize

```
kubectl -n kube-system  rollout status deploy/tiller-deploy
```

Once tiller is initialized, you can install the Submariner K8s Broker

```
helm repo update

SUBMARINER_BROKER_NS=submariner-k8s-broker

helm install submariner-latest/submariner-k8s-broker \
--name ${SUBMARINER_BROKER_NS} \
--namespace ${SUBMARINER_BROKER_NS}
```

Once you install the broker, you can retrieve the Kubernetes API server information (if not known) and service account token for the client by utilizing the following commands:

```
SUBMARINER_BROKER_URL=$(kubectl -n default get endpoints kubernetes -o jsonpath="{.subsets[0].addresses[0].ip}:{.subsets[0].ports[?(@.name=='https')].port}")

SUBMARINER_BROKER_CA=$(kubectl -n ${SUBMARINER_BROKER_NS} get secrets -o jsonpath="{.items[?(@.metadata.annotations['kubernetes\.io/service-account\.name']=='${SUBMARINER_BROKER_NS}-client')].data['ca\.crt']}")

SUBMARINER_BROKER_TOKEN=$(kubectl -n ${SUBMARINER_BROKER_NS} get secrets -o jsonpath="{.items[?(@.metadata.annotations['kubernetes\.io/service-account\.name']=='${SUBMARINER_BROKER_NS}-client')].data.token}"|base64 --decode)
```

These environment variables will be utilized in later steps, so keep the values in a safe place.

## Submariner Installation/Setup

Submariner is installed by using a helm chart. Once you populate the environment variables for the token and broker URL, you should be able to install Submariner into your clusters.

1. Generate a Pre-Shared Key for Submariner. This key will be used for all of your clusters, so keep it somewhere safe.

   ```
   SUBMARINER_PSK=$(cat /dev/urandom | LC_CTYPE=C tr -dc 'a-zA-Z0-9' | fold -w 64 | head -n 1)
   echo $SUBMARINER_PSK
   ```

1. Update the helm repository to pull the latest version of the Submariner charts

   ```
   helm repo update
   ```
   
#### Installation of Submariner in each cluster

Each cluster that will be connected must have Submariner installed within it. You must repeat these steps for each cluster that you add.
   
1. Set your kubeconfig context to your desired installation cluster

   ```
   kubectl config use-context <CLUSTER_CONTEXT>
   ```

1. Label your gateway nodes with the annotation `submariner.io/gateway=true`

   ```
   kubectl label node <DESIRED_NODE> "submariner.io/gateway=true"
   ```

1. Initialize Helm (if not yet done)

   ```
   kubectl -n kube-system create serviceaccount tiller

   kubectl create clusterrolebinding tiller \
     --clusterrole=cluster-admin \
     --serviceaccount=kube-system:tiller

   helm init --service-account tiller
   ```

1. Wait for Tiller to initialize

   ```
   kubectl -n kube-system  rollout status deploy/tiller-deploy
   ```
   
1. Install submariner into this cluster. The values within the following command correspond to the table below.

   ```
   helm install submariner-latest/submariner \
   --name submariner \
   --namespace submariner \
   --set ipsec.psk="${SUBMARINER_PSK}" \
   --set broker.server="${SUBMARINER_BROKER_URL}" \
   --set broker.token="${SUBMARINER_BROKER_TOKEN}" \
   --set broker.namespace="${SUBMARINER_BROKER_NS}" \
   --set broker.ca="${SUBMARINER_BROKER_CA}" \
   \
   --set submariner.clusterId="<CLUSTER_ID>" \
   --set submariner.clusterCidr="<CLUSTER_CIDR>" \
   --set submariner.serviceCidr="<SERVICE_CIDR>" \
   --set submariner.natEnabled="<NAT_ENABLED>"
   ```
   
   |Placeholder|Description|Default|Example|
   |:----------|:----------|:------|:------|
   |\<CLUSTER_ID>|Cluster ID (Must be RFC 1123 compliant)|""|west-cluster|
   |\<CLUSTER_CIDR>|Cluster CIDR for Cluster|""|`10.42.0.0/16`|
   |\<SERVICE_CIDR>|Service CIDR for Cluster|""|`10.43.0.0/16`|
   |\<NAT_ENABLED>|If in a cloud provider that uses 1:1 NAT between instances (for example, AWS VPC), you should set this to `true` so that Submariner is aware of the 1:1 NAT condition.|"false"|`false`|

## Validate Submariner is Working

Switch to the context of one of your clusters, i.e. `kubectl config use-context west`

Run an nginx container in this cluster, i.e. `kubectl run -n default nginx --image=nginx`

Retrieve the pod IP of the nginx container, looking under the "Pod IP" column for `kubectl get pod -n default`

Change contexts to your other workload cluster, i.e. `kubectl config use-context east`

Run a busybox pod and ping/curl the nginx pod:

```
kubectl run -i -t busybox --image=busybox --restart=Never
If you don't see a command prompt, try pressing enter.
/ # ping <NGINX_POD_IP>
/ # wget -O - <NGINX_POD_IP>
```

# Testing

## E2E testing

E2E testing purpose is to validate submariner behaviour from an integration point of
view. It needs to be executed in connection to an existing set of clusters.

To execute the E2E tests:

  ```bash
  cd test/e2e
  go test -args -kubeconfig=/path/to/kubeconfig -ginkgo.randomizeAllSpecs
  ```

If you want to execute just a subset of the available E2E tests, you can use:

  ```bash
  cd test/e2e
  go test -args -kubeconfig=/path/to/kubeconfig --ginkgo.focus=dataplane \
     -ginkgo.randomizeAllSpecs
  ```

It's possible to generate jUnit XML report files
  ```bash
  cd test/e2e
  go test -args -ginkgo.v -report-dir ./junit -ginkgo.randomizeAllSpecs
  ```

Suggested arguments
  ```
  -test.v       : verbose output from go test
  -ginkgo.v     : verbose output from ginkgo
  -ginkgo.trace : output stack track on failure
  -ginkgo.randomizeAllSpecs  : prevent test-ordering dependencies from creeping in
  ```

This is an example of how a E2E test run looks like
```
$ export KUBECONFIG=/Users/ajo/Documents/work/redhat/skynet-tools/creds/kubeconfig-c0_master
$ go test -args -ginkgo.v -report-dir junit
Running Suite: Submariner E2E suite
===================================
Random Seed: 1558715716
Will run 2 of 2 specs

[example] Basic example to demonstrate how to write tests using the framework
  Should be able to list existing nodes on the cluster
  /Users/ajo/go/src/github.com/rancher/submariner/test/e2e/example/example.go:35
STEP: Creating a kubernetes client
May 24 18:35:16.996: INFO: >>> kubeConfig: /Users/ajo/Documents/work/redhat/skynet-tools/creds/kubeconfig-c0_master
STEP: Building a namespace api object, basename basic-example
STEP: Creating a namespace to execute the test in
STEP: Created test namespace e2e-tests-basic-example-j447f
STEP: Requesting node list from API
May 24 18:35:17.930: INFO: Detected node with IP: 10.0.0.14
May 24 18:35:17.930: INFO: Detected node with IP: 10.0.0.7
May 24 18:35:17.930: INFO: Detected node with IP: 10.0.0.9
STEP: Destroying namespace "e2e-tests-basic-example-j447f" for this suite.
•
------------------------------
[example] Basic example to demonstrate how to write tests using the framework
  Should be able to create a pod using the provided client
  /Users/ajo/go/src/github.com/rancher/submariner/test/e2e/example/example.go:39
STEP: Creating a kubernetes client
May 24 18:35:18.086: INFO: >>> kubeConfig: /Users/ajo/Documents/work/redhat/skynet-tools/creds/kubeconfig-c0_master
STEP: Building a namespace api object, basename basic-example
STEP: Creating a namespace to execute the test in
STEP: Created test namespace e2e-tests-basic-example-pcxxv
STEP: Creating a bunch of pods
STEP: Waiting for the example-pod(s) to be scheduled and running
STEP: Collecting pod ClusterIPs just for fun
May 24 18:35:29.241: INFO: Detected pod with IP: 10.129.0.15
May 24 18:35:29.241: INFO: Detected pod with IP: 10.130.0.16
May 24 18:35:29.241: INFO: Detected pod with IP: 10.128.0.16
STEP: Destroying namespace "e2e-tests-basic-example-pcxxv" for this suite.

• [SLOW TEST:11.312 seconds]
[example] Basic example to demonstrate how to write tests using the framework
/Users/ajo/go/src/github.com/rancher/submariner/test/e2e/example/example.go:33
  Should be able to create a pod using the provided client
  /Users/ajo/go/src/github.com/rancher/submariner/test/e2e/example/example.go:39
------------------------------

Ran 2 of 2 Specs in 12.404 seconds
SUCCESS! -- 2 Passed | 0 Failed | 0 Pending | 0 Skipped
PASS
ok  	github.com/rancher/submariner/test/e2e	12.453s
```

It may be helpful to use the [delve debugger](https://github.com/derekparker/delve)
to gain insight into the test:

  ```bash
  cd test/e2e
  dlv test
  ```

  When using delve please note, the equivalent of `go test -args` is `dlv test --`,
  dlv test treats both single and double quotes literally.
  Neither `-ginkgo.focus="mytest"` nor `-ginkgo.focus='mytest'` will match `mytest`
  `-ginkgo.focus=mytest` is required, for example:

  ```bash
  cd test/e2e
  dlv test -- -ginkgo.v -ginkgo.focus=mytest
  ```

# Known Issues/Notes

### AWS Notes

When running in AWS, it is necessary to disable source/dest checking of the instances that are gateway hosts to allow the instances to pass traffic for remote clusters. 

# Building/Contributing

To build `submariner-engine` and `submariner-route-agent` you can trigger `make`, which will perform a Dapperized build of the components.

We welcome issues/PR's to Submariner, if you encounter issues that you'd like to fix while working on it or find new features that you'd like.

# TODO

- Potentially spin out Charon into it's own pod to help decrease downtime
- Better clean up of IPtables rules when node loses leader election
- Central API server that is hosted by Rancher
