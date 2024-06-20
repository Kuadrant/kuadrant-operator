# Install Kuadrant on a Kubernetes cluster

> [!NOTE]  
> You must perform these steps on each Kubernetes cluster where you want to use Kuadrant.


## Prerequisites

- Access to a Kubernetes cluster, with `kubeadmin` or an account with similar permissions

## Procedure

This guide will show you how to install Kuadrant onto a bare Kubernetes cluster.

Alternatively, if you are looking instead for a way to set up Kuadrant locally to evaluate or develop, consider running the kind & Kubernetes [quickstart script](https://docs.kuadrant.io/latest/getting-started-single-cluster/).

### Install Gateway API

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.1.0/standard-install.yaml
```

### Install [OLM](https://olm.operatorframework.io/)

```bash
curl -sL https://github.com/operator-framework/operator-lifecycle-manager/releases/download/v0.23.1/install.sh | bash -s v0.23.1
```

### Install Istio as a Gateway API provider

> [!NOTE]  
> There are several ways to install Istio (via `istioctl`, Helm chart or Operator) - this is just an example for starting from a bare Kubernetes cluster.


```bash
curl -sSL https://istio.io/downloadIstio | ISTIO_VERSION=1.20.7 sh -
./istio-1.20.7/bin/istioctl install --set profile=minimal -y
./istio-1.20.7/bin/istioctl operator init
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/main/config/dependencies/istio/istio-operator.yaml
```

### Install Kuadrant

```bash
kubectl create -f https://operatorhub.io/install/kuadrant-operator.yaml
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
If you want `RateLimitPolicy` to use shared, multicluster counters for Kuadrant's Limitador component, you'll need to configure Kuadrant with a Redis cluster URL. Create this secret:

```bash
# Replace this with an accessible Redis cluster URL
export REDIS_URL=redis://user:xxxxxx@some-redis.com:6379
kubectl -n kuadrant-system create secret generic redis-config \
  --from-literal=URL=$REDIS_URL
```

You'll also need to update your earlier created `Kuadrant` instance:

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

- [Secure, protect, and connect APIs with Kuadrant on OpenShift](../user-guides/secure-protect-connect-single-multi-cluster.md)
