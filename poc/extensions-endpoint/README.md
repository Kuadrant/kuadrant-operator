# PoC: fetch wasm plugin config from the operator via gRPC

**Status: working end-to-end on a live cluster.** Confirmed via real traffic:
Limitador's `authorized_calls` counter climbs 1:1 with requests sent through
the gateway, proving the *fetched* (real) ActionSet config is active and
enforcing. EnvoyFilter compact JSON
size measured at **2,834 bytes**, vs. **64,641 bytes** for the identical
scenario (a 19-match HTTPRoute and a single-limit TokenRateLimitPolicy) with
the full config inlined, mainline-style - a ~96% reduction, and this size is
now **constant** regardless of ActionSet/limit count, which was the whole
point.

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
- **Fetch once at startup**, with basic tick-based retry on failure. **No
  continuous poll or push for changes / hot config reload** implemented.
  This is a known gap.

## Architecture / how it works

### Operator side

Instead of embedding the full wasm plugin config inline in the EnvoyFilter,
the reconciler caches it per-Gateway (`internal/wasm/config_store.go`) and
serves it over a new RPC, `PluginConfigService.GetPluginConfig(gateway) ->
config_json`, added to the gRPC server the operator already runs for
protobuf descriptor fetching (`internal/extension/manager.go`) - same port,
same cluster, same lifecycle, no new server. The EnvoyFilter itself now only
carries a small bootstrap stand-in - empty `services`/`actionSets`, plus a
new `remoteConfig.gateway` field telling the wasm module what to ask for -
instead of the real config.

### wasm-shim side (companion branch)

A new `RemoteConfigFetcher` - modeled on the existing `DescriptorManager`'s
dispatch/retry-via-tick pattern - detects `remoteConfig` at `on_configure`,
dispatches a `dispatch_grpc_call` to the operator's new RPC over the
already-wired `kuadrant-operator-grpc` cluster, and on response decodes the
real config and re-runs `process_config` with it, swapping in the real
`PipelineFactory`.

## Observability

*Not yet investigated - deferred by design.* The question: can a Policy's
status (e.g. a TokenRateLimitPolicy's `Enforced` condition) accurately
reflect whether its config is actually active in the data plane yet, rather
than just "the operator reconciled successfully and cached the config for
the gRPC handler to serve"? This matters specifically for this PoC because
the wasm module fetches asynchronously, on its own schedule, after the
EnvoyFilter (with its bootstrap stand-in) has already been written and
observed by Kubernetes - there's a real window (and, per the fail-open
decision in Scope, a real enforcement gap) between "operator says done" and
"gateway has actually applied it." To be investigated and filled in here
once scoped.

## Upgrade impact

*Not yet investigated - deferred by design.* The question: starting from
today's mainline (inline wasm config in the EnvoyFilter) with an
already-running gateway under live traffic, what happens when the operator
is upgraded to this PoC's approach - is the gateway interrupted, does
enforcement fail open or fail closed temporarily, or is the transition
smooth? To be investigated (live-tested) and filled in here once scoped.

## Key findings

### Live-cluster findings

Tested against `kind-kuadrant-local` (`make local-setup`), using the
`llm-sim` backend with a 19-match HTTPRoute and a single-limit
TokenRateLimitPolicy (manifests in `manifests/`).

Two self-inflicted, PoC-only issues were found and fixed along the way -
documented as code comments at their call sites rather than here: a
bootstrap-stand-in-vs-mainline `EnvoyFilter` deletion bug
(`internal/controller/istio_extension_reconciler.go`, the `hasPolicies`
check) and a `dispatch_grpc_call` serialization requirement
(`remote_config.rs`'s `encode_grpc_service`, wasm-shim). One more thing
initially looked like a finding but turned out to be expected behavior, not
a surprise:

**The `kuadrant-operator-grpc` CLUSTER patch is only wired in when OOP
extensions have registered upstreams.** `reconcileUpstreamClusters`/
`buildUpstreamEnvoyFilter` adds it only when
`extension.GetRegisteredUpstreamsByTargetRef`/`CollectRouteUpstreams` return
non-empty, which isn't the case for a gateway with just a
TokenRateLimitPolicy attached - so it was correctly absent from Envoy's
config for this test scenario, and the gRPC dispatch had nowhere to route to
as a result. This PoC needs the cluster unconditionally (it doesn't depend
on OOP extensions being present), so `buildIstioEnvoyFilterForGateway` now
also adds it whenever `wasmConfig.RemoteConfig != nil`, independent of the
OOP-extensions code path - an addition for this PoC's own needs, not a fix
to existing behavior.

The actual blocker to a working end-to-end demo was a genuine
Envoy/proxy-wasm host quirk - see "Bug #4" below.

**Gotcha for anyone re-testing this: SHA-based EnvoyFilter hot-reload of the
wasm module was unreliable in this environment.** Changing the wasm binary
(and thus the `vm_config.code.remote.sha256` in the EnvoyFilter) triggered
an Envoy "Wasm remote code fetch" log line, but the *old* wasm module kept
running (confirmed via a debug log that only appeared after a full pod
delete, never after a SHA-only change). A full `kubectl delete pod -n
gateway-system -l istio=ingressgateway` was needed to guarantee the new
wasm binary was actually loaded when iterating - don't trust a SHA change
alone as proof of a reload when testing.

**Operator side confirmed 100% correct**, independent of any wasm-shim
issue: `kubectl port-forward svc/kuadrant-operator-grpc 50051:50051` +
`grpcurl -plaintext -import-path pkg/extension/grpc -proto
v1/plugin_config_service.proto -d '{"gateway":"<locator>"}' localhost:50051
kuadrant.v1.PluginConfigService/GetPluginConfig` returned the full, correct
config JSON (base64-encoded in the response, decodes to the exact
`services`/`actionSets` the reconciler computed) on the first try.

**wasm-shim got one full successful round-trip**: after the cluster-patch fix
above, a fresh gateway pod's `on_configure` correctly detected `remote_config`, dispatched
the gRPC call with a correctly-encoded `GrpcService` (verified via debug
log of the raw bytes), and received a real gRPC response - `status 14`
(`UNAVAILABLE`), most likely because the `STRICT_DNS` cluster hadn't
finished its first DNS resolution yet when the dispatch fired mere seconds
after pod start (a known class of Envoy warm-up race, not a config error).

### Bug #4 (fixed): Envoy silently disarms the tick timer after any gRPC response

This was the one thing standing between this PoC and a working end-to-end
demo, and turned out to be a genuine Envoy/proxy-wasm host quirk, not a bug
in this PoC's retry logic. After the first fetch failed (`UNAVAILABLE`),
`reset_pending` correctly reverted the fetcher's state back to `Missing`,
and `remote_config_fetcher.is_active()` was confirmed still `true` - but
`on_tick` fired exactly once and then never again, confirmed via an
unconditional debug log at the top of `on_tick` (printed once, immediately
after the failed response, never again despite waiting 25+ seconds - 12+
tick periods). No panics, no crash-loop, no further wasm log output at all.

**Root cause, confirmed empirically**: receiving *any* `on_grpc_call_response`
callback - success or failure, for the remote-config fetch or for
`DescriptorManager`'s descriptor fetch - silently disarms Envoy's tick timer
for that RootContext, regardless of what the module's own `tick_enabled`
flag believes. This likely also affects `DescriptorManager`'s existing
descriptor-retry logic (untested directly, since this repro has no dynamic
descriptor scenario, but the mechanism is identical) - another item in the
same low-urgency "OOP extension framework not yet in active use" bucket as
the `dispatch_grpc_call` serialization requirement noted above.

**Fix**: `on_grpc_call_response` (`root_context.rs`) now unconditionally
re-arms the tick timer (`self.set_tick_period(self.descriptor_manager.tick_period())`)
after handling *any* gRPC response, rather than relying on
`set_tick_enabled`'s edge-triggered guard (which only calls
`set_tick_period` once, on the disabled→enabled transition, and assumes the
host keeps honoring it afterward - which it doesn't). Verified this fix
directly: after it landed, a fresh gateway pod reliably fetched and applied
the real config within ~15-20s of startup, confirmed via Limitador's
`authorized_calls` counter climbing in lockstep with live test traffic sent
through the gateway (226 → 227 → ... on each subsequent request), proving
the *fetched* ActionSet config - not the empty bootstrap stand-in - is what
executes the rate-limit check against Limitador for every request.

### Confirmed live: fetch-once behavior

After changing a TRLP's `when` predicate on an already-running gateway, the
old predicate kept being enforced with no change in behavior; only a full
gateway pod restart picked up the new content. Deleting the policy entirely,
by contrast, took effect immediately with no restart needed, because that
removes the whole wasm filter from the EnvoyFilter (a normal Envoy
filter-chain/LDS update) rather than requiring the running wasm module to
notice a content change - a structurally different, more reliable mechanism
than in-place config refresh. This is the direct, observed consequence of
the "fetch once at startup" decision in Scope.

### Confirmed size win

EnvoyFilter compact JSON size (the metric that actually matters for etcd,
not `kubectl -o json`'s pretty-printed output): **2,834 bytes**, vs.
**64,641 bytes** for the identical `llm-sim`/HTTPRoute/TRLP scenario with the
full config inlined, mainline-style. That's the bootstrap payload plus
the two cluster patches (`kuadrant-operator-wasm`, `kuadrant-operator-grpc`)

- and, critically, this size is now **independent of ActionSet/TRLP-limit
count**, which is the entire point: adding more HTTPRoute rules or more
TRLP limits no longer grows the EnvoyFilter at all, since the growth all
happens in the cached config served over gRPC instead.

### Pre-existing gap found along the way: `DescriptorManager` has no hot-swap capability

Not new to this PoC, and not fixed by it - flagged here because
`RemoteConfigFetcher` was modeled on `DescriptorManager`'s pattern, and the
two gaps are the same shape. Once a `(cluster, service)` descriptor reaches
`Resolved` state (`descriptor_manager.rs`), `get_missing()` excludes it
permanently - `fetch_missing()` (called every tick) never re-fetches it
again. If an upstream gRPC service's schema changes after wasm-shim has
already resolved it once, there is currently no mechanism, short of a full
gateway pod restart, to pick up the new schema. This predates any of this
PoC's changes; see "Open follow-ups".

## How to deploy / reproduce

**This is not the `RELATED_IMAGE_WASMSHIM`-env-var + Kind-registry workflow
documented in the root `CLAUDE.md`.** That workflow is for pointing an
*already-running* operator at a wasm-shim image served over OCI at runtime.
This PoC's local-`kind` setup doesn't fetch wasm-shim that way at all: the
operator's own `Dockerfile` pulls the `.wasm` binary out of a wasm-shim
*image* at Docker build time (a multi-stage build: `FROM ${WASM_SHIM_IMAGE}
AS wasm-shim` then `COPY --from=wasm-shim /plugin.wasm /wasm/plugin.wasm`),
bakes it into the operator's own image, and the operator then serves that
file itself over HTTP (`internal/controller/wasm_server.go`) for Envoy to
fetch. So there are two separate build steps, and the wasm-shim image only
matters at the *operator's* build time, not at deploy/runtime.

**Use the same `:dev` tag the root `CLAUDE.md` local-setup workflow already
uses** (`make docker-build IMG=quay.io/kuadrant/kuadrant-operator:dev`, `make
local-setup IMG=quay.io/kuadrant/kuadrant-operator:dev`) rather than
inventing a separate tag for this PoC. Reusing `:dev` means the deployment
is already pointing at the right tag from initial cluster setup, so every
iteration is just rebuild + reload + restart - no separate "first time"
step to repoint the deployment via `kubectl set image`.

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

# Do NOT trust that the reconcile above means the new wasm code is running -
# see the SHA-based hot-reload gotcha in Key findings. Force a full gateway
# pod restart to be sure:
kubectl delete pod -n gateway-system -l istio=ingressgateway
```

Verify end-to-end:

1. Apply the manifests in `manifests/` (`./apply-all.sh`).
2. Confirm via `grpcurl` (command in "Operator side confirmed 100% correct"
   in Key findings) that the operator's `GetPluginConfig` returns the
   expected config for the gateway's locator string
   (`gateway.gateway.networking.k8s.io:<namespace>/<name>`).
3. Restart the gateway pod (`kubectl delete pod -n gateway-system -l
   istio=ingressgateway`) to guarantee a fresh wasm module load, then wait
   ~15-20s.
4. Send a request through the gateway and confirm Limitador's
   `authorized_calls{limitador_namespace="<ns>/<route>"}` counter
   (`kubectl port-forward svc/limitador-limitador 8080:8080` then `curl
   localhost:8080/metrics`) increments - this proves the real fetched
   config is active, not the empty bootstrap stand-in.
5. Confirm the size win by comparing the EnvoyFilter's compact JSON size
   (see "Confirmed size win" in Key findings) against the same scenario with
   the config inlined, mainline-style (no PoC changes).

## Open follow-ups (not yet actioned)

- **No continuous poll-for-changes / hot config reload** (see Scope) -
  prototyped as a continuous-polling design and proven to work, then
  explicitly reverted to keep this PoC to fetch-once-at-startup; real
  follow-up work if live policy updates need to propagate without a gateway
  pod restart.
- **`DescriptorManager` has no hot-swap capability either** (see "Key
  findings") - pre-existing, not introduced by this PoC, same shape as the
  fetch-once limitation above but for dynamic gRPC service descriptors
  rather than wasm plugin config.
- **`dispatch_grpc_call`'s bare-cluster-name bug in `DescriptorManager`**
  (see code comments in wasm-shim's `remote_config.rs`) likely reproduces
  there too - low urgency since the OOP extension framework isn't yet in
  active production use.
- **Envoy's tick-timer disarm-after-any-gRPC-response quirk** (see "Bug #4"
  in Key findings) likely also affects `DescriptorManager`'s own
  descriptor-retry logic - untested directly, same low-urgency bucket as
  above.
- Observability and Upgrade impact (see those sections above) - the two
  topics explicitly scoped for separate, dedicated investigation.
- **The `plugin_config_service.proto` copy in wasm-shim is kept in sync with
  the operator's copy by hand**, not code-generated from a shared source -
  acceptable for a PoC, would need a real solution (shared proto repo,
  build-time fetch, etc.) before this leaves PoC status.
- **Fail-open during the bootstrap gap** (see Scope) - explicitly flagged
  to revisit before this leaves PoC status, since it means every policy on
  the gateway, not just rate limiting, is briefly unenforced after a fresh
  gateway pod start.
