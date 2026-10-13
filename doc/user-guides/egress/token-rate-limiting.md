# Token Rate Limiting on Egress

This guide walks through applying TokenRateLimitPolicy (TRLP) to an Istio egress gateway to control AI token consumption for outbound LLM API calls. It covers global token limits, per-workload budgets using workload identity, and per-tier quotas.

TokenRateLimitPolicy uses the same wasm-shim and Limitador infrastructure as RateLimitPolicy. The gateway extracts `usage.total_tokens` from AI API responses and counts them against configured limits. No code changes are needed for egress versus ingress.

## Prerequisites

- Kubernetes cluster with Kuadrant operator and Istio installed. See the [Getting Started](/latest/getting-started) guide.
- Egress gateway infrastructure deployed. Run the base setup first:

```sh
curl -sL https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/hack/setup-egress.sh | bash
```

See the [Egress Gateway Setup](egress-gateway.md) guide for details on what this deploys.

Then deploy the mock AI API and test workloads:

```sh
curl -sL https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/hack/setup-egress-ai-mock.sh | bash
```

This deploys:

| Resource | Value |
|----------|-------|
| Mock AI API | `llm-sim` in `ai-mock` namespace (no Istio sidecar) |
| External hostname | `api.ai-mock.local` |
| Gateway | `kuadrant-egressgateway` in `gateway-system` |
| Test clients | `test-client` (default SA), `team-gold` (team-gold SA) in `egress-test` |

Export the gateway address:

```sh
export EGRESS_IP=$(kubectl get gtw kuadrant-egressgateway -n gateway-system \
    -o jsonpath='{.status.addresses[0].value}')
```

Verify the mock AI API is reachable:

```sh
kubectl exec test-client -n egress-test -- \
    curl -s -H "Host: api.ai-mock.local" http://${EGRESS_IP}/v1/models
```

## Mock AI API

The setup deploys [llm-d-inference-sim](https://github.com/llm-d/llm-d-inference-sim), an OpenAI-compatible simulator, as a cluster-internal service outside the Istio mesh. The egress gateway reaches it via a ServiceEntry with static IP resolution. Responses include the standard `usage` block:

```json
{
  "choices": [{"message": {"content": "..."}, "finish_reason": "stop"}],
  "usage": {
    "prompt_tokens": 5,
    "completion_tokens": 95,
    "total_tokens": 100
  }
}
```

TokenRateLimitPolicy extracts `usage.total_tokens` from this response automatically. In production, replace the ServiceEntry with your actual AI provider endpoint (for example, `api.openai.com`) and add a DestinationRule for TLS origination.

## Basic Token Rate Limiting

Apply a global token limit on all egress traffic to the AI mock:

```sh
kubectl apply -f - <<'EOF'
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: ai-token-limit
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: ai-mock-external
  limits:
    global:
      rates:
        - limit: 100
          window: 1m
EOF
```

This limits total token consumption across all workloads to 100 tokens per minute.

Wait for the policy to be accepted and enforced:

```sh
kubectl wait --timeout=60s tokenratelimitpolicy/ai-token-limit -n gateway-system \
    --for=jsonpath='{.status.conditions[?(@.type=="Accepted")].status}'=True
kubectl wait --timeout=60s tokenratelimitpolicy/ai-token-limit -n gateway-system \
    --for=jsonpath='{.status.conditions[?(@.type=="Enforced")].status}'=True
```

### Test token counting

Send a chat completion request:

```sh
kubectl exec test-client -n egress-test -- \
    curl -s -H "Host: api.ai-mock.local" \
         -H "Content-Type: application/json" \
         -X POST http://${EGRESS_IP}/v1/chat/completions \
         -d '{
               "model": "meta-llama/Llama-3.1-8B-Instruct",
               "messages": [{"role": "user", "content": "What is Kubernetes?"}],
               "max_tokens": 100,
               "stream": false,
               "usage": true
             }'
```

The response includes `usage.total_tokens`. The simulator returns approximately 5-15 tokens per request. Each request consumes tokens from the 100/minute budget. After enough requests, the gateway returns HTTP 429:

```sh
# Send requests until rate limited (limit is 100 tokens/min, ~10 tokens/request)
for i in $(seq 1 20); do
  CODE=$(kubectl exec test-client -n egress-test -- \
      curl -s -o /dev/null -w "%{http_code}" \
           -H "Host: api.ai-mock.local" \
           -H "Content-Type: application/json" \
           -X POST http://${EGRESS_IP}/v1/chat/completions \
           -d '{
                 "model": "meta-llama/Llama-3.1-8B-Instruct",
                 "messages": [{"role": "user", "content": "Hello"}],
                 "max_tokens": 100,
                 "stream": false,
                 "usage": true
               }')
  echo "Request $i: HTTP $CODE"
  [ "$CODE" = "429" ] && break
done
```

After cumulative tokens exceed 100, subsequent requests are rejected with 429 until the window resets.

Clean up before the next section:

```sh
kubectl delete tokenratelimitpolicy ai-token-limit -n gateway-system
```

## Per-Workload Token Limiting

To give each workload its own token budget, combine TRLP with workload identity via AuthPolicy. This uses the same [kubernetesTokenReview](egress-gateway.md#workload-identity) pattern as RateLimitPolicy on egress.

### Step 1: Apply workload identity

```sh
kubectl apply -f - <<'EOF'
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: workload-identity
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: ai-mock-external
  rules:
    authentication:
      "workload-sa":
        kubernetesTokenReview:
          audiences:
            - "https://kubernetes.default.svc.cluster.local"
    authorization:
      "allowed-namespaces":
        patternMatching:
          patterns:
            - predicate: auth.identity.user.username.startsWith('system:serviceaccount:egress-test:')
    response:
      success:
        filters:
          identity:
            json:
              properties:
                username:
                  selector: auth.identity.user.username
EOF
```

The `response.success.filters.identity` block exposes the authenticated username to downstream policies. Without it, counter expressions referencing `auth.identity.username` cannot resolve in the wasm-shim.

Workloads must include their SA token in requests. Requests without a valid token are rejected (401). Workloads from unauthorized namespaces are rejected (403).

### Step 2: Apply per-workload token limits

```sh
kubectl apply -f - <<'EOF'
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: ai-per-workload
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: ai-mock-external
  limits:
    per-workload:
      rates:
        - limit: 100
          window: 1m
      counters:
        - expression: auth.identity.username
EOF
```

Each ServiceAccount now gets an independent 100 tokens/minute budget.

### Verify per-workload limits

Exhaust the test-client budget and confirm that team-gold is unaffected:

```sh
# Exhaust test-client (default SA) budget
for i in $(seq 1 20); do
  CODE=$(kubectl exec test-client -n egress-test -- sh -c '
  curl -s -o /dev/null -w "%{http_code}" \
      -H "Host: api.ai-mock.local" \
      -H "Content-Type: application/json" \
      -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
      -X POST http://'"${EGRESS_IP}"'/v1/chat/completions \
      -d "{
            \"model\": \"meta-llama/Llama-3.1-8B-Instruct\",
            \"messages\": [{\"role\": \"user\", \"content\": \"Hello\"}],
            \"max_tokens\": 100,
            \"stream\": false,
            \"usage\": true
          }"
  ')
  echo "test-client request $i: HTTP $CODE"
  [ "$CODE" = "429" ] && break
done

# team-gold (team-gold SA) — independent budget, still allowed
CODE=$(kubectl exec team-gold -n egress-test -- sh -c '
curl -s -o /dev/null -w "%{http_code}" \
    -H "Host: api.ai-mock.local" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
    -X POST http://'"${EGRESS_IP}"'/v1/chat/completions \
    -d "{
          \"model\": \"meta-llama/Llama-3.1-8B-Instruct\",
          \"messages\": [{\"role\": \"user\", \"content\": \"Hello\"}],
          \"max_tokens\": 100,
          \"stream\": false,
          \"usage\": true
        }"
')
echo "team-gold: HTTP $CODE"
```

Rate limiting one workload does not affect the other. Each SA token counter is tracked independently.

Clean up before the next section:

```sh
kubectl delete tokenratelimitpolicy ai-per-workload -n gateway-system
```

## Per-Tier Token Limiting

For differentiated quotas (for example, free versus gold tiers), use `when` predicates to match workload identity patterns alongside per-identity counters.

Apply a TRLP with tier-based limits:

```sh
kubectl apply -f - <<'EOF'
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: ai-per-tier
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: ai-mock-external
  limits:
    default-tier:
      rates:
        - limit: 100
          window: 1m
      when:
        - predicate: auth.identity.username == 'system:serviceaccount:egress-test:default'
      counters:
        - expression: auth.identity.username
    gold-tier:
      rates:
        - limit: 500
          window: 1m
      when:
        - predicate: auth.identity.username == 'system:serviceaccount:egress-test:team-gold'
      counters:
        - expression: auth.identity.username
EOF
```

- `test-client` (using the `default` SA) gets 100 tokens/minute
- `team-gold` (using the `team-gold` SA) gets 500 tokens/minute

### Verify tier-based limits

```sh
# Exhaust the default tier (100 tokens, ~10 tokens/request)
for i in $(seq 1 20); do
  CODE=$(kubectl exec test-client -n egress-test -- sh -c '
  curl -s -o /dev/null -w "%{http_code}" \
      -H "Host: api.ai-mock.local" \
      -H "Content-Type: application/json" \
      -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
      -X POST http://'"${EGRESS_IP}"'/v1/chat/completions \
      -d "{
            \"model\": \"meta-llama/Llama-3.1-8B-Instruct\",
            \"messages\": [{\"role\": \"user\", \"content\": \"Hello\"}],
            \"max_tokens\": 100,
            \"stream\": false,
            \"usage\": true
          }"
  ')
  echo "default-tier request $i: HTTP $CODE"
  [ "$CODE" = "429" ] && break
done

# Gold tier (500 tokens) still has budget
CODE=$(kubectl exec team-gold -n egress-test -- sh -c '
curl -s -o /dev/null -w "%{http_code}" \
    -H "Host: api.ai-mock.local" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
    -X POST http://'"${EGRESS_IP}"'/v1/chat/completions \
    -d "{
          \"model\": \"meta-llama/Llama-3.1-8B-Instruct\",
          \"messages\": [{\"role\": \"user\", \"content\": \"Hello\"}],
          \"max_tokens\": 100,
          \"stream\": false,
          \"usage\": true
        }"
')
echo "gold-tier request: HTTP $CODE"
```

The default tier hits its limit, but the gold tier still has remaining budget.

## Streaming Responses

TRLP supports streaming OpenAI-style responses. The request must include `"stream": true` and `"stream_options": { "include_usage": true }` for usage to be extracted from the final stream event:

```sh
kubectl exec test-client -n egress-test -- sh -c '
curl -s \
    -H "Host: api.ai-mock.local" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
    -X POST http://'"${EGRESS_IP}"'/v1/chat/completions \
    -d "{
          \"model\": \"meta-llama/Llama-3.1-8B-Instruct\",
          \"messages\": [{\"role\": \"user\", \"content\": \"What is Kubernetes?\"}],
          \"max_tokens\": 100,
          \"stream\": true,
          \"stream_options\": {\"include_usage\": true}
        }"
'
```

The final SSE event contains the usage data. Because the counter is updated only after the full response completes, a streaming response delivers all its chunks before the token count is recorded. The updated limit applies to subsequent requests, not the stream already in progress.

If `stream_options.include_usage` is omitted when `stream: true`, token usage cannot be extracted. Depending on the wasm-shim failure mode, the request may be allowed without counting or rejected.

## Considerations

### TLS Origination and Response Body Access

When using a real external AI API (for example, OpenAI), the egress gateway terminates internal mTLS and originates a new TLS connection to the provider. The response body passes through the gateway unencrypted on the internal side, so token extraction from the response body works normally. The mock used in this guide skips TLS origination for simplicity.

### Concurrent Request Race Condition

The current TRLP implementation uses a two-phase protocol: the gateway checks limits before forwarding the request (without consuming tokens), then reports actual usage after receiving the response. Between these two phases, concurrent in-flight requests can race past the limit because nothing holds capacity during model processing. For most use cases this is acceptable because LLM responses are slow enough that burst patterns are uncommon. For strict enforcement under high concurrency, see [Token Limit Reservations](#token-limit-reservations-coming-soon) below.

### Supported Response Formats

Token extraction works with any back end returning an OpenAI-compatible response body with `usage.total_tokens`. This includes OpenAI, vLLM, kServe, Ollama, Azure OpenAI, and the Gemini OpenAI-compat endpoint. Anthropic and Gemini native formats are not yet supported. See [#1864](https://github.com/Kuadrant/kuadrant-operator/issues/1864) for tracking.

### Missing Token Usage in Responses

If the external API does not include `usage.total_tokens` in the response body, or if the field cannot be parsed, the token report phase fails silently and no tokens are counted. Because both the check and report services default to `failureMode: allow`, the request succeeds but the counter is never incremented. This means TRLP effectively becomes a no-op: no rate limiting is applied.

To reject requests when token extraction fails, set the `RATELIMIT_REPORT_SERVICE_FAILURE_MODE` environment variable to `deny` on the operator deployment. This causes the gateway to block any response where usage cannot be extracted. This is a blunt control that affects all TokenRateLimitPolicies in the cluster.

Verify that your AI provider returns `usage.total_tokens` in every response before relying on TRLP for enforcement. For streaming, verify that `stream_options.include_usage` is set to `true` in requests.

### Istio Only

Egress gateway support targets Istio as the Gateway API provider. Envoy Gateway is not supported for egress at this time.

## Token Limit Reservations (Coming Soon)

> This section describes a planned enhancement. The code does not exist yet. See [architecture#190](https://github.com/Kuadrant/architecture/pull/190) for the full RFC.

The current two-phase flow (check then report) has a known race condition: concurrent in-flight requests all pass the check phase before any of them report usage, allowing cumulative consumption to exceed the configured limit. Token limit reservations close this gap by holding estimated capacity at request time.

### How It Will Work

When a request arrives, the gateway will reserve an estimated token amount against the limit. If remaining capacity (accounting for all outstanding reservations) is insufficient, the request is rejected immediately. After the model responds, the actual usage is committed and the reservation is released.

```
Request arrives → Reserve(estimated amount, TTL) → Forward to model → Commit(actual usage) → Release reservation
```

If the model call fails or times out, the reservation expires on its own TTL. No cleanup call is needed.

### Policy Changes

A new optional `reservation` block on each limit will allow configuring the estimated amount and hold duration:

```yaml
# Not yet available — requires Limitador and operator support
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: ai-token-limit-with-reservations
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: ai-mock-external
  limits:
    per-workload:
      rates:
        - limit: 50000
          window: 24h
      counters:
        - expression: auth.identity.username
      reservation:
        amount: "uint(5000)"
        ttl: "duration('30s')"
```

- `reservation.amount`: CEL expression for estimated tokens to hold. Defaults to `uint(5000)` when omitted.
- `reservation.ttl`: how long to hold the reservation before auto-releasing. Defaults to the route's backend request timeout.

Policies that omit the `reservation` block automatically get safe defaults. No changes are required to existing TRLP resources.

### Cluster-Wide Mode Switch

The Kuadrant CR will gain a `tokenRateLimiting.mode` field to control the behavior cluster-wide:

```yaml
# Not yet available
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
spec:
  tokenRateLimiting:
    mode: Reservation   # default; set to CheckReport to revert to today's behavior
```

- `Reservation` (default when available): uses Reserve/Commit for all TokenRateLimitPolicies
- `CheckReport`: reverts to today's Check/Report behavior

## Cleanup

Remove all resources created by this guide:

```sh
# Remove policies
kubectl delete tokenratelimitpolicy -n gateway-system ai-token-limit ai-per-workload ai-per-tier --ignore-not-found
kubectl delete authpolicy workload-identity -n gateway-system --ignore-not-found

# Remove AI mock resources
curl -sL https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/hack/setup-egress-ai-mock.sh | bash -s cleanup

# Optionally remove the base egress gateway
curl -sL https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/hack/setup-egress.sh | bash -s cleanup
```

## References

- [RFC 0013: AI Policies](https://github.com/Kuadrant/architecture/blob/main/rfcs/0013-ai-policies.md)
- [RFC: Token Rate Limit Reservations](https://github.com/Kuadrant/architecture/pull/190) (draft)
- [TokenRateLimitPolicy Overview](../../overviews/token-rate-limiting.md)
- [Token Rate Limiting Tutorial](../tokenratelimitpolicy/authenticated-token-ratelimiting-tutorial.md)
- [Egress Gateway Setup](egress-gateway.md)
- [Credential Injection](credential-injection.md)
