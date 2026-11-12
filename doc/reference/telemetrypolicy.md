# The TelemetryPolicy Custom Resource Definition (CRD)

## TelemetryPolicy

| **Field** | **Type**                                        | **Required** | **Description**                                       |
|-----------|-------------------------------------------------|:------------:|-------------------------------------------------------|
| `spec`    | [TelemetryPolicySpec](#telemetrypolicyspec)     |     Yes      | The specification for TelemetryPolicy custom resource |
| `status`  | [TelemetryPolicyStatus](#telemetrypolicystatus) |      No      | The status for the custom resource                    |

## TelemetryPolicySpec

Custom logging is disabled by default. To enable `logging.default.fields`, an
administrator must set `spec.enableLoggingFields: true` on the Authorino CR.
Creating a TelemetryPolicy alone does not enable custom logging, and logging
fields apply only to routes with an AuthPolicy. This requires an Authorino and
Authorino Operator version supporting custom logging fields.

Metric label and logging field expressions that do not reference `auth.*` are evaluated
in the gateway's wasm filter and their values are forwarded verbatim: logging fields reach
Authorino as plain strings, outside its log redaction, and metric labels reach Limitador.
A policy whose expression reads a sensitive request header is therefore rejected with
`Accepted=False` and reason `Invalid`. The sensitive headers are `authorization`, `cookie`,
`x-api-key` and `apikey`, matched case-insensitively, the same list Authorino redacts by
default. Reading `request.headers` or `request` as a whole, or indexing `request.headers`
with a key that is not a string literal, is rejected for the same reason, as is an
expression that does not parse. Presence tests do not read the header value and are
accepted in both spellings: `has(request.headers.authorization) ? "bearer" : "anonymous"`,
and for hyphenated names, which `has()` cannot select,
`"x-tenant" in request.headers ? string(request.headers["x-tenant"]) : ""`. A membership
test naming a sensitive header, such as `"authorization" in request.headers`, is rejected
like a read of that header.

| **Field**   | **Type**                                                                                                                                    | **Required** | **Description**                                                                                                                                                                             |
|-------------|---------------------------------------------------------------------------------------------------------------------------------------------|--------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `targetRef` | [LocalPolicyTargetReferenceWithSectionName](#localpolicytargetreferencewithsectionname) | Yes          | Reference to a Kubernetes resource that the policy attaches to. For more [info](https://gateway-api.sigs.k8s.io/reference/spec/#localpolicytargetreferencewithsectionname)                                                                                                                              |
| `metrics`   | [MetricsSpec](#metricsspec) | No | Metrics holds the telemetry metrics configuration. At least one of `metrics` or `logging` must be defined. |
| `logging`   | [LoggingSpec](#loggingspec) | No | Logging holds the custom authorization decision logging configuration. At least one of `metrics` or `logging` must be defined. |

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

### MetricsSpec
| Field       | Type                     | Required | Description                                                                                                                                                                                                                         |
|-------------|--------------------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `default` | [MetricsConfig](#metricsconfig)  | Yes | Default metrics configuration that applies to all requests |

### MetricsConfig
| Field       | Type                     | Required | Description                                                                                                                                                                                                                         |
|-------------|--------------------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `labels` | Map<String: String>  | Yes | Labels to add to metrics, where keys are label names and values are CEL expressions. On the authorization action an unresolved `auth.*` label is omitted; any other evaluation failure follows the failure mode of the corresponding protection service, see [below](#evaluation-of-expressions) |

### LoggingSpec
| Field       | Type                     | Required | Description                                                                                                                                                                                                                         |
|-------------|--------------------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `default` | [LoggingConfig](#loggingconfig)  | Yes | Default logging configuration that applies to all requests |

### LoggingConfig
| Field       | Type                     | Required | Description                                                                                                                                                                                                                         |
|-------------|--------------------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `fields` | Map<String: String>  | Yes | Fields to add to authorization decision logs, where keys are field names and values are CEL expressions. An unresolved `auth.*` field is omitted from the record; any other evaluation failure follows the failure mode of the authorization service, see [below](#evaluation-of-expressions) |

#### Evaluation of expressions

On the authorization action, logging fields and metric labels that reference `auth.*` are
evaluated in the auth provider after authentication, and one that cannot be resolved is omitted
from the record or the metric. Every other expression is evaluated in the gateway, and one that
cannot be evaluated for a request, for example a read of a header the request does not carry,
fails the action it belongs to. What happens to the request then follows the failure mode of the
corresponding protection service: the authorization service defaults to `failureMode: deny`, so
the request fails with HTTP 500 and no decision record is written; the rate limit service
defaults to `failureMode: allow`, so the request proceeds without rate limiting. On a rate-limit
action that runs after authentication every metric label, including one that references
`auth.*`, is evaluated in the gateway as part of the descriptor and follows that rule; on one
that runs before authentication, `auth.*` labels are left out of the descriptor and are not
evaluated. The two sides also see identity differently: a Kubernetes TokenReview identity is
`auth.identity.user.username` in Authorino but `auth.identity.username` in the gateway, so one
expression cannot resolve on both, see the
[egress observability guide](../user-guides/egress/observability.md).

Guard optional attributes in the expression itself:
`"x-tenant" in request.headers ? request.headers["x-tenant"] : "unknown"` works for any header name,
and `has(request.headers.xtenant) ? string(request.headers.xtenant) : "unknown"` for names without
hyphens. The gateway exposes header names in lowercase, so write `request.headers["x-tenant"]`,
not `X-Tenant`. CEL optional field syntax (`.?`) is not supported.

### TelemetryPolicyStatus

| **Field**            | **Type**                                                                                     | **Description**                                                                                                                     |
|----------------------|----------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `observedGeneration` | String                                                                                       | Number of the last observed generation of the resource. Use it to check if the status info is up to date with latest resource spec. |
| `conditions`         | [][ConditionSpec](https://pkg.go.dev/k8s.io/apimachinery@v0.28.4/pkg/apis/meta/v1#Condition) | List of conditions that define that status of the resource.                                                                         |

## See Also

- [TelemetryPolicy Overview](../overviews/telemetrypolicy.md)
- [Token Rate Limiting Tutorial](../user-guides/tokenratelimitpolicy/authenticated-token-ratelimiting-tutorial.md)
- [Well-known Attributes](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md)
- [Gateway API Documentation](https://gateway-api.sigs.k8s.io/)
