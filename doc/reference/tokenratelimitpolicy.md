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
| `dataExtraction` | [DataExtraction](#dataextraction)                                                                                                                     | No           | Configures how token usage data is extracted from responses. If omitted, built-in defaults are used                                                                                        |

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
| `dataExtraction` | [DataExtraction](#dataextraction) | No    | Configures how token usage data is extracted from responses                                                                  |

### TokenLimit

| **Field** | **Type**                     | **Required** | **Description**                                                                                                              |
|-----------|------------------------------|--------------|------------------------------------------------------------------------------------------------------------------------------|
| `rates`   | [][Rate](#rate)              | No           | List of rate limit details including limit and window. If not specified, no rate limits are applied for this limit definition |
| `when`    | [][WhenPredicate](#whenpredicate)    | No           | List of predicates for this limit. Used in combination with top-level predicates                                     |
| `counters`| [][Counter](#counter)        | No           | CEL expressions that define counter keys for rate limiting. If not specified, rate limiting will be applied globally without user-specific tracking |
| `reservation`| [Reservation](#reservation) | No        | Tunes token reservation for this limit. Only takes effect when the Kuadrant CR `spec.tokenRateLimiting.mode` is `Reservation` (the default). Ignored in `Optimistic` mode. |

### Reservation

Configures how many tokens are reserved on request arrival and for how long, when the cluster is in `Reservation` mode (see the [Kuadrant CR `tokenRateLimiting`](kuadrant.md#tokenratelimiting) reference). Both fields are optional; when the whole `reservation` block is omitted, defaults are generated.

| **Field** | **Type** | **Required** | **Description**                                                                                                              |
|-----------|----------|:------------:|----------------------------------------------------------------------------------------------------------------------------|
| `amount`  | Integer or String | No | Either a literal integer number of tokens, or a CEL expression evaluating to the number of tokens (`uint`), to reserve on request arrival. **Defaults to `0` when omitted.** |
| `ttl`     | String   | No           | CEL expression evaluating to the maximum duration (`google.protobuf.Duration`) the reservation is held before it expires. When omitted, the value falls back to the route's `HTTPRoute.spec.rules[].timeouts.backendRequest`; if that is also unset, `ttl` is left unset and Limitador applies its own default. |

**`amount` defaults to `0`, which reserves no capacity at all.** `0` is a documented Limitador short-circuit: the Reserve/Commit calls still happen, but no capacity is held, so `Reservation` mode behaves identically to `Optimistic` mode for any limit that doesn't set `amount` explicitly — no protection against the concurrent-request race RFC [0021](https://github.com/Kuadrant/architecture/blob/main/rfcs/0021-token-rate-limit-reservations.md) exists to close. This is intentional: `Reservation` is the cluster-wide default mode, so a `0` default keeps upgrading behavior-neutral for every TokenRateLimitPolicy that predates reservations, instead of silently reserving an arbitrary flat amount for policies that were never tuned for it. **To get real protection against concurrent-request races, `amount` must be set explicitly** to a meaningful, non-zero estimate of tokens consumed per request.

The reserved `amount` is an estimate: once the upstream responds, the actual token usage — resolved via [`dataExtraction.response.totalTokens`](#dataextraction) and its built-in defaults — is committed and the unused portion of the reservation is released.

When omitted, `ttl` defaults to the route's own `HTTPRoute.spec.rules[].timeouts.backendRequest`, since a reservation only needs to survive as long as the request it protects can legitimately run; setting it much larger than that only extends how long an abandoned reservation (e.g. a disconnected client) blocks capacity for no benefit.

Example — reserve a flat 8000 tokens per request and hold the reservation for 30s:

```yaml
limits:
  chat:
    rates:
    - limit: 100000
      window: 1h
    reservation:
      amount: 8000
      ttl: 'duration("30s")'
```

`amount` also accepts a CEL expression as a quoted string, e.g. `amount: "1 + 1"`. Note: expressions that read the request body (e.g. a `requestBodyJSON(...)`-based token estimate) are not yet supported for `reservation.amount`.

### DataExtraction

| **Field**  | **Type**                                        | **Required** | **Description**                                     |
|------------|--------------------------------------------------|:------------:|-------------------------------------------------------|
| `response` | [ResponseDataExtraction](#responsedataextraction) | No           | Configures data extraction from the response body     |

### ResponseDataExtraction

`ResponseDataExtraction` is a map from an extraction target name to an ordered list (1-8 items) of JSON Pointer ([RFC 6901](https://www.rfc-editor.org/rfc/rfc6901)) expressions evaluated against the response body. For a given entry, the first pointer that resolves to a numeric value is used. Pointers may not contain `"` or `\` characters. Up to 16 target names are accepted.

| **Key**       | **Value Type** | **Required** | **Description**                                                                                                                                                                                       |
|---------------|----------------|:------------:|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `totalTokens` | []String       | No           | Ordered list of JSON Pointer expressions used to resolve total token usage. If omitted, a built-in default list is used (see [Token Usage Tracking](#token-usage-tracking)) |

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

TokenRateLimitPolicy automatically tracks token consumption from AI/LLM responses by evaluating an ordered list of JSON Pointer candidates against the response body — the first candidate that resolves to a numeric value is used (see [Data Extraction](#dataextraction)). This enables accurate usage-based rate limiting where:

- **Request Phase**: The policy evaluates predicates and descriptors during the request
- **Response Phase**: The policy extracts actual token usage from the response body
- **Rate Limiting**: Limitador receives the actual token count as `hits_addend` for precise accounting

### Supported Response Format

One of the built-in default candidates, `/usage/total_tokens`, matches the OpenAI-style response shape:
```json
{
  "usage": {
    "total_tokens": 150,
    "prompt_tokens": 100,
    "completion_tokens": 50
  }
}
```

This is one of several provider response shapes covered out of the box — see the full built-in default list below, and [`dataExtraction`](#dataextraction) for configuring custom candidates.

**What's actually checked**: Token extraction evaluates an ordered list of JSON Pointer ([RFC 6901](https://www.rfc-editor.org/rfc/rfc6901)) expressions against the response body — the first pointer that resolves to a numeric value is used. When `spec.dataExtraction.response.totalTokens` is unset, the following built-in default list is used, in order:

| Order | JSON Pointer                     | Provider(s)                                                                              |
|-------|-----------------------------------|-------------------------------------------------------------------------------------------|
| 1     | `/usage/total_tokens`             | OpenAI, Azure OpenAI, OpenAI-compatible servers (vLLM, kServe, Ollama, etc.), OpenAI Responses API (non-streaming) |
| 2     | `/usageMetadata/totalTokenCount`  | Google Gemini                                                                              |
| 3     | `/response/usage/total_tokens`    | OpenAI Responses API (streaming)                                                            |
| 4     | `/usage/totalTokens`              | AWS Bedrock Converse API (non-streaming)                                                    |

To target a provider not covered by the defaults, or to restrict/reorder the candidates, set `spec.dataExtraction.response.totalTokens` explicitly:

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: custom-data-extraction
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ai-gateway
  dataExtraction:
    response:
      totalTokens:
      - /usage/total_tokens
      - /usageMetadata/totalTokenCount
  limits:
    global:
      rates:
      - limit: 100000
        window: 1h
```

`dataExtraction` participates in the same defaults/overrides hierarchy as `limits`: it can be set at the top level, within `spec.defaults`, or within `spec.overrides`, and is inherited down the Gateway API hierarchy like any other policy rule.

**Streaming Support**: Both streaming and non-streaming responses are supported for providers whose streaming response shape is covered by the resolved pointer list. Many providers only include token usage in the final stream event when the client explicitly opts in — for example, OpenAI requires `"stream": true` and `"stream_options": { "include_usage": true }` in the request body; check your provider's documentation for the equivalent header or field.

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
