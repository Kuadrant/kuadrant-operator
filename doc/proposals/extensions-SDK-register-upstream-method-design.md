# Feature: Extension SDK RegisterUpstreamMethod

## Key Points

1. **New `RegisterUpstreamMethod` method on `KuadrantCtx`** — extensions call it with an `UpstreamConfig{URL, Service, Method}` to register an external gRPC service (Service and Method are reserved for future use)
2. **Operator creates Envoy cluster + wasm service entry** — cluster is STRICT_DNS/HTTP2, wasm service uses `auth` type (hardcoded until wasm-shim adds `dynamic` type)
3. **Two distinct generated names, both prefixed with `ext-`**:
   - **Envoy cluster name**: derived from URL host + port, invalid chars replaced with hyphens (e.g. `ext-my-service-ns-svc-cluster-local-8081`)
   - **Wasm service key**: hash of the service config values (type, endpoint, failureMode, timeout) — identical configs naturally deduplicate to the same key
4. **Policy-scoped lifecycle** — registrations are tied to a policy and cleaned up automatically via `ClearPolicy`, consistent with `AddDataTo`
5. **Reachability check on registration** — operator performs a gRPC dial to the URL; returns `ErrUpstreamUnreachable` if it fails, allowing extensions to requeue
6. **No new reconcilers** — extends existing `IstioExtensionReconciler` and `EnvoyGatewayExtensionReconciler` to handle cluster creation and wasm config injection
7. **Future work** — `UpstreamReachable` (data plane reachability via metrics) and `dynamic` ServiceType are deferred

## Summary

Add a `RegisterUpstreamMethod` method to `KuadrantCtx` so that extensions can register arbitrary gRPC services with the data plane. The operator creates the corresponding Envoy cluster and wasm service entry, enabling extensions to introduce new upstream service integrations beyond the built-in auth, ratelimit, and tracing services.

## Goals

- Allow extensions to register gRPC services via `KuadrantCtx.RegisterUpstreamMethod`
- Automatically create Envoy clusters (STRICT_DNS, HTTP/2) for registered services
- Automatically add registered services to the wasm config services map
- Generate deterministic, collision-resistant names: Envoy cluster name from URL, wasm service key from config hash (natural deduplication)
- Tie service registrations to policy lifecycle (cleaned up via `ClearPolicy`)
- Support both Istio (EnvoyFilter) and Envoy Gateway (EnvoyPatchPolicy) providers

## Non-Goals

- Adding new `ServiceType` values to the wasm-shim — extensions use existing types
- Supporting non-gRPC services (e.g. HTTP/1.1 upstreams)
- Allowing extensions to define custom Actions or ActionSets (separate concern)
- Multi-cluster service discovery

## Design

### Backwards Compatibility

No breaking changes. `RegisterUpstreamMethod` is a new additive method on the `KuadrantCtx` interface. Existing extensions that do not call `RegisterUpstreamMethod` are unaffected. The gRPC proto gains a new RPC; existing clients ignore it.

### Architecture Changes

```
Phase 1: Extension registers a service
──────────────────────────────────────

Extension              SDK Client           Operator (gRPC server)
   │                       │                        │
   │── RegisterUpstreamMethod  ──►│                        │
   │   (policy,            │── RegisterUpstreamMethod ────►│
   │    UpstreamConfig)     │   RPC (unix socket)    │
   │                       │                        │── Dial svc.URL (reachability check)
   │                       │                        │── Parse svc.URL → host + port
   │                       │                        │── Generate cluster name (from URL)
   │                       │                        │── Store in RegisteredDataStore
   │                       │◄── OK / Unavailable ───│
   │◄── nil / error  ──────│                        │── Trigger reconciliation
   │                       │                        │


Phase 2: Operator reconciles the registered service
────────────────────────────────────────────────────

RegisteredDataStore
   │
   └──► Existing extension reconcilers (IstioExtensionReconciler / EnvoyGatewayExtensionReconciler)
           │
           ├── ServiceBuilder picks up registered services → adds to wasm config services map
           │   (wasm service key = ext- + hash of config values, endpoint points to URL-based cluster name)
           │   (identical configs hash to same key → natural deduplication)
           │
           └── Creates cluster patches for registered services (cluster name from URL)
               ├── Istio:         adds EnvoyFilter with cluster patch
               └── EnvoyGateway:  adds EnvoyPatchPolicy with cluster patch
```

Registered service handling is integrated into the existing `IstioExtensionReconciler` and `EnvoyGatewayExtensionReconciler` rather than creating new reconcilers. These already build wasm configs and manage per-gateway resources, so cluster creation for registered services is a natural extension of their responsibilities.

### API Changes

#### KuadrantCtx Interface

```go
// pkg/extension/types/types.go

type UpstreamConfig struct {
    URL     string // e.g. "grpc://my-service:8081"
    Service string // gRPC service name, e.g. "envoy.service.auth.v3.Authorization" (currently unused)
    Method  string // gRPC method name, e.g. "Check" (currently unused)
}

// Defaults applied by the operator:
//   Type:        "auth" (will change to "dynamic" once supported by wasm-shim)
//   FailureMode: "deny"
//   Timeout:     "100ms"
//
// Generated names (not user-specified):
//   Envoy cluster name: "ext-" + URL host + port, invalid chars → hyphens
//   Wasm service key:   "ext-" + hash of (type, endpoint, failureMode, timeout)

type KuadrantCtx interface {
    Resolve(context.Context, Policy, string, bool) (celref.Val, error)
    ResolvePolicy(context.Context, Policy, string, bool) (Policy, error)
    AddDataTo(context.Context, Policy, Domain, string, string) error
    ReconcileObject(context.Context, client.Object, client.Object, MutateFn) (client.Object, error)
    RegisterUpstreamMethod(ctx context.Context, policy Policy, svc UpstreamConfig) error // new
}
```

#### gRPC Proto

```protobuf
// pkg/extension/grpc/v1/kuadrant.proto

service ExtensionService {
  // ... existing RPCs ...
  rpc RegisterUpstreamMethod(RegisterUpstreamMethodRequest) returns (google.protobuf.Empty) {}
}

message RegisterUpstreamMethodRequest {
  Policy policy = 1;
  string url = 2;               // e.g. "grpc://my-service:8081"
  string service = 3;           // gRPC service name (currently unused, reserved for future routing)
  string method = 4;            // gRPC method name (currently unused, reserved for future routing)
  // service_type, failure_mode, and timeout are reserved for future use.
  // Operator defaults: type = "auth", failure_mode = "deny", timeout = "100ms"
  // Names are generated server-side:
  //   Envoy cluster: from URL host+port
  //   Wasm service key: hash of service config values
}
```

#### Naming

The Envoy cluster name and wasm service key are distinct values, both generated by the operator — neither is user-specified. Both are prefixed with `ext-` to prevent collisions with built-in services.

**Envoy cluster name** — derived from the URL host and port. Non-alphanumeric characters (dots, colons, slashes) are replaced with hyphens:

```
URL:          grpc://authorino-authorino-authorization.kuadrant-system.svc.cluster.local:50051
Cluster name: ext-authorino-authorino-authorization-kuadrant-system-svc-cluster-local-50051
```

**Wasm service key** — generated by hashing the service config values (type, endpoint, failureMode, timeout) and prefixing with `ext-`. This provides natural deduplication: identical service configurations always produce the same key, so duplicate entries are impossible regardless of how many policies or targetRefs register the same service.

```
Service config:
  endpoint:    ext-authorino-authorino-authorization-kuadrant-system-svc-cluster-local-50051
  type:        auth
  failureMode: deny
  timeout:     100ms

Service key:   ext-<sha256 of "auth|ext-authorino-...-50051|deny|100ms">
               e.g. ext-a1b2c3d4e5f6...  (truncated hash)
```

Two policies registering the same URL produce the same cluster name and therefore the same service config — the hash matches and only one wasm service entry exists.

#### Error Handling

When `RegisterUpstreamMethod` is called, the operator performs a gRPC dial attempt to the provided URL with a short timeout (5 seconds). If the service is unreachable, the operator returns gRPC status `Unavailable`. The client-side SDK translates this into a typed sentinel error that extension authors can check:

```go
// pkg/extension/types/errors.go

// ErrUpstreamUnreachable is returned by RegisterUpstreamMethod when the operator
// cannot establish a gRPC connection to the provided URL.
var ErrUpstreamUnreachable = errors.New("upstream unreachable")
```

The client-side `RegisterUpstreamMethod` implementation inspects the gRPC status code and wraps appropriately:

```go
// pkg/extension/controller/controller.go (inside RegisterUpstreamMethod)

request := &extpb.RegisterUpstreamMethodRequest{
    Policy: convertPolicyToProtobuf(policy),
    Url:    svc.URL,
}
_, err := ec.extensionClient.client.RegisterUpstreamMethod(ctx, request)
if err != nil {
    if st, ok := status.FromError(err); ok && st.Code() == codes.Unavailable {
        return fmt.Errorf("%w: %s", types.ErrUpstreamUnreachable, st.Message())
    }
    return err
}
```

The server-side handler performs the dial check before storing the registration:

```go
// internal/extension/manager.go (inside RegisterUpstreamMethod handler)

conn, err := grpc.NewClient(request.Url,
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
if err != nil {
    return nil, status.Errorf(codes.Unavailable, "cannot reach service at %s: %v", request.Url, err)
}
conn.Close()
```

#### Extension Author Usage

```go
func (r *MyReconciler) Reconcile(ctx context.Context, req reconcile.Request, kCtx types.KuadrantCtx) (reconcile.Result, error) {
    policy := &v1alpha1.MyPolicy{}
    r.Client.Get(ctx, req.NamespacedName, policy)

    err := kCtx.RegisterUpstreamMethod(ctx, policy, types.UpstreamConfig{
        URL:     "grpc://my-authservice.my-ns.svc.cluster.local:8081",
        Service: "envoy.service.auth.v3.Authorization", // currently unused
        Method:  "Check",                                // currently unused
    })
    if errors.Is(err, types.ErrUpstreamUnreachable) {
        // Service not available yet — requeue and retry later
        return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
    }
    if err != nil {
        return reconcile.Result{}, err
    }

    // Envoy cluster created:   "ext-my-authservice-my-ns-svc-cluster-local-8081"
    // Wasm service key created: "ext-<hash of config values>" (deduplicated across policies)
    // Cleanup happens automatically when ClearPolicy is called for this policy

    return reconcile.Result{}, nil
}
```

### Component Changes

#### 1. RegisteredDataStore (internal/extension/registry.go)

Add a new `registeredUpstreams` map alongside the existing `dataProviders` and `subscriptions` maps:

```go
type RegisteredUpstreamKey struct {
    Policy ResourceID
    URL    string
}

type RegisteredUpstreamEntry struct {
    URL         string       // original URL from the extension
    ClusterName string       // generated: ext-{host}-{port}
    TargetRefs  []TargetRef  // extracted from the policy at registration time
    FailureMode string
    Timeout     string
}

type TargetRef struct {
    Group     string
    Kind      string
    Name      string
    Namespace string // same as the policy's namespace (local refs)
}
```

The key contains the `Policy` identity; the entry does not duplicate it. `TargetRefs` are extracted from the policy's `GetTargetRefs()` at registration time and stored in the entry so that reconcilers can determine which gateway's wasm config should include the upstream — without needing to re-resolve the policy object. `ClearPolicyData` clears all entries matching the policy's `ResourceID`.

#### 2. extensionService (internal/extension/manager.go)

New `RegisterUpstreamMethod` handler:
- Parses `request.Url` to extract host and port
- Performs a gRPC dial attempt to the URL with a 5-second timeout
- If dial fails, returns gRPC status `Unavailable` with descriptive message
- If dial succeeds, generates Envoy cluster name: `ext-` + host + port with invalid chars replaced by hyphens
- Extracts `TargetRefs` from the policy in the request
- Stores `RegisteredUpstreamEntry` (with `ClusterName` and `TargetRefs`) in `RegisteredDataStore` keyed by `(Policy, URL)`
- Triggers reconciliation via `changeNotifier`

#### 3. MutatorRegistry — WasmConfig mutation (internal/extension/registry.go)

Extend `mutateWasmConfig` (or add a new mutator) to inject registered services into the `Config.Services` map. For each registered service entry, build the `wasm.Service` and generate the wasm service key by hashing its config values. Identical configurations naturally produce the same hash, so deduplication is inherent — no extra logic needed.

```go
timeout := "100ms"
svc := wasm.Service{
    Endpoint:    entry.ClusterName, // points to the URL-based Envoy cluster
    Type:        wasm.AuthServiceType, // default; will change to "dynamic" once supported
    FailureMode: wasm.FailureModeDeny,
    Timeout:     &timeout,
}

// Hash the config values to produce a deterministic, deduplicated key
// Identical services (same endpoint, type, failureMode, timeout) → same hash → same key
wasmServiceKey := "ext-" + hashUpstreamConfig(svc) // sha256 of "auth|ext-...|deny|100ms"
wasmConfig.Services[wasmServiceKey] = svc
```

Multiple policies registering the same URL produce the same cluster name, and therefore the same service config hash — the map key is identical, so they naturally collapse to a single entry.

#### 4. IstioExtensionReconciler (internal/controller/istio_extension_reconciler.go)

Extend the existing reconciler to handle registered upstreams:
- In `buildWasmConfigs`: read registered upstreams from `RegisteredDataStore`, filter by `TargetRefs` to include only entries whose targets resolve to the current gateway, generate wasm service keys by hashing config values, and add them to the `ServiceBuilder` via `WithService(wasmServiceKey, service)` where `service.Endpoint = entry.ClusterName`
- In `Reconcile`: for each gateway, also create/update an `EnvoyFilter` with cluster patches for upstreams targeting that gateway using `buildClusterPatch(entry.ClusterName, host, port, false, true)` (HTTP/2 enabled)
- Cleanup: delete cluster EnvoyFilters when registered upstreams are removed

#### 5. EnvoyGatewayExtensionReconciler (internal/controller/envoy_gateway_extension_reconciler.go)

Same changes as the Istio variant:
- In `buildWasmConfigs`: filter registered upstreams by `TargetRefs` for the current gateway, generate wasm service keys by hashing config values, and add them to the `ServiceBuilder`
- In `Reconcile`: create/update `EnvoyPatchPolicy` resources with cluster patches for upstreams targeting that gateway using `BuildEnvoyPatchPolicyClusterPatch` with `entry.ClusterName`

#### 7. Extension Controller client side (pkg/extension/controller/controller.go)

Implement `RegisterUpstreamMethod` on `ExtensionController`:
- Convert `UpstreamConfig.URL` and policy to proto `RegisterUpstreamMethodRequest`
- Call `ec.extensionClient.client.RegisterUpstreamMethod(ctx, request)`
- Translate gRPC `Unavailable` status to `types.ErrUpstreamUnreachable`

### Security Considerations

- **URL validation**: The operator must validate that the URL is well-formed and uses the `grpc://` scheme. Reject URLs with credentials or query parameters.
- **No user-controlled names in Envoy/wasm config**: Both the cluster name and wasm service key are operator-generated, preventing injection of crafted identifiers.
- **No privilege escalation**: Extensions can only register services reachable from the data plane network. The cluster is created with the same access level as existing auth/ratelimit clusters.
- **Policy-scoped cleanup**: Services cannot outlive their owning policy, preventing resource leaks.

## Implementation Plan

1. Extend `RegisteredDataStore` with service registration storage and `ClearPolicyData` cleanup
2. Add `RegisterUpstreamMethod` RPC to the gRPC proto and regenerate
3. Implement server-side `RegisterUpstreamMethod` handler in `extensionService`
4. Extend `IstioExtensionReconciler` to add registered services to `ServiceBuilder` and create cluster EnvoyFilters
5. Extend `EnvoyGatewayExtensionReconciler` to add registered services to `ServiceBuilder` and create cluster EnvoyPatchPolicies
6. Implement client-side `RegisterUpstreamMethod` on `ExtensionController`
7. Add `RegisterUpstreamMethod` to `KuadrantCtx` interface and `UpstreamConfig` type

## Testing Strategy

- **Unit tests**: URL parsing, cluster name generation, service config hashing and deduplication, `RegisteredDataStore` service CRUD, `ClearPolicyData` cleanup, wasm config mutation, gRPC dial failure → `ErrUpstreamUnreachable` mapping
- **Integration tests**: End-to-end RegisterUpstreamMethod flow with Istio and EnvoyGateway — verify EnvoyFilter/EnvoyPatchPolicy creation, wasm config services map population, cleanup on policy deletion

## Open Questions

- Should mTLS be configurable per registered service, or always disabled for extension services?
- Should there be a limit on the number of services an extension can register?

## Future Work

### UpstreamReachable — Data Plane Reachability Check

A `UpstreamReachable` method on `KuadrantCtx` that checks whether the wasm-shim can actually reach a registered service, as opposed to `RegisterUpstreamMethod` which only validates reachability from the operator.

The intended approach is to query metrics emitted by the wasm-shim via a Prometheus client. However, this is **blocked** by two prerequisites:

1. **Wasm-shim per-service metrics**: The wasm-shim currently emits only aggregate counters (`kuadrant.hits`, `kuadrant.errors`, etc.) without per-service labels. Upstream changes to the wasm-shim are needed to add a `service` label so that errors and connectivity failures can be attributed to a specific registered service.

2. **Prometheus endpoint configuration**: The Kuadrant CR has no field for a Prometheus query endpoint. A new CRD field (e.g. `spec.observability.prometheus.url`) would be needed so the operator knows where to query.

Once those prerequisites are met, the API would look like:

```go
type KuadrantCtx interface {
    // ... existing methods ...
    RegisterUpstreamMethod(ctx context.Context, policy Policy, svc UpstreamConfig) error
    UpstreamReachable(ctx context.Context, policy Policy, svc UpstreamConfig) (bool, error)
}
```

Extension authors would use it to check data plane connectivity before relying on a service:

```go
reachable, err := kCtx.UpstreamReachable(ctx, policy, svcConfig)
if err != nil {
    return reconcile.Result{}, err
}
if !reachable {
    // Service registered but wasm-shim cannot reach it yet
    return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
}
```

### Dynamic ServiceType

The `UpstreamConfig` currently has no `Type` field — the operator hardcodes `auth` (ext_authz protocol). Once a `dynamic` service type is added to the wasm-shim, the operator will switch the hardcoded default from `auth` to `dynamic`. No `Type` field will be exposed on `UpstreamConfig` — registered services will always use the `dynamic` type.

### Upstream Method Routing

`UpstreamConfig` already includes `Service` and `Method` fields (e.g. `envoy.service.auth.v3.Authorization` / `Check`), but they are currently unused by the operator. Once the wasm-shim supports dynamic service types, these fields will be used to configure method-level routing — telling the wasm-shim which gRPC service and method to invoke on the upstream, rather than relying on the hardcoded ext_authz protocol.

## Demo: RegisterUpstreamMethod with Authorino

A two-part demo using the already-deployed Authorino instance to demonstrate RegisterUpstreamMethod without deploying any new services.

### Concept

Authorino is already running in the cluster as part of the Kuadrant stack and implements the `envoy.service.auth.v3.Authorization/Check` RPC. The demo extension registers Authorino as a second, extension-managed auth service via RegisterUpstreamMethod — proving the infrastructure works with a real, reachable gRPC service.

### DemoPolicy Extension

A minimal extension that registers the existing Authorino as an extension-managed service:

```yaml
# DemoPolicy CRD instance
apiVersion: extensions.kuadrant.io/v1alpha1
kind: DemoPolicy
metadata:
  name: demo-auth
  namespace: default
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: my-api
```

Extension reconciler:

```go
func (r *DemoPolicyReconciler) Reconcile(ctx context.Context, req reconcile.Request, kCtx types.KuadrantCtx) (reconcile.Result, error) {
    policy := &v1alpha1.DemoPolicy{}
    r.Client.Get(ctx, req.NamespacedName, policy)

    // Register the already-deployed Authorino as an extension-managed service
    // Authorino listens on port 50051 for gRPC authorization
    err := kCtx.RegisterUpstreamMethod(ctx, policy, types.UpstreamConfig{
        URL:     "grpc://authorino-authorino-authorization.kuadrant-system.svc.cluster.local:50051",
        Service: "envoy.service.auth.v3.Authorization",
        Method:  "Check",
    })
    if errors.Is(err, types.ErrUpstreamUnreachable) {
        return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
    }
    return reconcile.Result{}, err

    // Generated names:
    //   Envoy cluster: ext-authorino-authorino-authorization-kuadrant-system-svc-cluster-local-50051
    //   Wasm service:  ext-<hash of config values> (deduplicated by content)
}
```

### Demo Script

**Part 1: RegisterUpstreamMethod infrastructure**

```bash
# 1. Verify Authorino is running
kubectl get pods -n kuadrant-system -l app=authorino
#    Expected: authorino pod Running

# 2. Apply the DemoPolicy
kubectl apply -f examples/demo-policy/demo-policy.yaml

# 3. Verify the Envoy cluster was created (name derived from URL)
#    (Istio)
istioctl proxy-config cluster deploy/istio-ingressgateway -n istio-system | grep ext-authorino-authorino-authorization
#    Expected: ext-authorino-authorino-authorization-kuadrant-system-svc-cluster-local-50051   STRICT_DNS   ...

#    (Envoy Gateway)
kubectl get envoyPatchPolicy -n envoy-gateway-system -o yaml | grep ext-authorino-authorino-authorization

# 4. Verify the service appears in the wasm config (key is hash of config values)
#    (Istio)
kubectl get wasmplugin -n istio-system -o jsonpath='{.items[0].spec.pluginConfig}' | jq '.services'
#    Expected: "ext-<hash>": {
#      "endpoint": "ext-authorino-authorino-authorization-kuadrant-system-svc-cluster-local-50051",
#      "type": "auth", "failureMode": "deny", "timeout": "100ms"
#    }
#    Note: the built-in "auth-service" entry also remains — both coexist
#    Note: the key is a hash, so look for any "ext-" prefixed entry

# 5. Delete the DemoPolicy and verify cleanup
kubectl delete demopolicy demo-auth
istioctl proxy-config cluster deploy/istio-ingressgateway -n istio-system | grep ext-authorino-authorino-authorization
#    Expected: (empty — cluster removed, built-in auth cluster unaffected)
```

**Part 2: Traffic flow (manual action wiring)**

```bash
# 6. Re-apply the DemoPolicy
kubectl apply -f examples/demo-policy/demo-policy.yaml

# 7. Patch the WasmPlugin to add an action referencing the extension-registered service
#    This adds an action for all requests to the HTTPRoute, referencing the wasm service key
#    Find the ext- service key from step 4
kubectl patch wasmplugin kuadrant-istio-ingressgateway -n istio-system --type merge -p '
  ... (add action with service: "ext-<hash from step 4>") ...'

# 8. Send traffic — Authorino evaluates the request via the extension-registered service
curl -v http://my-api.example.com/anything
#    Authorino processes the request through the ext-authorino-...-50051 cluster

# 9. Check Authorino logs for the request
kubectl logs deploy/authorino -n kuadrant-system
```

### What the demo proves

- **Part 1**: RegisterUpstreamMethod creates a new Envoy cluster (named from URL) and wasm service entry (keyed by config hash) pointing to an already-running service. The built-in `auth-service` is unaffected — both coexist. Cleanup works when the policy is deleted.
- **Part 2**: The extension-registered service is callable from the data plane. The wasm-shim routes traffic to Authorino via the URL-derived cluster, independent of the built-in auth flow.
- **No new services deployed**: The demo uses only infrastructure already present in a standard Kuadrant installation.

**Note**: Part 2 requires manual action wiring. A future `RegisterAction` method on `KuadrantCtx` would allow extensions to automate this step.

## Execution

### Todo

- [ ] Extend RegisteredDataStore with service storage
  - [ ] Unit tests
- [ ] Add RegisterUpstreamMethod RPC to gRPC proto and regenerate
  - [ ] Unit tests
- [ ] Implement server-side RegisterUpstreamMethod handler
  - [ ] Unit tests
- [ ] Extend IstioExtensionReconciler with registered service support
  - [ ] Unit tests
  - [ ] Integration tests
- [ ] Extend EnvoyGatewayExtensionReconciler with registered service support
  - [ ] Unit tests
  - [ ] Integration tests
- [ ] Implement client-side RegisterUpstreamMethod on ExtensionController
  - [ ] Unit tests
- [ ] Add RegisterUpstreamMethod to KuadrantCtx interface and UpstreamConfig type
- [ ] Create demo entities and interactive demo script
  - [ ] DemoPolicy CRD and extension reconciler (`cmd/extensions/demo-policy/`)
  - [ ] DemoPolicy manifest (`examples/demo-policy/demo-policy.yaml`)
  - [ ] Interactive demo script (`examples/demo-policy/demo.sh`) that walks through Part 1 and Part 2, pausing at each step for discussion

### Completed

## Change Log

### 2026-03-06 — Add Service and Method to UpstreamConfig

- Added `Service` (gRPC service name) and `Method` (gRPC method name) fields to `UpstreamConfig` and proto `RegisterUpstreamMethodRequest`
- Both fields are currently unused — reserved for future method-level routing once wasm-shim supports dynamic service types
- Updated future work: "Upstream Method Definitions" → "Upstream Method Routing" since the API surface is already in place

### 2026-03-06 — Store TargetRefs, remove Policy from entry

- Removed `Policy` from `RegisteredUpstreamEntry` — it's already in the `RegisteredUpstreamKey`, no need to duplicate
- Added `TargetRefs` (group, kind, name, namespace) to `RegisteredUpstreamEntry`, extracted from the policy at registration time
- Reconcilers filter registered upstreams by `TargetRefs` to determine which gateway's wasm config should include each upstream
- Added `TargetRef` struct to hold target reference identity

### 2026-03-05 — Rename to RegisterUpstreamMethod

- Renamed `RegisterService` → `RegisterUpstreamMethod` throughout
- Renamed related types: `ServiceConfig` → `UpstreamConfig`, `ErrServiceUnreachable` → `ErrUpstreamUnreachable`, `RegisteredServiceEntry` → `RegisteredUpstreamEntry`, `RegisteredServiceKey` → `RegisteredUpstreamKey`
- Renamed future work: `ServiceReachable` → `UpstreamReachable`
- Added future work: upstream method definitions via `UpstreamConfig`

### 2026-03-05 — Hash-based wasm service key with natural deduplication

- Wasm service key is now `ext-` + SHA-256 hash of the service config values (type, endpoint, failureMode, timeout)
- Identical service configurations produce the same hash → natural deduplication without extra logic
- Replaces the earlier policy+targetRef-based naming approach
- Envoy cluster name remains URL-derived (unchanged)

### 2026-03-05 — Split cluster name and wasm service key

- Envoy cluster name is now derived from the URL (host + port, invalid chars → hyphens), not user-provided
- Removed `Name` field from `UpstreamConfig` — neither name is user-specified
- Both names still prefixed with `ext-` to prevent collisions with built-in services
- `RegisteredUpstreamKey` now keyed by `(Policy, URL)` instead of `(Policy, Name)`
- Removed `name` field from gRPC proto `RegisterUpstreamMethodRequest`

### 2026-03-04 — Initial design

- Chose policy-scoped lifecycle (consistent with AddDataTo pattern)
- Chose to extend existing IstioExtensionReconciler and EnvoyGatewayExtensionReconciler rather than creating new reconcilers
- UpstreamConfig contains only URL; Type hardcoded to `auth`, will change to `dynamic` once wasm-shim supports it
- No wasm-shim changes needed for initial implementation
- Added gRPC dial reachability check — returns `ErrUpstreamUnreachable` sentinel error via gRPC `Unavailable` status
- Deferred `UpstreamReachable` (data plane reachability via wasm-shim metrics) as future work — blocked on wasm-shim per-service metric labels and Prometheus endpoint configuration in Kuadrant CR

## References

- [wasm-shim envoy.yaml example (cluster config)](https://github.com/Kuadrant/wasm-shim/blob/main/e2e/basic/envoy.yaml#L118-L135)
