# Comparison: two PoCs for the EnvoyFilter/wasm config size problem

Both PoCs target the same problem (`kuadrant-operator` issue #2051,
RHOAIENG-95277/CONNLINK-1806): the Istio `EnvoyFilter`'s inline wasm
`PluginConfig.Configuration` grows with every HTTPRoute rule/match and every
TokenRateLimitPolicy limit, hitting etcd's object-size ceiling at MaaS
scale. Each PoC's own README is the authoritative, self-contained source for
its design, implementation, and findings:

- **`poc-extensions-endpoint` branch** (`poc/extensions-endpoint/README.md`):
  the wasm module itself fetches its configuration from the operator over a
  gRPC call it issues.
- **`poc-ecds-server` branch** (`poc/ecds-server/README.md`): the operator
  runs its own Envoy ExtensionConfigDiscoveryService (ECDS) server, so
  Envoy's own xDS client fetches and hot-swaps the wasm filter's config
  natively.

This document exists only to compare the two side by side - it doesn't
repeat either PoC's full implementation detail or evidence trail; see the
branch-specific READMEs for that.

## Architecture

| | `poc-extensions-endpoint` | `poc-ecds-server` |
|---|---|---|
| Who initiates config fetch | wasm module (`dispatch_grpc_call`) | Envoy's own xDS client (native ECDS) |
| New server component | None - reuses the operator's existing extensions gRPC server/port | New minimal ECDS gRPC server (`internal/ecds/`), new port/Service |
| New cluster patch required | None - reuses the existing `kuadrant-operator-grpc` CLUSTER patch (made unconditional for this PoC) | New `kuadrant-ecds` cluster, but **cannot** be added via a normal `EnvoyFilter` CLUSTER patch (see "Config delivery" below) |
| wasm-shim code changes | Yes - new `RemoteConfigFetcher` state machine, new proto, new field on `PluginConfiguration` | None - `on_configure` doesn't know or care how Envoy obtained the bytes |
| Operator code changes | New proto/RPC, new config cache (`internal/wasm/config_store.go`), reconciler changes | New `internal/ecds/` package, reconciler changes, new deployment plumbing |

## Config delivery mechanism - the key structural difference

`poc-extensions-endpoint`'s `dispatch_grpc_call` is a direct proxy-wasm host
call referencing a cluster by name - it bypasses Envoy's own xDS subsystem
entirely. `poc-ecds-server`'s ECDS fetch is itself an xDS subscription, which
means it inherits Envoy's own xDS validation rules - specifically, that an
`ApiConfigSource`'s target cluster must be **statically defined in the
proxy's bootstrap**, not added dynamically via CDS (which is exactly what an
`EnvoyFilter` CLUSTER patch produces). This is why `poc-ecds-server` needed
real additional research (`Gateway.spec.infrastructure.parametersRef`) to
get its cluster a legitimate static presence, while
`poc-extensions-endpoint` could just reuse an existing CLUSTER patch with no
special handling. This is the single biggest reason the two PoCs reached
working end-to-end state via different paths and different amounts of
unexpected work.

## EnvoyFilter size result

Both PoCs solve the literal size problem, to a similar degree:

| | `poc-extensions-endpoint` | `poc-ecds-server` |
|---|---|---|
| EnvoyFilter compact JSON size | 2,834 bytes | 2,937 bytes |
| `tlrp-test` baseline (inline config, same scenario) | 64,641 bytes | 64,641 bytes |
| Reduction | ~96% | ~95% |
| Size independent of policy/route count | Yes | Yes |

## Live config-update capability

This is where the two PoCs diverge sharply:

- **`poc-extensions-endpoint`**: confirmed live that editing a TRLP
  propagates to an already-running gateway within seconds, with **zero pod
  restart** - a digest of the real config is embedded in the bootstrap
  stand-in, so Envoy's own EnvoyFilter diffing re-triggers `on_configure`,
  which re-fetches. Not a native xDS push like ECDS's, but reaches the same
  outcome via a different path.
- **`poc-ecds-server`**: confirmed live that editing a TRLP's counter
  expression propagates to an already-running gateway **within seconds, with
  zero pod restart** - Envoy's own xDS client detects and applies the new
  `SetSnapshot` push natively. This is a direct consequence of config
  delivery being a real xDS subscription rather than a one-shot RPC call.

## Operational footprint

| | `poc-extensions-endpoint` | `poc-ecds-server` |
|---|---|---|
| New ports/Services on the operator | None | Yes - new `ecds` container port + Service |
| New NetworkPolicy rules needed | None (reuses existing `grpc` rule) | Yes - new ingress rule for the `ecds` port (found and fixed live; see that PoC's Key findings) |
| New per-Gateway resources the operator would need to own, if fully automated | None | Two ConfigMaps per Gateway (bootstrap fragment + deployment-patch parameters) for the static-cluster workaround |
| First time the operator would write to `Gateway.spec` (not just read/attach policies) | No | Yes - if the static-cluster provisioning is automated via `parametersRef` |

## Current maturity / remaining blockers

- **`poc-extensions-endpoint`**: fully working end-to-end as committed, no
  outstanding blocker to reach this PoC's own stated scope. Known
  limitation: fail-open during the bootstrap gap (cold start or wasm-binary
  upgrade), structural to fetching config via an ad-hoc gRPC call rather
  than a native xDS resource - see its "Open follow-ups" and "Upgrade
  impact".
- **`poc-ecds-server`**: fully working end-to-end, but only after a
  still-manual step (the `parametersRef` static-cluster provisioning) that
  is not yet automated by the operator. See its "Open follow-ups" for what
  automating it would require and the open design questions that come with
  it (writing to `Gateway.spec`, owning per-Gateway ConfigMaps).

## Observability comparison

*Pending.* Each PoC's own "Observability" section is still an open
investigation (deferred by design - see each README). This section will be
filled in once both are answered, since the comparison is likely to turn on
a real structural difference: `poc-extensions-endpoint`'s fetch success/
failure is known only to the wasm module itself (the operator has no
feedback channel), whereas `poc-ecds-server`'s ECDS delivery is a real xDS
ACK/NACK exchange that `go-control-plane` already tracks per node
(`SnapshotCache.GetStatusInfo`) - whether that's usable as a practical
observability signal hasn't been investigated yet in either PoC.

## Upgrade-impact comparison

- **`poc-extensions-endpoint`**: live-tested. Migrating an already-running
  gateway from mainline to this PoC under continuous traffic never failed a
  request, but exposed a structural fail-open gap: for the few seconds
  between the new wasm VM coming up and its first successful remote-config
  fetch, policy enforcement is inactive. Root cause: `on_configure` must
  return synchronously, and the proxy-wasm ABI has no blocking network
  call, so the wasm module has no way to tell Envoy "not ready yet" -
  Envoy marks the VM ready as soon as the empty bootstrap config is
  accepted. See its "Upgrade impact" for the live numbers.
- **`poc-ecds-server`**: live-tested, gap confirmed closed. Migrating from
  mainline needs one pod replace (the ECDS cluster must be statically
  present in the proxy's bootstrap - not patchable into a running pod),
  done via a standard Kubernetes rolling update with zero failures. The
  operator upgrade itself then applied as an in-place listener update
  (same pod, no restart), because the static cluster already existed. A
  tight before/after check across a policy edit (60 requests, Limitador's
  counter +60) showed **every request was rate-limited, no gap at all** -
  not just no dropped requests. This confirms the structural prediction:
  Envoy's listener-warming waits for the ECDS push before activating a
  listener, so the old, fully-enforcing listener keeps serving until the
  real config is ready. Extra cost found: this PoC's new infrastructure
  (ECDS Service/port/NetworkPolicy) isn't picked up by a bare operator
  image swap - it needs the updated manifests applied too, or the ECDS
  fetch never succeeds (no healthy upstream).

**Bottom line**: `poc-extensions-endpoint` can upgrade with zero pod
restarts but has a brief, structural fail-open window on every cold
start/upgrade. `poc-ecds-server` cannot avoid one pod replace for this
specific migration, but has zero enforcement gap at all once the static
cluster exists - a direct consequence of ECDS being a real xDS resource
instead of a wasm module doing its own ad-hoc fetch.
