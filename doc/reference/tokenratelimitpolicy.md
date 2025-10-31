# The TokenRateLimitPolicy Custom Resource Definition (CRD)

## TokenRateLimitPolicy

| **Field** | **Type**                                        | **Required** | **Description**                                       |
|-----------|-------------------------------------------------|:------------:|-------------------------------------------------------|
| `spec`    | [TokenRateLimitPolicySpec](#tokenratelimitpolicyspec)     |     Yes      | The specification for TokenRateLimitPolicy custom resource |
| `status`  | [TokenRateLimitPolicyStatus](#tokenratelimitpolicystatus) |      No      | The status for the custom resource                    |

## TokenRateLimitPolicySpec

| **Field**   | **Type**                                                                                                                                    | **Required** | **Description**                                                                                                                                                                             |
|-------------|---------------------------------------------------------------------------------------------------------------------------------------------|--------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `targetRef` | [LocalPolicyTargetReferenceWithSectionName](#localpolicytargetreferencewithsectionname) | Yes          | Reference to a Kubernetes resource that the policy attaches to. For more [info](https://gateway-api.sigs.k8s.io/reference/spec/#localpolicytargetreferencewithsectionname)                                                                                                                              |
| `defaults`  | [MergeableTokenRateLimitPolicySpec](#mergeabletokenratelimitpolicyspec)                                                                                     | No           | Default limit definitions. This field is mutually exclusive with the `limits` field                                                                                                         |
| `overrides` | [MergeableTokenRateLimitPolicySpec](#mergeabletokenratelimitpolicyspec)                                                                                     | No           | Overrides limit definitions. This field is mutually exclusive with the `limits` field and `defaults` field. This field is only allowed for policies targeting `Gateway` in `targetRef.kind` |
| `limits`    | Map<String: [TokenLimit](#tokenlimit)>                                                                                                                | No           | Limit definitions. This field is mutually exclusive with the [`defaults`](#mergeabletokenratelimitpolicyspec) field                                                                                 |

### LocalPolicyTargetReferenceWithSectionName
| **Field**       | **Type**                                | **Required** | **Description**                                            |
|------------------|-----------------------------------------|--------------|------------------------------------------------------------|
| `LocalPolicyTargetReference`         | [LocalPolicyTargetReference](#localpolicytargetreference)          | Yes          | Reference to a local policy target.               |
| `sectionName`    | [SectionName](#sectionname)                         | No           | Section name for further specificity (if needed). |

### LocalPolicyTargetReference
| **Field** | **Type**     | **Required** | **Description**                |
|-----------|--------------|--------------|--------------------------------|
| `group`   | `Group`      | Yes          | Group of the target resource. |
| `kind`    | `Kind`       | Yes          | Kind of the target resource.  |
| `name`    | `ObjectName` | Yes          | Name of the target resource.  |

### SectionName
| Field       | Type                     | Required | Description                                                                                                                                                                                                                         |
|-------------|--------------------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| SectionName | v1.SectionName (String)  | Yes      | SectionName is the name of a section in a Kubernetes resource. <br>In the following resources, SectionName is interpreted as the following: <br>* Gateway: Listener name<br>* HTTPRoute: HTTPRouteRule name<br>* Service: Port name |

### MergeableTokenRateLimitPolicySpec

| **Field** | **Type**                     | **Required** | **Description**                                                                                                              |
|-----------|------------------------------|--------------|------------------------------------------------------------------------------------------------------------------------------|
| `strategy`| String                       | No           | Merge strategy to apply when merging with other policies. Values: `atomic` (default), `merge`                               |
| `limits`  | Map<String: [TokenLimit](#tokenlimit)> | Yes           | Map of named token-based rate limit configurations                                                                   |

### TokenLimit

| **Field** | **Type**                     | **Required** | **Description**                                                                                                              |
|-----------|------------------------------|--------------|------------------------------------------------------------------------------------------------------------------------------|
| `rates`   | [][Rate](#rate)              | No           | List of rate limit details including limit and window. If not specified, no rate limits are applied for this limit definition |
| `when`    | [][WhenPredicate](#whenpredicate)    | No           | List of predicates for this limit. Used in combination with top-level predicates                                     |
| `counters`| [][Counter](#counter)        | No           | CEL expressions that define counter keys for rate limiting. If not specified, rate limiting will be applied globally without user-specific tracking |

### Rate

| **Field** | **Type** | **Required** | **Description**                                                |
|-----------|----------|--------------|----------------------------------------------------------------|
| `limit`   | Number   | Yes          | Maximum token count allowed for the given window               |
| `window`  | Duration | Yes          | Time window for the limit (e.g., "1h", "24h", "1m", "1d")    |

### WhenPredicate

| **Field**   | **Type** | **Required** | **Description**                                                                                          |
|-------------|----------|--------------|----------------------------------------------------------------------------------------------------------|
| `predicate` | String   | Yes          | CEL expression that must evaluate to true for the limit to apply. See [Well-known Attributes](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md) |

### Counter

| **Field**    | **Type** | **Required** | **Description**                                                                                         |
|--------------|----------|--------------|--------------------------------------------------------------------------------------------------------|
| `expression` | String   | Yes          | CEL expression that defines the counter key for rate limiting. See [Well-known Attributes](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md) |

## TokenRateLimitPolicyStatus

The status object for TokenRateLimitPolicy follows the [PolicyStatus](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1alpha2.PolicyStatus) pattern from Gateway API.

| **Field**      | **Type**                              | **Description**                                           |
|----------------|---------------------------------------|-----------------------------------------------------------|
| `observedGeneration` | Number                          | Generation of the resource that was last reconciled      |
| `conditions`   | [][Condition](#condition)             | Current state of the policy                              |

### Condition

Standard Kubernetes condition fields following Gateway API conventions:

| **Field**         | **Type**      | **Description**                                                    |
|-------------------|---------------|---------------------------------------------------------------------|
| `type`            | String        | Type of condition (e.g., "Accepted", "Enforced")                  |
| `status`          | String        | Status of the condition ("True", "False", "Unknown")              |
| `observedGeneration` | Number     | Generation observed when this condition was last updated           |
| `lastTransitionTime` | Timestamp  | Last time the condition transitioned from one status to another    |
| `reason`          | String        | Machine-readable reason for the condition's last transition        |
| `message`         | String        | Human-readable message indicating details about the last transition |

## Token Usage Tracking

TokenRateLimitPolicy automatically tracks token consumption from AI/LLM responses by monitoring the `usage.total_tokens` field in response bodies. This enables accurate usage-based rate limiting where:

- **Request Phase**: The policy evaluates predicates and descriptors during the request
- **Response Phase**: The policy extracts actual token usage from the response body
- **Rate Limiting**: Limitador receives the actual token count as `hits_addend` for precise accounting

### Supported Response Format

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

**Streaming Support**: Both streaming and non-streaming responses are supported:
- **Non-streaming**: Works with `stream: false` or when `stream` is omitted
- **Streaming**: Requires `"stream": true` and `"stream_options": { "include_usage": true }` to extract usage from the final stream event

## CEL Expression Context

TokenRateLimitPolicy provides access to request attributes through CEL expressions. For a comprehensive list of available attributes, see the [Well-known Attributes RFC](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md).

Common attributes include:

| Context | Available Attributes | Example Usage |
|---------|---------------------|---------------|
| **Request** | `request.method`, `request.url_path`, `request.headers` | `request.method == "POST"` |
| **Authentication** | `auth.identity.*`, `request.auth.claims.*` | `auth.identity.userid`, `request.auth.claims["tier"]` |
| **Remote Address** | `source.address`, `source.port` | `source.address` |

## Examples

### Basic Token Rate Limiting

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: basic-token-limit
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

### User-Based Token Limiting

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: user-token-limits
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: api-gateway
  limits:
    free:
      rates:
      - limit: 50000
        window: 24h
      when:
      - predicate: request.path == "/v1/chat/completions"
      - predicate: 'auth.identity.groups.split(",").exists(g, g == "free")'
      counters:
      - expression: auth.identity.userid
    gold:
      rates:
      - limit: 200000
        window: 24h
      when:
      - predicate: request.path == "/v1/chat/completions"
      - predicate: 'auth.identity.groups.split(",").exists(g, g == "gold")'
      counters:
      - expression: auth.identity.userid
```

### Gateway Overrides

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: org-wide-limits
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: api-gateway
  overrides:
    strategy: atomic
    limits:
      org-quota:
        rates:
        - limit: 1000000
          window: 24h
        counters:
        - expression: auth.identity.org_id
```

## See Also

- [TokenRateLimitPolicy Overview](../overviews/token-rate-limiting.md)
- [Token Rate Limiting Tutorial](../user-guides/tokenratelimitpolicy/authenticated-token-ratelimiting-tutorial.md)
- [RateLimitPolicy Reference](ratelimitpolicy.md)
- [AuthPolicy Reference](authpolicy.md)
- [Well-known Attributes](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md)
- [Gateway API Documentation](https://gateway-api.sigs.k8s.io/)
