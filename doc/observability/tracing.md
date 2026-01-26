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
    - name: jaeger-collector
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
      - name: jaeger-collector
        opentelemetry:
          port: 4317
          service: jaeger-collector.jaeger.svc.cluster.local
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
      defaultEndpoint: rpc://jaeger-collector.jaeger.svc.cluster.local:4317
      insecure: true
```

**Configuration Fields:**
- `defaultEndpoint`: The URL of the tracing collector backend (OTLP endpoint). Supported protocols:
  - `rpc://` for gRPC OTLP (port 4317) - recommended for Authorino
  - `grpc://` for gRPC OTLP (port 4317) - used by other components
  - `http://` for HTTP OTLP (port 4318)
- `insecure`: Set to `true` to skip TLS certificate verification (useful for development environments).

**Important:** Point to the **collector** service (e.g., `jaeger-collector`), not the query service. The collector receives traces from your applications, while the query service is only for viewing traces in the UI.

This configuration will be automatically propagated to:
- **Kuadrant Operator** (Control plane - reconciliation loops)
- **Authorino** (Auth service - authentication decisions)
- **Limitador** (Rate limiting service - rate limit checks)
- **WASM** modules (Envoy WebAssembly filters - gateway-level tracing)

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

## Control Plane Tracing

The Kuadrant operator itself (the control plane) exports traces to your OpenTelemetry collector, allowing you to observe the operator's reconciliation loops and internal operations. This is useful for debugging controller behavior, understanding operator performance, and tracking policy lifecycle events.

**Note:** Control plane tracing may already be enabled in your installation. Check if you can see `kuadrant-operator` service in your tracing UI before configuring.

### Control Plane vs Data Plane Tracing

Kuadrant supports tracing at two levels:

1. **Control Plane Tracing** (this section): Traces the operator's reconciliation loops and internal operations
   - Shows policy lifecycle events, topology building, resource creation
   - Helps debug operator behavior and performance

2. **Data Plane Tracing** (see configuration above): Traces actual user requests through the gateway and policy enforcement components
   - Shows request flows through Istio/Envoy, Authorino, Limitador, and WASM filters
   - Helps debug request-level issues and policy enforcement

Both are configured via the Kuadrant CR (`spec.observability.tracing`) and send traces to the same collector, providing a complete view of your Kuadrant system from policy reconciliation to request processing.

### What Control Plane Traces Show

Control plane traces capture operator activities such as:

- **Policy reconciliation**: When a policy (AuthPolicy, RateLimitPolicy, DNSPolicy, TLSPolicy) is created, updated, or deleted
- **Resource creation**: Creating Authorino AuthConfigs, Limitador configurations, Envoy WASM filters, etc.
- **Gateway topology discovery**: Analyzing Gateway API resources and computing policy attachments
- **Status updates**: Updating policy status conditions
- **Conflict detection**: Detecting and resolving policy conflicts
- **Error handling**: Tracking reconciliation errors and retries

These traces are separate from data plane traces (actual user requests) and help operators understand what the Kuadrant operator is doing behind the scenes.

### Viewing Control Plane Traces

Once control plane tracing is enabled, you can view operator traces in Jaeger or Grafana:

**Using Jaeger UI:**

1. Port-forward to Jaeger Query service:
   ```bash
   kubectl port-forward -n <jaeger-namespace> svc/jaeger-query 16686:80
   ```

   Or if using the Jaeger all-in-one deployment:
   ```bash
   kubectl port-forward -n <jaeger-namespace> svc/jaeger 16686:16686
   ```

2. Open http://localhost:16686

3. Select service: **kuadrant-operator**

4. Search for traces by:
   - **Operation name**: Look for operations like `controller.reconcile`, `workflow.data_plane_policies`
   - **Tags**: Filter by specific policy using `policy.name=my-policy-name`
   - **Duration**: Find slow reconciliations (e.g., min duration > 100ms)

**Example searches:**

Find all traces for a specific RateLimitPolicy:
```
Service: kuadrant-operator
Tags: policy.name=my-ratelimitpolicy
```

Find slow reconciliations for data plane policies:
```
Service: kuadrant-operator
Operation: workflow.data_plane_policies
Min Duration: 100ms
```

**Example Trace Spans:**

A typical reconciliation loop generates traces showing the workflow structure:

```
controller.reconcile (29.8ms)
├─ topology.build (495µs)
├─ workflow.init (12.56ms)
│  └─ init.topology_reconciler (10.41ms)
├─ workflow.data_plane_policies (15.73ms)
│  ├─ validation (72µs)
│  ├─ effective_policies (3.01ms)
│  │  ├─ effective_policies.auth
│  │  ├─ effective_policies.ratelimit
│  │  └─ effective_policies.token_ratelimit
│  ├─ reconciler.auth_configs (293µs)
│  ├─ reconciler.limitador_limits (216µs)
│  └─ wasm.BuildConfigForPath (142µs)
└─ status_update (12.64ms)
```

The trace structure reflects the operator's workflow-based reconciliation:

**Main Workflows:**
- **controller.reconcile**: Main reconciliation entry point for all policy changes
- **topology.build**: Building the Gateway API topology graph
- **workflow.init**: Initialization workflow (topology reconciliation, event logging)
- **workflow.data_plane_policies**: Auth and rate limiting policy reconciliation
- **workflow.dns**: DNS policy reconciliation (when DNS policies are present)
- **workflow.tls**: TLS policy reconciliation (when TLS policies are present)
- **workflow.observability**: Observability configuration reconciliation
- **workflow.limitador**: Limitador deployment reconciliation (when Limitador operator is installed)
- **workflow.authorino**: Authorino deployment reconciliation (when Authorino operator is installed)
- **status_update**: Final workflow for updating policy statuses

**Data Plane Policy Workflow Spans:**
- **validation**: Validates AuthPolicy, RateLimitPolicy, TokenRateLimitPolicy resources
- **effective_policies**: Computes effective policies for each HTTPRoute and Gateway
  - `effective_policies.auth`: Effective auth policies
  - `effective_policies.ratelimit`: Effective rate limit policies
  - `effective_policies.token_ratelimit`: Effective token rate limit policies
- **reconciler.auth_configs**: Reconciles Authorino AuthConfig resources
- **reconciler.limitador_limits**: Reconciles Limitador limit configurations
- **reconciler.istio_extension**: Reconciles Istio WasmPlugin and EnvoyFilter resources (when Istio is the gateway provider)
- **reconciler.envoy_gateway_extension**: Reconciles Envoy Gateway extension policies (when Envoy Gateway is the provider)
- **wasm.BuildConfigForPath**: Builds WASM filter configuration for a specific HTTPRoute path

### Tracing Policy Lifecycle

To trace a specific policy creation or update:

1. **Create or update a policy**:
   ```bash
   kubectl apply -f my-ratelimitpolicy.yaml
   ```

2. **Get the policy creation/update timestamp**:
   ```bash
   kubectl get ratelimitpolicy my-policy -o jsonpath='{.metadata.creationTimestamp}'
   ```

3. **Search in Jaeger**:
   - Set the time range around the policy change timestamp
   - Look for operation: `controller.reconcile`
   - Expand the trace to see workflow details like `workflow.data_plane_policies`
   - Look for specific reconciler spans like `reconciler.limitador_limits` or `reconciler.auth_configs`
   - Check timing information to identify performance bottlenecks

### Correlating Control Plane and Data Plane Traces

While control plane and data plane traces are separate, you can correlate them:

1. **Control plane trace**: Shows when a policy was reconciled and resources created
2. **Data plane trace**: Shows actual user requests processed with that policy

Example workflow:
1. Create a RateLimitPolicy at `15:30:00`
2. View control plane trace to see:
   - Policy reconciliation completed at `15:30:05`
   - Limitador configuration updated
   - WASM plugin deployed
3. Send a test request at `15:30:10`
4. View data plane trace to see:
   - Request processed through WASM filter
   - Rate limit check sent to Limitador
   - Response returned

### Local Development

For local development, you can run the operator with control plane tracing enabled by setting environment variables:

```bash
# Start Jaeger or an OTel collector
docker compose -f examples/otel/docker-compose.yaml up -d

# Run the operator with OpenTelemetry enabled
# Note: When running locally, env vars are used instead of the Kuadrant CR
export OTEL_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318
make run

# View traces in Jaeger (http://localhost:16686)
```

**Note:** When running the operator locally (outside the cluster), control plane tracing is configured via environment variables. When deployed in-cluster, both control plane and data plane tracing are configured via the Kuadrant CR.

See the [OpenTelemetry example](../../examples/otel/README.md) for complete local development setup with Grafana, Tempo, Jaeger, Loki, and Prometheus.

### Configuration Notes

When you configure tracing via the Kuadrant CR (`spec.observability.tracing`), it automatically enables tracing for:
- **Control Plane**: The Kuadrant operator's reconciliation loops
- **Data Plane**: Authorino, Limitador, and WASM filters

The tracing configuration you set in the Kuadrant CR propagates to all components, ensuring consistent tracing across your entire Kuadrant deployment.

### Troubleshooting Control Plane Tracing

**Traces not appearing in Jaeger:**

1. Check if tracing is configured in the Kuadrant CR:
   ```bash
   kubectl get kuadrant kuadrant -n kuadrant-system -o jsonpath='{.spec.observability.tracing}'
   ```

   Expected output should show `defaultEndpoint` configured:
   ```json
   {"defaultEndpoint":"rpc://jaeger-collector.jaeger.svc.cluster.local:4317","insecure":true}
   ```

2. Check operator logs for OTLP connection errors:
   ```bash
   kubectl logs -n kuadrant-system -l control-plane=controller-manager --tail=50 | grep -i otel
   ```

3. Verify the tracing collector endpoint is reachable from the operator:
   ```bash
   # For HTTP OTLP endpoint (port 4318)
   kubectl exec -n kuadrant-system deployment/kuadrant-operator-controller-manager -- \
     curl -v http://jaeger-collector.jaeger.svc.cluster.local:4318/v1/traces
   ```

4. Test sending traces to the collector directly:
   ```bash
   # Port-forward to the collector for local testing
   kubectl port-forward -n <jaeger-namespace> svc/jaeger-collector 4318:4318

   # Send a test trace
   curl -X POST http://localhost:4318/v1/traces \
     -H "Content-Type: application/json" \
     -d '{"resourceSpans":[]}'
   ```

**Traces are incomplete or missing spans:**

- Check for errors in the operator logs that might indicate reconciliation failures
- Verify the collector is not dropping spans due to rate limiting or storage issues
- Check the collector's own logs for processing errors

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
This method becomes even more powerful when combined with [metrics](https://docs.kuadrant.io/latest/kuadrant-operator/doc/observability/metrics/), [access logs](./envoy-access-logs.md), and [dashboards](https://docs.kuadrant.io/latest/kuadrant-operator/doc/observability/examples/) to get a more complete picture of your users traffic.
