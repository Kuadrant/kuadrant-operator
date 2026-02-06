# The Kuadrant Custom Resource Definition (CRD)

## kuadrant

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `spec`    | [KuadrantSpec](#kuadrantspec)     |      No      | Blank specification                  |
| `status`  | [KuadrantStatus](#kuadrantstatus) |      No      | The status for the custom resources. |

### KuadrantSpec

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `observability`    | [Observability](#observability)     | No | Kuadrant observability configuration. |
| `mtls`  | [mTLS](#mtls) |      No      | Two way authentication between kuadrant components. |
| `components`  | [Components](#components) |      No      | Optional Kuadrant components configuration. |

#### mTLS

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `enable`    | Boolean     |  No | Enable mutual authentication communication between the gateway and the kuadrant data plane components. Default: `false`|
| `limitador` | Boolean     |  No | Enable mutual authentication communication between the gateway and Limitador. Default: `not set`|
| `authorino` | Boolean     |  No | Enable mutual authentication communication between the gateway and Authorino. Default: `not set`|

The truth table for limitador component is as follows:

| Spec |  Limtador mTLS enabled |
| --- | ---  |
| `{Enable: false, limitador: null}` | false |
| `{Enable: true, limitador: null}` | true |
| `{Enable: false, limitador: false}` | false |
| `{Enable: false, limitador: true}` | false |
| `{Enable: true, limitador: false}` | false |
| `{Enable: true, limitador: true}` | true |

The truth table for authorino component is as follows:

| Spec |  Authorino mTLS enabled |
| --- | ---  |
| `{Enable: false, authorino: null}` | false |
| `{Enable: true, authorino: null}` | true |
| `{Enable: false, authorino: false}` | false |
| `{Enable: false, authorino: true}` | false |
| `{Enable: true, authorino: false}` | false |
| `{Enable: true, authorino: true}` | true |

#### Observability

Configures telemetry and monitoring settings for Kuadrant components. When enabled, it configures logging, tracing, and other observability features for both the control plane and data plane components.

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `enable`    | Boolean     |  No | Enable observability on kuadrant. Default: `false` |
| `dataPlane` | [DataPlane](#dataplane) | No | Configures observability settings for the data plane components. |
| `tracing` | [Tracing](#tracing) | No | Configures distributed tracing for request flows through the system. |

##### DataPlane

Configures observability for data plane components (WASM filters). Controls OpenTelemetry trace filtering and request correlation features.

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `defaultLevels` | [][LogLevel](#loglevel) | No | Specifies the OpenTelemetry trace filtering levels for WASM modules. Controls which trace spans are exported to your observability backend (Jaeger, Tempo, etc.). The highest priority level set determines the filter level. **Important:** This controls trace span filtering, not gateway pod log verbosity. To control logs visible via `kubectl logs`, configure Envoy's log level separately. |
| `httpHeaderIdentifier` | String | No | Specifies the HTTP header name used to identify and correlate requests in traces (e.g., "x-request-id", "x-correlation-id"). If set, this header value will be included in trace spans for request correlation across components. |

###### LogLevel

Defines a trace filtering level. The highest priority level set (DEBUG > INFO > WARN > ERROR) determines the OTEL trace filter. Only one field should be set per LogLevel entry.

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `debug` | String | No | Debug level for trace filtering - highest verbosity, most spans exported. **Current implementation (MVP):** Set to `"true"` to enable. **Future:** Will support CEL expressions for dynamic request-time evaluation. |
| `info` | String | No | Info level for trace filtering. **Current implementation (MVP):** Set to `"true"` to enable. **Future:** Will support CEL expressions. |
| `warn` | String | No | Warn level for trace filtering. **Current implementation (MVP):** Set to `"true"` to enable. **Future:** Will support CEL expressions. |
| `error` | String | No | Error level for trace filtering - lowest verbosity, minimal spans exported. **Current implementation (MVP):** Set to `"true"` to enable. **Future:** Will support CEL expressions. |

**Example:**
```yaml
dataPlane:
  defaultLevels:
    - debug: "true"  # Enable DEBUG level trace filtering
  httpHeaderIdentifier: x-request-id
```

##### Tracing

Configures distributed tracing integration for request flows. It enables tracing spans to be exported to external tracing systems (e.g., Jaeger, Zipkin, Tempo).

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `defaultEndpoint` | String | No | The default URL of the tracing collector backend where spans should be sent. This endpoint is used by Auth (Authorino), RateLimiting (Limitador) and WASM services for exporting trace data. If tracing endpoints have been configured directly in Authorino or Limitador CRs, those take precedence over this default value. Note: Per-gateway overrides are not currently supported. |
| `insecure` | Boolean | No | Controls whether to skip TLS certificate verification. Default: `false` |

#### Components

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `developerPortal`    | [DeveloperPortal](#developerportal)     |  No | Developer portal integration configuration. |

##### DeveloperPortal

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `enabled`    | Boolean     |  No | Enable the developer portal integration including APIProduct and APIKeyRequest CRDs. Default: `false` |

### KuadrantStatus

| **Field**            | **Type**                                                                                     | **Description**                                                                                                                     |
|----------------------|----------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `observedGeneration` | String                                                                                       | Number of the last observed generation of the resource. Use it to check if the status info is up to date with latest resource spec. |
| `conditions`         | [][ConditionSpec](https://pkg.go.dev/k8s.io/apimachinery@v0.28.4/pkg/apis/meta/v1#Condition) | List of conditions that define that status of the resource.                                                                         |
| `mtlsLimitador` | Boolean | Limitador mTLS enabled. |
| `mtlsAuthorino` | Boolean | Authorino mTLS enabled. |
