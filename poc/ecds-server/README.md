# PoC: kuadrant-operator as an Envoy ExtensionConfigDiscoveryService (ECDS) server

**Status: working end-to-end on a live cluster, with one manual
infrastructure step not yet automated.** Confirmed via real traffic and a
live, no-restart config update:

- EnvoyFilter stays constant-size regardless of policy size: **2,937 bytes**
  vs. the `tlrp-test` baseline's 64,641 bytes for the identical scenario.
- Limitador's `authorized_calls`/`limited_calls` counters respond 1:1 to real
  traffic through the gateway, proving the *fetched* (real) config is active
  and enforcing.
- **The key differentiator over `poc-extensions-endpoint` is confirmed**:
  editing the TRLP's counter expression (`request.method` → `request.path`)
  while the gateway pod kept running (same `creationTimestamp` before and
  after, zero restarts) changed the ECDS `version_info` and the wasm filter's
  live `configuration` within seconds - Envoy's own xDS client hot-swapped
  the config with **no wasm-side polling, no pod restart, no hand-rolled
  fetch/retry logic**. This is exactly the capability `poc-extensions-endpoint`
  could not achieve (that PoC required a full pod restart for any config
  change to take effect).

Getting here required solving a real, fundamental blocker found during
testing: Envoy rejects an ECDS `ApiConfigSource` whose target cluster was
added dynamically via an `EnvoyFilter` CLUSTER patch (Envoy requires it to be
a cluster **statically defined in the proxy's bootstrap**). The fix -
provisioning that cluster as a genuine static bootstrap cluster via
`Gateway.spec.infrastructure.parametersRef` - works, confirmed on Istio 1.29.1,
but is currently applied **by hand** (two ConfigMaps + a `kubectl patch` on
the `Gateway`, see `manifests/06-gateway-parametersref-patch.sh`), not
automated by the operator. See "Live-cluster findings" and "Comparison with
poc-extensions-endpoint" below for the full story, and "Open follow-ups" for
what full automation of this would require.

## What this PoC is

The other of two approaches being prototyped for the EnvoyFilter/WasmPlugin
object-size problem (see `kuadrant-operator` issue #2051,
RHOAIENG-95277/CONNLINK-1806): the Istio `EnvoyFilter`'s inline wasm
`PluginConfig.Configuration` grows with every HTTPRoute rule/match and every
TokenRateLimitPolicy limit, hitting etcd's object-size ceiling at MaaS scale.
The sibling approach (`poc-extensions-endpoint` branch) has the wasm module
itself fetch its config from the operator over a bespoke gRPC call, built
from scratch inside wasm-shim's Rust code. This PoC instead has the
**operator run its own minimal Envoy ExtensionConfigDiscoveryService (ECDS)
server**, using `go-control-plane` (the same library istiod itself is built
on) - so Envoy's own xDS client fetches and hot-swaps the wasm filter's
config natively. None of the fetch/retry/versioning logic needs to be
hand-rolled inside wasm-shim; wasm-shim needs **zero code changes**.

## Key research findings (grounding the plan)

- `github.com/envoyproxy/go-control-plane` (v0.14.0 base + v1.37.0 `envoy`
  proto submodule) resolved cleanly from the local module cache via
  `go mod tidy` once real imports landed - no network surprises, versions
  compatible with this repo's existing go/grpc/protobuf versions.
- `pkg/server/v3.NewServer(ctx, cache, callbacks)` returns a fat `Server`
  interface covering every xDS service, but gRPC registration is
  per-service - registering only
  `extensionv3.RegisterExtensionConfigDiscoveryServiceServer` exposes
  *only* ECDS; any other xDS RPC gets gRPC's standard "unknown service"
  error. Precedented in go-control-plane's own `pkg/test/v3/register.go`.
- `pkg/cache/v3.NewSnapshotCache(ads=false, nodeHash, logger)` +
  `cache.NewSnapshot(version, map[resource.Type][]types.Resource{...})` +
  `SnapshotCache.SetSnapshot(ctx, nodeKey, snapshot)` is the whole push
  model. `SetSnapshot` unconditionally replaces the per-node snapshot and
  auto-diffs against each open watch's last-ACKed version - no custom
  diffing/versioning logic needed.
- The ECDS resource type is `resource.ExtensionConfigType` ↦
  `*envoy/config/core/v3.TypedExtensionConfig{Name, TypedConfig *anypb.Any}`,
  keyed by `Name`, which **must equal** the `HttpFilter.Name` Envoy
  references in its listener config.
- **Node keying is the one real design decision - resolved, not just scoped
  out.** `NodeHash` is a pluggable `interface{ ID(*core.Node) string }`; the
  default `IDHash` keys by `node.GetId()`, which for Istio-managed proxies is
  per-pod, not per-Gateway. Initially implemented as a single fixed key
  (every connected Envoy gets the same snapshot) for the first pass, then
  replaced with real per-Gateway keying once multi-gateway correctness came
  up - see "Per-gateway keying" below for how.
- **Import gotcha**: `envoy/extensions/filters/http/wasm/v3` and
  `envoy/extensions/wasm/v3` both declare `package wasmv3` - one must be
  import-aliased (`httpwasmv3` in `internal/istio/utils.go`).
- Confirmed empirically (not just read from docs): `buildWasmFilterConfig`'s
  existing JSON output - the same `map[string]any` structure already used to
  build the current inline `typed_config` patch - parses directly via
  `protojson.Unmarshal` into the real generated `envoy.extensions.filters.http.wasm.v3.Wasm`
  proto with zero translation needed. Specifically: `"failure_policy":
  "FAIL_RELOAD"` matches the generated enum's JSON name exactly, and
  `"configuration": {"@type": "type.googleapis.com/google.protobuf.StringValue",
  "value": "..."}`  matches protojson's well-known-type special-casing for
  `google.protobuf.Any` fields (`PluginConfig.Configuration` is `*anypb.Any`).
  This meant no hand-built proto construction was needed for
  `BuildWasmExtensionConfigAny` - just marshal the existing map to JSON and
  `protojson.Unmarshal` it into `&httpwasmv3.Wasm{}`.
- **Warming was explicitly scoped out of this pass** (see Scope) - a
  `default_config` is always set, so there's no cold-start/warming
  behavior to characterize for this first iteration.
- **Open question from kuadrant-operator#2051, re: multi-wasm-module
  coexistence** (e.g. an Istio-native `WasmPlugin` CRD, which uses ECDS via
  istiod, adding its own wasm filter to the same gateway as this operator's
  own ECDS-delivered filter): reasoned to likely be fine - the ECDS/HTTP_FILTER
  proto model allows independently-named, independently-sourced filters in
  one filter chain; two distinct ECDS servers (istiod and this operator's)
  each owning a differently-named resource shouldn't conflict. **Not
  verified empirically** - flagged as a follow-up if this PoC progresses
  past prototype status.

## Scope (deliberately minimal for this first pass, mirroring poc-extensions-endpoint)

- **Istio only** - same reasoning as the sibling PoC (Envoy Gateway's wasm
  delivery is structurally different).
- **Per-Gateway `NodeHash` keying** - no longer scoped out; implemented and
  confirmed live. See "Per-gateway keying" below.
- **SOTW, not Delta xDS; `ads=false`** - simplest correct model, no
  cross-type (LDS/CDS/RDS) consistency needed since only one resource type
  is served.
- **Always set a `default_config`** (the same empty bootstrap-style stand-in
  used in `poc-extensions-endpoint`: `{"services": {}, "actionSets": []}`),
  rather than leaving it unset - keeps cold-start/fail-open behavior directly
  comparable to the sibling PoC rather than introducing a different,
  uninvestigated cold-start story. Warming characterization is explicit
  future work, not this pass.
- **Reuse the existing wasm binary delivery mechanism unchanged**
  (`internal/controller/wasm_server.go` HTTP server, `vm_config.code.remote`
  pointing at it) - ECDS only replaces how the filter's `configuration` (the
  `PluginConfig`) is delivered, not how the `.wasm` binary itself is
  fetched.
- **`go mod tidy`/`go.sum` updated, but `make bundle`/`make helm-build` NOT
  run** - this PoC only changes `config/manager/` (a new container port +
  Service), which `make deploy`/`make local-setup` picks up directly via
  kustomize; the generated OLM bundle/Helm chart artifacts weren't
  regenerated since they're not exercised by this PoC's verification path.

## Architecture / how it works

### New package: `internal/ecds/`

- **`internal/ecds/store.go`**: `gatewayNodeHash` (derives a per-Gateway
  cache key from the connecting Envoy's self-reported `Node` - see
  "Per-gateway keying" below), `GatewayNodeKey(namespace, name string) string`
  (the shared key format both the hash and `Push` callers use),
  `Store` wrapping a `cachev3.SnapshotCache`, and
  `Push(ctx context.Context, nodeKey, name string, typedConfig *anypb.Any) error`
  - builds a `*corev3.TypedExtensionConfig{Name, TypedConfig}`, computes a
  sha256-of-marshaled-proto version string (so redundant pushes are
  naturally deduped by the snapshot cache's own version comparison), and
  calls `SetSnapshot` for that specific Gateway's node key only - other
  Gateways' snapshots are untouched. Note: `SetSnapshot` replaces the
  *entire* per-type resource set for the node, not just the named resource -
  fine here since this PoC only ever serves one named resource per Gateway
  (`kuadrant-wasm-shim-ecds`).
- **`internal/ecds/server.go`**: `Server` wraps a `Store`, exposes
  `Store()`, and implements `controller.Runnable` (`Run(stopCh)`,
  `HasSynced() bool`) with the exact lifecycle shape of
  `internal/controller/wasm_server.go`'s `WasmServer`/`wasmServerRunnable`:
  env-var port (`ECDS_SERVICE_PORT`, default `50053`,
  `DefaultECDSServerPort`), `net.Listen`, `serverv3.NewServer(ctx, cache,
  CallbackFuncs{})`, `grpc.NewServer()` with only
  `RegisterExtensionConfigDiscoveryServiceServer` registered, `go Serve()`,
  graceful-stop-then-force-`Stop` with a 5s timeout. `ServerClusterName =
  "kuadrant-ecds"` is the CLUSTER name Envoy uses to reach it.

### Registration: `internal/controller/ecds_server.go` + `state_of_the_world.go`

- `ecdsServerRunnable(logger)` constructs an `ecds.Server`, stashes its
  `Store()` in the package-level `controllers.ECDSStore` var (same pattern
  as `WasmFileSHA256`), and returns it as a `controller.Runnable`.
- Registered in `state_of_the_world.go` right next to the existing
  `controller.WithRunnable("wasm server", wasmServerRunnable(b.logger))`
  line: `controller.WithRunnable("ecds server", ecdsServerRunnable(b.logger))`.

### `internal/istio/utils.go`: new functions

- **`BuildWasmExtensionConfigAny(wasmURL, imagePullSecret, imageSHA,
  clusterName string, pluginConfig *structpb.Struct) (*anypb.Any, error)`**:
  builds the real `envoy.extensions.filters.http.wasm.v3.Wasm` proto (see
  "Key research findings" above for why this is a direct `protojson.Unmarshal`
  of `buildWasmFilterConfig`'s existing JSON output, not a hand-built proto),
  wraps it in an `anypb.Any` via `anypb.New`.
- **`BuildDefaultWasmFilterConfig(wasmURL, imageSHA, clusterName string)
  (map[string]any, error)`**: the ECDS `default_config` value - same shape
  as `buildWasmFilterConfig` but with an empty `{"services": {},
  "actionSets": []}` plugin config, matching the bootstrap stand-in used by
  the sibling PoC.
- **`BuildEnvoyFilterWasmECDSPatch(resourceName, ecdsClusterName string,
  defaultWasmFilterConfig map[string]any) (...)`**: replaces
  `BuildEnvoyFilterWasmPatch`'s inline `typed_config` with a
  `config_discovery` block (same HTTP_FILTER/GATEWAY/listener-filter-chain
  match structure as the existing patch):
  ```json
  {
    "name": "kuadrant-wasm-shim-ecds",
    "config_discovery": {
      "config_source": {
        "resource_api_version": "V3",
        "api_config_source": {
          "api_type": "GRPC",
          "transport_api_version": "V3",
          "grpc_services": [{"envoy_grpc": {"cluster_name": "kuadrant-ecds"}}]
        }
      },
      "default_config": {
        "@type": "type.googleapis.com/udpa.type.v1.TypedStruct",
        "type_url": "type.googleapis.com/envoy.extensions.filters.http.wasm.v3.Wasm",
        "value": { /* BuildDefaultWasmFilterConfig output */ }
      },
      "type_urls": ["type.googleapis.com/envoy.extensions.filters.http.wasm.v3.Wasm"]
    }
  }
  ```
  `default_config` is wrapped in the same `udpa.type.v1.TypedStruct` escape
  hatch the existing codebase already uses for `typed_config` (both are
  `google.protobuf.Any` fields being expressed as a plain JSON `map[string]any`
  inside an Istio `EnvoyFilter_Patch`, which goes through `protobuf.Struct`
  JSON unmarshaling, not direct proto construction) - kept consistent with
  the existing pattern rather than introducing a different Any-JSON
  convention for this one patch.

### `internal/controller/istio_extension_reconciler.go`

- `Reconcile()` now also computes `ecdsServerHost`/`ecdsServerPort` (mirrors
  the existing `wasmServerHost`/`wasmServerPort` computation), from
  `kuadrant-operator-ecds.<namespace>.svc.cluster.local` and
  `ECDS_SERVICE_PORT`/`ecds.DefaultECDSServerPort`.
- `buildIstioEnvoyFilterForGateway` gained `ctx`, `ecdsServerHost`,
  `ecdsServerPort`, and `logger` parameters. When `len(wasmConfig.ActionSets)
  > 0`:
  1. Builds the real wasm filter `*anypb.Any` via `BuildWasmExtensionConfigAny`
     using the **real** `wasmConfig`, and pushes it to `ECDSStore` under
     `ecds.GatewayNodeKey(gateway.GetNamespace(), gateway.GetName())` +
     resource name `kuadrant-wasm-shim-ecds` - this is the direct
     operator-side equivalent of the sibling PoC's `wasm.SetConfig` call
     site, now correctly scoped to the one Gateway this reconcile iteration
     is for (see "Per-gateway keying" below).
  2. Builds the HTTP_FILTER patch via `BuildEnvoyFilterWasmECDSPatch`
     instead of `BuildEnvoyFilterWasmPatch`.
  3. Keeps the existing wasm-binary-server CLUSTER patch
     (`kuadrant-operator-wasm`) unchanged.
  4. Deliberately does **not** add a dynamic CLUSTER patch for
     `kuadrant-ecds` - see "Resolved: static cluster provisioned via
     `Gateway.spec.infrastructure.parametersRef`" below for why (Envoy
     rejects an ECDS cluster added via CDS).

### Deployment plumbing: `config/manager/`

- `manager.yaml`: new container port `ecds` (50053, TCP).
- New `ecds_service.yaml` (mirrors `wasm_service.yaml`): a `Service` named
  `ecds` (becomes `kuadrant-operator-ecds` after the `kuadrant-operator-`
  `namePrefix` in `config/default/kustomization.yaml`), port 50053 →
  `targetPort: ecds`.
- `kustomization.yaml`: added `ecds_service.yaml` to `resources`.

### wasm-shim

No code changes. Uses the existing public `quay.io/kuadrant/wasm-shim:latest`
image unmodified for this PoC's local-cluster testing - `on_configure`
doesn't know or care how Envoy obtained the config bytes, so nothing here
needed to change. This is itself a thing to verify, not assume - see
"Live-cluster findings".

## Per-gateway keying

The initial pass used a single fixed `NodeHash` (every connected Envoy
mapped to the same cache key), explicitly documented as correct only for a
single-test-gateway PoC. This was resolved by inspecting what `Node`
identity Istio-managed Gateway API proxies actually self-report over xDS,
rather than guessing.

**How it was found**: a temporary `StreamRequestFunc` callback was added to
`internal/ecds/server.go` to log the full `Node` proto (via `protojson`) the
first time a stream sent one, then the gateway pod was restarted to force a
fresh ECDS connection. The logged `Node.metadata` for the test gateway
contained (among many other `ISTIO_META_*`-sourced fields):

```json
{
  "NAMESPACE": "gateway-system",
  "LABELS": {
    "gateway.networking.k8s.io/gateway-name": "kuadrant-ingressgateway",
    "gateway.networking.k8s.io/gateway-class-name": "istio",
    "istio": "ingressgateway",
    ...
  },
  ...
}
```

`node.metadata.NAMESPACE` and `node.metadata.LABELS[gateway.networking.k8s.io/gateway-name]`
exactly match `gateway.GetNamespace()`/`gateway.GetName()` - sourced from the
pod's own labels via Istio's `istio-podinfo` downward-API volume and
`ISTIO_META_*` env vars (not anything this operator adds), confirmed present
on every Gateway API-managed gateway pod regardless of this PoC.

**Implementation** (`internal/ecds/store.go`):

- `GatewayNodeKey(namespace, name string) string` - the single shared key
  format (`"<namespace>/<name>"`) both the push side and the hash side must
  agree on.
- `gatewayNodeHash.ID(node *corev3.Node) string` - reads
  `node.Metadata.Fields["NAMESPACE"]` and
  `node.Metadata.Fields["LABELS"].GetStructValue().Fields[gatewayNameLabel]`,
  returning `""` (an invalid/unmatchable key) if either is missing rather
  than guessing or falling back to a shared key - a gateway whose identity
  can't be determined gets no snapshot at all, rather than silently sharing
  one with an unrelated gateway.
- `Store.Push` gained a `nodeKey` parameter; the reconciler computes it via
  `ecds.GatewayNodeKey(gateway.GetNamespace(), gateway.GetName())` right
  before pushing, so each Gateway's config lands only in its own snapshot.
- Covered by unit tests (`internal/ecds/store_test.go`): `gatewayNodeHash`
  behavior across missing-field cases and the real metadata shape above, plus
  a `Store.Push` test confirming two different Gateways' pushes don't
  clobber or leak into each other's snapshots.

**Confirmed live**: rebuilt and redeployed with per-gateway keying, restarted
the gateway pod to force a fresh connection under the new `NodeHash`, and
confirmed via `/config_dump`'s `EcdsConfigDump` that the gateway still
correctly received its own real wasm config (not the `default_config`
stand-in) keyed by `gateway-system/kuadrant-ingressgateway` - and that real
traffic through the gateway still returned `200`. Only one gateway exists in
this test cluster, so this confirms the *mechanism* resolves the documented
single-key correctly, not yet a true multi-gateway isolation test (that
would need a second Gateway + TRLP to prove two different snapshots exist
simultaneously without cross-talk) - the unit test above covers that
isolation property at the `Store` level instead.

## Build/test status

- `go build ./...`: clean.
- `go vet ./...` / `go vet -tags unit ./...`: clean.
- `go mod tidy`: added `github.com/envoyproxy/go-control-plane` (+
  `.../envoy` submodule) and transitive deps to `go.mod`/`go.sum`, resolved
  entirely from the local module cache plus two small transitive downloads
  (`planetscale/vtprotobuf`, `go-control-plane/ratelimit`) - no blocking
  network issues.
- `make test-unit` (full repo, `-race -tags unit`): all packages pass,
  including `internal/controller`, `internal/istio` (both touched by this
  PoC), and the new `internal/ecds` (no dedicated unit tests yet - PoC-stage
  parity with the sibling PoC's testing depth; see Open follow-ups).
- `Test_buildIstioEnvoyFilterForGateway` and
  `Test_buildEnvoyExtensionPolicyForGateway`
  (`internal/controller/extenstion_reconciler_test.go`) updated for the new
  `config_discovery`/`default_config` patch shape (previously navigated
  `typed_config` → `value` → `config`; now `config_discovery` →
  `default_config` → `value` → `config`) and the new
  `buildIstioEnvoyFilterForGateway` signature - both pass.

## Live-cluster findings

Tested against a fresh `kind-kuadrant-local` cluster (`make local-setup
IMG=quay.io/kuadrant/kuadrant-operator:dev GATEWAYAPI_PROVIDER=istio`),
reusing the `llm-sim` + 12-rule HTTPRoute + TRLP manifests from `tlrp-test`
(copied into `manifests/` here, with the same `request.method` counter
workaround as the sibling PoC - see below).

**EnvoyFilter size confirmed working as designed.** With the TRLP applied,
`kubectl get envoyfilter kuadrant-kuadrant-ingressgateway -n gateway-system -o json`
compact-JSON size measured at **2,937 bytes** (3 config patches: one
HTTP_FILTER `config_discovery` patch, two CLUSTER patches for
`kuadrant-operator-wasm` and `kuadrant-ecds`) - comparable to the sibling
PoC's 2,834 bytes, and a ~95% reduction from the `tlrp-test` baseline's
64,641 bytes for the same policy. The size mechanism itself works exactly as
designed.

**But the gateway's listener is rejected outright, taking down the entire
gateway.** As soon as the EnvoyFilter (HTTP_FILTER `config_discovery` patch +
`kuadrant-ecds` CLUSTER patch) is applied, the gateway pod's Envoy logs:

```
warning envoy config .../delta_subscription_state.cc:283 delta config for
type.googleapis.com/envoy.config.listener.v3.Listener rejected: Error
adding/updating listener(s) 0.0.0.0_80: envoy.config.core.v3.ApiConfigSource
must have a statically defined non-EDS cluster: 'kuadrant-ecds' does not
exist, was added via api, or is an EDS cluster
```

Confirmed via `/clusters` on the Envoy admin API that the `kuadrant-ecds`
cluster **does** exist and is healthy (`kuadrant-ecds::added_via_api::true`,
`health_flags::healthy`) - the error message's own wording ("was added via
api") is the actual diagnosis: Envoy's `ApiConfigSource` validation for ECDS
(and xDS config sources generally) requires the target cluster to be
**statically defined in the proxy's bootstrap config's
`static_resources.clusters`**, not added dynamically via CDS. An
`EnvoyFilter` `CLUSTER` patch is, by construction, a dynamic CDS addition
("added via api" in Envoy's own terms) - structurally incapable of
satisfying this requirement, no matter how the patch is written.

This explains *why* Istio's own native ECDS usage (for the `WasmPlugin` CRD)
works: inspecting the gateway pod's own bootstrap
(`/config_dump` → `BootstrapConfigDump` → `static_resources.clusters`) shows
exactly four statically-defined clusters baked in by `istio-agent` at
proxy startup: `prometheus_stats`, `agent`, `sds-grpc`, and **`xds-grpc`**
(the sidecar's connection back to istiod). Istio's `WasmPlugin` ECDS
`config_source` reuses that pre-existing static `xds-grpc`/ADS cluster,
which is why it satisfies Envoy's validation - it was never a cluster this
operator (or any EnvoyFilter) could add after the fact, because it's part
of the proxy's bootstrap, generated before any EnvoyFilter/xDS config is
even applied.

**Workaround tried and confirmed non-functional**: `EnvoyFilter`'s
`applyTo: BOOTSTRAP` (marked `DEPRECATED` in the vendored
`istio.io/api/networking/v1alpha3.EnvoyFilter_ApplyTo` enum, but still
present) is, in principle, the only `EnvoyFilter` mechanism that targets the
bootstrap rather than dynamic xDS output - tried adding a static cluster via
a `MERGE` patch to `static_resources.clusters`, restarted the gateway pod
(bootstrap is only read once at proxy startup, confirmed required for any
`BOOTSTRAP` patch to have a chance of taking effect), and confirmed via
`/config_dump`'s `BootstrapConfigDump` that **the static cluster list was
unchanged afterward** - still exactly the same four Istio-internal clusters,
no sign the patch was applied. Consistent with the field being deprecated in
current Istio/`istio.io/api` and evidently no longer honored by `istio-agent`
in this environment (Istio 1.29.1, per `gcr.io/istio-release/proxyv2:1.29.1`
in the gateway pod).

**Severity confirmed real, not cosmetic**: with the listener rejected, the
*whole* `0.0.0.0_80` listener fails to update - not just the wasm filter.
Every policy on that gateway (AuthPolicy, RateLimitPolicy, any other
HTTP-filter-based policy) would be affected, since Envoy's delta-xDS model
rejects the listener as a unit, not per-filter. Confirmed by deleting the
TRLP: `wasmConfig.ActionSets` drops to zero, the reconciler correctly
tags the EnvoyFilter for deletion (the existing, unmodified
`utils.TagObjectToDelete` logic - no regression here), the EnvoyFilter
disappears, and the gateway pod's listener update succeeds again
(`Readiness succeeded`, `Envoy proxy is ready`) within seconds - confirming
the rejection was specifically and only caused by this PoC's ECDS patch, and
that removing it cleanly restores a healthy gateway.

Also confirmed (same as the sibling PoC, not specific to either): the test
TRLP's original counter expression (`request.headers["x-test-user"]`,
containing embedded double quotes) triggers a **separate, mainline
Limitador bug** - embedded quotes in a counter expression break Limitador's
generated `variables: descriptors[0]["<expr>"]` field, silently rejecting
its entire limits file. Worked around here (as in the sibling PoC) by using
`request.method` instead, applied in `manifests/04-trlp-llm-sim.yaml`
before this round of testing - but this PoC never got far enough for that
workaround to matter, since the gateway was down before any traffic test
could run.

### Resolved: static cluster provisioned via `Gateway.spec.infrastructure.parametersRef`

Per the research in "Comparison with poc-extensions-endpoint" below, applied
live via `manifests/06-gateway-parametersref-patch.sh`:

1. `manifests/05-ecds-bootstrap-configmaps.yaml` creates two ConfigMaps in
   `gateway-system`: `kuadrant-ecds-bootstrap` (a `custom_bootstrap.json`
   fragment defining the `kuadrant-ecds` cluster as a real static
   `static_resources.clusters` entry, STRICT_DNS + HTTP/2, pointing at
   `kuadrant-operator-ecds.kuadrant-system.svc.cluster.local:50053`) and
   `kuadrant-ingressgateway-params` (a `deployment:` strategic-merge-patch
   key appending a ConfigMap-backed volume, a `volumeMount` on the existing
   `istio-proxy` container, and the `ISTIO_BOOTSTRAP_OVERRIDE` env var
   pointing at the mounted file).
2. `kubectl patch gateway kuadrant-ingressgateway --type=merge -p
   '{"spec":{"infrastructure":{"parametersRef":{"group":"","kind":"ConfigMap","name":"kuadrant-ingressgateway-params"}}}}'`.
3. Istio's gateway deployment controller strategic-merge-patched the
   generated Deployment exactly as expected - confirmed via
   `kubectl get deployment ... -o jsonpath` showing the new env var, volume,
   and volumeMount - and rolled a new gateway pod.
4. **Confirmed via the Envoy admin API** (`/config_dump`'s
   `BootstrapConfigDump`): `kuadrant-ecds` now appears in
   `static_resources.clusters` alongside Istio's own `prometheus_stats`,
   `agent`, `sds-grpc`, `xds-grpc`. `/clusters` shows
   `kuadrant-ecds::added_via_api::false` - the exact flag that was `true`
   (and fatal) before.
5. With the operator's dynamic `kuadrant-ecds` CLUSTER patch disabled (it
   would otherwise conflict with the now-static cluster of the same name -
   see the code comment in `buildIstioEnvoyFilterForGateway`) and the TRLP
   re-applied: **the gateway listener was accepted** - no more
   `ApiConfigSource must have a statically defined non-EDS cluster` warning,
   `Readiness succeeded`, `Envoy proxy is ready`.
6. `/config_dump`'s `EcdsConfigDump` confirmed the wasm filter's **actual,
   real pushed config** was fetched and applied (not just the
   `default_config` bootstrap stand-in) - the dump contains the TRLP's real
   `actionSets`, CEL predicates, and `messageBuilder` content.

### Full verification, now reached

- **Real traffic enforcement**: 5 requests through the gateway to
  `llm-sim` all returned `200`; Limitador's `authorized_calls` counter
  incremented from 0 to exactly 5 in the same window.
- **Live config update, zero pod restart (the key differentiator)**: with
  the gateway pod already running, the TRLP's counter expression was changed
  from `request.method` to `request.path` via `kubectl patch`. The gateway
  pod's `creationTimestamp` was identical before and after (confirmed via
  `kubectl get pod -o jsonpath`, zero restarts) - yet the ECDS
  `version_info` changed (`f8b995... → e54b4a...`) and the live
  `EcdsConfigDump` content flipped from containing `request.method` to
  `request.path`. Envoy's own xDS client detected and applied the new
  `SetSnapshot` push from `ECDSStore.Push` with no wasm-side polling, no
  pod restart, and no hand-rolled fetch/retry logic of any kind.
- **Rate limiting responds correctly to the live-updated config**: with the
  TRLP's limit separately lowered to 2/minute (a Limitador-side-only change,
  unrelated to ECDS - rate limit thresholds live in Limitador's own config,
  not in the wasm plugin config pushed via ECDS), a follow-up request
  correctly received `429` after the limit was exceeded, confirming the
  live-swapped wasm config was genuinely active and functioning, not just
  present in the config dump.

This directly answers the question left open at the top of this PoC's
investigation: the core idea - "let Envoy's native xDS client handle fetch,
retry, and hot-swap instead of hand-rolling it in wasm-shim" - **works**,
end-to-end, once the ECDS cluster has a legitimate static bootstrap
presence. The remaining gap is entirely about *how that static presence gets
provisioned* (manual today, see "Open follow-ups"), not about the viability
of ECDS itself for this use case.

## Comparison with poc-extensions-endpoint

This finding changes the comparison between the two approaches explored for
kuadrant-operator#2051:

- **`poc-extensions-endpoint`** (wasm module fetches its config from the
  operator via a `dispatch_grpc_call` the module itself issues) does **not**
  hit this restriction, because `dispatch_grpc_call` is a direct
  proxy-wasm host call referencing a cluster by name - it bypasses Envoy's
  own xDS subsystem entirely, so the "cluster must be statically defined for
  `ApiConfigSource`" rule (which is specifically an xDS-subsystem
  validation) never applies. This is the structural reason that PoC reached
  a fully working end-to-end state on a real Istio gateway, while this one
  did not.
- **This PoC's core idea - "let Envoy's native xDS client handle fetch/
  retry/versioning instead of hand-rolling it in wasm-shim" - is sound in
  principle** (confirmed by Istio's own native `WasmPlugin` ECDS usage
  working correctly against the same cluster), but requires the ECDS
  server's cluster to be reachable via a cluster that is part of the proxy's
  **static bootstrap**, which an externally-deployed, operator-owned gRPC
  server cannot become through any `EnvoyFilter`-based mechanism available
  in this Istio version.
- **Confirmed live (not just researched): `Gateway.spec.infrastructure.parametersRef`
  is a real, working path - but not via `sidecar.istio.io/bootstrapOverride`
  directly.** Traced through Istio 1.29's actual source, then proven on the
  cluster (see "Live-cluster findings" → "Resolved: static cluster
  provisioned via `Gateway.spec.infrastructure.parametersRef`")
  (`pilot/pkg/config/kube/gatewaycommon/deploymentcontroller.go` +
  `manifests/.../files/kube-gateway.yaml`, the exact template rendering this
  cluster's `kuadrant-ingressgateway-istio` Deployment - confirmed by the
  `sidecar.istio.io/inject: "false"` label and field-for-field match):
  - **`bootstrapOverride` itself is a dead end for Gateway-API-managed
    gateways specifically.** It's wired up (env var `ISTIO_BOOTSTRAP_OVERRIDE`
    + volume + volumeMount) only in the legacy sidecar-injection templates
    (`gateway-injection-template.yaml`, `injection-template.yaml`,
    `grpc-agent.yaml`). `kube-gateway.yaml` - the template actually used for
    Gateway API `Gateway` resources - has **no trace of `bootstrapOverride`
    at all**, confirmed by grepping the fetched template source. This
    explains, with version-matched certainty, the behavior the 2020 Istio
    issue (istio/istio#28302) described for the older ingress-gateway chart.
  - **But the underlying mechanism istio-agent uses is just an env var**
    (`pkg/envoy/proxy.go`: `istioBootstrapOverrideVar = env.Register("ISTIO_BOOTSTRAP_OVERRIDE", ...)`,
    read unconditionally in `envoy.Run()` regardless of which template
    produced the Deployment) **that gets passed to Envoy as a native
    `--config-yaml` flag** (a proto merge onto the bootstrap: singular
    fields replace, repeated fields - like `static_resources.clusters` -
    append). Nothing about this is specific to the sidecar-injection code
    path; it only needs that one env var plus a mounted file to exist on the
    `istio-proxy` container, by whatever means.
  - **`infrastructure.parametersRef`'s `deployment:` key supplies exactly
    that means, and is confirmed to use genuine Kubernetes strategic-merge-patch
    semantics** (`deploymentcontroller.go`: `strategicMergePatchYAML`,
    explicitly commented as "a small fork of `strategicpatch.StrategicMergePatch`
    to allow YAML patches"). Strategic merge patch merges named list entries
    (`containers` by `name`, `volumes` by `name`, `env` by `name`) rather
    than overwriting the whole list - so a `parametersRef` ConfigMap could
    safely *append* one new volume (backed by a second, operator-owned
    ConfigMap holding the custom bootstrap JSON with the static `kuadrant-ecds`
    cluster), one new `volumeMount` on the existing `istio-proxy` container,
    and one new `env` entry (`ISTIO_BOOTSTRAP_OVERRIDE=/etc/istio/custom-bootstrap/custom_bootstrap.json`)
    - reconstructing by hand exactly what the legacy templates'
    `bootstrapOverride` conditionals would have generated, without needing
    Istio to support that annotation for this gateway type at all.
  - **Confirmed available on the installed CRD**: `infrastructure.parametersRef`
    (and `infrastructure.annotations`/`.labels`) are present in this
    cluster's `gateways.gateway.networking.k8s.io` v1 CRD schema
    ("Support: Extended").
  - **Proven live, applied by hand, not yet automated by the operator - and
    it comes with real new costs worth deciding on explicitly before
    automating it**: this would be the first place this operator writes to
    `Gateway.spec` rather than only reading/attaching policies to it
    (`infrastructure.parametersRef` is a field on the `Gateway` object
    itself, not a Kuadrant CR), and it means owning two new ConfigMaps per
    Kuadrant-managed gateway (the bootstrap fragment and the deployment-patch
    parameters). Also still unverified: whether setting `parametersRef` on a
    `Gateway` that already has one (user- or GatewayClass-managed) composes
    safely, and whether this is acceptable across all Istio versions Kuadrant
    targets (checked against 1.29.1 only here).
  - Piggybacking on istiod's own `xds-grpc` cluster/ADS connection (e.g. by
    making the operator's ECDS service reachable *through* istiod somehow)
    was not investigated - likely impractical without modifying istiod
    itself.
  - This restriction is specific to **Istio-managed proxies with a
    pilot-agent-generated bootstrap**; it's plausible a hand-rolled static
    Envoy deployment (no Istio control plane) could add the ECDS cluster to
    its own bootstrap directly and avoid this problem entirely - irrelevant
    to Kuadrant's Istio integration, but worth noting as a boundary of where
    this finding applies.

## Open follow-ups (not yet actioned)

- **Primary remaining gap**: the static-bootstrap-cluster workaround
  (`Gateway.spec.infrastructure.parametersRef`) is proven to work but is
  applied **by hand** today (`manifests/06-gateway-parametersref-patch.sh`),
  not automated by the operator. Automating it would mean: the operator
  generating and owning two ConfigMaps per Kuadrant-managed Istio gateway
  (the bootstrap fragment, keyed by a stable name derived from the ECDS
  cluster's host/port; and the deployment-patch parameters), and writing to
  `Gateway.spec.infrastructure.parametersRef` - the first time this operator
  would write to `Gateway.spec` rather than only reading/attaching policies
  to it. Also still open: composing safely with a `parametersRef` a user or
  GatewayClass may have already set, and confirming this holds across Istio
  versions beyond the 1.29.1 tested here. This is a deliberate design
  decision to make, not just an implementation task - worth discussing
  before building.
- Also disabled for this round of testing, reversible once the above is
  automated: the `kuadrant-ecds` dynamic CLUSTER patch in
  `buildIstioEnvoyFilterForGateway` is commented out (it would conflict with
  the now-static cluster of the same name). Restoring proper automation
  means replacing that comment with code that knows whether the static
  cluster has been provisioned yet.
- A true multi-gateway live test (two Gateways + two TRLPs, confirming no
  cross-talk end-to-end, not just at the `Store` unit-test level) hasn't been
  run - only one gateway exists in the test cluster so far. See "Per-gateway
  keying" above.
- Warming (`apply_default_config_without_warming`) characterization -
  explicitly scoped out of this pass (see Scope); now more reachable than
  before since the gateway actually comes up, but still not investigated.
- Multi-wasm-module coexistence with Istio's native `WasmPlugin`/ECDS -
  reasoned likely fine, not empirically verified (see "Key research
  findings").
- `make bundle`/`make helm-build` not run - only matters if this PoC needs
  to be installable via OLM/Helm rather than `make deploy`/`local-setup`.
- The mainline Limitador CEL-quoting bug (embedded double quotes in counter
  expressions) is still unpatched - worked around here and in the sibling
  PoC, no standalone fix decided on yet.
