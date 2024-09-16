# Install Kuadrant on a Kubernetes cluster

!!! note

    You must perform these steps on each Kubernetes cluster where you want to use Kuadrant.

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
curl -L https://istio.io/downloadIstio | ISTIO_VERSION=1.21.4 sh -
./istio-1.21.4/bin/istioctl install --set profile=minimal
./istio-1.21.4/bin/istioctl operator init
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

Enable *EnvoyPatchPolicy* feature:

```bash
TMP=$(mktemp -d)
kubectl get configmap -n envoy-gateway-system envoy-gateway-config -o jsonpath='{.data.envoy-gateway\.yaml}' > ${TMP}/envoy-gateway.yaml
yq e '.extensionApis.enableEnvoyPatchPolicy = true' -i ${TMP}/envoy-gateway.yaml
kubectl create configmap -n envoy-gateway-system envoy-gateway-config --from-file=envoy-gateway.yaml=${TMP}/envoy-gateway.yaml -o yaml --dry-run=client | kubectl replace -f -
kubectl rollout restart deployment envoy-gateway -n envoy-gateway-system
```

Wait for Envoy Gateway to become available::

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


### (Optional) `DNSPolicy` setup

If you plan to use `DNSPolicy`, you will need an AWS Account with access to Route 53 (more providers coming soon), and a hosted zone.

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

    kubectl -n kuadrant-system create secret generic redis-config \
      --from-literal=URL=$REDIS_URL
    ```

This will create a secret named `redis-config` in the `kuadrant-system` namespace containing your Redis cluster URL, which Kuadrant will use for multi-cluster rate limiting.


You'll also need to update your earlier created `Kuadrant` instance to reconfigure Kuadrant to use Redis:

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

## Next Steps

- [Secure, protect, and connect APIs with Kuadrant on Kubernetes](../user-guides/secure-protect-connect.md)
