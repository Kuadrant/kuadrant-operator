# Install Kuadrant on an OpenShift cluster

!!! note

    You must perform these steps on each OpenShift cluster that you want to use Kuadrant on.

    In this document we use AWS route 53 as the example setup.

!!! warning

    Kuadrant uses a number of labels to search and filter resources on the cluster.
    All required labels are formatted as `kuadrant.io/*`.
    Removal of any labels with the prefix may cause unexpected behaviour and degradation of the product.

## Prerequisites

- OpenShift Container Platform 4.16.x or later with community Operator catalog available.
- AWS/Azure or GCP with DNS capabilities.
- Accessible Redis instance.

## Procedure

### Step 1 - Set up your environment

We use env vars for convenience only here. If you know these values you can setup the required yaml files in anyway that suites your needs.

```bash
export AWS_ACCESS_KEY_ID=xxxxxxx # Key ID from AWS with Route 53 access
export AWS_SECRET_ACCESS_KEY=xxxxxxx # Access key from AWS with Route 53 access
export REDIS_URL=redis://user:xxxxxx@some-redis.com:10340 # A Redis cluster URL
```

Set the version of Kuadrant to the latest released version: https://github.com/Kuadrant/kuadrant-operator/releases/

```
export KUADRANT_VERSION='vX.Y.Z'
```

### Step 2 - Install Gateway API v1

Before you can use Kuadrant, you must install Gateway API v1 as follows:

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.1.0/standard-install.yaml
```

### Step 3 - Install cert-manager

Before you can use Kuadrant, you must install cert-manager. Cert-Manager is used by kuadrant to manage TLS certificates for your gateways.

> The minimum supported version of cert-manager is v1.14.0.

Install one of the different flavours of the Cert-Manager.

#### Install community version of the cert-manager

Consider [installing cert-manager via OperatorHub](https://cert-manager.io/docs/installation/operator-lifecycle-manager/),
which you can do from the OpenShift web console.

More installation options at [cert-manager.io](https://cert-manager.io/docs/installation/)

#### Install cert-manager Operator for Red Hat OpenShift

You can install the [cert-manager Operator for Red Hat OpenShift](https://docs.openshift.com/container-platform/4.16/security/cert_manager_operator/cert-manager-operator-install.html)
by using the web console.

> **Note:** Before using Kuadrant's `TLSPolicy` you will need to setup a certificate issuer refer to the [cert-manager docs for more details](https://cert-manager.io/docs/configuration/acme/dns01/route53/#creating-an-issuer-or-clusterissuer)

### Step 4 - (Optional) Install and configure Istio with the Sail Operator

!!! note

    Skip this step if planing to use [Envoy Gateway](https://gateway.envoyproxy.io/) as Gateway API provider

Kuadrant integrates with Istio as a Gateway API provider. You can set up an Istio-based Gateway API provider by using the Sail Operator.

#### Install Istio

To install the Istio Gateway provider, run the following commands:

```bash
kubectl create ns gateway-system
```

```bash
kubectl  apply -f - <<EOF
kind: OperatorGroup
apiVersion: operators.coreos.com/v1
metadata:
  name: sail
  namespace: gateway-system
spec:
  upgradeStrategy: Default
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: sailoperator
  namespace: gateway-system
spec:
  channel: candidates
  installPlanApproval: Automatic
  name: sailoperator
  source: community-operators
  sourceNamespace: openshift-marketplace
EOF
```

Check the status of the installation as follows:

```bash
kubectl get installplan -n gateway-system -o=jsonpath='{.items[0].status.phase}'
```

When ready, the status will change from `installing` to `complete`.

#### Configure Istio

To configure the Istio Gateway API provider, run the following command:

```bash
kubectl apply -f - <<EOF
apiVersion: sailoperator.io/v1alpha1
kind: Istio
metadata:
  name: default
spec:
  namespace: gateway-system
  updateStrategy:
    type: InPlace
    inactiveRevisionDeletionGracePeriodSeconds: 30
  version: v1.23.0
  values:
    pilot:
      autoscaleEnabled: false
EOF
```

More details on what can be configured can be found by executing

```
 kubectl explain istio.spec.values
```

And in documention : [Istio Config](https://istio.io/1.23.0/docs/reference/config/)

Wait for Istio to be ready as follows:

```bash
kubectl wait istio.sailoperator.io/default -n gateway-system --for="condition=Ready=true"
```

### Step 5 - (Optional) Install Envoy Gateway as a Gateway API provider

!!! note

    Skip this step if planing to use [Istio](https://istio.io/) as Gateway API provider

!!! note

    There are several ways to install Envoy Gateway (via `egctl`, Helm chart or Kubernetes yaml) - this is just an example for starting from a bare Kubernetes cluster.

```bash
helm install eg oci://docker.io/envoyproxy/gateway-helm --version v1.1.0 -n envoy-gateway-system --create-namespace
```

Enable _EnvoyPatchPolicy_ feature:

```bash
TMP=$(mktemp -d)
kubectl get configmap -n envoy-gateway-system envoy-gateway-config -o jsonpath='{.data.envoy-gateway\.yaml}' > ${TMP}/envoy-gateway.yaml
yq e '.extensionApis.enableEnvoyPatchPolicy = true' -i ${TMP}/envoy-gateway.yaml
kubectl create configmap -n envoy-gateway-system envoy-gateway-config --from-file=envoy-gateway.yaml=${TMP}/envoy-gateway.yaml -o yaml --dry-run=client | kubectl replace -f -
kubectl rollout restart deployment envoy-gateway -n envoy-gateway-system
```

Wait for Envoy Gateway to become available::

```bash
kubectl wait --timeout=5m -n envoy-gateway-system deployment/envoy-gateway --for=condition=Available
```

### Step 6 - Optional: Configure observability and metrics (Istio only)

Kuadrant provides a set of example dashboards that use known metrics exported by Kuadrant and Gateway components to provide insight into different components of your APIs and Gateways. While not essential, it is recommended to set these up.
First, enable [monitoring for user-defined projects](https://docs.openshift.com/container-platform/4.17/observability/monitoring/enabling-monitoring-for-user-defined-projects.html#enabling-monitoring-for-user-defined-projects_enabling-monitoring-for-user-defined-projects).
This will allow the scraping of metrics from the gateway and Kuadrant components.
The [example dashboards and alerts](https://docs.kuadrant.io/latest/kuadrant-operator/doc/observability/examples/) for observing Kuadrant functionality use low-level CPU metrics and network metrics available from the user monitoring stack in OpenShift. They also use resource state metrics from Gateway API and Kuadrant resources.

To scrape these additional metrics, you can install a `kube-state-metrics` instance, with a custom resource configuration as follows:

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/main/config/observability/openshift/kube-state-metrics.yaml
kubectl apply -k https://github.com/Kuadrant/gateway-api-state-metrics/config/kuadrant?ref=0.6.0
```

To enable request metrics in Istio and scrape them, create the following resource:

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/config/observability/prometheus/monitors/istio/service-monitor-istiod.yaml
```

Some example dashboards show aggregations based on the path of requests.
By default, Istio metrics don't include labels for request paths.
However, you can enable them with the below Telemetry resource.
Note that this may lead to a [high cardinality](https://www.robustperception.io/cardinality-is-key/) label where multiple time-series are generated,
which can have an impact on performance and resource usage.

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/main/config/observability/openshift/telemetry.yaml
```

You can configure scraping of metrics from the various Kuadrant operators with the below resources.

```bash
kubectl create ns kuadrant-system
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/config/observability/prometheus/monitors/operators.yaml
```

!!! note

    There is 1 more metrics configuration that needs to be applied so that all relevant metrics are being scraped.
    That configuration depends on where you deploy your Gateway later.
    The steps to configure that are detailed in the follow on [Secure, protect, and connect](../user-guides/full-walkthrough/secure-protect-connect-openshift.md) guide.

The [example Grafana dashboards and alerts](https://docs.kuadrant.io/latest/kuadrant-operator/doc/observability/examples/) for observing Kuadrant functionality use low-level CPU metrics and network metrics available from the user monitoring stack in OpenShift. They also use resource state metrics from Gateway API and Kuadrant resources.

For Grafana installation details, see [installing Grafana on OpenShift](https://cloud.redhat.com/experts/o11y/ocp-grafana/). That guide also explains how to set up a data source for the Thanos Query instance in OpenShift. For more detailed information about accessing the Thanos Query endpoint, see the [OpenShift documentation](https://docs.openshift.com/container-platform/4.17/observability/monitoring/accessing-third-party-monitoring-apis.html#accessing-metrics-from-outside-cluster_accessing-monitoring-apis-by-using-the-cli).

!!! note

    For some dashboard panels to work correctly, HTTPRoutes must include a "service" and "deployment" label with a value that matches the name of the service & deployment being routed to. eg. "service=myapp, deployment=myapp".
    This allows low level Istio & envoy metrics to be joined with Gateway API state metrics.

### Step 7 - Setup the catalogsource

Before installing the Kuadrant Operator, you must enter the following commands to set up secrets that you will use later.
If you haven't aleady created the `kuadrant-system` namespace during the optional observability setup, do that first:

```bash
kubectl create ns kuadrant-system
```

Set up a `CatalogSource` as follows:

```bash
kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: kuadrant-operator-catalog
  namespace: kuadrant-system
spec:
  sourceType: grpc
  image: quay.io/kuadrant/kuadrant-operator-catalog:${KUADRANT_VERSION}
  displayName: Kuadrant Operators
  publisher: grpc
  updateStrategy:
    registryPoll:
      interval: 45m
EOF
```

### Step 8 - Install the Kuadrant Operator

To install the Kuadrant Operator, enter the following command:

```bash
kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: kuadrant-operator
  namespace: kuadrant-system
spec:
  channel: stable
  installPlanApproval: Automatic
  name: kuadrant-operator
  source: kuadrant-operator-catalog
  sourceNamespace: kuadrant-system
---
kind: OperatorGroup
apiVersion: operators.coreos.com/v1
metadata:
  name: kuadrant
  namespace: kuadrant-system
spec:
  upgradeStrategy: Default
EOF
```
**Authenticated Registry**

!!! note

    If you need to use a wasm image from a protected registry (such as the redhat registry), you will need to configure an image pull secret to use. This secret must exist in each namespace where there is a Gateway resource defined that you intend to target with `AuthPolicy` or `RatelimitPolicy` and it must be named `wasm-plugin-pull-secret`.

Example Creating Pull Secret:

```bash
kubectl create secret docker-registry wasm-plugin-pull-secret -n ${GATEWAY_NAMESPACE} \ --docker-server=my.registry.io \ --docker-username=your-registry-service-account-username \ --docker-password=your-registry-service-account-password
```

The configuration for the gateway will expect this secret to exist if the registry name begins with `registry.redhat.com` by default. This can be changed via the env var `PROTECTED_REGISTRY` set in the kuadrant-operator.

Wait for the Kuadrant Operators to be installed as follows:

```bash
kubectl get installplan -n kuadrant-system -o=jsonpath='{.items[0].status.phase}'
```

After some time, this command should return `complete`.

#### Set up a DNSProvider

The example here is for AWS Route 53. It is important the secret for the DNSProvider is setup in the same namespace as the gateway.

```bash
# note ingress-gateway is just an example

kubectl create ns ingress-gateway
```

```bash
kubectl -n ingress-gateway create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
```

For more details on other providers take a look at [DNS Providers](https://docs.kuadrant.io/latest/dns-operator/docs/provider/)

#### Setup TLS DNS verification credential

When setting up TLS certs via provider like lets-encrypt, cert-manager will do a DNS based verification. To allow it do this, it will need a similar credential to that one set up for the DNSProvider, but it needs to be created in the cert-manager namespace where the operator is installed.

```bash
kubectl -n cert-manager create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
```

### Step 9 - Install Kuadrant Components

To trigger your Kuadrant deployment, enter the following command:

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: kuadrant-system

EOF
```

Wait for Kuadrant to be ready as follows:

```bash
kubectl wait kuadrant/kuadrant --for="condition=Ready=true" -n kuadrant-system --timeout=300s
```

This will setup and configure a number of Kuadrant subcomponents. Some of these can also take additional configuration:

- Authorino (Enforcement Component for AuthPolicy)
  - Learn More: (Authorino CRD)[https://docs.kuadrant.io/latest/authorino-operator/#the-authorino-custom-resource-definition-crd]
- Limitador (Enforcement Component for RateLimitPolicy)
  - Learn More:(Limitador CRD)[https://docs.kuadrant.io/latest/limitador-operator/#features]
- DNS Operator (Enforcement Component for DNSPOlicy)

### Configuring Redis Storage for Limitador

#### Redis credentials for storage of rate limiting counters

In this installation we will show how to configure ratelimiting counters to be stored in redis. Before we go further we need to setup a redis secret to use later:

```bash
kubectl -n kuadrant-system create secret generic redis-credential --from-literal="URL"=$REDIS_URL
```

#### Update limitador config

To configure redis storage for Limatador, we must update the Limitador custom resource to use the secret we created:

You can run a command like the one below to add this configuration:

```
kubectl patch limitador limitador --type=merge -n kuadrant-system -p '
spec:
  storage:
    redis:
      configSecretRef:
        name: redis-credential
'
```

Check that limitador is back to ready:

```
kubectl wait limitador/limitador -n kuadrant-system --for="condition=Ready=true"

```

Kuadrant is now ready to use.

### Step 10 - Configure the Kuadrant Console Plugin

When running on OpenShift, the Kuadrant Operator will automatically install and configure the Kuadrant dynamic console plugin.

#### Enable the Console Plugin

To enable the Kuadrant console plugin:

1. Log in to OpenShift or OKD as an administrator.
2. Switch to the **Admin** perspective.
3. Navigate to **Home** > **Overview**.
4. In the **Dynamic Plugins** section of the status box, click **View all**.
5. In the **Console plugins** area, find the `kuadrant-console-plugin` plugin. It should be listed but disabled.
6. Click the **Disabled** button next to the `kuadrant-console-plugin` plugin.
7. Select the **Enabled** radio button, and then click **Save**.
8. Wait for the plugin status to change to **Loaded**.

Once the plugin is loaded, refresh the console. You should see a new **Kuadrant** section in the navigation sidebar.

## Next steps

- [Secure, protect, and connect APIs with Kuadrant on OpenShift](../user-guides/full-walkthrough/secure-protect-connect-openshift.md)
