# TokenRateLimitPolicy

## Overview

TokenRateLimitPolicy enables token-based rate limiting for service workloads in a Gateway API network. This policy allows you to create rate limits that are based on token usage metrics from AI/LLM workloads, authentication tokens, and claims. It automatically tracks token consumption from response bodies for accurate usage-based rate limiting.

## Key Features

- **Token-based Rate Limiting**: Create rate limits based on actual token usage from AI/LLM responses
- **Automatic Token Tracking**: Automatically tracks `usage.total_tokens` from response bodies
- **Multiple Named Limits**: Support for multiple named rate limits within a single policy
- **CEL Expression Support**: Use CEL expressions for flexible predicate and counter definitions
- **Gateway API Integration**: Targets Gateway and HTTPRoute resources
- **Multiple Time Windows**: Support for various time windows (seconds, minutes, hours, days)

## API Reference

### TokenRateLimitPolicySpec

| Field | Type | Description |
|-------|------|-------------|
| `targetRef` | `LocalPolicyTargetReferenceWithSectionName` | Reference to the Gateway or HTTPRoute to which this policy applies |
| `limits` | `map[string]TokenLimit` | Map of named token-based rate limit configurations |

### TokenLimit

| Field | Type | Description |
|-------|------|-------------|
| `rates` | `[]TokenRate` | List of rate limit details including limit and window |
| `when` | `WhenPredicates` | List of additional predicates for this limit |
| `predicate` | `string` | CEL expression that determines if this limit applies to the request |
| `counter` | `string` | CEL expression that defines the counter key for rate limiting |

### TokenRate

| Field | Type | Description |
|-------|------|-------------|
| `limit` | `int` | Maximum number of tokens allowed in the specified window |
| `window` | `string` | Time window using Gateway API Duration format (e.g., "1h", "30m", "1d") |

## Examples

### AI/LLM Token-based Rate Limiting

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: token-limit-free
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: my-llm-gateway
  limits:
    free:
      rates:
      - limit: 20000
        window: 1d
      predicate: 'request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")'
      counter: auth.identity.userid
    gold:
      rates:
      - limit: 200000
        window: 1d
      predicate: 'request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "gold")'
      counter: auth.identity.userid
```

This example:
- Targets a Gateway named `my-llm-gateway`
- Creates two named limits: "free" (20,000 tokens/day) and "gold" (200,000 tokens/day)
- Automatically tracks actual token usage from AI/LLM response bodies
- Uses JWT claims to determine user tier
- Uses the user ID as the counter key for tracking usage per user

### HTTPRoute-specific Rate Limiting

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: api-premium-users
  namespace: api-namespace
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: api-route
  limits:
    premium:
      rates:
      - limit: 1000000
        window: 1h
      predicate: 'request.auth.claims["subscription"] == "premium"'
      counter: request.auth.claims.sub
    basic:
      rates:
      - limit: 100000
        window: 1h
      predicate: 'request.auth.claims["subscription"] == "basic"'
      counter: request.auth.claims.sub
```

### Multiple Time Windows

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: tiered-limits
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ai-gateway
  limits:
    burst-protection:
      rates:
      - limit: 1000     # 1k tokens per minute (burst protection)
        window: 1m
      - limit: 50000    # 50k tokens per hour (sustained usage)
        window: 1h
      - limit: 500000   # 500k tokens per day (daily quota)
        window: 1d
      counter: auth.identity.userid
```

## Token Usage Tracking

TokenRateLimitPolicy automatically tracks token consumption from AI/LLM responses by monitoring the `usage.total_tokens` field in response bodies. This enables accurate usage-based rate limiting where:

- **Request Phase**: The policy evaluates predicates and descriptors during the request
- **Response Phase**: The policy extracts actual token usage from the response body
- **Rate Limiting**: Limitador receives the actual token count as `hits_addend` for precise accounting

### Supported Response Formats

The policy automatically parses token usage from response bodies in the following format:
```json
{
  "usage": {
    "total_tokens": 150,
    "prompt_tokens": 100,
    "completion_tokens": 50
  }
}
```

This is compatible with OpenAI-style API responses and similar AI/LLM services.

## CEL Expressions

TokenRateLimitPolicy supports CEL (Common Expression Language) for both predicates and counters:

### Common Predicate Examples

```cel
# Check user group membership
request.auth.claims["groups"].split(",").exists(g, g == "admin")

# Check subscription tier
request.auth.claims["subscription"] == "premium"

# Check request path
request.url_path.startsWith("/api/v1/")

# Combined conditions
request.auth.claims["tier"] == "gold" && request.method == "POST"

# Model-specific limiting (for request body inspection)
requestBodyJSON("model") == "gpt-4"
```

### Common Counter Examples

```cel
# Use user ID
auth.identity.userid

# Use JWT subject claim
request.auth.claims.sub

# Use organization ID
request.auth.claims["org_id"]

# Composite key
request.auth.claims["org_id"] + ":" + request.auth.claims.sub
```

## Status Conditions

TokenRateLimitPolicy reports status through standard Gateway API conditions:

- **Accepted**: Indicates whether the policy has been accepted by the controller
- **Enforced**: Indicates whether the policy is being actively enforced

## Limitations

- Currently supports Gateway and HTTPRoute targets only
- Requires authentication to be configured for token-based counter extraction
- CEL expressions must be valid and compile successfully
- Token usage tracking requires response bodies in OpenAI-compatible format with `usage.total_tokens` field
- Model detection (via `requestBodyJSON("model")`) requires request body inspection capabilities
- Only one TokenRateLimitPolicy per target resource is supported

## See Also

- [RateLimitPolicy](ratelimitpolicy.md) - For non-token-based rate limiting
- [AuthPolicy](authpolicy.md) - For authentication configuration
- [Gateway API Documentation](https://gateway-api.sigs.k8s.io/)
