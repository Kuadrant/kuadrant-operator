# OpenTelemetry Logging - Quick Start

Get OpenTelemetry logging running in under 5 minutes.

**Dual Logging Mode:** When OTel is enabled, logs are sent to **both** destinations:
- **Console (stdout)** - Zap logger respects `LOG_LEVEL` and `LOG_MODE` for readable local output
- **Remote (OTLP)** - OTel exporter sends all logs to collector for observability backends

## Prerequisites

- Docker installed
- Make and Go installed

## Quick Start

### 1. Start OTel Collector

From the repository root:

```bash
docker compose -f examples/otel/docker-compose.otel.yaml up -d
```

### 2. Run Operator with OTel Logging

```bash
export OTEL_LOGS_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4319
export OTEL_SERVICE_NAME=kuadrant-operator
make run
```

### 3. View Logs

In another terminal:

```bash
docker logs -f otel-collector
```

You should see log entries being received and processed.

## Environment Variables

| Variable                      | Required | Default             | Description                                        |
|-------------------------------|----------|---------------------|----------------------------------------------------|
| `OTEL_LOGS_ENABLED`           | No       | `false`             | Enable OpenTelemetry logging                       |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Yes*     | -                   | OTLP collector endpoint                            |
| `OTEL_SERVICE_NAME`           | No       | `kuadrant-operator` | Service name                                       |
| `OTEL_SERVICE_VERSION`        | No       | Build version       | Service version (defaults to version from ldflags) |

\* Required when `OTEL_LOGS_ENABLED=true`

## Kubernetes Deployment

Add to your operator deployment:

```yaml
env:
  - name: OTEL_LOGS_ENABLED
    value: "true"
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "otel-collector.observability.svc.cluster.local:4318"
  - name: OTEL_SERVICE_NAME
    value: "kuadrant-operator"
```

## Cleanup

```bash
docker compose -f examples/otel/docker-compose.otel.yaml down
```
