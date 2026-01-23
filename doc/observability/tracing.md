# Enabling tracing with a central collector

## Introduction

This guide outlines the steps to enable tracing in Istio and Kuadrant components (Authorino, Limitador, and WASM), directing traces to a central collector for improved observability and troubleshooting. We'll also explore a typical troubleshooting flow using traces and logs.

## Prerequisites

- A Kubernetes cluster with Istio and Kuadrant installed.
- A trace collector (e.g., Jaeger or Tempo) configured to support [OpenTelemetry](https://opentelemetry.io/) (OTel).

## Configuration Steps

### Istio Tracing Configuration

Enable tracing in Istio by using the [Telemetry API](https://istio.io/v1.20/docs/tasks/observability/distributed-tracing/telemetry-api/).
Depending on your method for installing Istio, you will need to configure a tracing `extensionProvider` in your MeshConfig, Istio or IstioOperator resource as well.
Here is an example Telemetry and Istio config to sample 100% of requests, if using the Istio Sail Operator.

```yaml
apiVersion: telemetry.istio.io/v1alpha1
kind: Telemetry
metadata:
  name: mesh-default
  namespace: gateway-system
spec:
  tracing:
  - providers:
    - name: tempo-otlp
    randomSamplingPercentage: 100
---
apiVersion: operator.istio.io/v1alpha1
kind: Istio
metadata:
  name: default
spec:
  namespace: gateway-system
  values:
    meshConfig:
      defaultConfig:
        tracing: {}
      enableTracing: true
      extensionProviders:
      - name: tempo-otlp
        opentelemetry:
          port: 4317
          service: tempo.tempo.svc.cluster.local
```

**Important:**

The OpenTelemetry collector protocol should be explicitly set in the service port `name` or `appProtocol` fields as per the [Istio documentation](https://istio.io/latest/docs/ops/configuration/traffic-management/protocol-selection/#explicit-protocol-selection). For example, when using gRPC, the port `name` should begin with `grpc-` or the `appProtocol` should be `grpc`.

### Kuadrant Tracing Configuration

Kuadrant components (Authorino, Limitador, and WASM) have request tracing capabilities.
The recommended approach is to configure tracing centrally via the Kuadrant CR, which will automatically propagate the configuration to all components.
Ensure the collector is the same one that Istio is sending traces so that they can be correlated later.

#### Centralized Configuration (Recommended)

Configure tracing once in the Kuadrant CR:

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: kuadrant-system
spec:
  observability:
    tracing:
      defaultEndpoint: grpc://tempo.tempo.svc.cluster.local:4317
      insecure: true
```

**Configuration Fields:**
- `defaultEndpoint`: The URL of the tracing collector backend. Supported protocols include `grpc://` and `http://`.
- `insecure`: Set to `true` to skip TLS certificate verification (useful for development environments).

This configuration will be automatically propagated to:
- **Authorino** (Auth service)
- **Limitador** (Rate limiting service)
- **WASM** modules (Envoy WebAssembly filters)

Once applied, the Authorino and Limitador components will be redeployed with tracing enabled.

#### Direct Configuration (Advanced)

For advanced use cases, you can configure tracing directly in the Authorino or Limitador CRs:

```yaml
apiVersion: operator.authorino.kuadrant.io/v1beta1
kind: Authorino
metadata:
  name: authorino
spec:
  tracing:
    endpoint: grpc://authorino-collector:4317
    insecure: true
---
apiVersion: limitador.kuadrant.io/v1alpha1
kind: Limitador
metadata:
  name: limitador
spec:
  tracing:
    endpoint: grpc://limitador-collector:4317
```

**Important:** When tracing is configured directly in Authorino or Limitador CRs, those settings take precedence over the Kuadrant CR configuration. The Kuadrant operator will cede ownership of the tracing field to you, allowing full control over component-specific tracing endpoints. This is useful when you need different collectors for different components.

#### Configuration Precedence

The tracing configuration follows this precedence order:

1. **Component-specific configuration** (Authorino/Limitador CR) - highest priority
2. **Centralized configuration** (Kuadrant CR) - applies when component CRs don't specify tracing

If you set tracing in the Kuadrant CR and later configure it directly in an Authorino or Limitador CR, the component-specific configuration will take precedence, and the Kuadrant operator will no longer manage that component's tracing settings.

**Note on Trace Continuity:**

Currently, trace IDs [do not propagate](https://github.com/envoyproxy/envoy/issues/22028) to WebAssembly modules in Istio/Envoy. This affects trace continuity when rate limiting is enforced via WASM filters, as requests may not have the relevant 'parent' trace ID in their trace information.

However, if the trace initiation point is outside of Envoy/Istio, the 'parent' trace ID will be available and included in traces passed to the collector. This limitation can impact correlating traces across the gateway, auth service, rate limiting, and other components in the request path.

Despite this, Kuadrant configures tracing for WASM modules when using the centralized configuration, ensuring that trace data is collected even if parent-child relationships may be limited in some scenarios.

## Troubleshooting Flow Using Traces and Logs

Using a tracing interface like the Jaeger UI or Grafana, you can search for trace information by the trace ID.
You may get the trace ID from logs, or from a header in a sample request you want to troubleshoot.
You can also search for recent traces, filtering by the service you want to focus on.

Here is an example trace in the Grafana UI showing the total request time from the gateway (Istio), the time to check the curent rate limit count (and update it) in limitador and the time to check auth in Authorino:

![Trace in Grafana UI](grafana_trace.png)

In limitador, it is possible to enable request logging with trace IDs to get more information on requests.
This requires the log level to be increased to at least debug, so the verbosity must be set to 3 or higher in the Limitador CR. For example:

```yaml
apiVersion: limitador.kuadrant.io/v1alpha1
kind: Limitador
metadata:
  name: limitador
spec:
  verbosity: 3
```

A log entry will look something like this, with the `traceparent` field holding the trace ID:

```
"Request received: Request { metadata: MetadataMap { headers: {"te": "trailers", "grpc-timeout": "5000m", "content-type": "application/grpc", "traceparent": "00-4a2a933a23df267aed612f4694b32141-00f067aa0ba902b7-01", "x-envoy-internal": "true", "x-envoy-expected-rq-timeout-ms": "5000"} }, message: RateLimitRequest { domain: "default/toystore", descriptors: [RateLimitDescriptor { entries: [Entry { key: "limit.general_user__f5646550", value: "1" }, Entry { key: "metadata.filter_metadata.envoy\\.filters\\.http\\.ext_authz.identity.userid", value: "alice" }], limit: None }], hits_addend: 1 }, extensions: Extensions }"
```

If you centrally aggregate logs using something like promtail and loki, you can jump between trace information and the relevant logs for that service:

![Trace and logs in Grafana UI](grafana_tracing_loki.png)

Using a combination of tracing and logs, you can visualise and troubleshoot request timing issues and drill down to specific services.
This method becomes even more powerful when combined with [metrics](https://docs.kuadrant.io/latest/kuadrant-operator/doc/observability/metrics/) and [dashboards](https://docs.kuadrant.io/latest/kuadrant-operator/doc/observability/examples/) to get a more complete picture of your users traffic.
