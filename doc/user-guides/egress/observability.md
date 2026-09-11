# Egress Observability: Metrics and Access Logging

This guide covers monitoring and troubleshooting outbound traffic through an Istio egress gateway using Prometheus metrics and Envoy access logs.

## Prerequisites

- Kubernetes cluster with Kuadrant operator and Istio installed. See the [Getting Started](/latest/getting-started) guide.
- Egress gateway environment deployed. See the [Egress Gateway Setup](egress-gateway.md) guide.
- Prometheus configured to scrape Istio proxy metrics (included in the [observability stack](../../observability/README.md)).

The examples in this guide use:

| Resource | Value |
|----------|-------|
| Gateway namespace | `gateway-system` |
| Gateway name | `kuadrant-egressgateway` |
| External service | `httpbin.org` |

## How Egress Observability Differs from Ingress

Observing egress traffic has specific challenges that do not exist for ingress:

| Aspect | Ingress | Egress |
|--------|---------|--------|
| Source identity | External client (IP, API key) | Internal workload (namespace and service account) |
| Destination | Known back-end services | External services (DNS, TLS, availability) |
| Metrics reporter | Both `source` and `destination` proxy | `source` only (no proxy on external services) |
| Failure modes | App errors, auth failures | DNS failures, TLS errors, upstream timeouts |

Understanding these differences is key to interpreting egress metrics and logs correctly.

## Egress Metrics

### Available Metrics

The egress gateway emits the same standard Istio proxy metrics as an ingress gateway. Because the egress gateway terminates HTTP from the workload and originates TLS to the external service, full L7 metrics are available:

| Metric | Type | Description |
|--------|------|-------------|
| `istio_requests_total` | Counter | Total requests by response code, method, destination |
| `istio_request_duration_milliseconds` | Histogram | Request latency distribution |
| `istio_request_bytes` | Histogram | Request body size distribution |
| `istio_response_bytes` | Histogram | Response body size distribution |

### Egress-Specific Label Values

The same labels appear on egress metrics as on ingress, but their values differ in important ways:

| Label | Egress Value | Notes |
|-------|-------------|-------|
| `destination_service` | External hostname (for example, `httpbin.org`) | From the ServiceEntry |
| `destination_service_name` | External hostname (for example, `httpbin.org`) | Same as `destination_service` |
| `destination_service_namespace` | Gateway namespace (for example, `gateway-system`) | Namespace where ServiceEntry is deployed |
| `destination_workload` | `unknown` | External services have no workload identity |
| `source_workload` | Gateway deployment name | The egress gateway, NOT the calling workload |
| `source_workload_namespace` | `gateway-system` | Gateway namespace |
| `reporter` | `source` | Always (no destination-side proxy exists) |
| `response_code` | HTTP status code | Works the same as ingress |
| `response_flags` | Envoy response flags | Key for diagnosing egress failures |
| `connection_security_policy` | `unknown` | Security policy of incoming connection (workload to gateway) |

**Understanding `source_workload`:** On the egress gateway, `source_workload` identifies the gateway itself, not the workload that initiated the request. To attribute egress traffic to specific workloads, use [workload identity via AuthPolicy](egress-gateway.md#workload-identity) and correlate using access logs or traces. Alternatively, if the route is rate limited, [TelemetryPolicy](#custom-metric-labels-with-telemetrypolicy) can record the calling workload directly as a label on the Kuadrant data plane metrics.

### Querying Egress Metrics

You can verify that metrics are being emitted by querying the egress gateway pod Prometheus endpoint directly:

```sh
EGRESS_POD=$(kubectl get pods -n gateway-system \
    -l gateway.networking.k8s.io/gateway-name=kuadrant-egressgateway \
    -o jsonpath='{.items[0].metadata.name}')

kubectl exec -n gateway-system $EGRESS_POD -- \
    pilot-agent request GET /stats/prometheus 2>/dev/null | grep "^istio_requests_total"
```

Example output:

```text
istio_requests_total{...,destination_service="httpbin.org",...,response_code="200",response_flags="-",...} 8
istio_requests_total{...,destination_service="httpbin.org",...,response_code="503",response_flags="-",...} 24
istio_requests_total{...,destination_service="unknown",...,response_code="404",response_flags="NR",...} 4
```

### PromQL Examples

These queries work when Prometheus is scraping the egress gateway pod.

**Request rate by external destination:**

```promql
sum(rate(istio_requests_total{source_workload="kuadrant-egressgateway-istio"}[5m])) by (destination_service)
```

**Error rate for egress traffic:**

```promql
sum(rate(istio_requests_total{
    source_workload="kuadrant-egressgateway-istio",
    response_code=~"[45].."
}[5m]))
/
sum(rate(istio_requests_total{
    source_workload="kuadrant-egressgateway-istio"
}[5m]))
```

**P99 latency to external services:**

```promql
histogram_quantile(0.99,
    sum(rate(istio_request_duration_milliseconds_bucket{
        source_workload="kuadrant-egressgateway-istio"
    }[5m])) by (destination_service, le)
)
```

**Bytes transferred by destination:**

```promql
sum(rate(istio_response_bytes_sum{
    source_workload="kuadrant-egressgateway-istio"
}[5m])) by (destination_service)
```

**Requests with no matching route (misconfigured clients):**

```promql
sum(rate(istio_requests_total{
    source_workload="kuadrant-egressgateway-istio",
    response_flags="NR"
}[5m])) by (destination_service)
```

### Identifying the Egress Gateway

For Kubernetes queries (kubectl, log filtering), use the pod label `gateway.networking.k8s.io/gateway-name=kuadrant-egressgateway`. This label is not a Prometheus metric label.

For PromQL queries, filter by `source_workload="kuadrant-egressgateway-istio"` to isolate egress traffic from ingress traffic on the same Prometheus instance. This is the Istio proxy workload name, which appears as a label on all `istio_*` metrics.

## Custom Metric Labels with TelemetryPolicy

The `istio_*` metrics above cannot tell you which workload made an outbound call: on an egress gateway, `source_workload` is always the gateway itself. [TelemetryPolicy](../../overviews/telemetrypolicy.md) closes that gap by adding custom labels, derived from CEL expressions, to the metrics Kuadrant's own data plane emits. The policy itself has no egress-specific fields, so a policy written for an egress gateway looks identical to one written for an ingress gateway.

### Which Metrics Get Labeled

TelemetryPolicy labels the **Limitador** counters, not the `istio_*` metrics. Custom labels appear on:

| Metric | Description |
|--------|-------------|
| `authorized_calls` | Requests allowed by a rate limit check |
| `limited_calls` | Requests rejected by a rate limit check |
| `authorized_hits` | Hits counted against a limit (for TokenRateLimitPolicy, token consumption) |
| `report_calls` | Calls to the Limitador `Report` method, which TokenRateLimitPolicy uses to report token usage after the response |

Each series also carries Limitador's own `limitador_namespace` label, which identifies the targeted route (for example, `gateway-system/ai-mock-external`). For the full set of Limitador counters and their built-in labels, see the [Limitador metrics guide](../observability/limitador-metrics.md).

These are a separate metric stream: adding a TelemetryPolicy does not change `istio_requests_total` in any way.

### Requirements for Emitting Labels

Neither of the following requirements is specific to egress, but both are commonly missed when the target is an egress gateway:

1. **A rate limit policy must be in effect on the route.** Custom labels are attached to the descriptor the wasm-shim sends to Limitador, so a route with no `RateLimitPolicy` or `TokenRateLimitPolicy` produces no Limitador metrics and therefore no labels. A TelemetryPolicy attached to a gateway whose routes are not rate limited is accepted and enforced, but emits nothing.
2. **`targetRef` must be a Gateway.** The API rejects any other kind, so a TelemetryPolicy cannot be scoped to an individual HTTPRoute. Labels apply to every rate limited route on the gateway.

Additionally, labels that reference `auth.identity.*` require an [AuthPolicy](egress-gateway.md#workload-identity) establishing workload identity on the route.

The examples below use the resources from the [Egress Gateway Setup](egress-gateway.md) guide, plus an AI service route that carries both an AuthPolicy and a TokenRateLimitPolicy:

| Resource | Value |
|----------|-------|
| Route | `ai-mock-external` |
| External service | `api.ai-mock.local` |
| Calling workload | `team-gold` service account in `egress-test` |

### Attributing Egress Traffic to the Calling Workload

Attach a TelemetryPolicy to the egress gateway, labeling each metric with the authenticated identity of the calling workload and the external destination:

```sh
kubectl apply -f - <<'EOF'
apiVersion: extensions.kuadrant.io/v1alpha1
kind: TelemetryPolicy
metadata:
  name: egress-telemetry
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: kuadrant-egressgateway
  metrics:
    default:
      labels:
        workload: auth.identity.username
        destination: request.host
        source_ip: source.address.substring(0, source.address.lastIndexOf(":")).replace("[", "").replace("]", "")
EOF
```

Confirm the policy was accepted and enforced:

```sh
kubectl get telemetrypolicy egress-telemetry -n gateway-system \
    -o jsonpath='{range .status.conditions[*]}{.type}={.status}{"\n"}{end}'
```

Expected output:

```text
Accepted=True
Enforced=True
```

Send some authenticated traffic through the egress gateway, then read the Limitador metrics:

```sh
kubectl exec -n egress-test test-client -- \
    curl -sS --max-time 15 \
    http://limitador-limitador.kuadrant-system.svc.cluster.local:8080/metrics \
    | grep -v '^#' | grep authorized_calls
```

Expected output:

```text
authorized_calls{destination="api.ai-mock.local",source_ip="10.244.0.25",workload="system:serviceaccount:egress-test:team-gold",limitador_namespace="gateway-system/ai-mock-external"} 2
```

The `workload` label now identifies the service account that originated the outbound request.

The same labels appear on `limited_calls` when a workload exceeds its limit, so you can see exactly who is being throttled and on which destination:

```text
limited_calls{source_ip="10.244.0.24",workload="system:serviceaccount:egress-test:default",destination="api.ai-mock.local",limitador_namespace="gateway-system/ai-mock-external"} 12
```

### CEL Attributes Available on Egress

Each label is attached to both the rate limit check descriptor and the report descriptor. The check runs in the **request** phase, before the external service responds, so every expression must resolve at that point. This is the single most important constraint on egress: response attributes are not available, even for labels that only appear on `report_calls`.

| Expression | Available | Example value |
|------------|-----------|---------------|
| `auth.identity.username` | Yes | `system:serviceaccount:egress-test:team-gold` |
| `request.host` | Yes | `api.ai-mock.local` |
| `request.path` | Yes | `/v1/chat/completions` |
| `request.method` | Yes | `POST` |
| `request.scheme` | Yes | `http` |
| `source.address` | Yes | `10.244.0.25:54146`, includes the port (see [cardinality](#label-cardinality)) |
| A literal, for example `"egress"` | Yes | `egress` |
| `response.code` | **No** | Breaks the metrics for the route, see [known limitations](#known-limitations) |

Note that `request.scheme` describes the workload-to-gateway leg, not the gateway-to-external-service leg. A workload calling the gateway over plain HTTP reports `http` even when the gateway originates TLS to the external service.

To label by response status, use the `istio_requests_total` metric and its `response_code` label instead, as shown in [PromQL examples](#promql-examples).

### Label Cardinality

Every distinct combination of label values creates a new Prometheus time series, so a high-cardinality expression on a busy egress gateway is expensive.

`source.address` is the common trap: it includes the ephemeral source port, so **every connection produces a new series**. The same caveat applies when using it as a [rate limit counter](egress-gateway.md#rate-limiting).

Envoy renders the address as `10.244.0.24:54146` for IPv4 and `[2001:db8:85a3::8a2e:370:7334]:41235` for IPv6, so strip everything after the last colon rather than splitting on the first one:

```yaml
source_ip: source.address.substring(0, source.address.lastIndexOf(":")).replace("[", "").replace("]", "")
```

`lastIndexOf` finds the colon that precedes the port, so the colons inside an IPv6 address are left alone, and the two `replace` calls remove the brackets Envoy puts around it. The expression is plain string manipulation, so the address is passed through unchanged: the examples above yield `10.244.0.24` and `2001:db8:85a3::8a2e:370:7334`. Avoid `source.address.split(":")[0]`: it works for IPv4 but truncates an IPv6 address to `[2001`.

Prefer bounded dimensions such as the workload identity, the destination host, or the request method. Avoid `request.path` unless the path set is small and known; on an API with per-resource paths the series count grows without limit.

### PromQL with Custom Labels

After the labels exist, egress traffic can be broken down by workload and destination.

**Request rate by calling workload:**

```promql
sum(rate(authorized_calls[5m])) by (workload)
```

**Rate limited requests by workload and destination:**

```promql
sum(rate(limited_calls[5m])) by (workload, destination)
```

**Token consumption per workload (with TokenRateLimitPolicy):**

```promql
sum(rate(authorized_hits[5m])) by (workload)
```

**Which workloads are calling which external services:**

```promql
sum(rate(authorized_calls[5m])) by (workload, destination)
```

### Known Limitations

**A label expression referencing an unavailable attribute silently stops the metrics.** An expression such as `response.code` is not dropped. Evaluating the Limitador descriptor fails, the gateway logs the following for every request, and no metrics are recorded for the affected route:

```text
Failed to evaluate message builder: CelError::Resolve { UndeclaredReference("response") }
```

Whether the request itself still succeeds depends on the failure mode of the services on the route. The rate limit services default to `failureMode: allow`, so a route carrying only a `RateLimitPolicy` keeps returning 200 while quietly losing its metrics. A route that also carries an AuthPolicy, which defaults to `failureMode: deny`, can instead fail with HTTP 500. Routes with no rate limit policy build no descriptor and are unaffected either way.

The policy still reports `Accepted=True` and `Enforced=True`, so check both traffic and metrics after applying a new label. Tracked in [#2242](https://github.com/Kuadrant/kuadrant-operator/issues/2242).

**Removing or renaming a label in the spec does not remove it.** Editing a TelemetryPolicy leaves the old binding in the generated configuration, so editing cannot undo a bad label. Bindings accumulate across every edit, and because a single failing expression discards the whole descriptor, one stale label is enough to stop all the metrics for the route. Delete the policy and reapply it instead:

```sh
kubectl delete telemetrypolicy egress-telemetry -n gateway-system
```

Then reapply the policy as shown in [Attributing Egress Traffic to the Calling Workload](#attributing-egress-traffic-to-the-calling-workload). Tracked in [#2243](https://github.com/Kuadrant/kuadrant-operator/issues/2243).

## Access Logging

### Enabling Access Logs

Enable access logs on the egress gateway using the Istio Telemetry API. A Telemetry resource in the gateway namespace applies to all proxies in that namespace, including the egress gateway:

```yaml
apiVersion: telemetry.istio.io/v1
kind: Telemetry
metadata:
  name: egress-access-logs
  namespace: gateway-system
spec:
  accessLogging:
    - providers:
      - name: envoy
```

Access logs appear in the egress gateway pod stdout:

```sh
kubectl logs -n gateway-system -l gateway.networking.k8s.io/gateway-name=kuadrant-egressgateway -f
```

### Reading Egress Access Logs

The Envoy default log format includes fields that are particularly useful for egress troubleshooting:

```text
[2026-07-07T14:33:57.697Z] "GET /get HTTP/1.1" 200 - via_upstream - "-" 0 1172 1879 1879
  "10.244.0.18" "curl/8.21.0" "4fe1434d-fd3b-4c68-92ac-c15700dfccc7" "httpbin.org"
  "100.59.144.143:443" outbound|443||httpbin.org 10.244.0.17:40974 10.244.0.17:80
  10.244.0.18:43722 - gateway-system.httpbin-external.0
```

Each access log entry captures both connection legs of the egress path: the incoming connection from the workload pod to the gateway (downstream), and the outgoing connection from the gateway to the external service (upstream). Key fields for egress:

| Field | Format Variable | Example Value | Egress Use |
|-------|----------------|---------------|------------|
| Response code | `%RESPONSE_CODE%` | `200` | External service response status |
| Response flags | `%RESPONSE_FLAGS%` | `-` | Envoy-level error indicators |
| Response code details | `%RESPONSE_CODE_DETAILS%` | `via_upstream` | Where the response came from |
| Duration (ms) | `%DURATION%` | `1879` | Total request time including external service |
| Upstream service time (ms) | `%RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)%` | `1879` | External service response time |
| X-Forwarded-For | `%REQ(X-FORWARDED-FOR)%` | `10.244.0.18` | Client IP (same as downstream for egress) |
| Request ID | `%REQ(X-REQUEST-ID)%` | `4fe1434d-...` | Correlation across components |
| Authority (Host) | `%REQ(:AUTHORITY)%` | `httpbin.org` | Destination hostname after rewrite |
| Upstream host | `%UPSTREAM_HOST%` | `100.59.144.143:443` | Resolved external IP address |
| Upstream cluster | `%UPSTREAM_CLUSTER%` | `outbound\|443\|\|httpbin.org` | Routing destination |
| Downstream remote address | `%DOWNSTREAM_REMOTE_ADDRESS%` | `10.244.0.18:43722` | Source workload pod IP and port |
| Route name | `%ROUTE_NAME%` | `gateway-system.httpbin-external.0` | Which HTTPRoute matched |

### Troubleshooting with Access Logs

**External service errors**

When the external service returns an error (for example, 503), the access log shows:

```text
[...] "GET /get HTTP/1.1" 503 - via_upstream - "-" 0 162 326 326
  "10.244.0.18" "curl/8.21.0" "e0d56588-..." "httpbin.org"
  "32.193.74.35:443" outbound|443||httpbin.org ...
```

- `response_code=503` with `response_flags=-` and `response_code_details=via_upstream` means the 503 came from the external service, not the gateway.
- `upstream_host=32.193.74.35:443` shows which IP served the response, which is useful when the external service has multiple backends.

To dig deeper, query the Envoy admin endpoint to see per-endpoint connection and request statistics for the upstream cluster:

```sh
EGRESS_POD=$(kubectl get pods -n gateway-system \
    -l gateway.networking.k8s.io/gateway-name=kuadrant-egressgateway \
    -o jsonpath='{.items[0].metadata.name}')

kubectl exec -n gateway-system $EGRESS_POD -- \
    pilot-agent request GET /clusters 2>/dev/null | grep "outbound|443||httpbin.org::" | \
    grep -E "(cx_total|cx_connect_fail|rq_total|rq_error|rq_success|rq_timeout|health_flags)"
```

Example output:

```text
outbound|443||httpbin.org::54.156.228.155:443::cx_total::1
outbound|443||httpbin.org::54.156.228.155:443::cx_connect_fail::0
outbound|443||httpbin.org::54.156.228.155:443::rq_total::1
outbound|443||httpbin.org::54.156.228.155:443::rq_error::1
outbound|443||httpbin.org::54.156.228.155:443::rq_success::0
outbound|443||httpbin.org::54.156.228.155:443::health_flags::healthy
outbound|443||httpbin.org::54.205.27.0:443::cx_total::1
outbound|443||httpbin.org::54.205.27.0:443::cx_connect_fail::0
outbound|443||httpbin.org::54.205.27.0:443::rq_total::1
outbound|443||httpbin.org::54.205.27.0:443::rq_error::1
outbound|443||httpbin.org::54.205.27.0:443::rq_success::0
outbound|443||httpbin.org::54.205.27.0:443::health_flags::healthy
```

This shows each resolved IP for the external service with its connection count (`cx_total`), connection failures (`cx_connect_fail`), request successes and errors (`rq_success`, `rq_error`), and health status. In this example, both IPs accepted connections (`cx_connect_fail::0`) but returned errors (`rq_error::1`), confirming the issue is the external service, not connectivity.

**No route configured for a destination**

When a workload sends traffic to a hostname with no matching HTTPRoute:

```text
[...] "GET /get HTTP/1.1" 404 NR route_not_found - "-" 0 0 0 -
  "10.244.0.18" "curl/8.21.0" "a326cb9d-..." "unknown-api.example.com"
  "-" - - 10.244.0.17:80 10.244.0.18:41290 - -
```

- `response_flags=NR` (No Route): no HTTPRoute matched the request.
- `response_code_details=route_not_found`: confirms the issue is routing, not the external service.
- `upstream_host=-`: no upstream was selected.
- `authority=unknown-api.example.com`: shows which hostname the workload tried to reach.

This indicates that no HTTPRoute matched the requested hostname. Check that an HTTPRoute exists for this destination, and if the external hostname also needs a ServiceEntry to be routable.

**Isolating gateway latency from external service latency**

The access log contains two timing fields that together show where time is being spent:

- **Duration** (position 10): total time from when the gateway received the request to when the response was sent back to the workload.
- **Upstream service time** (position 11): time the external service took to respond.

The difference between these two values approximates non-upstream latency, which includes proxy processing, TLS handshake, policy evaluation, network transit, and queueing. For example:

```text
[...] "GET /get HTTP/1.1" 200 - via_upstream - "-" 0 1172 1879 1650 ...
```

Here the total duration is 1,879 ms and the upstream service time is 1,650 ms, so approximately 229 ms was spent outside the external service (TLS origination, proxy processing, network). A large and persistent gap may indicate gateway-side issues such as policy evaluation latency, connection pool exhaustion, or network problems. When both values are close, most of the time is spent waiting for the external service to respond.

**Client timeout (downstream disconnect)**

When the calling workload times out before receiving a response:

- `response_code=0` with `response_flags=DC` (Downstream Connection termination).
- This means the client gave up before the external service responded. Investigate whether the external service is slow or the client timeout is too short.

### Response Flags Reference

Response flags in access logs indicate where and why a request failed. Flags most relevant to egress:

| Flag | Meaning | Egress Interpretation |
|------|---------|----------------------|
| `-` | No flags | Request completed normally (success or external service error) |
| `NR` | No route found | No HTTPRoute matches the destination hostname |
| `UH` | No healthy upstream | DNS resolution succeeded but no healthy endpoints |
| `UF` | Upstream connection failure | Gateway could not connect to the external service |
| `UT` | Upstream request timeout | External service did not respond within the configured timeout |
| `DC` | Downstream connection termination | Calling workload disconnected before response arrived |
| `URX` | Upstream retry limit exceeded | All retry attempts to the external service failed |
| `UPE` | Upstream protocol error | Protocol mismatch (for example, expecting HTTP/2 but got HTTP/1.1) |

The distinction between `response_flags=-` and other flags is critical: a 503 with flags `-` means the external service returned 503. A 503 with `UF` means the gateway could not reach the external service at all.

### Filtering Access Logs

To reduce log volume, filter access logs to only capture errors. Use `!has(response.code)` to also capture connection failures where no HTTP response code is generated (for example, `UF`, `UH`, `UT` flags):

```yaml
apiVersion: telemetry.istio.io/v1
kind: Telemetry
metadata:
  name: egress-access-logs
  namespace: gateway-system
spec:
  accessLogging:
    - providers:
      - name: envoy
      filter:
        expression: "!has(response.code) || response.code >= 400"
```

Other useful filters:

```yaml
# Only log requests to a specific destination
filter:
  expression: 'request.host == "api.openai.com"'

# Exclude health checks
filter:
  expression: '!request.url_path.startsWith("/healthz")'
```

### JSON-Formatted Access Logs

For integration with log aggregation systems (Loki, Elasticsearch), configure JSON access logs via the Istio mesh configuration. See the [Envoy Access Logs guide](../../observability/envoy-access-logs.md#structured-logging-json-format) for setup instructions.

A recommended JSON format for egress includes these egress-relevant fields:

```json
{
  "start_time": "%START_TIME%",
  "method": "%REQ(:METHOD)%",
  "path": "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%",
  "protocol": "%PROTOCOL%",
  "response_code": "%RESPONSE_CODE%",
  "response_flags": "%RESPONSE_FLAGS%",
  "response_code_details": "%RESPONSE_CODE_DETAILS%",
  "bytes_received": "%BYTES_RECEIVED%",
  "bytes_sent": "%BYTES_SENT%",
  "duration": "%DURATION%",
  "upstream_service_time": "%RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)%",
  "downstream_remote_address": "%DOWNSTREAM_REMOTE_ADDRESS%",
  "request_id": "%REQ(X-REQUEST-ID)%",
  "authority": "%REQ(:AUTHORITY)%",
  "upstream_host": "%UPSTREAM_HOST%",
  "upstream_cluster": "%UPSTREAM_CLUSTER%",
  "route_name": "%ROUTE_NAME%"
}
```

## Ensuring Prometheus Scrapes the Egress Gateway

Istio gateway pods expose metrics on port 15020 (`/stats/prometheus`). If you deployed the observability stack using the [observability guide](../../observability/README.md) and applied the Istio service monitors, Prometheus is already scraping the egress gateway pod.

Verify that the egress gateway pod annotations include Prometheus scrape configuration:

```sh
kubectl get pods -n gateway-system \
    -l gateway.networking.k8s.io/gateway-name=kuadrant-egressgateway \
    -o jsonpath='{.items[0].metadata.annotations}' | python3 -m json.tool
```

If Prometheus uses annotation-based discovery, verify that the pod has `prometheus.io/scrape: "true"` and `prometheus.io/port: "15020"`. If it uses ServiceMonitor-based discovery, ensure that a ServiceMonitor selects the egress gateway Kubernetes Service.

## Next Steps

- [Distributed tracing for egress](../../observability/tracing.md): trace requests end-to-end from workload through the egress gateway to the external service
- [TelemetryPolicy](../../overviews/telemetrypolicy.md): the full policy API behind the [custom metric labels](#custom-metric-labels-with-telemetrypolicy) described above
- [TokenRateLimitPolicy](../../overviews/rate-limiting.md): cap AI inference costs by token consumption per workload

## References

- [Egress Gateway Setup](egress-gateway.md)
- [Istio Standard Metrics](https://istio.io/latest/docs/reference/config/metrics/)
- [Istio Telemetry API](https://istio.io/latest/docs/reference/config/telemetry/)
- [Envoy Access Log Format](https://www.envoyproxy.io/docs/envoy/latest/configuration/observability/access_log/usage#format-strings)
- [Kuadrant Observability Stack](../../observability/README.md)
- [Kuadrant Metrics Reference](../../observability/metrics.md)
- [Limitador Metrics User Guide](../observability/limitador-metrics.md)
- [TelemetryPolicy CRD Reference](../../reference/telemetrypolicy.md)
- [Token Metrics User Guide](../observability/token-metrics.md)
- [Envoy Access Logs and Request Correlation](../../observability/envoy-access-logs.md)
