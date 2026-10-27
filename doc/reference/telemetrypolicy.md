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
| `labels` | Map<String: String>  | Yes | Labels to add to metrics, where keys are label names and values are CEL expressions |

### LoggingSpec
| Field       | Type                     | Required | Description                                                                                                                                                                                                                         |
|-------------|--------------------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `default` | [LoggingConfig](#loggingconfig)  | Yes | Default logging configuration that applies to all requests |

### LoggingConfig
| Field       | Type                     | Required | Description                                                                                                                                                                                                                         |
|-------------|--------------------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `fields` | Map<String: String>  | Yes | Fields to add to authorization decision logs, where keys are field names and values are CEL expressions |

Logging field and metric label expressions that do not reference `auth.*` are evaluated in
the gateway's wasm filter, inside the single expression that builds the Authorino check
request. A field or label whose expression is a bare read of one request header,
`request.headers.<name>` or `request.headers["<name>"]`, is rendered with a presence guard and
yields `""` when the header is absent, so a request that lacks the header still proceeds. Any
other expression that reads a header, for example
`request.headers["x-tenant"] + "-" + request.path`, needs its own guard, such as
`"x-tenant" in request.headers ? ... : ""` or `has(request.headers.<name>) ? ... : ""`;
without one, a request that lacks the header fails with a 500 before Authorino is called.
Metric labels are also sent to Limitador with each rate-limit action, and that path is not
guarded: a label that reads an absent header fails the rate-limit action, which by default
lets the request through without rate limiting and without the metric, so give a label on an
optional header an explicit `has()` or `in` guard. Header names are matched as written and
the gateway exposes them in lowercase, so write `request.headers["x-tenant"]`, not `X-Tenant`.

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
