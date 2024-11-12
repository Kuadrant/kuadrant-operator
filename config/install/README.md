# Install Kaudrant and Sail via OLM

- Pre-Req is that OLM (operator lifecycle manager) is already installed

- (optional dependencies)
  - If you want to use `TLSPolicy` you should install the cert-manager operator. 
  - AWS/Azure or GCP with DNS capabilities if you want to make use of `DNSPolicy`.
  - Accessible Redis instance, if you want persistent storage for your rate limit counters.


Install the Sail and Kuadrant Operators via OLM:


> Note: By default this will install the "latest" or "main" of kuadrant. To change that, pick a release from the releases page in the kuadrant operator and change the image in the `config/deploy/olm/catalogsource.yaml` or if you are familiar with kustomize you could apply your own kustomization.

```
kubectl apply -k config/install/standard
``` 

3) verify kuadrant and sail operators are installed. Note this can take a while. You can also take a look at the subscription and installplan resource to help with debugging but the end state should be as below:

```
kubectl get deployments -n kuadrant-system
```

```

NAME                                    READY   UP-TO-DATE   AVAILABLE   AGE
authorino-operator                      1/1     1            1           83m
dns-operator-controller-manager         1/1     1            1           83m
kuadrant-console-plugin                 1/1     1            1           83m
kuadrant-operator-controller-manager    1/1     1            1           83m
limitador-operator-controller-manager   1/1     1            1           83m

```



```
kubectl get deployments -n gateway-system
```

```

NAME            READY   UP-TO-DATE   AVAILABLE   AGE
sail-operator   1/1     1            1           81m

```

## Configure the installation

### TLS and DNS integration

To setup the DNS and TLS integration (TLS also uses DNS for verification) follow these steps:

1) Depending on your choice of cloud provider:
    - setup the needed `$CLOUD_PROVIDER-credentals.env` in the cloud provider directory. E.G create `aws-credentials.env` in the `install/configure/aws` directory

3) execute the configure for that cloud provider

```
kubectl apply -k config/install/configure/aws

```

This will configure Kuadrant and Sail installing their components as well as setup the the credentials needed for access DNS zones in the cloud provider and create a lets-encrypt cluster issuer configured to use DNS based validation.

### Validate

Validate Kuadrant is ready via the kuadrant resource status condition

```
kubectl get kuadrant kuadrant -n kuadrant-system -o=yaml

```

At this point Kuadrant is ready to use. Below are some additonal configuration that can be applied.

### External Redis

create a `redis-credential.env` in the `config/install/configure/redis-storage` dir

```
kubectl apply -k config/install/configure/redis-storage

```

This will setup limitador to use provided redis connection URL as a backend store for ratelimit counters. Limitador will becomes temporarilly unavailable as it restarts.

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

(expected output)
enableUserWorkload: true
```

Install the gateway & kuadrant metrics components and configuration, including Grafana.

```bash
kubectl apply -k config/install/configure/observability
```

Configure the openshift thanos-query instance as a data source in Grafana.

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
