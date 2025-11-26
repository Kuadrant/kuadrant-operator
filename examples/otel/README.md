# OpenTelemetry for Kuadrant Operator

This example demonstrates how to enable OpenTelemetry logging, tracing, and metrics export from the Kuadrant Operator.

## Features

- **Dual Logging**: Logs to both console (Zap) and remote collector (OTLP) with automatic trace correlation
- **Trace Correlation**: Logs include `trace_id` and `span_id` for distributed tracing
- **Metrics Bridge**: Export existing Prometheus metrics via OTLP without code changes
- **Unified Configuration**: Single `OTEL_ENABLED` switch for all signals
- **Local Development Stack**: Complete observability stack (Loki, Grafana, Tempo, Prometheus) with Docker Compose

## Architecture

```
┌─────────────────────────────────────────┐
│     Kuadrant Operator                   │
│                                         │
│  ┌──────────────────────────────────┐   │
│  │  Zap Logger (Tee Core)           │   │
│  │  ┌─────────────┬──────────────┐  │   │
│  │  │ Console Core│ OTel Core    │  │   │
│  │  │ (formatted) │ (otelzap     │  │   │
│  │  │             │  bridge)     │  │   │
│  │  └──────┬──────┴──────┬───────┘  │   │
│  └─────────┼─────────────┼──────────┘   │
│         stdout      OTLP (logs)         │
│                          │              │
│  ┌──────────────────────┼──────────┐    │
│  │  Prometheus Metrics  │          │    │
│  │  • controller_runtime_*         │    │
│  │  • kuadrant_dns_policy_ready    │    │
│  └──────────┬───────────┼──────────┘    │
│             │           │               │
│  ┌──────────▼───────────┼──────────┐    │
│  │  OTel Prometheus Bridge         │    │
│  │  (zero code changes)            │    │
│  └──────────┬───────────┼──────────┘    │
│             │           │               │
└─────────────┼───────────┼───────────────┘
              │ OTLP (metrics)
              │           │
    ┌─────────▼───────────▼──────────┐
    │  OTel Collector                │
    │  • Logs pipeline               │
    │  • Traces pipeline             │
    │  • Metrics pipeline            │
    └─────────┬──────────────────────┘
              │
      ┌───────┴──────────────────┐
      │                          │
  ┌───▼────┐  ┌────▼─────┐  ┌───▼────────┐
  │  Loki  │  │  Tempo   │  │ Prometheus │
  │ (Logs) │  │ (Traces) │  │ (Metrics)  │
  └────────┘  └──────────┘  └────────────┘
      │            │              │
      └────────────┴──────────────┘
                   │
            ┌──────▼──────┐
            │   Grafana   │
            │ (Dashboards)│
            └─────────────┘
```

## Quick Start

### 1. Start Observability Stack

```bash
docker compose -f examples/otel/docker-compose.yaml up -d
```

This starts:
- **OTel Collector** - Receives OTLP logs, traces, and metrics on ports 4317 (gRPC) and 4318 (HTTP)
- **Loki** - Stores logs with full-text search and label filtering on port 3100
- **Tempo** - Distributed tracing backend on port 3200
- **Jaeger** - Alternative distributed tracing UI on port 16686
- **Prometheus** - Stores and queries metrics on port 9090
- **Grafana** - Unified observability UI on port 3000 (admin/admin)

### 2. Run Operator with OpenTelemetry Enabled

```bash
# Set environment variables
export OTEL_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318
export OTEL_METRICS_INTERVAL_SECONDS=5

# Run the operator
make run
```

### 3. Verify Logs

**View logs in Loki via Grafana:**

```bash
# Open Grafana
open http://localhost:3000  # Login: admin/admin

# Navigate to Explore → Loki
# Query: {service_name="kuadrant-operator"}
```

**View logs in OTel Collector debug output:**

```bash
docker logs -f kuadrant-otel-collector
```

### 4. Verify Metrics

**Query metrics in Prometheus:**

```bash
# Open Prometheus UI
open http://localhost:9090

# Or query via API
curl 'http://localhost:9090/api/v1/query?query=controller_runtime_active_workers'
```

**Access operator Prometheus endpoint directly:**

```bash
curl http://localhost:8080/metrics
```

### 5. Verify Traces

**View traces in Tempo via Grafana:**

```bash
# Open Grafana
open http://localhost:3000

# Navigate to Explore → Tempo
# Search by trace ID from logs
```

**Or view in Jaeger UI:**

```bash
# Open Jaeger UI
open http://localhost:16686

# Select "kuadrant-operator" service
```

### 6. Unified Observability in Grafana

Grafana provides a unified view across all signals:

1. **Logs (Loki)**: Full-text search with label filtering
2. **Traces (Tempo)**: Distributed request tracing
3. **Metrics (Prometheus)**: Time-series metrics and dashboards
4. **Correlation**: Click trace IDs in logs to jump to traces

## How It Works

### Dual Logging with Trace Correlation

The operator uses a **Tee core architecture** powered by the official `go.opentelemetry.io/contrib/bridges/otelzap` library:

**Console Core:**
- Formats logs for human readability (JSON or console format based on `LOG_MODE`)
- Respects `LOG_LEVEL` for verbosity filtering
- Extracts and displays `trace_id` and `span_id` from context for correlation
- Filters out noisy context objects

**OTel Core (otelzap bridge):**
- Sends structured logs to OTLP collector
- Automatically extracts trace context from `context.Context` fields
- Preserves all log attributes and severity levels
- Enables correlation with traces in Tempo/Jaeger

**Usage in code:**

```go
import (
    "context"
    "github.com/kuadrant/policy-machinery/controller"
    "go.opentelemetry.io/otel"
)

func (r *MyReconciler) Reconcile(ctx context.Context) (controller.Result, error) {
    // Start a tracing span (required for trace_id/span_id)
    tracer := otel.Tracer("kuadrant-operator")
    ctx, span := tracer.Start(ctx, "MyReconciler.Reconcile")
    defer span.End()

    // Get logger from context and attach context for trace extraction
    logger := controller.LoggerFromContext(ctx).WithValues("context", ctx)

    logger.Info("reconciling resource")  // Automatically includes trace_id and span_id
    return controller.Result{}, nil
}
```

**Important Notes:**
- The tracing span **must** be started before getting the logger for trace IDs to be present
- The `tracer.Start()` call enriches the context with trace context
- The `.WithValues("context", ctx)` passes the enriched context to the logger for extraction

Both cores receive the same log records from the Tee, ensuring consistent logging across console and remote backends.

### Dual Metrics Export

The operator exposes metrics in **two ways simultaneously**:
1. **Prometheus `/metrics` endpoint** (`:8080/metrics`) - Native Prometheus scraping
2. **OTLP push** (when `OTEL_ENABLED=true`) - Push to OTel Collector

Both expose the **same underlying metrics** from the same Prometheus registry. The OTel bridge reads from the Prometheus registry and converts to OTLP format.

#### Important: Avoid Metric Duplication

When configuring Prometheus scraping, choose **one** of these options:

**Option 1 (Recommended):** Scrape via OTel Collector

```yaml
# prometheus.yaml (default in this example)
- job_name: 'kuadrant-operator'
  static_configs:
    - targets: [ 'otel-collector:8889' ]
```

✅ Use when `OTEL_ENABLED=true`
✅ Allows OTel processing/filtering before Prometheus
✅ Consistent with OTel-first approach

**Option 2:** Scrape operator directly

```yaml
# prometheus.yaml (alternative)
- job_name: 'kuadrant-operator'
  static_configs:
    - targets: [ 'host.docker.internal:8080' ]
```

✅ Use when `OTEL_ENABLED=false`
✅ Traditional Prometheus setup
✅ No OTel Collector needed

**❌ Don't scrape both** - This creates duplicate time series with different labels.

## Environment Variables

### Shared OpenTelemetry Configuration

| Variable                              | Required | Default             | Description                                          |
|---------------------------------------|----------|---------------------|------------------------------------------------------|
| `OTEL_ENABLED`                        | No       | `false`             | Enable OpenTelemetry (logs, traces, metrics)         |
| `OTEL_EXPORTER_OTLP_ENDPOINT`         | Yes*     | `localhost:4318`    | OTLP collector endpoint (default for all signals)    |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`    | No       | -                   | Override endpoint specifically for logs              |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`  | No       | -                   | Override endpoint specifically for traces            |
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | No       | -                   | Override endpoint specifically for metrics           |
| `OTEL_EXPORTER_OTLP_INSECURE`         | No       | `true`              | Disable TLS for OTLP export (for local dev)          |
| `OTEL_SERVICE_NAME`                   | No       | `kuadrant-operator` | Service name shown in Grafana/Tempo/Jaeger          |
| `OTEL_SERVICE_VERSION`                | No       | Build version       | Service version (defaults to version from ldflags)   |

\* Required when `OTEL_ENABLED=true`

### Metrics-Specific Configuration

| Variable                        | Default | Description                    |
|---------------------------------|---------|--------------------------------|
| `OTEL_METRICS_INTERVAL_SECONDS` | `15`    | Export interval in seconds     |

## Available Metrics

### Controller-Runtime Metrics

All standard controller-runtime metrics are automatically exported:

- `controller_runtime_reconcile_total` - Total reconciliations per controller
- `controller_runtime_reconcile_errors_total` - Reconciliation errors
- `controller_runtime_reconcile_time_seconds` - Reconciliation duration
- `controller_runtime_max_concurrent_reconciles` - Worker count
- `controller_runtime_active_workers` - Active workers

### Custom Kuadrant Metrics

- `kuadrant_dns_policy_ready` - DNS Policy ready status
  - Labels: `dns_policy_name`, `dns_policy_namespace`, `dns_policy_condition`

### Go Runtime Metrics

Standard Go metrics from `prometheus/client_golang`:

- `go_memstats_*` - Memory statistics
- `go_goroutines` - Number of goroutines
- `go_threads` - Number of OS threads
- `process_*` - Process metrics

## Kubernetes Deployment

Add to your operator deployment:

```yaml
env:
  - name: OTEL_ENABLED
    value: "true"
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "otel-collector.observability.svc.cluster.local:4318"
  - name: OTEL_EXPORTER_OTLP_INSECURE
    value: "false"  # Use TLS in production
  - name: OTEL_SERVICE_NAME
    value: "kuadrant-operator"
  - name: OTEL_METRICS_INTERVAL_SECONDS
    value: "60"
```

## Configuration

### OTel Collector

Edit `otel-collector-config.yaml` to add remote OTLP export:

```yaml
exporters:
  debug:
    verbosity: detailed
  prometheus:
    endpoint: "0.0.0.0:8889"

  # Add remote OTLP exporter
  otlphttp:
    endpoint: https://your-observability-backend.com
    headers:
      authorization: "Bearer <your-api-key>"

service:
  pipelines:
    logs:
      receivers: [otlp]
      processors: [batch, resource]
      exporters: [debug, otlphttp]  # Export logs to multiple backends

    traces:
      receivers: [otlp]
      processors: [batch, resource]
      exporters: [debug, otlphttp]  # Export traces to multiple backends

    metrics:
      receivers: [otlp]
      processors: [batch, resource]
      exporters: [debug, prometheus, otlphttp]  # Export metrics to multiple backends
```

## Implementation Details

### Logging Architecture

The operator uses a sophisticated logging setup that provides:

1. **Official OTel Integration**: Uses `go.opentelemetry.io/contrib/bridges/otelzap` for robust OTel support
2. **Tee Core Pattern**: Single Zap logger with two cores (console + OTel) via `zapcore.NewTee()`
3. **Trace Context Extraction**: Custom `contextFilterCore` extracts `trace_id` and `span_id` for console output
4. **Clean Console Output**: Filters noisy context objects while preserving trace correlation
5. **Zero Overhead When Disabled**: Standard Zap logger when `OTEL_ENABLED=false`

**Key Files:**
- `internal/log/otel.go` - OTel logging setup with Tee architecture and `contextFilterCore`
- `internal/log/log.go` - Standard logging setup
- `cmd/main.go` - Conditional OTel initialization based on env vars

### Trace Context Propagation

To enable trace correlation in logs, you must start a tracing span and attach the context to the logger:

```go
import "go.opentelemetry.io/otel"

// Without tracing span - no trace context
logger := controller.LoggerFromContext(ctx).WithValues("context", ctx)
logger.Info("message")  // No trace_id (no active span)

// With tracing span - full trace correlation
tracer := otel.Tracer("kuadrant-operator")
ctx, span := tracer.Start(ctx, "MyOperation")
defer span.End()

logger := controller.LoggerFromContext(ctx).WithValues("context", ctx)
logger.Info("message")  // Includes trace_id and span_id
```

The `contextFilterCore` in `internal/log/otel.go` handles the context field differently for each core:
- **Console core**: Extracts `trace_id` and `span_id` as readable strings, filters out noisy context object
- **OTel core**: Uses official otelzap bridge to include full trace context in OTLP records

## Cleanup

```bash
docker compose -f examples/otel/docker-compose.yaml down
```
