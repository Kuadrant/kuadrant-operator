# Token Metrics Monitoring Guide For AI Traffic

## Overview

This tutorial walks you through using Kuadrant to monitor token metrics for AI workloads.
LLM APIs have varying computational costs based on token usage.

The use case is as follows: As a platform engineer, I want token usage and limit metrics to be exposed in Prometheus format so that I can monitor how many tokens are being used per user or route and compare them to defined limits for alerting and capacity planning.

This guide covers exposing usage data as metrics suitable for Prometheus collection.
These metrics are critical for visibility into LLM costs and usage patterns and will allow platform teams to build dashboards or alerts.

This feature is for observability only and does not affect policy enforcement, but it is critical for diagnosing policy effectiveness and model usage behavior.

## Prerequisites

- A Kubernetes cluster with the Kuadrant operator installed. See our [Getting Started](/latest/getting-started) guide, which lets you quickly set up a local cluster for evaluation purposes.
- A monitoring stack deployed, specifically the Prometheus component for this tutorial.
  - For bare-metal Kubernetes: See our [Observability stack](../../observability/README.md) guide.
  - For OpenShift: See the [Configuring user workload monitoring](https://docs.redhat.com/en/documentation/openshift_container_platform/4.17/html/monitoring/configuring-user-workload-monitoring#preparing-to-configure-the-monitoring-stack-uwm) guide.
- Configure Observability for Gateway and Kuadrant components. See our [Configure Observability](monitors.md) guide for more information.
- Activate TelemetryPolicy feature. See our [TelemetryPolicy](../../overviews/telemetrypolicy.md) overview for more information.
- The [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) command-line tool.
- [Optional] The [jq](https://jqlang.org/) command-line tool.

### Set the environment variables

Set the following environment variables for convenience in this tutorial:

```bash
export KUADRANT_GATEWAY_NS=api-gateway # Namespace for the example Gateway
export KUADRANT_GATEWAY_NAME=external # Name for the example Gateway
export KUADRANT_DEVELOPER_NS=llm # Namespace for an example LLM app
export KUADRANT_LLM_DOMAIN=llm.example.com # Domain name for an example LLM app
```

## Step 1: Deploy an LLM service

Create the namespace for the LLM service:

```bash
kubectl create ns ${KUADRANT_DEVELOPER_NS}
```

Deploy a simulated LLM service that mimics OpenAI-compatible APIs:

```yaml
kubectl apply -n ${KUADRANT_DEVELOPER_NS} -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: llm-sim
  labels:
    app: llm-sim
spec:
  replicas: 1
  selector:
    matchLabels:
      app: llm-sim
  template:
    metadata:
      labels:
        app: llm-sim
    spec:
      containers:
      - name: simulator
        image: ghcr.io/llm-d/llm-d-inference-sim:v0.1.1
        args:
          - --model
          - meta-llama/Llama-3.1-8B-Instruct
          - --port
          - "8000"
        ports:
          - containerPort: 8000
---
apiVersion: v1
kind: Service
metadata:
  name: llm-sim
spec:
  selector:
    app: llm-sim
  ports:
    - port: 80
      targetPort: 8000
      protocol: TCP
EOF
```

## Step 2: Create a Gateway

Create the namespace for the gateway:

```bash
kubectl create ns ${KUADRANT_GATEWAY_NS}
```

Create a gateway that will accept traffic for the LLM API:

```yaml
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: ${KUADRANT_GATEWAY_NAME}
  namespace: ${KUADRANT_GATEWAY_NS}
spec:
  gatewayClassName: istio
  listeners:
  - name: http
    protocol: HTTP
    port: 80
    hostname: ${KUADRANT_LLM_DOMAIN}
    allowedRoutes:
      namespaces:
        from: All
EOF
```

Check the gateway status:

```bash
kubectl get gateway ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}{"\n"}{.status.conditions[?(@.type=="Programmed")].message}{"\n"}'
```

Export the gateway URL for use in requests:

```bash
export KUADRANT_INGRESS_HOST=$(kubectl get gtw ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o jsonpath='{.status.addresses[0].value}')
export KUADRANT_INGRESS_PORT=$(kubectl get gtw ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o jsonpath='{.spec.listeners[?(@.name=="http")].port}')
export KUADRANT_GATEWAY_URL=${KUADRANT_INGRESS_HOST}:${KUADRANT_INGRESS_PORT}
```

## Step 3: Expose the service via HTTPRoute

Create an HTTPRoute to expose the LLM service:

```yaml
kubectl apply -n ${KUADRANT_DEVELOPER_NS} -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: llm-sim
spec:
  hostnames:
    - ${KUADRANT_LLM_DOMAIN}
  parentRefs:
    - name: ${KUADRANT_GATEWAY_NAME}
      namespace: ${KUADRANT_GATEWAY_NS}
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: "/"
      backendRefs:
        - name: llm-sim
          port: 80
EOF
```

Test connectivity to the LLM service:

```bash
curl --resolve $KUADRANT_LLM_DOMAIN:$KUADRANT_INGRESS_PORT:$KUADRANT_INGRESS_HOST http://$KUADRANT_LLM_DOMAIN:$KUADRANT_INGRESS_PORT/v1/models
# HTTP/1.1 200 OK
```

## Step 4: Set up API key authentication

Create API keys for different user tiers. This example creates two tiers: "free" and "gold":

```bash
# Create a free tier user
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: api-key-free-user-1
  namespace: ${KUADRANT_DEVELOPER_NS}
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: my-llm
  annotations:
    kuadrant.io/groups: free
    kuadrant.io/user-id: user-1
stringData:
  api_key: iamafreeuser
type: Opaque
EOF
```

```bash
# Create a gold tier user
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: api-key-gold-user-1
  namespace: ${KUADRANT_DEVELOPER_NS}
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: my-llm
  annotations:
    kuadrant.io/groups: gold
    kuadrant.io/user-id: user-2
stringData:
  api_key: iamagolduser
type: Opaque
EOF
```

Create an AuthPolicy that validates API keys and extracts user information:

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: llm-api-keys
  namespace: ${KUADRANT_DEVELOPER_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: llm-sim
  rules:
    authentication:
      "api-key-users":
        apiKey:
          allNamespaces: true
          selector:
            matchLabels:
              app: my-llm
        credentials:
          authorizationHeader:
            prefix: APIKEY
    response:
      success:
        filters:
          identity:
            json:
              properties:
                groups:
                  selector: auth.identity.metadata.annotations.kuadrant\.io/groups
                userid:
                  selector: auth.identity.metadata.annotations.kuadrant\.io/user-id
    authorization:
      "allow-groups":
        opa:
          rego: |
            groups := split(object.get(input.auth.identity.metadata.annotations, "kuadrant.io/groups", ""), ",")
            allow { groups[_] == "free" }
            allow { groups[_] == "gold" }
EOF
```

## Step 5: Apply token rate limiting

Create a `TokenRateLimitPolicy` with different token limits for each tier:

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: token-limits
  namespace: ${KUADRANT_DEVELOPER_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: llm-sim
  limits:
    free:
      rates:
        - limit: 100 # 100 tokens per minute for free users (small for testing)
          window: 1m
      when:
        - predicate: request.path == "/v1/chat/completions"
        - predicate: |
            auth.identity.groups.split(",").exists(g, g == "free")
      counters:
        - expression: auth.identity.userid
    gold:
      rates:
        - limit: 500 # 500 tokens per minute for gold users (small for testing)
          window: 1m
      when:
        - predicate: request.path == "/v1/chat/completions"
        - predicate: |
            auth.identity.groups.split(",").exists(g, g == "gold")
      counters:
        - expression: auth.identity.userid
EOF
```

## Step 6: Apply TelemetryPolicy to expose metrics usage per user and tier

Create a `TelemetryPolicy`:

```bash
kubectl apply -f - <<EOF
apiVersion: extensions.kuadrant.io/v1alpha1
kind: TelemetryPolicy
metadata:
  name: user-group
  namespace: ${KUADRANT_GATEWAY_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ${KUADRANT_GATEWAY_NAME}
  metrics:
    default:
      labels:
        user: auth.identity.userid
        group: auth.identity.groups
EOF
```

## Step 7: Generate Traffic to Populate Token Metrics

Define a bash function to send a chat completion request on behalf of a user (through the API key).

```bash
function call_llm {
    curl --resolve $KUADRANT_LLM_DOMAIN:$KUADRANT_INGRESS_PORT:$KUADRANT_INGRESS_HOST \
	    --write-out 'Status: %{http_code}\n' --silent --output /dev/null \
	    -H 'Content-Type: application/json' \
        -H "Authorization: APIKEY $1" \
        -X POST \
        -d '{
           "model": "meta-llama/Llama-3.1-8B-Instruct",
           "messages": [
             { "role": "user", "content": "What is Kubernetes?" }
           ],
           "max_tokens": 100,
           "stream": false,
           "stream_options": {
             "include_usage": true
           }
         }'  \
        http://llm.example.com:$KUADRANT_INGRESS_PORT/v1/chat/completions
}
```

The function accepts one parameter, which is the API key. For example, `call_llm iamafreeuser`.

Send some requests as `user-1`, which belongs to the `free` tier.

```bash
for i in {1..30}; do
    echo "Request $i:"
    call_llm iamafreeuser
done
```

Send some requests as `user-2`, which belongs to the `gold` tier.

```bash
for i in {1..30}; do
    echo "Request $i:"
    call_llm iamagolduser
done
```

## Step 8: View token metrics in Prometheus

The metrics exposed by the rate-limiting service can be found in the
[Limitador Metrics Monitoring](limitador-metrics.md) guide.

In the current context, `authorized_hits` will represent the usage of tokens per user and tier.

#### Using the Web UI:
* Visit http://localhost:9090/graph
* Create three panels:
  * PromQL: `authorized_hits`
  * PromQL: `authorized_calls`
  * PromQL: `limited_calls`

#### Using the API:

* `authorized_hits`

```bash
curl -s "http://${PROMETHEUS_HOST}:${PROMETHEUS_PORT}/api/v1/query?query=authorized_hits" | jq '.data.result'
```

* `authorized_calls`

```bash
curl -s "http://${PROMETHEUS_HOST}:${PROMETHEUS_PORT}/api/v1/query?query=authorized_calls" | jq '.data.result'
```

* `limited_calls`

```bash
curl -s "http://${PROMETHEUS_HOST}:${PROMETHEUS_PORT}/api/v1/query?query=limited_calls" | jq '.data.result'
```

> Note: The Prometheus installation documentation should provide the *PROMETHEUS_HOST* and *PROMETHEUS_PORT* variables.

## Next steps

- Experiment with different custom labels in the TelemetryPolicy specification.
- Integrate with your actual LLM service.

## Troubleshooting

If requests are being rejected unexpectedly:
1. Verify that the API key is correct.
2. Ensure the LLM response includes `usage.total_tokens`.
3. Review the `AuthPolicy` and `TokenRateLimitPolicy` statuses.

```bash
# Check AuthPolicy status
kubectl get authpolicy -n ${KUADRANT_GATEWAY_NS} llm-api-keys -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'

# Check TokenRateLimitPolicy status
kubectl get tokenratelimitpolicy -n ${KUADRANT_GATEWAY_NS} token-limits -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
```

## Cleanup

To remove all resources created in this tutorial:

```bash
# Delete the LLM service namespace
kubectl delete namespace ${KUADRANT_DEVELOPER_NS}

# Delete the gateway namespace
kubectl delete namespace ${KUADRANT_GATEWAY_NS}
```
