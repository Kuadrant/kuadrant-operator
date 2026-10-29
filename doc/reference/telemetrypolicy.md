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
| `labels` | Map<String: String>  | Yes | Labels to add to metrics, where keys are label names and values are CEL expressions. Evaluated on every request; an expression that cannot be evaluated fails the request rather than being skipped, see [below](#evaluation-of-expressions) |

### LoggingSpec
| Field       | Type                     | Required | Description                                                                                                                                                                                                                         |
|-------------|--------------------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `default` | [LoggingConfig](#loggingconfig)  | Yes | Default logging configuration that applies to all requests |

### LoggingConfig
| Field       | Type                     | Required | Description                                                                                                                                                                                                                         |
|-------------|--------------------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `fields` | Map<String: String>  | Yes | Fields to add to authorization decision logs, where keys are field names and values are CEL expressions. An `auth.*` expression that cannot be resolved is omitted from the record; any other expression that cannot be evaluated fails the request, see [below](#evaluation-of-expressions) |

#### Evaluation of expressions

Logging fields that reference `auth.*` are resolved by Authorino after authentication, and a
field that cannot be resolved is omitted from the record. Every other logging field, and every
metric label, is evaluated in the gateway as part of the single expression that builds the
authorization request, so an expression that cannot be evaluated for a request, for example one
that reads a header the request does not carry, fails that request with HTTP 500 before
Authorino is called, and no record is written. Metric labels are also evaluated as part of each
rate-limit descriptor, where a failing expression fails the rate-limit action; by default the
request then proceeds without rate limiting.

Guard optional attributes in the expression itself:
`"x-tenant" in request.headers ? request.headers["x-tenant"] : ""` works for any header name,
and `has(request.headers.xtenant) ? string(request.headers.xtenant) : ""` for names without
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
