# Token-based Rate Limiting for Large Language Model APIs

This tutorial walks you through configuring token-based rate limiting using Kuadrant's `TokenRateLimitPolicy` to protect Large Language Model (LLM) APIs. Unlike traditional request counting, this approach limits API usage based on actual token consumption.

> **Note:** Currently, `TokenRateLimitPolicy` only supports non-streaming responses (where `stream: false` or is omitted in the request). Support for streaming responses is planned for future releases.

## Overview

Traditional rate limiting counts requests, but LLM APIs have varying computational costs based on token usage. `TokenRateLimitPolicy` addresses this by:

- Counting actual tokens consumed from LLM responses
- Setting different limits for different user tiers when combined with `AuthPolicy`
- Integrating seamlessly with authentication policies

## Prerequisites

- Kubernetes cluster with Kuadrant operator installed. See our [development](doc/overviews/development.md) guide for more information.
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) command line tool.
- Basic understanding of [Gateway API](https://gateway-api.sigs.k8s.io/).

You should also have an instance of `Kuadrant` installed:

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: kuadrant-system
EOF
```

## Environment variables

Set the following environment variables used throughout this tutorial:

```bash
export KUADRANT_GATEWAY_NS=gateway-system
export KUADRANT_GATEWAY_NAME=trlp-tutorial-gateway
export KUADRANT_SYSTEM_NS=$(kubectl get kuadrant -A -o jsonpath='{.items[0].metadata.namespace}')
```

## Step 1: Deploy an LLM service

Deploy a simulated LLM service that mimics OpenAI-compatible APIs:

```bash
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: trlp-tutorial-llm-sim
  labels:
    app: trlp-tutorial-llm-sim
spec:
  replicas: 1
  selector:
    matchLabels:
      app: trlp-tutorial-llm-sim
  template:
    metadata:
      labels:
        app: trlp-tutorial-llm-sim
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
  name: trlp-tutorial-llm-sim
spec:
  selector:
    app: trlp-tutorial-llm-sim
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

```bash
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
    hostname: "trlp-tutorial.example.com"
    allowedRoutes:
      namespaces:
        from: All
EOF
```

Check the gateway status:

```bash
kubectl get gateway ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}{"\n"}{.status.conditions[?(@.type=="Programmed")].message}{"\n"}'
```

## Step 3: Expose the service via HTTPRoute

Create an HTTPRoute to expose the LLM service:

```bash
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: trlp-tutorial-llm-sim
spec:
  hostnames:
    - trlp-tutorial.example.com
  parentRefs:
    - name: ${KUADRANT_GATEWAY_NAME}
      namespace: ${KUADRANT_GATEWAY_NS}
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: "/"
      backendRefs:
        - name: trlp-tutorial-llm-sim
          port: 80
EOF
```

Export the gateway URL for use in requests:

```bash
export KUADRANT_INGRESS_HOST=$(kubectl get gtw ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o jsonpath='{.status.addresses[0].value}')
export KUADRANT_INGRESS_PORT=$(kubectl get gtw ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o jsonpath='{.spec.listeners[?(@.name=="http")].port}')
export KUADRANT_GATEWAY_URL=${KUADRANT_INGRESS_HOST}:${KUADRANT_INGRESS_PORT}
```

Test connectivity to the service:

```bash
curl -H 'Host: trlp-tutorial.example.com' http://$KUADRANT_GATEWAY_URL/v1/models -i
# HTTP/1.1 200 OK
```

> **Note**: If the command above fails to hit the service on your environment, try forwarding requests to the gateway and accessing over localhost:
>
> ```bash
> kubectl port-forward -n ${KUADRANT_GATEWAY_NS} service/${KUADRANT_GATEWAY_NAME}-istio 9080:80 >/dev/null 2>&1 &
> export KUADRANT_GATEWAY_URL=localhost:9080
> ```

## Step 4: Set up API key authentication

Create API keys for different user tiers. This example creates two tiers: "free" and "gold":

```bash
# Create a free tier user
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: trlp-tutorial-api-key-free-user-1
  namespace: ${KUADRANT_SYSTEM_NS}
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: my-llm
  annotations:
    kuadrant.io/groups: free
    secret.kuadrant.io/user-id: user-1
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
  name: trlp-tutorial-api-key-gold-user-1
  namespace: ${KUADRANT_SYSTEM_NS}
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: my-llm
  annotations:
    kuadrant.io/groups: gold
    secret.kuadrant.io/user-id: user-2
stringData:
  api_key: iamagolduser
type: Opaque
EOF
```

## Step 5: Configure authentication policy

Create an AuthPolicy that validates API keys and extracts user information:

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: trlp-tutorial-llm-api-keys
  namespace: ${KUADRANT_GATEWAY_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ${KUADRANT_GATEWAY_NAME}
  rules:
    authentication:
      api-key-users:
        apiKey:
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
                  selector: auth.identity.metadata.annotations.secret\.kuadrant\.io/user-id
    authorization:
      allow-groups:
        opa:
          rego: |
            groups := split(object.get(input.auth.identity.metadata.annotations, "kuadrant.io/groups", ""), ",")
            allow { groups[_] == "free" }
            allow { groups[_] == "gold" }
EOF
```

## Step 6: Apply token rate limiting

Create a `TokenRateLimitPolicy` with different token limits for each tier:

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: trlp-tutorial-token-limits
  namespace: ${KUADRANT_GATEWAY_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ${KUADRANT_GATEWAY_NAME}
  limits:
    free:
      rates:
        - limit: 50 # 50 tokens per minute for free users (small for testing)
          window: 1m
      when:
        - predicate: request.path == "/v1/chat/completions"
        - predicate: |
            auth.identity.groups.split(",").exists(g, g == "free")
      counters:
        - expression: auth.identity.userid
    gold:
      rates:
        - limit: 200 # 200 tokens per minute for gold users (small for testing)
          window: 1m
      when:
        - predicate: request.path == "/v1/chat/completions"
        - predicate: |
            auth.identity.groups.split(",").exists(g, g == "gold")
      counters:
        - expression: auth.identity.userid
EOF
```

## Step 7: Test the configuration

### Test with a free user

Make a chat completion request. Note that `stream: false` is explicitly set to ensure a non-streaming response:

```bash
curl -H 'Host: trlp-tutorial.example.com' \
     -H 'Authorization: APIKEY iamafreeuser' \
     -H 'Content-Type: application/json' \
     -X POST http://$KUADRANT_GATEWAY_URL/v1/chat/completions \
     -d '{
           "model": "meta-llama/Llama-3.1-8B-Instruct",
           "messages": [
             { "role": "user", "content": "What is Kubernetes?" }
           ],
           "max_tokens": 100,
           "stream": false,
           "usage": true
         }'
```

The response includes token usage information:

```json
{
  "choices": [...],
  "usage": {
    "prompt_tokens": 5,
    "completion_tokens": 95,
    "total_tokens": 100
  }
}
```

> **Note:** The `TokenRateLimitPolicy` automatically extracts the `total_tokens` value from this response and counts it against the user's limit.

### Test with a gold user

```bash
curl -H 'Host: trlp-tutorial.example.com' \
     -H 'Authorization: APIKEY iamagolduser' \
     -H 'Content-Type: application/json' \
     -X POST http://$KUADRANT_GATEWAY_URL/v1/chat/completions \
     -d '{
           "model": "meta-llama/Llama-3.1-8B-Instruct",
           "messages": [
             { "role": "user", "content": "Explain cloud native architecture" }
           ],
           "max_tokens": 200,
           "stream": false,
           "usage": true
         }'
```

## How it works

1. **Authentication**: The AuthPolicy validates API keys and enriches requests with user metadata
2. **Token Extraction**: `TokenRateLimitPolicy` automatically extracts `usage.total_tokens` from LLM responses
3. **Rate Limiting**: Tokens are counted against user-specific limits based on their tier
4. **Enforcement**: When limits are exceeded, requests are rejected with HTTP 429 (Too Many Requests)

## Understanding the policy

The `TokenRateLimitPolicy` uses several key concepts:

- **`rates`**: Define the token limits and time windows
- **`when`**: Conditions that determine when a limit applies (based on user groups)
- **`counters`**: Identify what to count (user ID in this case)
- **Token extraction**: Automatically reads `usage.total_tokens` from non-streaming JSON responses

## Monitoring usage

Check the generated rate limit configuration:

```bash
# View the generated WasmPlugin configuration
kubectl get wasmplugin -n ${KUADRANT_GATEWAY_NS} kuadrant-${KUADRANT_GATEWAY_NAME} -o yaml
```

## Next steps

- Experiment with different token limits and time windows
- Add more user tiers with different limits
- Integrate with your actual LLM service

## Troubleshooting

If requests are being rejected unexpectedly:
1. Verify the API key is correct
2. Check if the user has exceeded their token limit
3. Ensure the LLM response includes `usage.total_tokens`
4. Verify the request is not using streaming (`stream: true`) as this is not currently supported
5. Review the `AuthPolicy` and `TokenRateLimitPolicy` status

```bash
# Check AuthPolicy status
kubectl get authpolicy -n ${KUADRANT_GATEWAY_NS} trlp-tutorial-llm-api-keys -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'

# Check TokenRateLimitPolicy status
kubectl get tokenratelimitpolicy -n ${KUADRANT_GATEWAY_NS} trlp-tutorial-token-limits -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'

# View full policy details if needed
kubectl get authpolicy -n ${KUADRANT_GATEWAY_NS} trlp-tutorial-llm-api-keys -o yaml
kubectl get tokenratelimitpolicy -n ${KUADRANT_GATEWAY_NS} trlp-tutorial-token-limits -o yaml
```

## Cleanup

To remove all resources created in this tutorial:

```bash
# Delete policies
kubectl delete tokenratelimitpolicy -n ${KUADRANT_GATEWAY_NS} trlp-tutorial-token-limits
kubectl delete authpolicy -n ${KUADRANT_GATEWAY_NS} trlp-tutorial-llm-api-keys

# Delete API key secrets
kubectl delete secret -n ${KUADRANT_SYSTEM_NS} trlp-tutorial-api-key-free-user-1 trlp-tutorial-api-key-gold-user-1

# Delete HTTPRoute
kubectl delete httproute trlp-tutorial-llm-sim

# Delete Gateway
kubectl delete gateway -n ${KUADRANT_GATEWAY_NS} ${KUADRANT_GATEWAY_NAME}

# Delete LLM service and deployment
kubectl delete service trlp-tutorial-llm-sim
kubectl delete deployment trlp-tutorial-llm-sim

# Delete the gateway namespace (if not used by other resources)
# kubectl delete namespace ${KUADRANT_GATEWAY_NS}
```
