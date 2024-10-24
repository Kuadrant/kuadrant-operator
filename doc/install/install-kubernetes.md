# Install Kuadrant on a Kubernetes cluster

!!! note

    You must perform these steps on each Kubernetes cluster where you want to use Kuadrant.

!!! warning

    Kuadrant uses a number of labels to search and filter resources on the cluster.
    All required labels are formatted as `kuadrant.io/*`.
    Removal of any labels with the prefix may cause unexpected behaviour and degradation of the product.


## Prerequisites

- Access to a Kubernetes cluster, with `kubeadmin` or an account with similar permissions
- `cert-manager` [installed](https://cert-manager.io/docs/installation/)

## Procedure

This guide will show you how to install Kuadrant onto a bare Kubernetes cluster.

Alternatively, if you are looking instead for a way to set up Kuadrant locally to evaluate or develop, consider running the kind & Kubernetes [quickstart script](https://docs.kuadrant.io/latest/getting-started-single-cluster/).

### Install Gateway API

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.1.0/standard-install.yaml
```

### Install [OLM](https://olm.operatorframework.io/)

!!! note

    Currently, we recommend installing our operator via OLM. We plan to support Helm soon.

```bash
curl -sL https://github.com/operator-framework/operator-lifecycle-manager/releases/download/v0.28.0/install.sh | bash -s v0.28.0
```

### (Optional) Install Istio as a Gateway API provider

!!! note

    Skip this step if planing to use [Envoy Gateway](https://gateway.envoyproxy.io/) as Gateway API provider

!!! note

    There are several ways to install Istio (via `istioctl`, Helm chart or Operator) - this is just an example for starting from a bare Kubernetes cluster.

```bash
curl -L https://istio.io/downloadIstio | ISTIO_VERSION=1.22.5 sh -
./istio-1.22.5/bin/istioctl install --set profile=minimal
./istio-1.22.5/bin/istioctl operator init
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/main/config/dependencies/istio/istio-operator.yaml
```

### (Optional) Install Envoy Gateway as a Gateway API provider

!!! note

    Skip this step if planing to use [Istio](https://istio.io/) as Gateway API provider

!!! note

    There are several ways to install Envoy Gateway (via `egctl`, Helm chart or Kubernetes yaml) - this is just an example for starting from a bare Kubernetes cluster.

```bash
helm install eg oci://docker.io/envoyproxy/gateway-helm --version v1.1.0 -n envoy-gateway-system --create-namespace
```

Kuadrant relies on the Envoy Gateway patch policy feature to function correctly - enable the *EnvoyPatchPolicy* feature like so:

```bash
TMP=$(mktemp -d)
kubectl get configmap -n envoy-gateway-system envoy-gateway-config -o jsonpath='{.data.envoy-gateway\.yaml}' > ${TMP}/envoy-gateway.yaml
yq e '.extensionApis.enableEnvoyPatchPolicy = true' -i ${TMP}/envoy-gateway.yaml
kubectl create configmap -n envoy-gateway-system envoy-gateway-config --from-file=envoy-gateway.yaml=${TMP}/envoy-gateway.yaml -o yaml --dry-run=client | kubectl replace -f -
kubectl rollout restart deployment envoy-gateway -n envoy-gateway-system
```

Wait for Envoy Gateway to become available:

```bash
kubectl wait --timeout=5m -n envoy-gateway-system deployment/envoy-gateway --for=condition=Available
```

### Install Kuadrant

```bash
kubectl create -f https://operatorhub.io/install/kuadrant-operator.yaml
kubectl get crd --watch | grep -m 1 "kuadrants.kuadrant.io"
```

### Request a Kuadrant instance

```bash
kubectl create namespace kuadrant-system
kubectl -n kuadrant-system apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
spec: {}
EOF
```

Kuadrant should now install. You can check the operator's install status with:

```bash
kubectl wait --for=jsonpath='{.status.state}'=AtLatestKnown subscription/my-kuadrant-operator -n operators --timeout=600s
```

Kuadrant is now ready to use.

### (Optional) Observability setup (Istio only)

There is a set of example dashboards and alerts in the kuadrant-operator repo that can be used for obserability of the Kuadrant components and gateway.
To make use of these, first install the example monitoring stack and configuration:

```bash
kubectl apply -k github.com/Kuadrant/kuadrant-operator/config/observability?ref=main --dry-run=client -o yaml | docker run --rm -i docker.io/ryane/kfilt -i kind=CustomResourceDefinition | kubectl apply --server-side -f -
kubectl apply -k github.com/Kuadrant/kuadrant-operator/config/observability?ref=main --dry-run=client -o yaml | docker run --rm -i docker.io/ryane/kfilt -x kind=CustomResourceDefinition | kubectl apply -f -
kubectl apply -k github.com/Kuadrant/kuadrant-operator/config/thanos?ref=main
kubectl apply -k github.com/Kuadrant/kuadrant-operator/examples/dashboards?ref=main
kubectl apply -k github.com/Kuadrant/kuadrant-operator/examples/alerts?ref=main
THANOS_RECEIVE_ROUTER_IP=$(kubectl -n monitoring get svc thanos-receive-router-lb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
kubectl -n monitoring patch prometheus k8s --type='merge' -p '{"spec":{"remoteWrite":[{"url":"http://'"$THANOS_RECEIVE_ROUTER_IP"':19291/api/v1/receive", "writeRelabelConfigs":[{"action":"replace", "replacement":"'"$KUADRANT_CLUSTER_NAME"'", "targetLabel":"cluster_id"}]}]}}'
kubectl apply -k github.com/Kuadrant/kuadrant-operator/config/observability/prometheus/monitors/istio?ref=main
```

This will deploy prometheus, alertmanager and Grafana into the monitoring namespace, along with metrics scrape configuration for Istio and Envoy Proxy. Thanos will also be deployed with prometheus configured to remote write to it.

To access Grafana and Prometheus, you can port forward to the services:

```bash
kubectl -n monitoring port-forward service/grafana 3000:3000
```

The Grafana UI can then be found at http://127.0.0.1:3000/ (default user/pass of admin & admin).

```bash
kubectl -n monitoring port-forward service/prometheus-k8s 9090:9090
```

The Prometheus UI can then be found at http://127.0.0.1:9090.

### (Optional) `DNSPolicy` setup

If you plan to use `DNSPolicy`, this doc uses an AWS Account with access to Route 53. There are other providers that you can also use for DNS integration: 

[DNS Providers](https://docs.kuadrant.io/latest/dns-operator/docs/provider/)

Export the following environment variables for setup:

```bash
export AWS_ACCESS_KEY_ID=xxxxxxx # Key ID from AWS with Route 53 access
export AWS_SECRET_ACCESS_KEY=xxxxxxx # Access key from AWS with Route 53 access
```

Create an AWS credentials secret:

```bash
kubectl -n kuadrant-system create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
```

### (Optional) Multi-cluster `RateLimitPolicy`

To enable `RateLimitPolicy` to use shared, multi-cluster counters for Kuadrant's Limitador component, you need to configure Kuadrant with a Redis cluster URL. Redis URIs can be either `redis://` for standard connections or `rediss://` for secure connections.

Follow these steps to create the necessary secret:

1. Replace `some-redis.com:6379` with the URL of your accessible Redis cluster. Ensure you include the appropriate URI scheme (`redis://` or `rediss://`).

2. Execute the following commands:

    ```bash
    # Replace this with an accessible Redis cluster URL
    export REDIS_URL=redis://user:xxxxxx@some-redis.com:6379
    
    ```    
3. Create the secret:
    
    ```bash
    kubectl -n kuadrant-system create secret generic redis-config \
      --from-literal=URL=$REDIS_URL
    ```

This will create a secret named `redis-config` in the `kuadrant-system` namespace containing your Redis cluster URL, which Kuadrant will use for multi-cluster rate limiting.


You'll also need to update the `Limitador` instance (the component that handles rate limiting) to reconfigure Kuadrant to use Redis:

```bash
kubectl patch limitador limitador --type=merge -n kuadrant-system -p '
spec:
  storage:
    redis:
      configSecretRef:
        name: redis-config
'

kubectl wait limitador/limitador -n kuadrant-system --for="condition=Ready=true"

```

### Metal LB (local setup)

If you are using a local kind cluster, we recommend using [metallb](https://metallb.universe.tf/) to allow the service type loadbalancer to be used with your gateways and an IP to be assigned to your gateway address rather than an internal service name.

## Next Steps

- [Secure, protect, and connect APIs with Kuadrant on Kubernetes](../user-guides/secure-protect-connect.md)
