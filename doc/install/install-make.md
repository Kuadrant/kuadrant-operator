# Installing Kuadrant via make targets

## Overview
The following doc will show you how to install the Kuadrant Operator using make targets in the Kuadrant operator repo. What will be installed is Istio, Kubernetes Gateway API and Kuadrant itself.

For other methods of installation see
- [k8s](https://github.com/Kuadrant/kuadrant-operator/blob/main/doc/install/install-kubernetes.md)
- [Openshift](https://github.com/Kuadrant/kuadrant-operator/blob/main/doc/install/install-openshift.md)

> **Note:** In production environment, these steps are usually performed by a cluster operator with administrator privileges over the Kubernetes cluster.

### Pre-requisites
- [Kind](https://kind.sigs.k8s.io)
- [Docker](https://docker.io) or [Podman](https://podman.io/)

### Setup

Clone the project:
```sh
git clone https://github.com/Kuadrant/kuadrant-operator && cd kuadrant-operator
```

Setup the environment (This will also create a kind cluster. If your using Pod man use the env var CONTAINER_ENGINE=podman with the make target below.):
```sh
make local-setup
```

Request an instance of Kuadrant:
```sh
kubectl -n kuadrant-system apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
spec: {}
EOF
```
