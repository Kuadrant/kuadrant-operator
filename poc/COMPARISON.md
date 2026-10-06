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

This document exists only to compare the two side by side.

## Architecture

| | `poc-extensions-endpoint` | `poc-ecds-server` |
|---|---|---|
| Who initiates config fetch | wasm module (`dispatch_grpc_call`) | Envoy's own xDS client (native ECDS) |
| New server component | None - reuses the operator's existing extensions gRPC server/port | New minimal ECDS gRPC server (`internal/ecds/`), new port/Service |
| New cluster patch required | None - reuses the existing `kuadrant-operator-grpc` CLUSTER patch | New `kuadrant-ecds` cluster |
| wasm-shim code changes | Yes - new `RemoteConfigFetcher` state machine, new proto, new `digest` field | None - `on_configure` doesn't know or care how Envoy obtained the bytes |
| Operator code changes | New proto/RPC, new config cache, reconciler changes | New `internal/ecds/` package, reconciler changes, new deployment plumbing |

## EnvoyFilter size result

| | `poc-extensions-endpoint` | `poc-ecds-server` |
|---|---|---|
| EnvoyFilter compact JSON size | 2,834 bytes | 2,937 bytes |
| Same scenario, inline config (mainline) | 64,641 bytes | 64,641 bytes |
| Reduction | ~96% | ~95% |
| Size independent of policy/route count | Yes | Yes |

## Live config-update capability

Both confirmed live, by different mechanisms:

- **`poc-extensions-endpoint`**: a digest (SHA256 of the real config) is
  embedded in the bootstrap stand-in, so its content - and the EnvoyFilter's -
  changes whenever the real config does, re-triggering `on_configure` to
  re-fetch.
- **`poc-ecds-server`**: editing a policy changes the ECDS `version_info`;
  Envoy's own xDS client detects and hot-swaps it - no wasm-side polling, no
  pod restart, no hand-rolled retry logic. Falls out of using a real xDS
  resource, not something this PoC had to build.

## Observability comparison

Both start from the same gap: today's `Enforced` policy condition is a pure
object-level check (does the EnvoyFilter object exist and match desired
state), never a data-plane check - for Istio specifically it's a hardcoded
`return true`, since Istio never populates EnvoyFilter status. Neither PoC
has fixed this yet; they differ in how hard it would be to:

- **`poc-ecds-server`**: already receives Envoy's own ACK/NACK for every
  config it pushes - confirmed live (an ACK for the pushed version arrived
  within ~20s). The raw signal exists.
- **`poc-extensions-endpoint`**: its `PluginConfigService` is a plain unary
  RPC with no acknowledgment built in - zero visibility into whether
  wasm-shim ever applied the fetched config. Closing this gap needs new
  protocol work neither PoC has today: a digest-aware fetch plus a
  purpose-built RPC for wasm-shim to report apply success/failure back -
  built from scratch, not reused from an existing protocol.

## Upgrade impact comparison

Both live-tested migrating an already-running mainline gateway under
continuous traffic, with zero dropped requests. They differ in how the
migration happens and what gap remains:

- **`poc-extensions-endpoint`**: an operator image swap alone triggers an
  in-place wasm VM swap, no pod restart. But there's a real **~6 second
  fail-open window** on every cold start and upgrade, because `on_configure`
  must return synchronously and the proxy-wasm ABI has no blocking network
  call - the wasm module has no way to tell Envoy "not ready yet." The
  fail-open window is structural: no amount of implementation maturity
  removes it without new wasm-level machinery.
- **`poc-ecds-server`**: the initial migration needs one pod replace (the
  ECDS cluster must be statically present in the proxy's bootstrap, which
  can't be patched into a running pod) - a standard Kubernetes rolling
  update (old pod keeps serving until the new one is ready), not a cost.
  The operator upgrade itself then applies in-place. Payoff: a tight
  before/after check (60 requests, Limitador's counter +60) showed **every
  request was rate-limited, no gap at all** - Envoy's listener-warming
  keeps the old, fully-enforcing listener serving until the real config is
  ready.

## Overall

Both solve the size problem to the same degree and both enforce correctly
and update without dropping traffic. `poc-extensions-endpoint` is simpler to
deploy (one image, no new infrastructure), at the cost of a recurring,
structural enforcement gap and an observability story that would need to be
built from scratch. `poc-ecds-server` needs new infrastructure, but gets a
gap-free upgrade story and a usable observability signal essentially for
free, by leaning on Envoy's own xDS machinery instead of re-implementing a
piece of it.
