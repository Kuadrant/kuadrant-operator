# Multi cluster configuration for DNS

## Overview
This guide includes the manual steps required to configure the *dns-operator*, a sub component of kuadrant to run in a multi cluster setup. 
The delegation of dns polices to **primary cluster** will be possible in this mode.

## Prerequisites

- Kaudrant installed in multiple clusters
- kubeconfig with access to all the clusters

## Terminology

- **primary cluster**: A primary cluster can reconcile dns records for itself, other primaries and **secondary clusters**.
- **secondary cluster**: A cluster that will delegate the reconciliation of dns records to a **primary cluster**.
- **cluster secret**: A secret that contains the kubeconfig to access a cluster

## Configuration

### Secondary cluster

For the **secondary cluster** the *dns-operator* needs to be in `delegation-role = secondary`.
This can be achieved by setting the `DELEGATION_ROLE: secondary` in the `dns-operator-controller-env` configmap.

```sh
export NAME dns-operator-controller-env
export NAMESPACE $(kubectl get configmap --all-namespaces --no-headers | grep -w $NAME | head -1 | awk '{print $1}')
kubectl patch configmap $NAME --namespace $NAMESPACE --type merge -p '{"data":{"DELEGATION_ROLE":"secondary"}}'
```

Once the configmap has being updated the *dns-operator* requires a restart.

```sh
export NAME=dns-operator-controller-manager
export NAMESPACE=$(kubectl get deployment --all-namespaces --no-headers | grep -w $NAME | head -1 | awk '{print $1}')
kubectl scale deployment/$NAME --namespace $NAMESPACE --replicas=0
kubectl scale deployment/$NAME --namespace $NAMESPACE --replicas=1
kubectl rollout status deployment/$NAME --namespace $NAMESPACE --timeout=300s
```

The **secondary cluster** will now delegate the reconciliation of dns records to the **primary clusters**.
A **secondary cluster** can still reconcile dns policies that are not delegated.

### Primary Clusters

The **primary clusters** need more configuration than the **secondary clusters**, as they require **cluster secrets**.
The default mode for the *dns-operator* is `primary`, but for completeness this is how to configure the deployment.
```sh
export NAME=dns-operator-controller-env
export NAMESPACE=$(kubectl get configmap --all-namespaces --no-headers | grep -w $NAME | head -1 | awk '{print $1}')
kubectl patch configmap $NAME --namespace $NAMESPACE --type merge -p '{"data":{"DELEGATION_ROLE":"primary"}}'
```

Once the configmap has being updated the *dns-operator* requires a restart.

```sh
export NAME=dns-operator-controller-manager
export NAMESPACE=$(kubectl get deployment --all-namespaces --no-headers | grep -w $NAME | head -1 | awk '{print $1}')
kubectl scale deployment/$NAME --namespace $NAMESPACE --replicas=0
kubectl scale deployment/$NAME --namespace $NAMESPACE --replicas=1
kubectl rollout status deployment/$NAME --namespace $NAMESPACE --timeout=300s
```

#### Cluster secrets

**Cluster secrets** are required on the **primary clusters** to connect with the **secondary clusters**.
A **cluster secret** is a secret in the *dns-operator* namespace that has connection details to a service account on the **secondary cluster**.
These connection details are in the form of a kubeconfig.

The `kubectl-dns` plugin by the *dns-operator* provides helpful commands to create the service accounts on the **secondary cluster**, and add the **cluster secret** to the primary.
Refer to the [CLI documentation](https://github.com/Kuadrant/dns-operator/blob/main/docs/cli.md) for more information on the `kubectl-dns`.

Assuming the `kubectl-dns` plugin is in the system path.
Ensure the current context is set to the **primary cluster**.
```sh
kubectl config use-context <primary cluster>
```
The **cluster secret** must be created in the same namespace as the *dns-operator*.
```sh
export NAME=dns-operator-controller-manager
export NAMESPACE=$(kubectl get deployment --all-namespaces --no-headers | grep -w $NAME | head -1 | awk '{print $1}')
kubectl-dns secret-generation --context <secondary cluster> --namespace $NAMESPACE
```

The creation of **cluster secrets** is repeated for all **secondary clusters** that are in the multi cluster setup.

If there are more than one **primary cluster** in the multi cluster setup.
Not only does each **primary cluster** require a **cluster secret** for each **secondary cluster**, the **primary clusters** also require a **cluster secret** to the other **primary clusters**.
