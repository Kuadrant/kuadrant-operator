# Control Plane Tracing API — Design Doc

**Issue:** [#1750 feat: control plane tracing API](https://github.com/Kuadrant/kuadrant-operator/issues/1750)
**PoC PR:** [#1772 feat: runtime trace provider enablement via config map](https://github.com/Kuadrant/kuadrant-operator/pull/1772)
**Status:** In Progress

---

## Overview

Kuadrant operators use OpenTelemetry (OTEL) for control-plane reconciliation traces. Currently, **kuadrant-operator** and **limitador-operator** emit traces; **authorino-operator** and **dns-operator** do not yet instrument their control-plane reconciliation. For operators that do emit traces, the endpoint is configured only via environment variables set at pod startup, so reconfiguring tracing requires a pod restart and, in OLM-managed environments, risks being overwritten by OLM reconciliation.

This design introduces a **runtime tracing API** that allows operators and users to enable, disable, or reconfigure distributed tracing **without restarting pods**, using a well-known Kubernetes ConfigMap (`kuadrant-tracing`) as the configuration surface. The same API is designed to be adopted by authorino-operator and dns-operator once they add OTEL instrumentation.

---

## Goals

- Enable/disable and reconfigure control-plane trace exports at runtime (no pod restart).
- Provide a single control point shared by all Kuadrant control-plane operators.
- Require no new CRDs, keeping adoption lightweight and standalone-deployment-friendly.
- Maintain backwards compatibility with existing env var configuration.

## Non-Goals

- Configuring **Authorino** (the proxy service) tracing — Authorino is both control-plane and data-plane; its tracing is controlled via the Authorino CR and is out of scope for this API. This does not preclude authorino-operator (the Kubernetes controller process) from adopting the `kuadrant-tracing` ConfigMap for its own control-plane traces.
- Per-operator trace endpoint configuration (single shared endpoint is sufficient for v1).
- Log or metric export configuration (possible future extension, tracked via open questions).
- Enabling/disabling tracing per individual Kuadrant operator (all operators sharing the ConfigMap all respond to it uniformly in v1).

---

## Design

### API: The `kuadrant-tracing` ConfigMap

A Kubernetes `ConfigMap` named `kuadrant-tracing` in the operator namespace serves as the runtime tracing configuration. All Kuadrant operators are assumed to be deployed cluster-wide in the same namespace (e.g. `kuadrant-system`), so a single ConfigMap configures all of them simultaneously.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kuadrant-tracing
  namespace: kuadrant-system   # must match the operator's namespace
data:
  endpoint: "http://otel-collector:4318"   # OTLP exporter URL
  insecure: "true"                          # optional, defaults to false
```

**Supported URL schemes for `endpoint`:**

| Scheme     | Transport | Notes                              |
|------------|-----------|------------------------------------|
| `grpc://`  | gRPC      | e.g. `grpc://otel-collector:4317` — canonical form |
| `rpc://`   | gRPC      | alias for `grpc://`; kept for parity with Authorino's tracing convention |
| `http://`  | HTTP      | always insecure                    |
| `https://` | HTTP      | TLS; set `insecure: "true"` to skip verify |

**Behaviour:**

| ConfigMap state                          | Effect                                      |
|------------------------------------------|---------------------------------------------|
| ConfigMap created or updated             | Trace provider hot-swapped to new endpoint  |
| ConfigMap deleted                        | Reverts to env var endpoint (or noop)       |
| ConfigMap exists but `endpoint` is empty | Reverts to env var endpoint (or noop)       |

### Configuration Precedence

```
ConfigMap endpoint > Env var endpoint > Disabled (noop)
```

Env vars (`TRACING_ENDPOINT`, `TRACING_INSECURE`) remain supported as a fallback for backwards compatibility and for operator upgrade scenarios where the ConfigMap has not yet been applied.

### Key Components

#### `internal/trace.DynamicProvider`

Wraps a `Provider` (OTLP tracer provider) and supports runtime reconfiguration without restarting the operator. The underlying exporter is hot-swapped by replacing the global OTEL tracer provider (`otel.SetTracerProvider`).

```
DynamicProvider
  ├── Reconfigure(ctx, endpoint, insecure) → hot-swaps provider
  ├── RevertToFallback(ctx)               → reverts to env var config
  ├── Shutdown(ctx)                       → graceful flush + shutdown
  └── GlobalTracer(name) → globalTracerProxy
          └── delegates to otel.Tracer() on every Start()
                          ↑
              always reflects the current global provider
```

The `globalTracerProxy` is critical: controllers cache a `Tracer` instance at startup. Without the proxy, spans created after a hot-swap would still go to the old provider. The proxy always calls `otel.Tracer(name)` at span-start time, so it transparently picks up the swapped provider.

#### `internal/controller.TracingConfigMapReconciler`

A policy-machinery reconciler that subscribes to `Create/Update/Delete` events on the `kuadrant-tracing` ConfigMap in the operator namespace. On each event it calls `DynamicProvider.Reconfigure` or `DynamicProvider.RevertToFallback` as appropriate.

#### RBAC

Each adopting operator requires `get`, `watch`, and `list` permissions on `ConfigMaps` in its namespace to observe the `kuadrant-tracing` ConfigMap. These must be added to the operator's `ClusterRole` (or `Role`) manifest.

#### `cmd/main.go` changes

- `DynamicProvider` is created at startup (non-fatal if the initial endpoint is unreachable).
- The global propagator (`TraceContext` + `Baggage`) is set unconditionally so it is in place when tracing is later enabled via ConfigMap.
- `GlobalTracer` is passed to the controller via `controller.WithTracer`.
- `DynamicProvider.Shutdown` is deferred for graceful flush on exit.
- `TracingConfigMapReconciler` is registered as a workflow task in `state_of_the_world.go`.

### Sequence Diagram

```
User                   k8s API          TracingConfigMapReconciler   DynamicProvider
 |                        |                       |                        |
 |-- kubectl apply CM --> |                       |                        |
 |                        |-- watch event ------> |                        |
 |                        |                       |-- Reconfigure(ep) ---> |
 |                        |                       |                   hot-swap provider
 |                        |                       |                        |
 |-- kubectl delete CM -> |                       |                        |
 |                        |-- watch event ------> |                        |
 |                        |                       |-- RevertToFallback --> |
 |                        |                       |                   env var or noop
```

---

## Open Questions

1. **Per-operator control and endpoints** — Should the API allow enabling/disabling tracing or using a different OTLP endpoint per Kuadrant operator independently? The current design applies uniformly to all operators watching the ConfigMap. Could be extended with per-operator keys (e.g. `kuadrant-operator.endpoint`, `dns-operator.endpoint`). A single shared endpoint is simpler but less flexible for multi-tenant observability stacks.

2. **Log and metric export** — Should `kuadrant-tracing` be renamed/extended to `kuadrant-observability` in the future to also control log (OTLP log exporter) and metric (OTLP metric exporter) endpoints? This would be a backwards-compatible extension (new keys).

---

## Implementation Plan

- [x] Write design doc and get alignment on open questions
- [ ] Implement `DynamicProvider` and `TracingConfigMapReconciler` in kuadrant-operator
  - [x] `internal/trace/dynamic_provider.go` — `DynamicProvider` + `globalTracerProxy`
  - [x] `internal/controller/tracing_configmap_reconciler.go`
  - [x] Wire into `cmd/main.go` and `state_of_the_world.go`
  - [ ] Add `grpc://` as canonical gRPC scheme alongside `rpc://` alias in `internal/trace/provider.go`
  - [ ] Unit tests for `DynamicProvider`
  - [ ] Unit tests for `TracingConfigMapReconciler`
  - [ ] Integration tests: create ConfigMap, verify tracing enabled; delete ConfigMap, verify revert to env var config
- [ ] Adopt same `kuadrant-tracing` ConfigMap convention in limitador-operator
- [ ] Adopt same `kuadrant-tracing` ConfigMap convention in dns-operator (requires adding OTEL instrumentation first)
- [ ] Adopt same `kuadrant-tracing` ConfigMap convention in authorino-operator (requires adding OTEL instrumentation first)
- [ ] Update documentation / user guides

---

## Alternatives Considered

| Approach | Reason rejected |
|----------|-----------------|
| Env vars only | Requires pod restart to reconfigure; in OLM environments, setting env vars via `Subscription spec.config.env` or CSV patches risks being overwritten on upgrades or reinstalls |
| Extend `Kuadrant` CR | CR manages Authorino/Limitador instances, not operator configuration itself |
| New CRD | Other operators would need to watch it, coupling them together and complicating standalone deployments |
| Patch operator Deployments or OLM `Subscription spec.config.env` | Requires a pod restart; Deployment patches are overwritten by OLM reconciliation; Subscription config can be overwritten on upgrades or by GitOps tooling; neither works in non-OLM deployments |

---

## References

- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/languages/go/)
- [Authorino tracing configuration](https://github.com/Kuadrant/authorino/blob/main/docs/user-guides/tracing.md)
- [Issue #1750](https://github.com/Kuadrant/kuadrant-operator/issues/1750)
- [PoC PR #1772](https://github.com/Kuadrant/kuadrant-operator/pull/1772)

---

## Change Log

| Date       | Author | Notes                                                                     |
|------------|--------|---------------------------------------------------------------------------|
| 2026-03-11 | KevFan | Initial design captured from issue #1750 comment + PoC PR #1772 analysis |
| 2026-03-13 | KevFan | Clarified authorino-operator vs Authorino proxy distinction; added authorino-operator and dns-operator as adoption targets (pending OTEL instrumentation); added `grpc://` as canonical gRPC scheme with `rpc://` as alias; clarified namespace assumption; added RBAC note; merged overlapping open questions |
