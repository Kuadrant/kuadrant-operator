# Install Kuadrant and Sail via OLM

## Prerequisites  
- Clone the[ Kuadrant-operator](https://github.com/Kuadrant/kuadrant-operator) repo
- OLM (operator lifecycle manager)
- cert-manager 
  - [cert-manager Operator for Red Hat OpenShift](https://docs.openshift.com/container-platform/4.16/security/cert_manager_operator/cert-manager-operator-install.html)
  - [installing cert-manager via OperatorHub](https://cert-manager.io/docs/installation/operator-lifecycle-manager/)
- AWS, Azure or GCP with DNS capabilities. (Optional)
- Accessible Redis instance, for persistent storage for your rate limit counters. (Optional)


> Note: By default the following guide will install the "latest" or "main" version of Kuadrant. To pick a specific version, change the image in the `config/deploy/install/standard/kustomization.yaml`. All versions available can be found on the Kuadrant operator [release page](https://github.com/Kuadrant/kuadrant-operator/releases)

> Note: for multiple clusters, it would make sense to do the installation via a tool like [argocd](https://argo-cd.readthedocs.io/en/stable/). For other methods of addressing multiple clusters take a look at the [kubectl docs](https://kubernetes.io/docs/tasks/access-application-cluster/configure-access-multiple-clusters/)

## Setup the environment

```
kubectl apply -k config/install/standard
``` 

Verify both Kuadrant and sail operators are installed. Note, that this can take a while. You can also take a look at the subscription and installplan resource to help with debugging but the end state should be as below:

```
kubectl get deployments -n kuadrant-system


# NAME                                    READY   UP-TO-DATE   AVAILABLE   AGE
# authorino-operator                      1/1     1            1           83m
# dns-operator-controller-manager         1/1     1            1           83m
# kuadrant-console-plugin                 1/1     1            1           83m
# kuadrant-operator-controller-manager    1/1     1            1           83m
# limitador-operator-controller-manager   1/1     1            1           83m
```



```
kubectl get deployments -n gateway-system


# NAME            READY   UP-TO-DATE   AVAILABLE   AGE
# istiod          1/1     1            1           61s
# sail-operator   1/1     1            1           81m
```

## Configure the installation

### TLS and DNS integration


Create the `$CLOUD_PROVIDER-credentials.env file` in the cloud provider directory `config/install/configure/$CLOUD_PROVIDER.` e.g. `aws-credentials.env` in the `config/install/configure/aws` directory. Apply the configuration for the desired cloud provider. Example AWS

```
kubectl apply -k config/install/configure/aws
```

This will configure Kuadrant and Sail to install their components, set the credentials needed to access DNS zones in the cloud provider, and create a Let's Encrypt cluster issuer configured to use DNS-based validation.

### Validate

Validate Kuadrant is ready via the kuadrant resource status condition

```
kubectl get kuadrant kuadrant -n kuadrant-system -o=yaml
```

At this point Kuadrant is ready to use. Below are some additional configuration that can be applied.

### External Redis

create a `redis-credential.env` in the `config/install/configure/redis-storage` dir

```
kubectl apply -k config/install/configure/redis-storage
```

This will setup limitador to use provided redis connection URL as a backend store for ratelimit counters. Limitador will becomes temporarily unavailable as it restarts.

### Validate

Validate Kuadrant is in a ready state as before:

```
kubectl get kuadrant kuadrant -n kuadrant-system -o=yaml
```

## Set up observability

Verify that user workload monitoring is enabled in your Openshift cluster.
If it not enabled, check the [Openshift documentation](https://docs.openshift.com/container-platform/4.17/observability/monitoring/enabling-monitoring-for-user-defined-projects.html) for how to do this.


```bash
kubectl get configmap cluster-monitoring-config -n openshift-monitoring -o jsonpath='{.data.config\.yaml}'|grep enableUserWorkload
# (expected output)
# enableUserWorkload: true
```

Install the gateway & Kuadrant metrics components and configuration, including Grafana.

```bash
kubectl apply -k config/install/configure/observability
```

Configure the Openshift thanos-query instance as a data source in Grafana.

```bash
TOKEN="Bearer $(oc whoami -t)"
HOST="$(kubectl -n openshift-monitoring get route thanos-querier -o jsonpath='https://{.status.ingress[].host}')"
echo "TOKEN=$TOKEN" > config/observability/openshift/grafana/datasource.env
echo "HOST=$HOST" >> config/observability/openshift/grafana/datasource.env
kubectl apply -k config/observability/openshift/grafana
```

Create the example dashboards in Grafana

```bash
kubectl apply -k examples/dashboards
```

Access the Grafana UI, using the default user/pass of root/secret.
You should see the example dashboards in the 'monitoring' folder.
For more information on the example dashboards, check out the [documentation](https://docs.kuadrant.io/latest/kuadrant-operator/doc/observability/examples/).

```bash
kubectl -n monitoring get routes grafana-route -o jsonpath="https://{.status.ingress[].host}"
```
