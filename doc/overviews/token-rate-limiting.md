# Kuadrant Token Rate Limiting

A Kuadrant TokenRateLimitPolicy custom resource enables token-based rate limiting for AI/LLM workloads in a Gateway API network:

1. Targets Gateway API networking resources such as [HTTPRoutes](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPRoute) and [Gateways](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.Gateway)
2. Automatically tracks actual token consumption from OpenAI-compatible API responses
3. Supports user segmentation, model-specific limits, and sophisticated limiting strategies
4. Integrates with AuthPolicy for user-based rate limiting using authentication claims
5. Enables cluster operators to set defaults that govern behaviour at the lower levels of the network, until a more specific policy is applied

## How it works

### Token Tracking Protocol

Kuadrant's Token Rate Limit implementation extends the Envoy [Rate Limit Service (RLS)](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ratelimit/v3/rls.proto) protocol with automatic token usage extraction. The workflow per request goes:

1. On incoming request, the gateway evaluates matching rules and predicates from TokenRateLimitPolicy resources
2. If the request matches, the gateway prepares rate limit descriptors and monitors the response
3. After receiving the response, the gateway extracts `usage.total_tokens` from the response body
4. The gateway sends a [RateLimitRequest](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ratelimit/v3/rls.proto#service-ratelimit-v3-ratelimitrequest) to Limitador with the actual token count as `hits_addend`
5. Limitador tracks the cumulative token usage and responds with either `OK` or `OVER_LIMIT`

This approach ensures accurate usage-based rate limiting where limits are enforced based on actual AI/LLM token consumption rather than simple request counts.

**Important**: Currently, TokenRateLimitPolicy only supports non-streaming OpenAI-style API responses (where `stream: false` or is omitted in the request). Support for streaming responses is planned for future releases.

### The TokenRateLimitPolicy custom resource

#### Overview

The `TokenRateLimitPolicy` spec includes, basically, two parts:

- A reference to an existing Gateway API resource (`spec.targetRef`)
- Limit definitions (`spec.limits`)

Each limit definition includes:

- A set of rate limits (`spec.limits.<limit-name>.rates[]`)
- (Optional) A set of dynamic counter qualifiers (`spec.limits.<limit-name>.counters[]`)
- (Optional) A set of additional dynamic conditions to activate the limit (`spec.limits.<limit-name>.when[]`)

The limit definitions (`limits`) can be declared at the top-level level of the spec (with the semantics of _defaults_) or alternatively within explicit `defaults` or `overrides` blocks.

<table>
  <tbody>
    <tr>
      <td>Check out Kuadrant <a href="https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md">RFC 0002</a> to learn more about the Well-known Attributes that can be used to define counter qualifiers (<code>counters</code>) and conditions (<code>when</code>).</td>
    </tr>
  </tbody>
</table>

Check out the [API reference](../reference/tokenratelimitpolicy.md) for a full specification of the TokenRateLimitPolicy CRD.

## Key Features

### Automatic Token Tracking

TokenRateLimitPolicy automatically extracts token usage from AI/LLM responses without requiring any additional configuration:

- **Zero configuration**: Works out-of-the-box with OpenAI-compatible APIs
- **Response parsing**: Automatically extracts `usage.total_tokens` from response bodies
- **Accurate accounting**: Tracks actual token consumption, not estimates
- **Graceful fallback**: If token parsing fails, falls back to request counting

### Model-Specific Rate Limiting

Different AI models have different costs and capabilities. TokenRateLimitPolicy enables sophisticated model-based limiting:

```yaml
limits:
  gpt-4-limit:
    rates:
    - limit: 100000
      window: 24h
    when:
    - predicate: 'requestBodyJSON("model") == "gpt-4"'
    counters:
    - expression: auth.identity.userid
```

### User Segmentation

Create different limits for different user tiers, organisations, or teams:

```yaml
limits:
  free-tier:
    rates:
    - limit: 20000
      window: 24h
    when:
    - predicate: 'auth.identity.groups.split(",").exists(g, g == "free")'
    counters:
    - expression: auth.identity.userid
```

### Multiple Time Windows

Protect against both burst usage and sustained overconsumption:

```yaml
limits:
  burst-protection:
    rates:
    - limit: 1000     # 1k tokens per minute (burst protection)
      window: 1m
    - limit: 50000    # 50k tokens per hour (sustained usage)
      window: 1h
    - limit: 500000   # 500k tokens per day (daily quota)
      window: 1d
```

## Policy Hierarchy and Precedence

TokenRateLimitPolicy supports three modes of operation that provide different levels of precedence:

### Implicit Defaults (using `limits`)
When a policy specifies `limits` directly at the spec level, these act as **implicit defaults**:
- Applied to the target resource (`Gateway` or `HTTPRoute`) 
- When targeting a Gateway: Can be overridden by more specific policies targeting individual routes
- Most common usage pattern for single-policy scenarios

### Explicit Defaults (using `defaults`) 
When a policy uses the `defaults` field:
- Applied as default rules for routes that lack more specific policies
- Useful for Gateway-level policies that provide baseline limits  
- Can be overridden by HTTPRoute-level policies or Gateway overrides
- Supports merge strategies (`atomic` or `merge`)

### Overrides (using `overrides`)
When a policy uses the `overrides` field:
- Takes precedence over all other policies in the hierarchy
- Cannot be overridden by more specific policies
- Only allowed for Gateway-targeted policies
- Useful for enforcing organisation-wide limits that cannot be bypassed

## Common Use Cases

### AI/LLM API Protection

Protect your AI/LLM APIs from token exhaustion while ensuring fair usage across different user tiers:

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: llm-protection
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ai-gateway
  limits:
    free-users:
      rates:
      - limit: 10000    # 10k tokens per day for free tier
        window: 24h
      when:
      - predicate: 'auth.identity.subscription == "free"'
      counters:
      - expression: auth.identity.userid
    
    pro-users:
      rates:
      - limit: 100000   # 100k tokens per day for pro tier
        window: 24h
      when:
      - predicate: 'auth.identity.subscription == "pro"'
      counters:
      - expression: auth.identity.userid
```

### Multi-Model Management

Different models have different costs - set appropriate limits for each:

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: model-limits
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: chat-api
  limits:
    expensive-models:
      rates:
      - limit: 50000
        window: 24h
      when:
      - predicate: 'requestBodyJSON("model") in ["gpt-4", "claude-3-opus"]'
      counters:
      - expression: auth.identity.userid
    
    standard-models:
      rates:
      - limit: 200000
        window: 24h
      when:
      - predicate: 'requestBodyJSON("model") in ["gpt-3.5-turbo", "claude-3-haiku"]'
      counters:
      - expression: auth.identity.userid
```

### Organisation-Wide Quotas

Enforce organisation-level limits that cannot be bypassed by individual teams:

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: org-quotas
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: api-gateway
  overrides:
    limits:
      org-total:
        rates:
        - limit: 10000000  # 10M tokens per month org-wide
          window: 720h
        counters:
        - expression: auth.identity.org_id
```

## Integration with AuthPolicy

TokenRateLimitPolicy works seamlessly with Kuadrant's AuthPolicy to enable user-based rate limiting. When using API key authentication with groups, ensure the AuthPolicy exposes the necessary attributes:

```yaml
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: api-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: api-gateway
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
```

This configuration makes `auth.identity.groups` and `auth.identity.userid` available to TokenRateLimitPolicy for use in predicates and counters.

## See Also

- [TokenRateLimitPolicy API Reference](../reference/tokenratelimitpolicy.md)
- [Token Rate Limiting Tutorial](../user-guides/tokenratelimitpolicy/authenticated-token-ratelimiting-tutorial.md)
- [RateLimitPolicy Overview](rate-limiting.md) - For non-token-based rate limiting
- [AuthPolicy Overview](auth.md) - For authentication configuration
- [Gateway API Documentation](https://gateway-api.sigs.k8s.io/)
