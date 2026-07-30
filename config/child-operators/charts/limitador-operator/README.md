# Limitador Operator

This is the Helm Chart to install the official Limitador Kubernetes Operator

## Installation

```sh
helm repo add kuadrant https://kuadrant.io/helm-charts/ --force-update
helm install \
 limitador-operator kuadrant/limitador-operator \
 --create-namespace \
 --namespace limitador-system
```

### Parameters

**Coming soon!** At the moment, there's no configuration parameters exposed.

## Usage

Read the documentation and user guides in the [Getting Started guide](https://github.com/Kuadrant/limitador/?tab=readme-ov-file#getting-started).
