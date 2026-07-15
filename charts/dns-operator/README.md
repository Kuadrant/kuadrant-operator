# DNS Operator

This is the Helm Chart to install the official Kuadrant DNS Kubernetes Operator

## Installation

```sh
helm repo add kuadrant https://kuadrant.io/helm-charts/ --force-update
helm install \
 dns-operator kuadrant/dns-operator \
 --create-namespace \
 --namespace kuadrant-system
```

### Parameters

**Coming soon!** At the moment, there's no configuration parameters exposed.

## Usage

Read the documentation and user guides in the [Getting Started guide](https://github.com/Kuadrant/dns-operator/?tab=readme-ov-file#getting-started).
