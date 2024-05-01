# Install Kuadrant on an OpenShift Cluster

Note these steps need to be taken on each cluster you want to use Kuadrant on.

## Prerequisites

- OpenShift Container Platform 4.14.x or later with community Operator catalog available
- AWS account with Route 53 and zone 
- Accessible Redis Instance

## Setup Environment

```bash
export AWS_ACCESS_KEY_ID=xxxxxxx # Key ID from AWS with Route 53 access
export AWS_SECRET_ACCESS_KEY=xxxxxxx # Access Key from AWS with Route 53 access
export REDIS_URL=redis://user:xxxxxx@some-redis.com:10340 # A Redis cluster URL
```

## Installing the dependencies

Kuadrant integrates with Istio as a Gateway API provider. Before you can try Kuadrant, we need to setup an Istio based Gateway API provider. For this we will use the `Sail` operator.

### Install v1 of Gateway API:

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.0.0/standard-install.yaml
```

### Install and Configure Istio via the Sail Operator

To install, run the following:

```bash
kubectl create ns istio-system
```

```bash
kubectl  apply -f - <<EOF
kind: OperatorGroup
apiVersion: operators.coreos.com/v1
metadata:
  name: sail
  namespace: istio-system
spec: 
  upgradeStrategy: Default  
---  
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: sailoperator
  namespace: istio-system
spec:
  channel: 3.0-dp1
  installPlanApproval: Automatic
  name: sailoperator
  source: community-operators
  sourceNamespace: openshift-marketplace
EOF
```

To check the status of the install, you can run:

```bash
kubectl get installplan -n istio-system -o=jsonpath='{.items[0].status.phase}'
```

Once ready it will be marked `complete` while installing it will be marked `installing`.

#### Configure Istio

```bash
kubectl apply -f - <<EOF
apiVersion: operator.istio.io/v1alpha1
kind: Istio
metadata:
  name: default
spec:
  version: v1.21.0
  namespace: istio-system
  # Disable autoscaling to reduce dev resources
  values:
    pilot:
      autoscaleEnabled: false
EOF
```

Wait for Istio to be ready:

```bash
kubectl wait istio/default -n istio-system --for="condition=Ready=true"
```


### (Recommended) Thanos and Observability Stack

Kuadrant provides a set of sample dashboards that use the known metrics exported by Kuadrant and Gateway components to provide insight into the different areas of your APIs and Gateways. While this is not essential, it is recommended that you set up an observability stack. Below are links to the OpenShift docs on this and also a link to help with the setup of Thanos for metrics storage.

OpenShift supports a user facing monitoring stack. This can be cofigured and setup this documentation:

https://docs.openshift.com/container-platform/4.14/observability/monitoring/configuring-the-monitoring-stack.html

If you have user workload monitoring enabled. We Recommend configuring  remote write to a central storage system such as Thanos: 
- [Remote Write Config](https://docs.openshift.com/container-platform/4.14/observability/monitoring/configuring-the-monitoring-stack.html#configuring_remote_write_storage_configuring-the-monitoring-stack)

- [Kube Thanos](https://github.com/thanos-io/kube-thanos)

- [Kuadrant Examples](https://docs.kuadrant.io/kuadrant-operator/doc/observability/examples/)


### Install Kuadrant

To install Kuadrant, we will use the Kuadrant Operator. Prior to installing, we will set up some secrets that we will use later:

```bash
kubectl create ns kuadrant-system
```

AWS Route 53 credentials for TLS verification:

```bash
kubectl -n kuadrant-system create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
```

Redis credentials for shared (multi-cluster) counters support for Kuadrant's Limitador component:

```bash
kubectl -n kuadrant-system create secret generic redis-config \
  --from-literal=URL=$REDIS_URL  
```  

```bash
kubectl create ns ingress-gateway
```

AWS Route 53 credentials for managing DNS records:

```bash
kubectl -n ingress-gateway create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
```  

Finally, to install the Kuadrant Operator:

```bash
kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: kuadrant-operator
  namespace: kuadrant-system
spec:
  channel: preview
  installPlanApproval: Automatic
  name: kuadrant-operator
  source: community-operators
  sourceNamespace: openshift-operators
---
kind: OperatorGroup
apiVersion: operators.coreos.com/v1
metadata:
  name: kuadrant
  namespace: kuadrant-system
spec: 
  upgradeStrategy: Default 
EOF
```  

Wait for Kuadrant operators to be installed:

```bash
kubectl get installplan -n kuadrant-system -o=jsonpath='{.items[0].status.phase}'
```

After some time, this should return `complete`.

#### Configure Kuadrant

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: kuadrant-system
spec:
  limitador:
    storage:
      redis-cached:
        configSecretRef:
          name: redis-config 
EOF          
```      

Wait for Kuadrant to be ready:

```bash
kubectl wait kuadrant/kuadrant --for="condition=Ready=true" -n kuadrant-system --timeout=300s
```

Kuadrant is now ready to use.

Next up: [Secure, Protect and Connect on single or multiple clusters](../user-guides/secure-protect-connect-single-multi-cluster.md)
