# PoC: fetch wasm plugin config from the operator via gRPC

**Status: working end-to-end on a live cluster.** Confirmed via real traffic:
Limitador's `authorized_calls` counter climbs 1:1 with requests sent through
the gateway, proving the *fetched* (real) ActionSet config is active and
enforcing. EnvoyFilter compact JSON
size measured at **2,834 bytes**, vs. **64,641 bytes** for the identical
scenario (a 19-match HTTPRoute and a single-limit TokenRateLimitPolicy) with
the full config inlined, mainline-style - a ~96% reduction, and this size is
now **constant** regardless of ActionSet/limit count, which was the whole
point. Also confirmed live: editing a policy on an already-running gateway
now takes effect **without a gateway pod restart**, via a digest-driven
re-fetch (see "Confirmed live: digest-driven config updates" in Key
findings).

## What this PoC is

One approach being prototyped for the EnvoyFilter/WasmPlugin object-size
problem (see `kuadrant-operator` issue #2051, RHOAIENG-95277/CONNLINK-1806):
the Istio `EnvoyFilter`'s inline wasm `PluginConfig.Configuration` grows with
every HTTPRoute rule/match and every TokenRateLimitPolicy limit, hitting
etcd's object-size ceiling at MaaS scale. This PoC has the **wasm module
itself fetch its configuration from the operator over a gRPC call**, keeping
the EnvoyFilter constant-size regardless of policy count.

## Scope (deliberately minimal for this first pass)

- **Istio only.** Envoy Gateway's equivalent code path
  (`buildEnvoyExtensionPolicyForGateway`,
  `internal/controller/envoy_gateway_extension_reconciler.go:615`) embeds
  the config differently and has a structurally different wasm-binary delivery
  mechanism (OCI image pull vs. Istio's HTTP `remote.http_uri`). Out of
  scope here.

## Architecture / how it works

### Operator side

Instead of embedding the full wasm plugin config inline in the EnvoyFilter,
the reconciler caches it per-Gateway (`internal/wasm/config_store.go`) and
serves it over a new RPC, `PluginConfigService.GetPluginConfig(gateway) ->
config_json`, added to the gRPC server the operator already runs for
protobuf descriptor fetching (`internal/extension/manager.go`) - same port,
same cluster, same lifecycle, no new server. The EnvoyFilter itself now only
carries a small bootstrap stand-in - empty `services`/`actionSets`, plus a
`remoteConfig` field telling the wasm module what to ask for - instead of
the real config.

`remoteConfig` also carries a `digest`: the SHA256 of the real (uncached)
config JSON, computed by `wasm.SetConfig` whenever the operator recomputes a
gateway's config. The real config itself is never embedded, but the digest
is, so the bootstrap stand-in's own JSON content - and therefore the
EnvoyFilter's content - changes whenever the real config does. That's what
turns a policy edit into a live update: Envoy already diffs EnvoyFilter
content and only re-invokes `on_configure` when it changes, so the digest
is the only "push" signal needed, no separate poll/notify mechanism.

### wasm-shim side (companion branch)

A new `RemoteConfigFetcher` detects `remoteConfig` at `on_configure`,
dispatches a `dispatch_grpc_call` to the operator's new RPC over the
already-wired `kuadrant-operator-grpc` cluster, and on response decodes the
real config and re-runs `process_config` with it, swapping in the real
`PipelineFactory`. `process_config` tracks whether a real config has ever
been applied (`has_real_config`); once it has, a later bootstrap stand-in
(a digest-driven re-configure) does not reset the pipeline back to empty -
it keeps serving the current real config while the new one is fetched in
the background, so a live policy edit doesn't reopen the fail-open window
that only exists on cold start (see "Fail-open during the bootstrap gap on
cold start" in Open follow-ups).

## Observability

A policy's `Enforced` status condition is meant to answer one question: is
this policy actually active on the data plane? That's only true if the
condition reflects real data-plane state.

**The kuadrant operator, playing the role of `PluginConfigService`, cannot
tell whether a fetch is ever applied.** `GetPluginConfig`
(`internal/extension/manager.go:701`) is a plain unary RPC: it looks up the
cached config and returns it, discarding even the request context (`_
context.Context`) - no peer identity, no logging, nothing recorded about
which gateway fetched or when. Once the response leaves the handler, the
operator has zero visibility into whether `on_configure` applied it,
rejected it, or never ran at all. `Enforced`'s own logic confirms this has
no bearing today either way - it only checks EnvoyFilter object sync, the
same `return true` stub used for every Istio gateway regardless of PoC.

This is fixable, but requires real protocol work: a digest-aware fetch (so
the operator can tell *which* version a gateway has, not just that it
fetched *something*) plus a second, purpose-built RPC for wasm-shim to
report apply success/failure back. Neither exists today, and both would
need to be designed and built from scratch.

## Upgrade impact

**Confirmed live:** migrating an already-running gateway from mainline to
this PoC under continuous traffic (400 requests, no pod restart) never
failed a single request - but for the ~6s between the EnvoyFilter
switching and the first successful remote-config fetch, rate limiting was
not enforced (empty bootstrap pipeline). That's the same fail-open
bootstrap gap already tracked in Open follow-ups.

Mechanics observed: operator image swap -> reconcile rewrites the
EnvoyFilter (new wasm binary SHA256 + `remoteConfig` bootstrap stand-in in
the same patch) -> Envoy spins up a new wasm VM in place
-> `on_configure` dispatches the fetch -> real
config applied **5.8s** later. No pod restart, no new migration logic - the
digest/`vm_id` mechanisms already built for live policy updates handle this
case too, unmodified.

**The gap is structural, not a tuning problem.** `on_configure` must return
`true`/`false` synchronously, and the proxy-wasm ABI has no blocking
network call, so the wasm module cannot hold Envoy's "not ready yet" while
it fetches. Closing it would need wasm-level request pause/resume.

## Config size limits

This PoC removes the EnvoyFilter size ceiling, but the real config still
has to be delivered and held in memory somewhere - confirmed live that this
introduces two independent ceilings of its own, found by growing a TRLP to
hundreds/thousands of limits.

**At default resource limits, both sides can be OOM-killed before any
transport question is even reached.** The operator's own reconcile cost
scales with policy count - compiling ~1000 limits peaked around 700Mi,
exceeding the operator's shipped default memory limit (fixed in
kuadrant-operator#2321, which raised it from 300Mi/limit to 500Mi/limit,
but that's a higher ceiling, not an unbounded one - larger policy sets
still OOM it). Separately, even after a fetch succeeds, the gateway pod
itself can be OOM-killed applying an oversized config: a ~15MB config
(300 TRLP limits) is stable at the gateway's default 1Gi memory limit, but
~30MB (600 limits) **crashes the whole gateway pod** (crash-loop, zero traffic) at that same default -
notably worse than a clean rejection, since it takes the whole proxy down,
not just this policy.

**With memory ruled out as a confound** (both operator and gateway given
generous headroom), a separate, genuine transport-level limit appeared:
fetches around ~75MB (1500 limits) saw repeated `status 13 (INTERNAL)`
failures before eventually succeeding on retry, and at ~151MB (3000
limits) the fetch failed
**consistently, every retry, with memory staying flat and low** - confirming
this is the gRPC channel rejecting the message, not a memory problem. No
explicit message-size limit is configured anywhere in this PoC's code; this
behavior comes from the underlying gRPC/HTTP2 client Envoy uses internally
for `dispatch_grpc_call`.

## Key findings

### Live-cluster findings

Tested against `kind-kuadrant-local` (`make local-setup`), using the
`llm-sim` backend with a 19-match HTTPRoute and a single-limit
TokenRateLimitPolicy (manifests in `manifests/`). A couple of PoC-only
issues and one Envoy/proxy-wasm host quirk were found and fixed along the
way, documented as code comments at their call sites rather than here (see
`on_grpc_call_response` in wasm-shim's `root_context.rs` for the host
quirk). None were blocking once fixed - a fresh gateway pod reliably
fetches and applies the real config within ~15-20s of startup, confirmed
via Limitador's `authorized_calls` counter incrementing on live traffic.

### Confirmed live: digest-driven config updates

Editing a TRLP's counter expression on an already-running gateway, with no
gateway pod restart, produced the full chain live: the EnvoyFilter's
embedded digest changed, Envoy re-invoked `on_configure` on the running VM
(diffing the changed EnvoyFilter content), the wasm module re-fetched and
applied the new real config (`"fetched remote plugin config (sha256:
...), applying"`), and traffic kept flowing through the filter to the
backend throughout - no fail-open/deny markers logged during the
transition.

## How to deploy / reproduce

**Prerequisite:** both repos must be on their `poc-extensions-endpoint`
branch - kuadrant-operator's `poc-extensions-endpoint` (this branch) and
wasm-shim's `poc-extensions-endpoint` companion branch - before running the
build loop below.

**Build & deploy loop** (repeat every time you change either repo's code):

```sh
# 1. Build the wasm-shim image from your changes (wasm-shim repo)
docker build -t quay.io/kuadrant/wasm-shim:dev /path/to/wasm-shim

# 2. Rebuild the operator image, telling its Dockerfile to pull the .wasm
#    binary from that image (kuadrant-operator repo). RELATED_IMAGE_WASMSHIM
#    here is a Makefile variable that becomes the WASM_SHIM_IMAGE build-arg -
#    it is NOT setting a deployment env var, despite the name overlap with
#    the CLAUDE.md workflow.
make docker-build IMG=quay.io/kuadrant/kuadrant-operator:dev \
  RELATED_IMAGE_WASMSHIM=quay.io/kuadrant/wasm-shim:dev

# 3. Load the operator image into the kind cluster (the wasm-shim image
#    itself is never deployed/pulled by the cluster - it's only consumed as
#    a local build-arg source in step 2 - so it doesn't need loading).
#    kind load replaces the cached content under the :dev tag in the node,
#    so the deployment doesn't need to change which tag it references.
kind load docker-image quay.io/kuadrant/kuadrant-operator:dev --name kuadrant-local

# 4. Restart the operator to pick up the freshly loaded :dev content.
kubectl rollout restart deployment kuadrant-operator-controller-manager -n kuadrant-system
kubectl rollout status deployment kuadrant-operator-controller-manager -n kuadrant-system --timeout=120s
```

Then finish with:

```sh
# Trigger a reconcile so the EnvoyFilter picks up the new SHA256 (any touch
# to a watched resource works; annotating the TRLP is a quick way).
kubectl annotate tokenratelimitpolicy llm-sim-trlp -n llm force-reconcile="$(date +%s)" --overwrite

# If the new wasm code doesn't seem to be running after the reconcile above,
# force a full gateway pod restart (see internal/istio/utils.go's vm_id
# comment and kuadrant-operator#2351 for why this can be needed):
kubectl delete pod -n gateway-system -l istio=ingressgateway
```

Verify end-to-end:

1. Apply the manifests in `manifests/` (`./apply-all.sh`).
2. Confirm the operator's `GetPluginConfig` returns the expected config for
   the gateway's locator string
   (`gateway.gateway.networking.k8s.io:<namespace>/<name>`):
   `kubectl port-forward svc/kuadrant-operator-grpc 50051:50051` +
   `grpcurl -plaintext -import-path pkg/extension/grpc -proto
   v1/plugin_config_service.proto -d '{"gateway":"<locator>"}' localhost:50051
   kuadrant.v1.PluginConfigService/GetPluginConfig`.
3. Restart the gateway pod (`kubectl delete pod -n gateway-system -l
   istio=ingressgateway`) to guarantee a fresh wasm module load, then wait
   ~15-20s.
4. Send a request through the gateway and confirm Limitador's
   `authorized_calls{limitador_namespace="<ns>/<route>"}` counter
   (`kubectl port-forward svc/limitador-limitador 8080:8080` then `curl
   localhost:8080/metrics`) increments - this proves the real fetched
   config is active, not the empty bootstrap stand-in.
5. Confirm the size win by comparing the EnvoyFilter's compact JSON size
   (see the size numbers in the status banner above) against the same
   scenario with the config inlined, mainline-style (no PoC changes).

## Open follow-ups (not yet actioned)

- Observability and Upgrade impact (see those sections above) - the two
  topics explicitly scoped for separate, dedicated investigation.
- **The `plugin_config_service.proto` copy in wasm-shim is kept in sync with
  the operator's copy by hand**, not code-generated from a shared source -
  acceptable for a PoC, would need a real solution (shared proto repo,
  build-time fetch, etc.) before this leaves PoC status.
- **Fail-open during the bootstrap gap on cold start** (see Architecture) -
  digest-driven updates confirmed this gap does not reopen on every policy
  edit (a prior real config is kept serving while a new one is fetched),
  but the one-time gap on a fresh gateway pod start is still unaddressed -
  explicitly flagged to revisit before this leaves PoC status, since it
  means every policy on the gateway, not just rate limiting, is briefly
  unenforced after a fresh gateway pod start.
