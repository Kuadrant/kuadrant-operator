# Install Kuadrant on an OpenShift cluster

NOTE: You must perform these steps on each cluster that you want to use Kuadrant on.

## Prerequisites

- OpenShift Container Platform 4.14.x or later with community Operator catalog available
- AWS account with Route 53 and zone 
- Accessible Redis Instance

## Set up your environment

```bash
export AWS_ACCESS_KEY_ID=xxxxxxx # Key ID from AWS with Route 53 access
export AWS_SECRET_ACCESS_KEY=xxxxxxx # Access Key from AWS with Route 53 access
export REDIS_URL=redis://user:xxxxxx@some-redis.com:10340 # A Redis cluster URL
```

## Install the dependencies

Kuadrant integrates with Istio as a Gateway API provider. Before you can use Kuadrant, you must set up an Istio-based Gateway API provider. For this step, you will use the Sail Operator.

### Install v1 of Gateway API:

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.0.0/standard-install.yaml
```

### Install and configure Istio with the Sail Operator

To install Istio, run the following command:

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

When ready, the status will change from `installing` to `complete`.

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


### Best practices for metrics and observability

Kuadrant provides a set of sample dashboards that use known metrics exported by Kuadrant and Gateway components to provide insight into different areas of your APIs and Gateways. While not essential, it is best to set up an observability stack. This section provides links to OpenShift and Thanos documentation on configuring monitoring and metrics storage.

OpenShift supports a user facing monitoring stack. This can be cofigured and setup this documentation:

https://docs.openshift.com/container-platform/latest/observability/monitoring/configuring-the-monitoring-stack.html

If you have user workload monitoring enabled. We Recommend configuring remote write to a central storage system such as Thanos:

- [Remote Write Config](https://docs.openshift.com/container-platform/latest/observability/monitoring/configuring-the-monitoring-stack.html#configuring_remote_write_storage_configuring-the-monitoring-stack)
- [Kube Thanos](https://github.com/thanos-io/kube-thanos)

There are a set of [example dashboards and alerts](https://docs.kuadrant.io/kuadrant-operator/doc/observability/examples/) for observing Kuadrant functionality.
These dashboards and alerts make use of low level cpu, metrics and network metrics available from the user monitoring stack in Openshift. They also make use of resource state metrics from Gateway API and Kuadrant resources.
To scrape these additional metrics, you can install a kube-state-metrics instance, with a custom resource config:

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/main/config/observability/openshift/kube-state-metrics.yaml
kubectl apply -k https://github.com/Kuadrant/gateway-api-state-metrics?ref=main
```

To enable request metrics in Istio, you will need to create a Telemetry resource:

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/main/config/observability/openshift/telemetry.yaml
```

The [dashboards](https://docs.kuadrant.io/kuadrant-operator/doc/observability/examples) can be imported into Grafana, if you have it installed in your cluster.
You'll find an example of how to install Grafana on Openshift [here](https://cloud.redhat.com/experts/o11y/ocp-grafana/). Once installed, you will need to add your Thanos instance as a data source to Grafana. Alternatively, if you are just using the user workload monitoring stack in your Openshift cluster (and not writing metrics to an external thanos instance), you can set up a data source to the [thanos-querier route in the Openshift cluster](https://docs.openshift.com/container-platform/4.15/observability/monitoring/accessing-third-party-monitoring-apis.html#accessing-metrics-from-outside-cluster_accessing-monitoring-apis-by-using-the-cli).

### Install Kuadrant

To install Kuadrant, use the Kuadrant Operator. Before installing, you will set up some secrets that you will use later:

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

Redis credentials for shared multicluster counters for Kuadrant's Limitador component:

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

Wait for Kuadrant Operators to be installed:

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

## Next steps 
- [Secure, protect, and connect APIs on single or multiple clusters](../user-guides/secure-protect-connect-single-multi-cluster.md)
