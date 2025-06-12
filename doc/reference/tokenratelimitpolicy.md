# TokenRateLimitPolicy

## Overview

TokenRateLimitPolicy enables token-based rate limiting for service workloads in a Gateway API network. This policy creates rate limits based on actual token usage metrics from AI/LLM workloads rather than simple request counts. It automatically tracks token consumption from response bodies for accurate usage-based rate limiting.

**Note**: While this policy uses "token" in the name referring to AI/LLM usage tokens, it can also utilise authentication tokens and claims for counter definitions and predicates.

## Key Features

- **Token-based Rate Limiting**: Create rate limits based on actual token usage from AI/LLM responses
- **Automatic Token Tracking**: Automatically tracks `usage.total_tokens` from response bodies
- **Model-specific Rate Limiting**: Support for rate limiting specific AI models through automatic model detection
- **Multiple Named Limits**: Support for multiple named rate limits within a single policy
- **CEL Expression Support**: Use CEL expressions for flexible predicate and counter definitions
- **Gateway API Integration**: Targets Gateway and HTTPRoute resources
- **Multiple Time Windows**: Support for various time windows (seconds, minutes, hours, days)
- **Policy Hierarchy**: Support for defaults, overrides, and implicit defaults with proper precedence
- **Comprehensive Validation**: Built-in CEL validation ensures proper policy configuration

## API Reference

**Note**: This API reference reflects the current stable v1alpha1 specification.

### TokenRateLimitPolicySpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `targetRef` | `LocalPolicyTargetReferenceWithSectionName` | **Required** | Reference to the Gateway or HTTPRoute to which this policy applies |
| `defaults` | `MergeableTokenRateLimitPolicySpec` | Optional | Default limit definitions. Mutually exclusive with `limits` field |
| `overrides` | `MergeableTokenRateLimitPolicySpec` | Optional | Override limit definitions. Mutually exclusive with `limits` and `defaults` fields. Only allowed for Gateway targets |
| `limits` | `map[string]TokenLimit` | Optional | Implicit default limit definitions. Mutually exclusive with `defaults` field. At least one limit must be defined when not using `defaults` or `overrides` |

### MergeableTokenRateLimitPolicySpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `strategy` | `string` | Optional | Merge strategy to apply when merging with other policies. Values: `atomic` (default), `merge` |
| `limits` | `map[string]TokenLimit` | **Required** | Map of named token-based rate limit configurations |

### TokenLimit

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `rates` | `[]kuadrantv1.Rate` | Optional | List of rate limit details including limit and window. If not specified, no rate limits are applied for this limit definition |
| `when` | `kuadrantv1.WhenPredicates` | Optional | List of predicates for this limit. Used in combination with top-level predicates |
| `counters` | `[]kuadrantv1.Counter` | Optional | CEL expressions that define counter keys for rate limiting. If not specified, rate limiting will be applied globally without user-specific tracking |


## Default Behaviour

TokenRateLimitPolicy has several automatic behaviours and defaults:

### Automatic Token Tracking
- **Automatic**: Token usage is automatically tracked from response bodies containing `usage.total_tokens` field
- **No configuration required**: Works out-of-the-box with OpenAI-compatible API responses

### Counter Defaults
- **When `counter` is specified**: Rate limiting tracks usage per the specified counter (e.g., per user, per organisation)
- **When `counter` is omitted**: Rate limiting applies globally to all requests matching the predicate

### Predicate Defaults  
- **When `predicate` is specified**: Limit only applies to requests matching the CEL expression
- **When `predicate` is omitted**: Limit applies to all requests (subject to top-level `when` conditions)

### Model Detection (Advanced Feature)
The policy can automatically detect and track AI model usage:
- **Trigger**: When any predicate contains `requestBodyJSON("model")` 
- **Behaviour**: Model information is automatically extracted from request bodies and tracked in rate limiting descriptors
- **Use case**: Enables model-specific rate limiting (e.g., different limits for GPT-4 vs GPT-3.5)
- **Transparency**: This happens automatically without additional user configuration

## Policy Hierarchy and Precedence

TokenRateLimitPolicy supports three modes of operation that provide different levels of precedence in multi-policy scenarios:

### Implicit Defaults (using `limits`)
When a policy specifies `limits` directly at the spec level, these act as **implicit defaults**:
- Applied to the target resource (Gateway or HTTPRoute) 
- When targeting a Gateway: Can be overridden by more specific policies targeting individual routes
- When targeting an HTTPRoute: Applies directly to that route
- Most common usage pattern for single-policy scenarios

### Explicit Defaults (using `defaults`) 
When a policy uses the `defaults` field:
- Applied as default rules for routes that lack more specific policies
- Useful for Gateway-level policies that provide baseline limits  
- Can be overridden by HTTPRoute-level policies or Gateway overrides
- Same behaviour as implicit defaults, but with explicit merge strategy control
- Mutually exclusive with `limits` and `overrides`

### Overrides (using `overrides`)
When a policy uses the `overrides` field:
- **Takes precedence over**: 
  - All other "default" policies in the hierarchy
  - Other override policies downwards in the hierarchy
- Cannot be overridden by more specific policies
- Only allowed for Gateway-targeted policies
- Useful for enforcing organisation-wide limits that cannot be bypassed
- Mutually exclusive with `limits` and `defaults`

### Merge Strategies
Both `defaults` and `overrides` support merge strategies:
- **`atomic`** (default): Replace the entire policy configuration
- **`merge`**: Merge individual limits with existing policies

## Examples

### Minimal Configuration

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: simple-token-limit
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ai-gateway
  limits:
    global:
      rates:
      - limit: 100000
        window: 1h
```

This minimal example:
- Applies a 100,000 token limit per hour
- Tracks tokens automatically from `usage.total_tokens` in response bodies
- Applies globally to all requests (no user-specific tracking)
- No predicate conditions (applies to all requests)

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
      when:
      - predicate: 'request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")'
      counters:
      - expression: auth.identity.userid
    gold:
      rates:
      - limit: 200000
        window: 1d
      when:
      - predicate: 'request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "gold")'
      counters:
      - expression: auth.identity.userid
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
      when:
      - predicate: 'request.auth.claims["subscription"] == "premium"'
      counters:
      - expression: request.auth.claims.sub
    basic:
      rates:
      - limit: 100000
        window: 1h
      when:
      - predicate: 'request.auth.claims["subscription"] == "basic"'
      counters:
      - expression: request.auth.claims.sub
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
      counters:
      - expression: auth.identity.userid
```

### Gateway Defaults (Baseline Limits)

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: gateway-defaults
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: api-gateway
  defaults:
    strategy: merge
    limits:
      baseline:
        rates:
        - limit: 10000      # 10k tokens per hour baseline
          window: 1h
        counters:
        - expression: auth.identity.userid
      unauthenticated:
        rates:
        - limit: 1000       # 1k tokens per hour for unauthenticated
          window: 1h
        when:
        - predicate: '!has(request.auth.claims)'
```

This Gateway-level policy provides default limits that apply to all routes attached to the gateway, unless overridden by more specific HTTPRoute-level policies.

### Gateway Overrides (Enforced Limits)

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: organisation-overrides
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: api-gateway
  overrides:
    strategy: atomic
    limits:
      org-wide-limit:
        rates:
        - limit: 1000000    # 1M tokens per day org-wide limit
          window: 1d
        counters:
        - expression: 'request.auth.claims["org_id"]'
      security-limit:
        rates:
        - limit: 100        # 100 tokens per minute security limit
          window: 1m
        when:
        - predicate: 'request.headers["x-suspicious"] != ""'
        counters:
        - expression: 'request.headers["x-client-ip"]'
```

This policy enforces organisation-wide limits that **cannot be overridden** by any HTTPRoute-level policies.

### Policy Hierarchy in Action

```yaml
# Gateway defaults
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: gateway-baseline
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ai-gateway
  defaults:
    limits:
      default-limit:
        rates:
        - limit: 50000      # 50k tokens/day default
          window: 1d
        counters:
        - expression: auth.identity.userid

---
# HTTPRoute specific policy (overrides the gateway default)
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: premium-api-limits
  namespace: api-namespace
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: premium-api-route
  limits:
    premium:
      rates:
      - limit: 500000      # 500k tokens/day for premium API
        window: 1d
      counters:
      - expression: auth.identity.userid
```

In this scenario:
- Routes without specific policies get the 50k tokens/day limit from the Gateway defaults
- The `premium-api-route` gets the 500k tokens/day limit from its specific policy

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

TokenRateLimitPolicy supports CEL (Common Expression Language) for both predicates and counters, providing powerful flexibility for complex rate limiting scenarios.

### CEL Context and Available Attributes

TokenRateLimitPolicy provides access to request attributes through CEL expressions. For a comprehensive list of well-known attributes, see the [Well-Known Attributes RFC](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md).

| Context | Available Attributes | Example Usage |
|---------|---------------------|---------------|
| **Request** | `request.method`, `request.url_path`, `request.headers` | `request.method == "POST"` |
| **Authentication** | `request.auth.claims.*`, `auth.identity.userid` | `request.auth.claims["sub"]` |
| **Request Body** | `requestBodyJSON(path)` | `requestBodyJSON("model")` |
| **Remote Address** | `source.address`, `source.port` | `source.address` |

### Predicate Examples

Predicates determine **when** a limit applies. Multiple predicates can be combined using logical operators.

#### Authentication-based Predicates

```cel
# Check if user is authenticated
has(request.auth.claims)

# Check user subscription tier
request.auth.claims["subscription"] == "premium"

# Check multiple subscription tiers
request.auth.claims["subscription"] in ["premium", "enterprise"]

# Check user group membership (comma-separated groups)
request.auth.claims["groups"].split(",").exists(g, g == "admin")

# Check organisation ID
request.auth.claims["org_id"] == "acme-corp"

# Combine user and organisation conditions
request.auth.claims["org_id"] == "acme-corp" && 
request.auth.claims["role"] == "developer"
```

#### Request-based Predicates

```cel
# HTTP method checks
request.method == "POST"
request.method in ["POST", "PUT", "PATCH"]

# Path-based limiting
request.url_path.startsWith("/api/v1/chat")
request.url_path.matches("/api/v1/models/.*")

# Header-based conditions
request.headers["content-type"].startsWith("application/json")
has(request.headers["x-api-version"])

# Query parameter checks
request.url_path.contains("?model=gpt-4")
```

#### AI/LLM-specific Predicates

```cel
# Model-specific limiting
requestBodyJSON("model") == "gpt-4"
requestBodyJSON("model").startsWith("gpt-4")
requestBodyJSON("model") in ["gpt-4", "gpt-4-turbo", "claude-3"]

# Request type limiting
requestBodyJSON("stream") == true
requestBodyJSON("temperature") > 0.8

# Message length limiting
size(requestBodyJSON("messages")) > 10
requestBodyJSON("max_tokens") > 1000

# Combined AI conditions
requestBodyJSON("model") == "gpt-4" && 
requestBodyJSON("max_tokens") > 500 &&
request.auth.claims["subscription"] != "premium"
```

#### Time and Rate-based Predicates

```cel
# Time-based limiting (requires external time context)
request.headers["x-time-of-day"] == "peak-hours"

# Source-based limiting
source.address.startsWith("192.168.")
!source.address.startsWith("10.0.0.")

# Combined complex conditions
(request.method == "POST" && requestBodyJSON("model") == "gpt-4") ||
(request.method == "GET" && request.url_path.contains("/expensive-endpoint"))
```

### Counter Examples

Counters define **what to count** - they create separate rate limit buckets for each unique counter value.

#### User-based Counters

```cel
# Individual user limiting
auth.identity.userid
request.auth.claims.sub
request.auth.claims["user_id"]

# User within organisation
request.auth.claims["org_id"] + ":" + request.auth.claims.sub

# User with role context
request.auth.claims.sub + ":" + request.auth.claims["role"]
```

#### Organisation-based Counters

```cel
# Organisation-wide limiting
request.auth.claims["org_id"]

# Organisation with subscription tier
request.auth.claims["org_id"] + ":" + request.auth.claims["subscription"]

# Team-based limiting
request.auth.claims["org_id"] + ":" + request.auth.claims["team"]
```

#### Resource-based Counters

```cel
# Per-model limiting
requestBodyJSON("model")

# Model per user
auth.identity.userid + ":" + requestBodyJSON("model")

# API endpoint per organisation
request.auth.claims["org_id"] + ":" + request.url_path

# Source IP limiting (for anonymous requests)
source.address

# Complex composite keys
request.auth.claims["org_id"] + ":" + 
requestBodyJSON("model") + ":" + 
string(requestBodyJSON("max_tokens") > 1000)
```

### CEL Validation Rules

TokenRateLimitPolicy enforces the following CEL validation rules:

- **Mutual exclusivity**: Only one of `limits`, `defaults`, or `overrides` can be specified
- **Required limits**: When using `defaults` or `overrides`, at least one limit must be defined
- **Target validation**: Only Gateway and HTTPRoute targets are supported
- **Override restrictions**: `overrides` can only be used with Gateway targets

### CEL Best Practices

1. **Performance**: Keep predicates simple and avoid complex nested operations
2. **Readability**: Use clear, descriptive expressions that explain the intent
3. **Testing**: Test CEL expressions with real request data before deploying
4. **Error handling**: Use `has()` functions to check for field existence before accessing
5. **Composite keys**: When using composite counter keys, ensure they produce reasonable cardinality

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
- Policy configuration modes (`limits`, `defaults`, `overrides`) are mutually exclusive
- `overrides` mode is only allowed for Gateway targets, not HTTPRoute targets
- At least one limit must be defined in the applicable policy section

## See Also

- [RateLimitPolicy](ratelimitpolicy.md) - For non-token-based rate limiting
- [AuthPolicy](authpolicy.md) - For authentication configuration
- [Gateway API Documentation](https://gateway-api.sigs.k8s.io/)
