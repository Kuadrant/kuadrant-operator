# OpenTelemetry Metrics for Kuadrant Operator

This example demonstrates how to enable OpenTelemetry metrics export from the Kuadrant Operator using the Prometheus
bridge pattern.

## Architecture

```
┌─────────────────────────────────────────┐
│     Kuadrant Operator                   │
│                                         │
│  ┌──────────────────────────────────┐   │
│  │  Prometheus Metrics:             │   │
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
              │
              │ OTLP/HTTP
              │
    ┌─────────▼──────────┐
    │  OTel Collector    │
    │  (receivers: OTLP) │
    │  (exporters: ...)  │
    └─────────┬──────────┘
              │
      ┌───────┴────────┐
      │                │
  Prometheus      Other backends
   :9090          (Cloud providers, etc.)
```

## Features

- **Zero Code Changes**: Bridge existing Prometheus metrics to OTLP
- **Dual Export**: Metrics available via both Prometheus scrape AND OTLP push
- **Controller-Runtime Metrics**: All default metrics automatically exported
- **Custom Metrics**: `kuadrant_dns_policy_ready` included automatically
- **Local Development Stack**: Complete observability setup with Docker Compose

## Quick Start

### 1. Start Observability Stack

```bash
docker-compose -f examples/otel/docker-compose.yaml up -d
```

This starts:

- **OTel Collector** - Receives OTLP metrics on ports 4317 (gRPC) and 4318 (HTTP)
- **Prometheus** - Stores and queries metrics on port 9090

### 2. Run Kuadrant Operator with OTel Metrics Enabled

```bash
# Set environment variables
export OTEL_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318
export OTEL_METRICS_INTERVAL_SECONDS=5

# Run the operator
make run
```

### 3. Verify Metrics

**View metrics in collector logs:**

```bash
docker logs -f kuadrant-otel-collector
```

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

## How It Works: Dual Export

The operator exposes metrics in **two ways simultaneously**:

1. **Prometheus `/metrics` endpoint** (`:8080/metrics`) - Native Prometheus scraping
2. **OTLP push** (when `OTEL_ENABLED=true`) - Push to OTel Collector

Both expose the **same underlying metrics** from the same Prometheus registry. The OTel bridge reads from the Prometheus
registry and converts to OTLP format.

### Important: Avoid Metric Duplication

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

## Cleanup

```bash
docker compose -f examples/otel/docker-compose.yaml down
```
