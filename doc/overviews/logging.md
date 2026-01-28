# Logging

The kuadrant operator outputs 3 levels of log messages: (from lowest to highest level)

1. `debug`
2. `info` (default)
3. `error`

`info` logging is restricted to high-level information. Actions like creating, deleteing or updating kubernetes resources will be logged with reduced details about the corresponding objects, and without any further detailed logs of the steps in between, except for errors.

Only `debug` logging will include processing details.

To configure the desired log level, set the environment variable `LOG_LEVEL` to one of the supported values listed above. Default log level is `info`.

Apart from log level, the operator can output messages to the logs in 2 different formats:

- `production` (default): each line is a parseable JSON object with properties `{"level":string, "ts":int, "msg":string, "logger":string, extra values...}`
- `development`: more human-readable outputs, extra stack traces and logging info, plus extra values output as JSON, in the format: `<timestamp-iso-8601>\t<log-level>\t<logger>\t<message>\t{extra-values-as-json}`

To configure the desired log mode, set the environment variable `LOG_MODE` to one of the supported values listed above. Default log level is `production`.

## OpenTelemetry Logging

The Kuadrant operator supports exporting logs to OpenTelemetry (OTel) collectors for integration with observability backends.

### Quick Start

For a hands-on quick start guide, see **[examples/otel/README.md](../../examples/otel/README.md)** which provides step-by-step instructions to get OTel logging running in under 5 minutes.

### Configuration

OpenTelemetry logging is disabled by default and can be enabled via environment variables:

| Environment Variable               | Description                                                                                                       | Default                      |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ---------------------------- |
| `OTEL_EXPORTER_OTLP_ENDPOINT`      | OTLP collector endpoint for all signals (logs, traces, metrics). Supports `http://`, `https://`, `rpc://` schemes | - (disabled)                |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` | OTLP logs-specific endpoint (overrides `OTEL_EXPORTER_OTLP_ENDPOINT`)                                             | -                            |
| `OTEL_EXPORTER_OTLP_INSECURE`      | Disable TLS for OTLP export                                                                                       | `false`                      |
| `OTEL_SERVICE_NAME`                | Service name for telemetry data                                                                                   | `kuadrant-operator`          |
| `OTEL_SERVICE_VERSION`             | Service version for telemetry data                                                                                | Build version (from ldflags) |

**Note:** Logging is enabled when either `OTEL_EXPORTER_OTLP_ENDPOINT` or `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` is set to a non-empty value.

### Architecture

**Dual Logging Mode:**

When OTel is enabled (by setting an endpoint), the operator uses a tee logger that writes to both destinations simultaneously:

```
Application Code (log.Log.Info(...))
        ↓
    logr.Logger (tee sink)
        ↓
    ┌───────────────────┴────────────────────┐
    ↓                                        ↓
Zap Logger                            OTel Logger
(console output)                    (remote export)
    ↓                                        ↓
stdout (respects                    OTLP HTTP → Collector
LOG_LEVEL & LOG_MODE)               (all logs with metadata)
```

**Key Features:**

- **Dual Output**: Logs go to both console (Zap) and remote collector (OTel) simultaneously
- **Separate Filtering**: Zap logger respects `LOG_LEVEL`/`LOG_MODE` for console, OTel exports everything for remote analysis
- **No Code Changes**: Controllers use standard `logr` interface; dual logging happens transparently
- **Resource Metadata**: Logs include service name/version, git SHA, Go version, and more
- **Batch Export**: Remote logs are batched and exported asynchronously to minimize performance impact
- **Graceful Shutdown**: Ensures all pending logs are flushed before termination
- **Fallback**: If OTel setup fails, continues with Zap-only logging

### Deployment Configuration

When deploying the operator, configure OpenTelemetry logging via the deployment manifest:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kuadrant-operator
spec:
  template:
    spec:
      containers:
        - name: manager
          image: quay.io/kuadrant/kuadrant-operator:latest
          env:
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "https://otel-collector.observability.svc.cluster.local:4318"
            # Or enable logs specifically (overrides global endpoint)
            # - name: OTEL_EXPORTER_OTLP_LOGS_ENDPOINT
            #   value: "http://loki-gateway.observability.svc.cluster.local:3100"
            - name: OTEL_EXPORTER_OTLP_INSECURE
              value: "false" # Use TLS in production
            - name: OTEL_SERVICE_NAME
              value: "kuadrant-operator"
            # Optional: Control console output separately
            - name: LOG_LEVEL
              value: "info" # Console shows info+, remote gets everything
            - name: LOG_MODE
              value: "production" # JSON format for console
```
