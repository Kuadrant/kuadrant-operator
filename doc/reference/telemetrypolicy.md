# The TelemetryPolicy Custom Resource Definition (CRD)

## TelemetryPolicy

| **Field** | **Type**                                        | **Required** | **Description**                                       |
|-----------|-------------------------------------------------|:------------:|-------------------------------------------------------|
| `spec`    | [TelemetryPolicySpec](#telemetrypolicyspec)     |     Yes      | The specification for TelemetryPolicy custom resource |
| `status`  | [TelemetryPolicyStatus](#telemetrypolicystatus) |      No      | The status for the custom resource                    |

## TelemetryPolicySpec

| **Field**   | **Type**                                                                                                                                    | **Required** | **Description**                                                                                                                                                                             |
|-------------|---------------------------------------------------------------------------------------------------------------------------------------------|--------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `targetRef` | [LocalPolicyTargetReferenceWithSectionName](#localpolicytargetreferencewithsectionname) | Yes          | Reference to a Kubernetes resource that the policy attaches to. For more [info](https://gateway-api.sigs.k8s.io/reference/spec/#localpolicytargetreferencewithsectionname)                                                                                                                              |
| `metrics`   | [MetricsSpec](#metricsspec) | Yes | Metrics holds the telemetry metrics configuration |

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

## See Also

- [TelemetryPolicy Overview](../overviews/telemetrypolicy.md)
- [Token Rate Limiting Tutorial](../user-guides/tokenratelimitpolicy/authenticated-token-ratelimiting-tutorial.md)
- [Well-known Attributes](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md)
- [Gateway API Documentation](https://gateway-api.sigs.k8s.io/)
