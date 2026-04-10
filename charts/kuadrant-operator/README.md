# Kuadrant Operator

This is the Helm Chart to install the official Kuadrant Kubernetes Operator

## Installation

### Install Dependencies

Follow the steps below to install the following dependencies for Kuadrant Operator:
- [cert-manager](https://cert-manager.io/)
- [Gateway API](https://gateway-api.sigs.k8s.io/)
- [Istio](https://istio.io/latest/docs/ambient/install/helm/)

[Envoy Gateway](https://gateway.envoyproxy.io/) is also an option as a gateway provider, **Steps Coming soon!**

#### Install cert-manager

 ```sh
 helm repo add jetstack https://charts.jetstack.io --force-update
 helm install cert-manager jetstack/cert-manager \
   --namespace cert-manager \
   --create-namespace \
   --version v1.16.1 \
   --set crds.enabled=true
 ```

#### Install Kubernetes Gateway API:

 ```sh
 kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.1/standard-install.yaml
 ```

#### Install Istio:

 ```sh
 helm repo add istio https://istio-release.storage.googleapis.com/charts --force-update
 helm install istio-base istio/base \
   --set defaultRevision=default \
   --namespace=istio-system \
   --create-namespace \
   --version 1.29.1
 helm install istiod istio/istiod \
   --namespace=istio-system \
   --version 1.29.1 \
   --wait
 ```

### Install Kuadrant

```sh
helm repo add kuadrant https://kuadrant.io/helm-charts/ --force-update
helm install kuadrant-operator kuadrant/kuadrant-operator \
 --create-namespace \
 --namespace kuadrant-system
```

### Parameters

**Coming soon!** At the moment, there's no configuration parameters exposed.

## Usage

Read the documentation and user guides in [Kuadrant.io](https://docs.kuadrant.io).
