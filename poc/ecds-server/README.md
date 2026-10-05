# PoC: kuadrant-operator as an Envoy ExtensionConfigDiscoveryService (ECDS) server

**Status: working end-to-end on a live cluster.** Confirmed via real traffic
and a live, no-restart config update:

- EnvoyFilter stays constant-size regardless of policy size: **2,937 bytes**
  vs. **64,641 bytes** for the identical scenario (a 19-match HTTPRoute and a
  single-limit TokenRateLimitPolicy) with the config inlined, mainline-style.
- Limitador's `authorized_calls`/`limited_calls` counters respond 1:1 to real
  traffic through the gateway, proving the *fetched* (real) config is active
  and enforcing.
- **Live config updates with zero gateway pod restart are confirmed**:
  editing a TokenRateLimitPolicy's counter expression while the gateway pod
  kept running (same `creationTimestamp` before and after, zero restarts)
  changed the ECDS `version_info` and the wasm filter's live `configuration`
  within seconds - Envoy's own xDS client hot-swapped the config with **no
  wasm-side polling, no pod restart, no hand-rolled fetch/retry logic**.

## What this PoC is

One of the approaches being prototyped for the EnvoyFilter/WasmPlugin
object-size problem (see `kuadrant-operator` issue #2051,
RHOAIENG-95277/CONNLINK-1806): the Istio `EnvoyFilter`'s inline wasm
`PluginConfig.Configuration` grows with every HTTPRoute rule/match and every
TokenRateLimitPolicy limit, hitting etcd's object-size ceiling at MaaS scale.
This PoC has the **operator run its own minimal Envoy
ExtensionConfigDiscoveryService (ECDS) server**, using `go-control-plane`
(the same library istiod itself is built on) - so Envoy's own xDS client
fetches and hot-swaps the wasm filter's config natively. None of the
fetch/retry/versioning logic needs to be hand-rolled inside wasm-shim;
wasm-shim needs **zero code changes**.

## Scope (deliberately minimal for this first pass)

- **Istio only** - Envoy Gateway's wasm delivery is structurally different
  and would need its own investigation, not attempted here.
- **One manual infrastructure step, not yet automated**: the ECDS cluster's
  static bootstrap presence is provisioned by hand via
  `manifests/06-gateway-parametersref-patch.sh`, not by the operator. See
  Architecture above for the high-level approach and "Open follow-ups" for
  what automating it would require.
- **Always set a `default_config`** (an empty bootstrap-style stand-in:
  `{"services": {}, "actionSets": []}`), rather than leaving it unset - keeps
  cold-start behavior simple and predictable: Envoy applies the default
  immediately, with no warming/blocking question to characterize for this
  first iteration.
- **Reuse the existing wasm binary delivery mechanism unchanged**
  (`internal/controller/wasm_server.go` HTTP server, `vm_config.code.remote`
  pointing at it) - ECDS only replaces how the filter's `configuration` (the
  `PluginConfig`) is delivered, not how the `.wasm` binary itself is
  fetched.

## Architecture / how it works

The operator runs a small, standalone Envoy ExtensionConfigDiscoveryService
(ECDS) server (new `internal/ecds` package), built on `go-control-plane` -
the same library istiod itself is built on. On every reconcile, instead of
embedding the full wasm plugin config inline in the EnvoyFilter, the
reconciler pushes it into this server's in-memory snapshot store, keyed
per-Gateway. The EnvoyFilter itself now only carries a small
`config_discovery` stub: where to fetch the real config from (a new
`kuadrant-ecds` cluster) and what to use in the meantime (an empty default
config). Envoy's own xDS client does the rest - fetching the real config,
and hot-swapping it on any future push, natively. wasm-shim needed **no code
changes**: `on_configure` doesn't know or care how Envoy obtained the bytes.

Two problems had to be solved beyond "stand up an xDS server":

- **Routing the right config to the right gateway.** The ECDS server must
  serve a different snapshot per Gateway. It derives a cache key from the
  identity Istio-managed Envoy proxies already self-report over xDS
  (namespace + Gateway name), matched independently on the reconciler side
  from the Kubernetes `Gateway` object it already has in hand. See
  "Per-gateway keying" in Key findings for how that identity shape was
  confirmed, and `internal/ecds/store_test.go` for test coverage.
- **Giving the ECDS cluster a legitimate home in Envoy's config.** Envoy
  requires the cluster an `ApiConfigSource` points at to be statically
  defined in its bootstrap - it will not accept one added dynamically via an
  `EnvoyFilter` CLUSTER patch (see "The static-cluster blocker" in Key
  findings for the full diagnosis). This PoC provisions that cluster via
  `Gateway.spec.infrastructure.parametersRef`, applied by hand today
  (`manifests/05-06`), not yet automated by the operator - see "Open
  follow-ups".

Supporting changes: a new container port/Service for the ECDS server
(`config/manager/`), and a NetworkPolicy rule allowing the gateway to reach
it - found missing live (see "The NetworkPolicy gap" in Key findings).

## Observability

*Not yet investigated - deferred by design.* The question: can a Policy's
status (e.g. a TokenRateLimitPolicy's `Enforced` condition) accurately
reflect whether its config is actually active in the data plane yet, rather
than just "the operator reconciled successfully and wrote the EnvoyFilter"?
This matters specifically for this PoC because config delivery is
asynchronous (an xDS push that Envoy may ACK or NACK) rather than inline in
the object Kubernetes already confirmed was written. To be investigated and
filled in here once scoped.

## Upgrade impact

**Confirmed live: migrating an already-running gateway from mainline to
this PoC, under continuous traffic, never failed a request and never
opened an enforcement gap.** Unlike an in-place config patch, this upgrade
needs the ECDS cluster statically present in the proxy's bootstrap (see
Architecture) - not something an existing pod can be patched into, so one
pod replace is unavoidable. The test: with the gateway on mainline (full
inline config) and traffic running, the `parametersRef` bootstrap patch was
applied first (standard rolling update: Kubernetes surges a new pod,
keeps the old one serving until the new one passes its *existing*,
ECDS-unaware readiness probe, then retires the old one - zero failures,
nothing ECDS-specific needed here since this new pod is still running
mainline at this point). Then the operator was upgraded to this PoC's
image: it rewrote the EnvoyFilter to the ECDS stub, and - because the
static cluster already existed from the prior step - Envoy applied this as
an **in-place listener update, same pod, no restart**.

The key result: a tight before/after check across a policy edit (60
requests sent, Limitador's `authorized_calls` counter increased by exactly
60) showed **every single request was rate-limited, with no gap** - not
just no dropped requests. This is the structural payoff of ECDS being a
real xDS resource: Envoy's own listener-warming waits for the ECDS push
before activating the new listener version, so the *old* (already fully
enforcing) listener keeps serving every request until the real config is
ready - there's no window where an empty/default config is live.

## Key findings

### go-control-plane API research (grounding the implementation)

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
- `NodeHash` is a pluggable `interface{ ID(*core.Node) string }`; the
  library's default `IDHash` just returns `node.GetId()` (per-pod unique for
  Istio-managed proxies, not per-Gateway) - this PoC supplies its own
  `gatewayNodeHash` instead (see Architecture above, and the discovery
  narrative below).
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
  "value": "..."}` matches protojson's well-known-type special-casing for
  `google.protobuf.Any` fields (`PluginConfig.Configuration` is `*anypb.Any`).
  This meant no hand-built proto construction was needed for
  `BuildWasmExtensionConfigAny` - just marshal the existing map to JSON and
  `protojson.Unmarshal` it into `&httpwasmv3.Wasm{}`.
- **Open question from kuadrant-operator#2051, re: multi-wasm-module
  coexistence** (e.g. an Istio-native `WasmPlugin` CRD, which uses ECDS via
  istiod, adding its own wasm filter to the same gateway as this operator's
  own ECDS-delivered filter): reasoned to likely be fine - the ECDS/HTTP_FILTER
  proto model allows independently-named, independently-sourced filters in
  one filter chain; two distinct ECDS servers (istiod and this operator's)
  each owning a differently-named resource shouldn't conflict. **Not
  verified empirically** - flagged as a follow-up if this PoC progresses
  past prototype status.

### The static-cluster blocker (found live, root-caused, resolved)

The first approach tried was to wire up the ECDS server the same way
Kuadrant wires up everything else: an `EnvoyFilter` with a small
`config_discovery` HTTP_FILTER patch, plus a CLUSTER patch dynamically
defining `kuadrant-ecds` to point at the ECDS server. **That failed.**

The EnvoyFilter itself did shrink exactly as designed: 2,937 bytes compact
JSON, ~95% smaller than the 64,641 bytes measured for the same
HTTPRoute/TokenRateLimitPolicy with the full wasm config inlined - because
the EnvoyFilter now only carries the small `config_discovery` stub, not the
actual `actionSets`/CEL predicates. But the gateway's listener was rejected
outright: Envoy's error was `ApiConfigSource must have a statically defined
non-EDS cluster: 'kuadrant-ecds' does not exist, was added via api, or is an
EDS cluster` - even though the cluster existed and was healthy. The
message's own wording is the diagnosis: Envoy requires an `ApiConfigSource`'s
cluster to be statically defined in the proxy's bootstrap, not added
dynamically via CDS - which is exactly what the `EnvoyFilter` CLUSTER patch
produces, no matter how it's written. This is also why Istio's own native
`WasmPlugin` ECDS usage works: it reuses `xds-grpc`, the one cluster
`istio-agent` already bakes into every proxy's bootstrap for its own
connection to istiod - not something any `EnvoyFilter` could add after the
fact.

The one plausible `EnvoyFilter`-only workaround - `applyTo: BOOTSTRAP`
(deprecated in the Istio API, but still present) - was tried and confirmed
non-functional: the static cluster list was unchanged after applying it and
restarting the gateway pod.

This isn't cosmetic: the *whole* listener fails to update when this
happens, taking down every policy on the gateway, not just the wasm filter -
confirmed by deleting the TRLP (which removes the patch) and watching the
gateway recover within seconds.

### Researching and confirming the fix: `Gateway.spec.infrastructure.parametersRef`

Traced through Istio 1.29's actual source (the `kube-gateway.yaml` template
that renders this gateway's Deployment, and the deployment controller that
applies `parametersRef`):

- `sidecar.istio.io/bootstrapOverride` is a dead end for Gateway-API-managed
  gateways - it's only wired up in the legacy sidecar-injection templates,
  confirmed absent from `kube-gateway.yaml`.
- The underlying mechanism is just an env var (`ISTIO_BOOTSTRAP_OVERRIDE`,
  pointing at a file on disk) that `istio-agent` passes to Envoy as a native
  `--config-yaml` flag - nothing sidecar-specific about it, so anything that
  can get that env var and a mounted file onto the `istio-proxy` container
  works.
- `infrastructure.parametersRef`'s `deployment:` key supplies exactly that: a
  genuine Kubernetes strategic-merge-patch onto the generated Deployment,
  confirmed capable of safely appending a new volume, volumeMount, and env
  var without disturbing the rest of the spec.
- `ProxyConfig` (the obvious-looking alternative) can't substitute - it has
  no volume-mounting capability at all, only env vars.

The wiring, end to end (`manifests/05-06`):

```yaml
# Gateway points at a ConfigMap of Deployment overrides.
spec:
  infrastructure:
    parametersRef:
      group: ""
      kind: ConfigMap
      name: kuadrant-ingressgateway-params
```

```yaml
# That ConfigMap's `deployment:` key is strategic-merge-patched onto the
# generated Deployment: mounts a second ConfigMap as a file, points
# ISTIO_BOOTSTRAP_OVERRIDE at it.
spec:
  template:
    spec:
      containers:
      - name: istio-proxy
        env:
        - name: ISTIO_BOOTSTRAP_OVERRIDE
          value: /etc/istio/custom-bootstrap/custom_bootstrap.json
        volumeMounts:
        - name: custom-bootstrap-volume
          mountPath: /etc/istio/custom-bootstrap
      volumes:
      - name: custom-bootstrap-volume
        configMap:
          name: kuadrant-ecds-bootstrap
```

```json
// kuadrant-ecds-bootstrap's custom_bootstrap.json (trimmed): the static
// cluster Envoy reads straight from its own bootstrap at startup.
{
  "static_resources": {
    "clusters": [{
      "name": "kuadrant-ecds",
      "type": "STRICT_DNS",
      "load_assignment": {
        "cluster_name": "kuadrant-ecds",
        "endpoints": [{"lb_endpoints": [{"endpoint": {"address": {"socket_address": {
          "address": "kuadrant-operator-ecds.kuadrant-system.svc.cluster.local",
          "port_value": 50053
        }}}}]}]
      }
    }]
  }
}
```

Applied live via `manifests/06-gateway-parametersref-patch.sh`: confirmed via
`kubectl get deployment` that the patch landed correctly, via
`/config_dump`'s `BootstrapConfigDump` that `kuadrant-ecds` now appears as a
genuine static cluster (`added_via_api::false`), and - with the dynamic
CLUSTER patch disabled - that the gateway listener was accepted and serving
the real, fetched wasm config.

### Full functional verification

- **Real traffic enforcement**: 5 requests through the gateway to `llm-sim`
  all returned `200`; Limitador's `authorized_calls` counter incremented
  from 0 to exactly 5 in the same window.
- **Live config update, zero pod restart**: with the gateway pod already
  running, the TRLP's counter expression was changed from `request.method`
  to `request.path` via `kubectl patch`. The gateway pod's
  `creationTimestamp` was identical before and after (confirmed via
  `kubectl get pod -o jsonpath`, zero restarts) - yet the ECDS
  `version_info` changed (`f8b995... → e54b4a...`) and the live
  `EcdsConfigDump` content flipped from containing `request.method` to
  `request.path`. Envoy's own xDS client detected and applied the new
  `SetSnapshot` push from `ECDSStore.Push` with no wasm-side polling, no pod
  restart, and no hand-rolled fetch/retry logic of any kind.
- **Rate limiting responds correctly to the live-updated config**: with the
  TRLP's limit separately lowered to 2/minute (a Limitador-side-only change,
  unrelated to ECDS - rate limit thresholds live in Limitador's own config,
  not in the wasm plugin config pushed via ECDS), a follow-up request
  correctly received `429` after the limit was exceeded, confirming the
  live-swapped wasm config was genuinely active and functioning, not just
  present in the config dump.
- **HTTPRoute changes are still reconciled entirely by Istio, unaffected by
  this PoC**: requested a brand-new path (`/llm/connlink-1806-test`) before
  it had any matching HTTPRoute rule - got `404`. Patched the HTTPRoute to
  add a rule matching that exact path. Re-requested it - got `200`. The
  gateway pod's identity (name, `creationTimestamp`) was unchanged
  throughout. This PoC only patches the HTTP_FILTER/CLUSTER layers of the
  listener; routing (RDS) stays entirely Istio's own responsibility, exactly
  as in mainline.

This confirms the core idea - "let Envoy's native xDS client handle fetch,
retry, and hot-swap instead of hand-rolling it in wasm-shim" - **works**,
end-to-end, once the ECDS cluster has a legitimate static bootstrap
presence. The remaining gap is entirely about *how that static presence
gets provisioned* (manual today, see "Open follow-ups"), not about the
viability of ECDS itself for this use case.

### Per-gateway keying: how the real `Node` metadata shape was found

The initial implementation used a single fixed `NodeHash` (every connected
Envoy mapped to the same cache key), explicitly scoped as correct only for a
single-test-gateway pass. This was resolved by inspecting what `Node`
identity Istio-managed Gateway API proxies actually self-report over xDS,
rather than guessing: a temporary `StreamRequestFunc` callback was added to
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
    "istio": "ingressgateway"
  }
}
```

`node.metadata.NAMESPACE` and
`node.metadata.LABELS["gateway.networking.k8s.io/gateway-name"]` exactly
match `gateway.GetNamespace()`/`gateway.GetName()` - sourced from the pod's
own labels via Istio's `istio-podinfo` downward-API volume and
`ISTIO_META_*` env vars (not anything this operator adds), confirmed present
on every Gateway API-managed gateway pod regardless of this PoC. The
temporary logging callback was removed once `gatewayNodeHash` (see
Architecture) was implemented against this confirmed shape.

**Confirmed live**: rebuilt and redeployed with per-gateway keying,
restarted the gateway pod to force a fresh connection under the new
`NodeHash`, and confirmed via `/config_dump`'s `EcdsConfigDump` that the
gateway still correctly received its own real wasm config (not the
`default_config` stand-in) keyed by `gateway-system/kuadrant-ingressgateway`

- and that real traffic through the gateway still returned `200`. Only one
gateway exists in this test cluster, so this confirms the *mechanism*
resolves the documented single-key correctly, not yet a true multi-gateway
isolation test (that would need a second Gateway + TRLP to prove two
different snapshots exist simultaneously without cross-talk) -
`internal/ecds/store_test.go` covers that isolation property at the `Store`
level instead.

### The NetworkPolicy gap (found live, fixed)

The operator's generated `NetworkPolicy` didn't allow ingress on the new ECDS
port, silently dropping the gateway's connection at the network layer - fixed
in `operator_networkpolicy_reconciler.go` by adding a third ingress rule,
same pattern as the existing `grpc`/`wasm` ones.

## How to deploy / reproduce

Reproduction steps, on a fresh Kind cluster:

```sh
# 1. Build and deploy the operator with this PoC's changes.
make local-setup IMG=quay.io/kuadrant/kuadrant-operator:dev GATEWAYAPI_PROVIDER=istio

# 2. Apply the test workload: Kuadrant CR, llm-sim backend, a 19-match
#    HTTPRoute, and a TRLP.
kubectl apply -f poc/ecds-server/manifests/00-kuadrant-cr.yaml
kubectl apply -f poc/ecds-server/manifests/01-llm-namespace.yaml
kubectl apply -f poc/ecds-server/manifests/02-llm-sim.yaml
kubectl apply -f poc/ecds-server/manifests/03-httproute-llm-sim.yaml
kubectl apply -f poc/ecds-server/manifests/04-trlp-llm-sim.yaml

# 3. Provision the ECDS cluster as a static bootstrap cluster (manual today
#    - see Architecture above).
poc/ecds-server/manifests/06-gateway-parametersref-patch.sh

# 4. Verify: EnvoyFilter size, real traffic, and a live config update.
kubectl get envoyfilter kuadrant-kuadrant-ingressgateway -n gateway-system -o json | python3 -c \
  "import json,sys; print(len(json.dumps(json.load(sys.stdin), separators=(',', ':'))))"

GWIP=$(kubectl get gateway -n gateway-system kuadrant-ingressgateway -o jsonpath='{.status.addresses[0].value}')
curl -s -o /dev/null -w '%{http_code}\n' http://$GWIP/llm/llm-sim/v1/completions \
  -X POST -H 'Content-Type: application/json' -d '{"model":"Qwen/Qwen2.5-1.5B-Instruct","prompt":"hi","max_tokens":5}'
```

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
