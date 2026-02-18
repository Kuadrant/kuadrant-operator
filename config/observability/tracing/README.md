# Distributed Tracing with Jaeger

Enable distributed tracing across Istio and Kuadrant components for local development.

## Quick Start

```bash
# 1. Install Jaeger v2
make install-jaeger

# 2. Deploy tracing configurations
kubectl apply -k config/observability/tracing

# 3. Access Jaeger UI
make jaeger-port-forward
```

Open http://localhost:16686 to view traces.

## What Gets Configured

**Jaeger v2** (Helm chart 4.5.0)
- All-in-one deployment with in-memory storage
- OTLP endpoints: `jaeger.observability.svc.cluster.local:4317`

**Trace Sources:**
- Istio gateway (100% sampling via Telemetry API)
- Authorino (auth traces)
- Limitador (rate limit traces)
- Kuadrant Operator (reconciliation traces)
- Limitador Operator (reconciliation traces)

## Viewing Traces

1. Open Jaeger UI: http://localhost:16686
2. Select a service from the dropdown:
   - `istio-ingressgateway` - Gateway traces
   - `authorino` - Authentication traces
   - `limitador` - Rate limiting traces
   - `kuadrant-operator` - Operator control plane
   - `limitador-operator` - Limitador operator control plane
3. Click "Find Traces" to see recent activity
4. Click a trace to see the full request flow with timing breakdowns

## Verify Configuration

```bash
# Check Jaeger is running
kubectl get pods -n observability

# Verify tracing is enabled in Kuadrant CR
kubectl get kuadrant kuadrant -n kuadrant-system -o jsonpath='{.spec.observability.tracing}'
```

## Cleanup

```bash
make uninstall-jaeger
```

## Makefile Targets

```bash
make install-jaeger         # Install Jaeger v2 via Helm
make uninstall-jaeger       # Remove Jaeger
make jaeger-port-forward    # Port-forward to UI (http://localhost:16686)
make deploy-tracing         # Same as kubectl apply -k (optional)
```

## Configuration Files

This directory contains kustomize configs that get applied via `kubectl apply -k`:

- `istio-tracing.yaml` - Istio Telemetry resource (100% sampling)
- `istio-patch.yaml` - Istio CR patch for OpenTelemetry provider
- `kuadrant-tracing.yaml` - Kuadrant CR with tracing endpoint
- `kuadrant-operator-patch.yaml` - Kuadrant operator OTEL env vars
- `limitador-operator-patch.yaml` - Limitador operator OTEL env vars
- `kustomization.yaml` - Orchestrates all resources and patches
