# Limitador Metrics Monitoring Guide

This guide explains how to monitor Limitador rate limiting metrics using Prometheus, including how to set up a complete development environment, deploy the observability stack, and generate traffic to see metrics in action.

## Metrics Exposed by Limitador

Limitador exposes the following metrics through its `/metrics` endpoint on port 8080:

| Metric Name | Type | Description | Labels                               | Usage |
|-------------|------|-------------|--------------------------------------|-------|
| `limitador_up` | Gauge | Health indicator (always 1 when running) | None                                 | Service health monitoring |
| `authorized_calls` | Counter | Successfully processed (non-rate-limited) requests | `limitador_namespace`                | Track allowed requests |
| `limited_calls` | Counter | Rate-limited (rejected) requests | `limitador_namespace`, `limit_name`  | Track blocked requests |
| `datastore_partitioned` | Gauge | Datastore connectivity (0=connected, 1=partitioned) | None                                 | Backend health monitoring |
| `datastore_latency` | Histogram | Latency to underlying counter datastore | None                                 | Performance monitoring |

**Notes:**
- `limitador_namespace`: Format is `"{k8s_namespace}/{route_name}"` (e.g., `"toystore/toystore"`)
- `limit_name`: Contains the actual limit name from your RateLimitPolicy (e.g., `"alice-limit"`, `"bob-limit"`) 
- `authorized_calls` and `limited_calls` only appear after traffic is processed

## Enabling `limit_name` Labels

To include `limit_name` labels in your Limitador metrics, you need to configure the Limitador instance to use exhaustive telemetry.
Set the `telemetry` field to `exhaustive` in your Limitador CR:

```bash
kubectl patch limitador limitador -n kuadrant-system --type='merge' -p='{"spec":{"telemetry":"exhaustive"}}'
```


## Setting Up the Development Environment

### Step 1: Create a Local Kubernetes Cluster with Kuadrant

Set up a local cluster:

```bash
make local-setup
```

### Step 2: Create Kuadrant CR with Observability Enabled

Create a Kuadrant CR with observability features enabled 

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: kuadrant-system
spec:
  observability:
    enable: true
EOF
```

### Step 3: Deploy the Observability Stack

Deploy Prometheus, Grafana, Alertmanager, and related monitoring infrastructure using the commands from the [observability README](https://github.com/Kuadrant/kuadrant-operator/tree/main/doc/observability#accessing-grafana--prometheus):

```bash
# Deploy Prometheus Operator and monitoring stack
./bin/kustomize build ./config/observability/ | docker run --rm -i docker.io/ryane/kfilt -i kind=CustomResourceDefinition | kubectl apply --server-side -f -
./bin/kustomize build ./config/observability/ | docker run --rm -i docker.io/ryane/kfilt -x kind=CustomResourceDefinition | kubectl apply -f -

# Deploy Thanos for long-term storage (optional)
./bin/kustomize build ./config/thanos | kubectl apply -f -

# Deploy example dashboards and alerts
./bin/kustomize build ./examples/dashboards | kubectl apply -f -
./bin/kustomize build ./examples/alerts | kubectl apply -f -

# Configure gateway-specific monitoring (choose based on your setup)
# For Istio:
./bin/kustomize build ./config/observability/prometheus/monitors/istio | kubectl apply -f -

# For Envoy Gateway:
# ./bin/kustomize build ./config/observability/prometheus/monitors/envoy | kubectl apply -f -
```

### Step 4: Verify Limitador is Deployed

Ensure Limitador is running in your cluster:

```bash
# Check for Limitador pods
kubectl get pods -n kuadrant-system -l app=limitador
```

## Setting Up Limitador Metrics Monitoring

After completing the development environment setup above, you need to create a PodMonitor specifically for Limitador application instances, as the default observability setup only monitors operators.

### Create PodMonitor for Limitador Instances

Create a PodMonitor to scrape metrics from actual Limitador pods:

```yaml
kubectl apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: limitador-metrics
  namespace: kuadrant-system
  labels:
    app: limitador
spec:
  podMetricsEndpoints:
    - path: /metrics
      port: http
      scheme: http
      interval: 30s
  selector:
    matchLabels:
      app: limitador
  namespaceSelector:
    matchNames:
      - kuadrant-system
EOF
```

### Accessing Prometheus and Grafana

To access the monitoring interfaces, use port forwarding as described in the [observability README](https://github.com/Kuadrant/kuadrant-operator/tree/main/doc/observability#accessing-grafana--prometheus):

#### Access Prometheus

```bash
# Port forward to Prometheus
kubectl -n monitoring port-forward service/prometheus-k8s 9090:9090
```

The Prometheus UI is available at [http://127.0.0.1:9090](http://127.0.0.1:9090).

#### Access Grafana

```bash
# Port forward to Grafana  
kubectl -n monitoring port-forward service/grafana 3000:3000
```

The Grafana UI is available at [http://127.0.0.1:3000/](http://127.0.0.1:3000/) with default credentials:
- **Username**: `admin`
- **Password**: `admin`


### Verify Prometheus Discovery

Check that Prometheus has discovered your Limitador targets:

**Using the Web UI:**
1. Visit http://localhost:9090/targets
2. Look for: "kuadrant-system/limitador-metrics" with state "UP"

**Using the API:**
```bash
curl -s http://localhost:9090/api/v1/targets | jq '.data.activeTargets[] | select(.labels.job | contains("limitador")) | {job: .labels.job, health: .health, scrapeUrl: .scrapeUrl}'
```

Expected output:
```json
{
  "job": "kuadrant-system/limitador-metrics",
  "health": "up", 
  "scrapeUrl": "http://10.244.0.29:8080/metrics"
}
```

### Query Basic Metrics

Test that metrics are being collected:

```bash
# Check limitador health
curl -s "http://localhost:9090/api/v1/query?query=limitador_up" | jq '.data.result'

# Check datastore connectivity
curl -s "http://localhost:9090/api/v1/query?query=datastore_partitioned" | jq '.data.result'
```

## Generating Traffic to Populate Rate Limiting Metrics

The `authorized_calls` and `limited_calls` metrics only appear after processing requests. Follow this section to generate traffic using the [authenticated rate limiting user guide](../ratelimiting/authenticated-rl-for-app-developers.md).

### Step 1: Set Up Environment

Set environment variables for the toystore example:

```bash
export KUADRANT_GATEWAY_NS=api-gateway
export KUADRANT_GATEWAY_NAME=external  
export KUADRANT_DEVELOPER_NS=toystore
```

### Step 2: Create Gateway and Application

Create the gateway:

```bash
kubectl create ns ${KUADRANT_GATEWAY_NS}

kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: ${KUADRANT_GATEWAY_NAME}
  namespace: ${KUADRANT_GATEWAY_NS}
  labels:
    kuadrant.io/gateway: "true"
spec:
  gatewayClassName: istio
  listeners:
    - name: http
      protocol: HTTP
      port: 80
      allowedRoutes:
        namespaces:
          from: All
EOF
```

Wait for the Gateway to be ready and check its status:

```bash
# Check Gateway status
kubectl get gateway ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o yaml

# Verify Gateway is Accepted and Programmed
kubectl get gateway ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}{"\n"}{.status.conditions[?(@.type=="Programmed")].message}{"\n"}'
```

Deploy the toystore application:

```bash
kubectl create ns ${KUADRANT_DEVELOPER_NS}
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/Kuadrant-operator/main/examples/toystore/toystore.yaml -n ${KUADRANT_DEVELOPER_NS}
```

Create the HTTPRoute:

```bash
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: toystore
  namespace: ${KUADRANT_DEVELOPER_NS}
spec:
  parentRefs:
  - name: ${KUADRANT_GATEWAY_NAME}
    namespace: ${KUADRANT_GATEWAY_NS}
  hostnames:
  - api.toystore.com
  rules:
  - matches:
    - path:
        type: Exact
        value: "/toy"
      method: GET
    backendRefs:
    - name: toystore
      port: 80
EOF
```

Get the gateway URL:

```bash
export KUADRANT_INGRESS_HOST=$(kubectl get gtw ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o jsonpath='{.status.addresses[0].value}')
export KUADRANT_INGRESS_PORT=$(kubectl get gtw ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o jsonpath='{.spec.listeners[?(@.name=="http")].port}')
export KUADRANT_GATEWAY_URL=${KUADRANT_INGRESS_HOST}:${KUADRANT_INGRESS_PORT}
```

### Step 3: Configure Authentication

Create an AuthPolicy:

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: toystore
  namespace: ${KUADRANT_DEVELOPER_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  rules:
    authentication:
      "api-key-users":
        apiKey:
          selector:
            matchLabels:
              app: toystore
          allNamespaces: true
        credentials:
          authorizationHeader:
            prefix: APIKEY
    response:
      success:
        filters:
          "identity":
            json:
              properties:
                "userid":
                  selector: auth.identity.metadata.annotations.secret\.kuadrant\.io/user-id
EOF
```

Create API keys for test users:

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: alice-key
  namespace: ${KUADRANT_DEVELOPER_NS}
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
  annotations:
    secret.kuadrant.io/user-id: alice
stringData:
  api_key: IAMALICE
type: Opaque
---
apiVersion: v1
kind: Secret
metadata:
  name: bob-key
  namespace: ${KUADRANT_DEVELOPER_NS}
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
  annotations:
    secret.kuadrant.io/user-id: bob
stringData:
  api_key: IAMBOB
type: Opaque
EOF
```

### Step 4: Configure Rate Limiting

Create a RateLimitPolicy with different limits for Alice and Bob:

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: toystore
  namespace: ${KUADRANT_DEVELOPER_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  limits:
    "alice-limit":
      rates:
      - limit: 5
        window: 10s
      when:
      - predicate: "auth.identity.userid == 'alice'"
    "bob-limit":
      rates:
      - limit: 2
        window: 10s
      when:
      - predicate: "auth.identity.userid == 'bob'"
EOF
```


### Step 5: Generate Traffic to Populate Metrics

Now generate requests to populate the metrics:

#### Generate Authorized Requests (Alice - within limits)

```bash
# Send 3 requests as Alice (under her 5/10s limit)
for i in {1..3}; do 
  echo "Request $i:"
  curl --write-out 'Status: %{http_code}\n' --silent --output /dev/null \
    -H 'Authorization: APIKEY IAMALICE' \
    -H 'Host: api.toystore.com' \
    http://$KUADRANT_GATEWAY_URL/toy
  sleep 1
done
```

#### Generate Rate Limited Requests (Alice - exceed limits)

```bash
# Send 8 rapid requests as Alice to trigger rate limiting
echo "Rapid requests (triggering rate limits):"
for i in {1..8}; do 
  curl --write-out '%{http_code} ' --silent --output /dev/null \
    -H 'Authorization: APIKEY IAMALICE' \
    -H 'Host: api.toystore.com' \
    http://$KUADRANT_GATEWAY_URL/toy
done
echo ""
```

#### Generate Traffic for Bob

```bash
# Send requests as Bob (2/10s limit)
echo "Bob's requests:"
for i in {1..5}; do 
  curl --write-out '%{http_code} ' --silent --output /dev/null \
    -H 'Authorization: APIKEY IAMBOB' \
    -H 'Host: api.toystore.com' \
    http://$KUADRANT_GATEWAY_URL/toy
  sleep 1
done
echo ""
```

### Step 6: Monitor Metrics in Prometheus

After generating traffic, check the metrics in Prometheus:

```bash
# Port forward to Prometheus (if not already done)
kubectl port-forward -n monitoring service/prometheus-k8s 9090:9090
```

Visit http://localhost:9090 and run these queries:

#### Basic Health Metrics

```promql
# Limitador health (should always be 1)
limitador_up

# Datastore connectivity (should be 0 for connected)
datastore_partitioned
```

#### Rate Limiting Metrics

```promql
# Total authorized calls
authorized_calls

# Total limited calls  
limited_calls

```

#### Metrics by Namespace

```promql
# Authorized calls by namespace/route
authorized_calls{limitador_namespace=~".*toystore.*"}

# Limited calls by namespace/route
limited_calls{limitador_namespace=~".*toystore.*"}
```


