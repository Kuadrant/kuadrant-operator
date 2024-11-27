
## Prerequisites
- Installation of kuadrant on a cluster e.g. [standard installation](../standard)
- AWS, Azure or GCP with DNS capabilities.

## Setup the environment

Verify kuadrant is installed and components are running:

```shell
kubectl get deployment -n kuadrant-system
NAME                                    READY   UP-TO-DATE   AVAILABLE   AGE
authorino-operator                      1/1     1            1           52m
dns-operator-controller-manager         1/1     1            1           52m
kuadrant-operator-controller-manager    1/1     1            1           50m
limitador-operator-controller-manager   1/1     1            1           52m
```

Scale down the cluster scoped dns operator installed as part of the kuadrant installation:
```shell
kubectl scale deployment --replicas=0 -n kuadrant-system -l app.kubernetes.io/created-by=dns-operator
```

Create credentials files for AWS, Azure and GCP in the installation overlay directory:

```shell
touch config/install/namespaced-dns-operator/dns-providers/aws-credentials.env 
touch config/install/namespaced-dns-operator/dns-providers/azure-credentials.env 
touch config/install/namespaced-dns-operator/dns-providers/gcp-credentials.env 
```

Refer to the dns [operator provider guide]((https://github.com/Kuadrant/dns-operator/blob/main/docs/provider.md)) for the appropriate contents of these files.

Apply namespaced dns operator installation overlay:
```shell
kubectl apply -k config/install/namespaced-dns-operator
```

Verify the expected dns operator instances are running:

```shell
kubectl get deployment -l app.kubernetes.io/created-by=dns-operator -A
NAMESPACE                  NAME                                 READY   UP-TO-DATE   AVAILABLE   AGE
kuadrant-dns-operator-1    dns-operator-controller-manager-1    1/1     1            1           10m
kuadrant-dns-operator-2    dns-operator-controller-manager-2    1/1     1            1           10m
kuadrant-dns-operator-3    dns-operator-controller-manager-3    1/1     1            1           10m
kuadrant-dns-operator-4    dns-operator-controller-manager-4    1/1     1            1           10m
kuadrant-dns-operator-5    dns-operator-controller-manager-5    1/1     1            1           10m
kuadrant-system            dns-operator-controller-manager      0/0     0            0           48
```

The cluster is now configured with several dns operators running only watching for dns resources (DNSRecords etc..) in their own namespace. 
Any DNSPolicies created in these namespaces will result in DNSRecord resources that are processes by the dns controllers running in that namespace. 


## Tailing operator pod logs

It's not possible to tail logs across namespaces with `kubectl logs -f`, but third party plugins such as [stern](https://github.com/stern/stern) can be used instead.

```shell
kubectl stern -l control-plane=dns-operator-controller-manager --all-namespaces
```
