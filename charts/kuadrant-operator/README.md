# Kuadrant Operator

This is the Helm Chart to install the official Kuadrant Kubernetes Operator

## Installation

### Prerequisites
- [Gateway API](https://gateway-api.sigs.k8s.io/)
- A Gateway Provider (I.E: [Istio](https://istio.io/latest/docs/ambient/install/helm/),
[Envoy](https://www.envoyproxy.io/docs/envoy/latest/start/start))

### Install Gateway API and Gateway Provider

1. Install Kubernetes Gateway API:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.1.0/standard-install.yaml
```

2. Install a Gateway Controller, i.e: Istio:

```sh
helm install sail-operator \
		--create-namespace \
		--namespace istio-system \
		--wait \
		--timeout=300s \
		https://github.com/istio-ecosystem/sail-operator/releases/download/0.1.0/sail-operator-0.1.0.tgz

kubectl apply -f -<<EOF
apiVersion: sailoperator.io/v1alpha1
kind: Istio
metadata:
  name: default
spec:
  # Supported values for sail-operator v0.1.0 are [v1.22.4,v1.23.0]
  version: v1.23.0
  namespace: istio-system
  # Disable autoscaling to reduce dev resources
  values:
    pilot:
      autoscaleEnabled: false
EOF
```

### Install Kuadrant

```sh
helm repo add kuadrant https://kuadrant.io/helm-charts/ --force-update
helm install \
 kuadrant-operator kuadrant/kuadrant-operator \
 --create-namespace \
 --namespace kuadrant-system
```

### Parameters

At the moment, there's no configuration parameters exposed.

## Usage

Read the documentation and user guides in [Kuadrant.io](https://docs.kuadrant.io).
