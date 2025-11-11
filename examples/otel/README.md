# OpenTelemetry for Kuadrant Operator

This example demonstrates how to enable OpenTelemetry logging and metrics export from the Kuadrant Operator.

## Features

- **Dual Logging**: Logs to both console (Zap) and remote collector (OTLP)
- **Metrics Bridge**: Export existing Prometheus metrics via OTLP without code changes
- **Unified Configuration**: Single `OTEL_ENABLED` switch for all signals
- **Local Development Stack**: Complete observability setup with Docker Compose

## Architecture

```
┌─────────────────────────────────────────┐
│     Kuadrant Operator                   │
│                                         │
│  ┌──────────────────────────────────┐   │
│  │  Zap Logger                      │   │
│  │  • Console output (LOG_LEVEL)    │   │
│  └──────────┬───────────────────────┘   │
│             │                           │
│  ┌──────────▼───────────────────────┐   │
│  │  OTel Logger + Zap Exporter      │   │
│  │  (dual destination)              │   │
│  └──────────┬───────────────────────┘   │
│             │                           │
│  ┌──────────────────────────────────┐   │
│  │  Prometheus Metrics              │   │
│  │  • controller_runtime_*          │   │
│  │  • kuadrant_dns_policy_ready     │   │
│  └──────────┬───────────────────────┘   │
│             │                           │
│  ┌──────────▼───────────────────────┐   │
│  │  OTel Prometheus Bridge          │   │
│  │  (zero code changes)             │   │
│  └──────────┬───────────────────────┘   │
│             │                           │
└─────────────┼───────────────────────────┘
              │ OTLP/HTTP
              │
    ┌─────────▼──────────┐
    │  OTel Collector    │
    │  • Logs pipeline   │
    │  • Metrics pipeline│
    └─────────┬──────────┘
              │
      ┌───────┴────────┐
      │                │
   Loki/ES        Prometheus
   (Logs)         (Metrics)
```

## Quick Start

### 1. Start Observability Stack

```bash
docker compose -f examples/otel/docker-compose.yaml up -d
```

This starts:
- **OTel Collector** - Receives OTLP logs and metrics on ports 4317 (gRPC) and 4318 (HTTP)
- **Prometheus** - Stores and queries metrics on port 9090

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

**View logs in collector:**

```bash
docker logs -f kuadrant-otel-collector
```

You should see log entries being received and processed.

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

## How It Works

### Dual Logging

When OTel is enabled, logs are sent to **both** destinations:
- **Console (stdout)** - Zap logger respects `LOG_LEVEL` and `LOG_MODE` for readable local output
- **Remote (OTLP)** - OTel exporter sends all logs to collector for observability backends

Both destinations receive the same log records. The Zap exporter provides formatted console output, while the OTLP exporter sends structured logs with trace correlation.

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

| Variable                              | Required | Default             | Description                                        |
|---------------------------------------|----------|---------------------|----------------------------------------------------|
| `OTEL_ENABLED`                        | No       | `false`             | Enable OpenTelemetry (logs, traces, metrics)       |
| `OTEL_EXPORTER_OTLP_ENDPOINT`         | Yes*     | `localhost:4318`    | OTLP collector endpoint (default for all signals)  |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`    | No       | -                   | Override endpoint specifically for logs            |
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | No       | -                   | Override endpoint specifically for metrics         |
| `OTEL_EXPORTER_OTLP_INSECURE`         | No       | `true`              | Disable TLS for OTLP export (for local dev)        |
| `OTEL_SERVICE_NAME`                   | No       | `kuadrant-operator` | Service name                                       |
| `OTEL_SERVICE_VERSION`                | No       | Build version       | Service version (defaults to version from ldflags) |

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

    metrics:
      receivers: [otlp]
      processors: [batch, resource]
      exporters: [debug, prometheus, otlphttp]  # Export metrics to multiple backends
```

## Cleanup

```bash
docker compose -f examples/otel/docker-compose.yaml down
```
