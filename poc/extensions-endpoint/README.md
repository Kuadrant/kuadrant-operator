# PoC: fetch wasm plugin config from the operator via gRPC

**Status: working end-to-end on a live cluster.** Confirmed via real traffic:
Limitador's `authorized_calls` counter climbs 1:1 with requests sent through
the gateway, proving the *fetched* (real) ActionSet config is active and
enforcing - not just the empty bootstrap stand-in. EnvoyFilter compact JSON
size measured at **2,834 bytes**, vs. **64,641 bytes** for the identical
scenario with the full config inlined (the `tlrp-test` baseline) - a ~96%
reduction, and this size is now **constant** regardless of ActionSet/limit
count, which was the whole point. See "Live-cluster findings" below for the
full debugging story: four things found along the way (one PoC-only issue,
one likely-pre-existing-but-dormant bug, one non-issue initially mislabeled
as a finding, and one genuine Envoy/proxy-wasm host quirk that was the
actual blocker), all resolved. Both sides build clean and pass their existing test
suites with no regressions (operator: `go test ./... -tags unit`, one
pre-existing unrelated failure in `cmd/extensions/oidc-policy` confirmed
present on unmodified code too; wasm-shim: `cargo test -p kuadrant-filter -p
wasm-shim`, all 298+ tests pass, clippy clean).

## What this PoC is

One of two approaches being prototyped for the EnvoyFilter/WasmPlugin
object-size problem (see `kuadrant-operator` issue #2051,
RHOAIENG-95277/CONNLINK-1806): the Istio `EnvoyFilter`'s inline wasm
`PluginConfig.Configuration` grows with every HTTPRoute rule/match and every
TokenRateLimitPolicy limit, hitting etcd's object-size ceiling at MaaS scale.
The other approach (`poc-ecds-server` branch) uses Envoy's
ExtensionConfigDiscoveryService; this one instead has the wasm module pull
its configuration from the operator over gRPC, keeping the EnvoyFilter
constant-size regardless of policy count.

Full design plan (context, alternatives considered, rationale): see the
plan this PoC was built from — ask if you need the original plan doc
(`~/.claude/plans/streamed-hopping-sun.md` on the machine this was written
on; copy its content here if this repo needs to be self-contained).

## Key finding that shaped this approach

wasm-shim already had a fully working, already-deployed async
fetch-over-gRPC pipeline before this PoC touched anything: `DescriptorService`,
used to fetch protobuf descriptors for dynamic gRPC services
(`crates/kuadrant-filter/src/descriptor_manager.rs` in wasm-shim). Both the
Istio and Envoy Gateway reconcilers already emit a CLUSTER patch named
`kuadrant-operator-grpc` pointing at the operator's own gRPC server
(`internal/extension/manager.go`, port `EXTENSIONS_DESCRIPTOR_SERVICE_PORT`,
default 50051) in **every** EnvoyFilter/EnvoyExtensionPolicy generated
today, whether or not descriptors are actually used. This PoC adds one new
RPC (`PluginConfigService.GetPluginConfig`) to that already-running server
and already-wired cluster, instead of inventing a new fetch mechanism from
scratch (there is no `dispatch_http_call` usage anywhere in wasm-shim
today — that was considered and ruled out for exactly this reason).

## Scope (deliberately minimal for this first pass)

- **Istio only.** Envoy Gateway's equivalent code path
  (`buildEnvoyExtensionPolicyForGateway`,
  `internal/controller/envoy_gateway_extension_reconciler.go:615`) embeds
  the config differently (a native typed field on the EG CRD, not a
  string-typed `Any`) and has a structurally different wasm-binary delivery
  mechanism (OCI image pull vs. Istio's HTTP `remote.http_uri`). Out of
  scope here.
- **Fetch once at startup**, with basic tick-based retry on failure. **No
  continuous poll-for-changes / hot config reload** in this pass — the goal
  is proving the EnvoyFilter shrinks and that a fetched config actually
  enforces policy, not live propagation of policy edits after the gateway
  is already running. This is a known, explicit follow-up, not a silent gap.
  **Confirmed live** (see "Live-cluster findings" below): after changing a
  TRLP's `when` predicate on an already-running gateway, the old predicate
  kept being enforced with no change in behavior; only a full gateway pod
  restart picked up the new content. Deleting the policy entirely, by
  contrast, took effect immediately with no restart needed, because that
  removes the whole wasm filter from the EnvoyFilter (a normal Envoy
  filter-chain/LDS update) rather than requiring the running wasm module to
  notice a content change - a structurally different, more reliable
  mechanism than in-place config refresh. A continuous-polling design (same
  tick timer, poll every cycle instead of stopping after first success,
  `sha256`-based skip when content is unchanged) was prototyped and proven
  to work, then explicitly reverted - it's real follow-up work, not
  something to carry as unfinished code in this PoC.
- **This is not new to this PoC: `DescriptorManager` has no hot-swap
  capability either, and that is a gap, not an accepted design choice.**
  Once a `(cluster, service)` descriptor reaches `Resolved` state
  (`descriptor_manager.rs`), `get_missing()` excludes it permanently -
  `fetch_missing()` (called every tick) never re-fetches it again. If an
  upstream gRPC service's schema changes after wasm-shim has already
  resolved it once, there is currently no mechanism, short of a full
  gateway pod restart, to pick up the new schema. This PoC didn't introduce
  this gap (it predates any of this PoC's changes) and didn't fix it either
  - flagging it here because `RemoteConfigFetcher` was modeled on
  `DescriptorManager`'s pattern, and the two gaps are the same shape:
  neither mechanism refreshes previously-successful state.
- **Confirmed decision: fail-open during the bootstrap gap.** Between
  `on_configure` returning (bootstrap-only, empty `PipelineFactory`) and the
  real config arriving via the async fetch, an empty factory means no
  ActionSet matches any request → `Action::Continue`. **Every policy on
  that gateway, AuthPolicy included, is unenforced** for that window, not
  just rate limiting. This was explicitly considered (fail-closed was the
  alternative, requiring a new readiness/deny-until-ready state machine)
  and fail-open was chosen as the simplest option for a first PoC. Revisit
  before this leaves PoC status.
- **Cache the last-computed config, don't compute it per-request.**
  Considered computing the wasm config fresh inside the gRPC handler
  instead. Ruled out: `buildWasmConfigs`
  (`internal/controller/istio_extension_reconciler.go:402`) depends on
  `state.Load(StateEffectiveAuthPolicies/RateLimitPolicies/TokenRateLimitPolicies)`,
  populated by separate upstream reconciler tasks earlier in the same
  workflow cycle — not derivable from topology alone. `state` is a fresh,
  empty `sync.Map` allocated per reconcile cycle by `policy-machinery`, not
  reconstructable outside it. Computing fresh per-request would mean
  decoupling that whole pipeline from the reconcile workflow — a real
  refactor, out of scope for a PoC. Since the controller is already
  reactive (any relevant resource change triggers a reconcile that
  recomputes this near-instantly), caching the last-computed result at the
  end of the existing reconcile is already "as fresh as the last relevant
  change."

## Architecture / how it works

### Operator side (implemented)

1. **New proto + RPC**: `pkg/extension/grpc/v1/plugin_config_service.proto`
   defines `PluginConfigService.GetPluginConfig(gateway) -> (config_json, sha256)`,
   generated with `protoc-gen-go`/`protoc-gen-go-grpc` (see
   `pkg/extension/grpc/generate_proto.sh` for the existing pattern this
   follows — not yet added to that script; ran `protoc` manually for now).

2. **New cache**, `internal/wasm/config_store.go`: a
   `sync.RWMutex`-protected `map[gatewayLocator]CachedConfig{ConfigJSON, SHA256}`,
   with `SetConfig`/`GetConfig`. Lives in `internal/wasm` (a leaf package)
   rather than `internal/controller` deliberately: `internal/controller`
   already imports `internal/extension`, so putting the store in
   `internal/controller` and having `internal/extension`'s gRPC handler read
   it back would be an import cycle. `internal/wasm` is a package both
   `internal/controller` and `internal/extension` already import.

3. **Registered on the already-running gRPC server**:
   `internal/extension/manager.go`, `startDescriptorServer()` now also does
   `extpb.RegisterPluginConfigServiceServer(server, svc)` right next to the
   existing `RegisterDescriptorServiceServer` call — same port, same
   cluster, same lifecycle, no new server/listener. `extensionService` (the
   concrete type backing both) now also embeds
   `extpb.UnimplementedPluginConfigServiceServer` and implements
   `GetPluginConfig`, which reads from `wasm.GetConfig`.

4. **Populate the cache**: `internal/controller/istio_extension_reconciler.go`,
   right after `wasmConfig` is computed and mutated for a gateway, if the
   gateway has any policies (`hasPolicies := len(wasmConfig.ActionSets) > 0`):
   marshal the full config to JSON and call `wasm.SetConfig(gateway.GetLocator(), json)`.

5. **Shrink the inline `PluginConfig.Configuration`**:
   `buildIstioEnvoyFilterForGateway` now takes an explicit `hasPolicies bool`
   (previously it inferred "does this gateway need a wasm filter at all"
   from `len(wasmConfig.ActionSets) > 0` — that check still decides whether
   to add the wasm filter, but can no longer also look at the *content* of
   `wasmConfig`, since that content is now the bootstrap stand-in). When
   `hasPolicies` is true, the config embedded in the EnvoyFilter is:
   ```json
   {"services": {}, "actionSets": [], "descriptorService": "kuadrant-operator-grpc", "remoteConfig": {"gateway": "<gateway-locator>"}}
   ```
   `services`/`actionSets` stay required-and-empty rather than removed, so
   the wasm-shim's existing `PluginConfiguration` struct doesn't need its
   required fields loosened. `remoteConfig` (new field, see
   `internal/wasm/types.go`: `Config.RemoteConfig *RemoteConfigRef`) is the
   one additive signal telling the wasm module to fetch the real config.
   `descriptorService` is set explicitly to `wasm.DescriptorServiceClusterName`
   (it already defaults to the same value on the wasm-shim side, but set
   explicitly here rather than relying on both sides' defaults happening to
   agree) - it doubles as the target cluster name for the new fetch, since
   it's the same cluster already used for descriptor fetching. The existing
   `kuadrant-operator-grpc` CLUSTER patch already lets Envoy reach the
   operator for this — no new cluster patch was needed.

### wasm-shim side (implemented, companion branch)

1. **`crates/kuadrant-filter/src/configuration.rs`**: `PluginConfiguration`
   gained one new optional field, `remote_config: Option<RemoteConfigRef>`
   (`RemoteConfigRef { gateway: String }`), `#[serde(default)]` so it's
   fully backward compatible with configs that don't set it.

2. **New proto**: `vendor-protobufs/kuadrant/v1/plugin_config_service.proto`
   is a byte-for-byte copy of the operator's
   `pkg/extension/grpc/v1/plugin_config_service.proto` (kept in sync
   manually for this PoC — not code-generated from a shared source; see
   Open questions). Wired into `crates/kuadrant-filter/build.rs`'s existing
   `prost_build.compile_protos(...)` call (same one that already compiles
   `descriptor_service.proto`) - generates
   `crate::proto::kuadrant::v1::{GetPluginConfigRequest, GetPluginConfigResponse}`.
   Note `proto` is `pub(crate)` in `kuadrant-filter`'s `lib.rs`, so this
   logic had to live inside `kuadrant-filter`, not in the `wasm-shim` binary
   crate where `root_context.rs` lives.

3. **New module**, `crates/kuadrant-filter/src/filter/remote_config.rs`
   (`pub use`'d from `filter/mod.rs`, alongside `DescriptorManager`):
   `RemoteConfigFetcher`, a small state machine (`Idle` / `Missing{gateway,
   cluster}` / `Pending{gateway, cluster, token}`) mirroring
   `DescriptorManager`'s dispatch/retry-via-tick pattern, but for a single
   (gateway, cluster) target rather than a map of descriptor keys, and
   without `DescriptorManager`'s `Mutex`-wrapped interior mutability -
   `RemoteConfigFetcher` is owned exclusively by `FilterRoot` (not
   `Arc`-shared), so plain `&mut self` methods suffice. Key methods:
   `set_pending`, `fetch` (dispatches via `dispatch_grpc_call` to
   `kuadrant.v1.PluginConfigService`/`GetPluginConfig`, no-ops if already
   pending/idle), `is_pending_token`, `reset_pending` (reverts to `Missing`
   for retry), `clear`, `decode_response`.

4. **`crates/wasm-shim/src/filter/root_context.rs`** wiring:
   - `FilterRoot` gained a `remote_config_fetcher: RemoteConfigFetcher` field.
   - `process_config`: if `config.remote_config.is_some()`, calls
     `remote_config_fetcher.set_pending(...)` before building the
     `PipelineFactory` as usual (the bootstrap config's empty
     `services`/`action_sets` still produce a valid, empty factory exactly
     like today), then immediately attempts the first fetch. `set_tick_enabled`
     now also fires when the fetcher is active, so retries happen on the
     existing tick cadence (`descriptor_manager.tick_period()`, 2s) without a
     second timer.
   - New `handle_remote_config_response`: on a successful gRPC response,
     decodes it, `serde_json::from_slice`s the embedded `config_json` into a
     full `PluginConfiguration`, and calls `self.process_config(config)`
     again — confirmed idempotent (just swaps `Arc<PipelineFactory>`).
   - `on_tick` and `on_grpc_call_response` both now check
     `remote_config_fetcher` first (via `is_pending_token`) before falling
     through to the existing descriptor-fetch handling, since both share the
     same gRPC call token namespace.

Verified: `cargo build` (native) and `cargo build --target=wasm32-wasip1`
both succeed; `cargo test -p kuadrant-filter -p wasm-shim` passes (298+
tests, no regressions); `cargo clippy --all-targets` clean.

## Building & deploying custom images for testing

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
# see the SHA-based hot-reload gotcha below. Force a full gateway pod
# restart to be sure:
kubectl delete pod -n gateway-system -l istio=ingressgateway
```

## Live-cluster findings

Tested against `kind-kuadrant-local` (`make local-setup`), reusing the
`llm-sim` + 12-rule HTTPRoute + single-limit TRLP manifests from the
`tlrp-test` repro (copied into `manifests/` here).

Four things found along the way, in the order encountered: one self-inflicted
PoC-only issue (point 1), one real finding with low urgency since it's in
unused-in-production code (point 2), one non-issue that was initially
mislabeled as a finding (point 3 - expected/known behavior, not a surprise),
and one genuine Envoy/proxy-wasm host quirk that was the actual blocker
("Bug #4" further below) - now fixed, with the whole thing working
end-to-end.

1. **Self-inflicted, PoC-only - not a bug in mainline `kuadrant-operator`,
   verified against a clean `main` checkout.** `buildIstioEnvoyFilterForGateway`
   had `if len(wasmConfig.ActionSets) == 0 { utils.TagObjectToDelete(...) }`
   to decide whether the wasm EnvoyFilter should exist at all. In mainline
   this is correct, because `wasmConfig` is always the real, full config
   there. This PoC breaks that assumption by passing a bootstrap stand-in
   (with deliberately-emptied `ActionSets`) as `wasmConfig`, so the mainline
   check started deleting the EnvoyFilter on every reconcile. Fixed *for
   this PoC* by adding an explicit `hasPolicies bool` parameter, computed
   from the real config before it gets replaced by the bootstrap stand-in,
   and using `!hasPolicies` instead of re-deriving it from (now-always-empty)
   `wasmConfig.ActionSets`. **No standalone fix needed in mainline** - this
   only exists because of how this PoC repurposes the `wasmConfig` parameter.

2. **`dispatch_grpc_call`'s `upstream_name` must be a serialized
   `envoy.config.core.v3.GrpcService`, not a bare cluster name string**
   (wasm-shim). Passing the plain string `"kuadrant-operator-grpc"` (what
   `DescriptorManager::fetch_missing` in `descriptor_manager.rs` *also*
   does) returns `ParseFailure` immediately, confirmed against a real
   Envoy. `DescriptorManager`'s dispatch uses the identical bare-string
   pattern, and its only test coverage is against the
   `proxy-wasm-test-framework` mock, which doesn't validate `upstream_name`'s
   wire format - so the same failure would very likely reproduce there
   against a real Envoy too. **Low urgency, not a live production bug**:
   the OOP extension framework (which is what would trigger a real dynamic
   descriptor fetch, i.e. `has_expected()` returning true) is shipped but
   not yet in active production use, so nobody is hitting this path today -
   worth a note for whenever that framework does get adopted, not an
   incident. Fixed here via `encode_grpc_service` in
   `remote_config.rs` (hand-rolled protobuf encoding using
   `prost::encoding::encode_varint` for the two-string-field message,
   rather than vendoring the full `envoy.config.core.v3` proto for this).

3. **The `kuadrant-operator-grpc` CLUSTER patch is only wired in when OOP
   extensions have registered upstreams - expected/known behavior, not a
   surprise.** `reconcileUpstreamClusters`/`buildUpstreamEnvoyFilter` adds it
   only when `extension.GetRegisteredUpstreamsByTargetRef`/
   `CollectRouteUpstreams` return non-empty, which isn't the case for a
   gateway with just a TokenRateLimitPolicy attached - so it was correctly
   absent from Envoy's config for this test scenario, and the gRPC dispatch
   had nowhere to route to as a result. This PoC needs the cluster
   unconditionally (it doesn't depend on OOP extensions being present), so
   `buildIstioEnvoyFilterForGateway` now also adds it whenever
   `wasmConfig.RemoteConfig != nil`, independent of the OOP-extensions code
   path - an addition for this PoC's own needs, not a fix to existing
   behavior.

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

**wasm-shim got one full successful round-trip**: after point 3's cluster-patch fix, a fresh
gateway pod's `on_configure` correctly detected `remote_config`, dispatched
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
bug #2.

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

### Confirmed size win

EnvoyFilter compact JSON size (the metric that actually matters for etcd,
not `kubectl -o json`'s pretty-printed output - see the methodology note in
the `tlrp-test` repo): **2,834 bytes**, vs. **64,641 bytes** for the
identical `llm-sim`/HTTPRoute/TRLP scenario with the full config inlined
(the `tlrp-test` baseline measurement). That's the bootstrap payload plus
the two cluster patches (`kuadrant-operator-wasm`, `kuadrant-operator-grpc`)
- and, critically, this size is now **independent of ActionSet/TRLP-limit
count**, which is the entire point: adding more HTTPRoute rules or more
TRLP limits no longer grows the EnvoyFilter at all, since the growth all
happens in the cached config served over gRPC instead.

## How to verify

1. Build & load custom images (see "Building & deploying custom images for
   testing" above for the exact commands and why it's a two-step build).
2. Apply the manifests in `manifests/` (`./apply-all.sh`).
3. Confirm via `grpcurl` (command above) that the operator's
   `GetPluginConfig` returns the expected config for the gateway's locator
   string (`gateway.gateway.networking.k8s.io:<namespace>/<name>`).
4. Restart the gateway pod (`kubectl delete pod -n gateway-system -l
   istio=ingressgateway`) to guarantee a fresh wasm module load, then wait
   ~15-20s.
5. Send a request through the gateway and confirm Limitador's
   `authorized_calls{limitador_namespace="<ns>/<route>"}` counter
   (`kubectl port-forward svc/limitador-limitador 8080:8080` then `curl
   localhost:8080/metrics`) increments - this proves the real fetched
   config is active, not the empty bootstrap stand-in.
6. Confirm the size win by comparing the EnvoyFilter's compact JSON size
   (see "Confirmed size win" above) against `tlrp-test`'s baseline
   measurements for the same scenario without this PoC's changes.
