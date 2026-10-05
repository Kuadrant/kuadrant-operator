# Authorino Operator

This is the Helm Chart to install the official Authorino Kubernetes Operator

## Installation

```sh
helm repo add kuadrant https://kuadrant.io/helm-charts/ --force-update
helm install \
 authorino-operator kuadrant/authorino-operator \
 --create-namespace \
 --namespace authorino-system
```

### Parameters

**Coming soon!** At the moment, there's no configuration parameters exposed.

## Usage

Read the documentation and user guides in the [Getting Started guide](https://github.com/Kuadrant/authorino/blob/main/docs/getting-started.md).
