# Feature: Extension SDK RegisterService

## Key Points

1. **New `RegisterService` method on `KuadrantCtx`** — extensions call it with a `ServiceConfig{Name, URL}` to register an external gRPC service
2. **Operator creates Envoy cluster + wasm service entry** — cluster is STRICT_DNS/HTTP2, wasm service uses `auth` type (hardcoded until wasm-shim adds `dynamic` type)
3. **Names prefixed with `ext-`** — extension-provided name is prefixed to avoid collisions with built-in services (e.g. `"my-auth"` → `"ext-my-auth"`)
4. **Policy-scoped lifecycle** — registrations are tied to a policy and cleaned up automatically via `ClearPolicy`, consistent with `AddDataTo`
5. **Reachability check on registration** — operator performs a gRPC dial to the URL; returns `ErrServiceUnreachable` if it fails, allowing extensions to requeue
6. **No new reconcilers** — extends existing `IstioExtensionReconciler` and `EnvoyGatewayExtensionReconciler` to handle cluster creation and wasm config injection
7. **Future work** — `ServiceReachable` (data plane reachability via metrics) and `dynamic` ServiceType are deferred

## Summary

Add a `RegisterService` method to `KuadrantCtx` so that extensions can register arbitrary gRPC services with the data plane. The operator creates the corresponding Envoy cluster and wasm service entry, enabling extensions to introduce new upstream service integrations beyond the built-in auth, ratelimit, and tracing services.

## Goals

- Allow extensions to register gRPC services via `KuadrantCtx.RegisterService`
- Automatically create Envoy clusters (STRICT_DNS, HTTP/2) for registered services
- Automatically add registered services to the wasm config services map
- Generate deterministic, collision-resistant cluster names from service type + host
- Tie service registrations to policy lifecycle (cleaned up via `ClearPolicy`)
- Support both Istio (EnvoyFilter) and Envoy Gateway (EnvoyPatchPolicy) providers

## Non-Goals

- Adding new `ServiceType` values to the wasm-shim — extensions use existing types
- Supporting non-gRPC services (e.g. HTTP/1.1 upstreams)
- Allowing extensions to define custom Actions or ActionSets (separate concern)
- Multi-cluster service discovery

## Design

### Backwards Compatibility

No breaking changes. `RegisterService` is a new additive method on the `KuadrantCtx` interface. Existing extensions that do not call `RegisterService` are unaffected. The gRPC proto gains a new RPC; existing clients ignore it.

### Architecture Changes

```
Phase 1: Extension registers a service
──────────────────────────────────────

Extension              SDK Client           Operator (gRPC server)
   │                       │                        │
   │── RegisterService  ──►│                        │
   │   (policy,            │── RegisterService ────►│
   │    ServiceConfig)     │   RPC (unix socket)    │
   │                       │                        │── Dial svc.URL (reachability check)
   │                       │                        │── Parse svc.URL → host + port
   │                       │                        │── Generate cluster name
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
           │   (cluster name = service key = endpoint)
           │
           └── Creates cluster patches for registered services
               ├── Istio:         adds EnvoyFilter with cluster patch
               └── EnvoyGateway:  adds EnvoyPatchPolicy with cluster patch
```

Registered service handling is integrated into the existing `IstioExtensionReconciler` and `EnvoyGatewayExtensionReconciler` rather than creating new reconcilers. These already build wasm configs and manage per-gateway resources, so cluster creation for registered services is a natural extension of their responsibilities.

### API Changes

#### KuadrantCtx Interface

```go
// pkg/extension/types/types.go

type ServiceConfig struct {
    Name string // cluster name and wasm service key, e.g. "my-auth-service"
    URL  string // e.g. "grpc://my-service:8081"
}

// Defaults applied by the operator:
//   Type:        "auth" (will change to "dynamic" once supported by wasm-shim)
//   FailureMode: "deny"
//   Timeout:     "100ms"

type KuadrantCtx interface {
    Resolve(context.Context, Policy, string, bool) (celref.Val, error)
    ResolvePolicy(context.Context, Policy, string, bool) (Policy, error)
    AddDataTo(context.Context, Policy, Domain, string, string) error
    ReconcileObject(context.Context, client.Object, client.Object, MutateFn) (client.Object, error)
    RegisterService(ctx context.Context, policy Policy, svc ServiceConfig) error // new
}
```

#### gRPC Proto

```protobuf
// pkg/extension/grpc/v1/kuadrant.proto

service ExtensionService {
  // ... existing RPCs ...
  rpc RegisterService(RegisterServiceRequest) returns (google.protobuf.Empty) {}
}

message RegisterServiceRequest {
  Policy policy = 1;
  string name = 2;              // cluster name and wasm service key
  string url = 3;               // e.g. "grpc://my-service:8081"
  // service_type, failure_mode, and timeout are reserved for future use.
  // Operator defaults: type = "auth", failure_mode = "deny", timeout = "100ms"
}
```

#### Cluster Naming

The cluster name is provided by the extension author via `ServiceConfig.Name`. The operator prefixes it with `ext-` to prevent collisions with built-in services (e.g. `kuadrant-auth-service`, `kuadrant-ratelimit-service`). The prefixed value is used as both the Envoy cluster name and the wasm config service map key.

```
Input:  Name = "my-auth-service"
Result: "ext-my-auth-service" (cluster name + wasm service key)
```

The operator validates the name is non-empty and contains only valid Envoy cluster identifier characters (alphanumeric, hyphens, underscores).

#### Error Handling

When `RegisterService` is called, the operator performs a gRPC dial attempt to the provided URL with a short timeout (5 seconds). If the service is unreachable, the operator returns gRPC status `Unavailable`. The client-side SDK translates this into a typed sentinel error that extension authors can check:

```go
// pkg/extension/types/errors.go

// ErrServiceUnreachable is returned by RegisterService when the operator
// cannot establish a gRPC connection to the provided URL.
var ErrServiceUnreachable = errors.New("service unreachable")
```

The client-side `RegisterService` implementation inspects the gRPC status code and wraps appropriately:

```go
// pkg/extension/controller/controller.go (inside RegisterService)

request := &extpb.RegisterServiceRequest{
    Policy: convertPolicyToProtobuf(policy),
    Name:   svc.Name,
    Url:    svc.URL,
}
_, err := ec.extensionClient.client.RegisterService(ctx, request)
if err != nil {
    if st, ok := status.FromError(err); ok && st.Code() == codes.Unavailable {
        return fmt.Errorf("%w: %s", types.ErrServiceUnreachable, st.Message())
    }
    return err
}
```

The server-side handler performs the dial check before storing the registration:

```go
// internal/extension/manager.go (inside RegisterService handler)

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

    err := kCtx.RegisterService(ctx, policy, types.ServiceConfig{
        Name: "my-authservice",
        URL:  "grpc://my-authservice:8081",
    })
    if errors.Is(err, types.ErrServiceUnreachable) {
        // Service not available yet — requeue and retry later
        return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
    }
    if err != nil {
        return reconcile.Result{}, err
    }

    // Service is now available in wasm config as "ext-my-authservice"
    // Cluster "ext-my-authservice" is created in Envoy config
    // Cleanup happens automatically when ClearPolicy is called for this policy

    return reconcile.Result{}, nil
}
```

### Component Changes

#### 1. RegisteredDataStore (internal/extension/registry.go)

Add a new `registeredServices` map alongside the existing `dataProviders` and `subscriptions` maps:

```go
type RegisteredServiceEntry struct {
    Policy ResourceID
    Name   string
    URL    string
    FailureMode string
    Timeout     string
}

type RegisteredServiceKey struct {
    Policy ResourceID
    Name   string
}
```

Services are stored keyed by `(Policy, Name)`. `ClearPolicyData` is extended to also clear registered services for the deleted policy.

#### 2. extensionService (internal/extension/manager.go)

New `RegisterService` handler:
- Parses `request.Url` to extract host and port
- Performs a gRPC dial attempt to the URL with a 5-second timeout
- If dial fails, returns gRPC status `Unavailable` with descriptive message
- If dial succeeds, prefixes `request.Name` with `ext-` to form cluster name
- Stores in `RegisteredDataStore`
- Triggers reconciliation via `changeNotifier`

#### 3. MutatorRegistry — WasmConfig mutation (internal/extension/registry.go)

Extend `mutateWasmConfig` (or add a new mutator) to inject registered services into the `Config.Services` map. For each registered service entry, add:

```go
timeout := "100ms"
clusterName := "ext-" + entry.Name
wasmConfig.Services[clusterName] = wasm.Service{
    Endpoint:    clusterName,
    Type:        wasm.AuthServiceType, // default; will change to "dynamic" once supported
    FailureMode: wasm.FailureModeDeny,
    Timeout:     &timeout,
}
```

#### 4. IstioExtensionReconciler (internal/controller/istio_extension_reconciler.go)

Extend the existing reconciler to handle registered services:
- In `buildWasmConfigs`: read registered services from `RegisteredDataStore` and add them to the `ServiceBuilder` via `WithService("ext-"+entry.Name, service)`
- In `Reconcile`: for each gateway, also create/update an `EnvoyFilter` with cluster patches for all registered services using `buildClusterPatch(clusterName, host, port, false, true)` (HTTP/2 enabled)
- Cleanup: delete cluster EnvoyFilters when registered services are removed

#### 5. EnvoyGatewayExtensionReconciler (internal/controller/envoy_gateway_extension_reconciler.go)

Same changes as the Istio variant:
- In `buildWasmConfigs`: add registered services to the `ServiceBuilder`
- In `Reconcile`: create/update `EnvoyPatchPolicy` resources with cluster patches for registered services using `BuildEnvoyPatchPolicyClusterPatch`

#### 7. Extension Controller client side (pkg/extension/controller/controller.go)

Implement `RegisterService` on `ExtensionController`:
- Extract `URL` from `ServiceConfig`, convert to proto `RegisterServiceRequest`
- Call `ec.extensionClient.client.RegisterService(ctx, request)`
- Translate gRPC `Unavailable` status to `types.ErrServiceUnreachable`

### Security Considerations

- **URL validation**: The operator must validate that the URL is well-formed and uses the `grpc://` scheme. Reject URLs with credentials or query parameters.
- **Cluster name validation**: Extension-provided names must be valid Envoy cluster identifiers (alphanumeric, hyphens, underscores). The operator rejects invalid names.
- **No privilege escalation**: Extensions can only register services reachable from the data plane network. The cluster is created with the same access level as existing auth/ratelimit clusters.
- **Policy-scoped cleanup**: Services cannot outlive their owning policy, preventing resource leaks.

## Implementation Plan

1. Extend `RegisteredDataStore` with service registration storage and `ClearPolicyData` cleanup
2. Add `RegisterService` RPC to the gRPC proto and regenerate
3. Implement server-side `RegisterService` handler in `extensionService`
4. Extend `IstioExtensionReconciler` to add registered services to `ServiceBuilder` and create cluster EnvoyFilters
5. Extend `EnvoyGatewayExtensionReconciler` to add registered services to `ServiceBuilder` and create cluster EnvoyPatchPolicies
6. Implement client-side `RegisterService` on `ExtensionController`
7. Add `RegisterService` to `KuadrantCtx` interface and `ServiceConfig` type

## Testing Strategy

- **Unit tests**: URL parsing, name validation, `RegisteredDataStore` service CRUD, `ClearPolicyData` cleanup, wasm config mutation, gRPC dial failure → `ErrServiceUnreachable` mapping
- **Integration tests**: End-to-end RegisterService flow with Istio and EnvoyGateway — verify EnvoyFilter/EnvoyPatchPolicy creation, wasm config services map population, cleanup on policy deletion

## Open Questions

- Should mTLS be configurable per registered service, or always disabled for extension services?
- Should there be a limit on the number of services an extension can register?

## Future Work

### ServiceReachable — Data Plane Reachability Check

A `ServiceReachable` method on `KuadrantCtx` that checks whether the wasm-shim can actually reach a registered service, as opposed to `RegisterService` which only validates reachability from the operator.

The intended approach is to query metrics emitted by the wasm-shim via a Prometheus client. However, this is **blocked** by two prerequisites:

1. **Wasm-shim per-service metrics**: The wasm-shim currently emits only aggregate counters (`kuadrant.hits`, `kuadrant.errors`, etc.) without per-service labels. Upstream changes to the wasm-shim are needed to add a `service` label so that errors and connectivity failures can be attributed to a specific registered service.

2. **Prometheus endpoint configuration**: The Kuadrant CR has no field for a Prometheus query endpoint. A new CRD field (e.g. `spec.observability.prometheus.url`) would be needed so the operator knows where to query.

Once those prerequisites are met, the API would look like:

```go
type KuadrantCtx interface {
    // ... existing methods ...
    RegisterService(ctx context.Context, policy Policy, svc ServiceConfig) error
    ServiceReachable(ctx context.Context, policy Policy, svc ServiceConfig) (bool, error)
}
```

Extension authors would use it to check data plane connectivity before relying on a service:

```go
reachable, err := kCtx.ServiceReachable(ctx, policy, svcConfig)
if err != nil {
    return reconcile.Result{}, err
}
if !reachable {
    // Service registered but wasm-shim cannot reach it yet
    return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
}
```

### Dynamic ServiceType

The `ServiceConfig` currently has no `Type` field — the operator hardcodes `auth` (ext_authz protocol). Once a `dynamic` service type is added to the wasm-shim, the operator will switch the hardcoded default from `auth` to `dynamic`. No `Type` field will be exposed on `ServiceConfig` — registered services will always use the `dynamic` type.

## Demo: RegisterService with Authorino

A two-part demo using the already-deployed Authorino instance to demonstrate RegisterService without deploying any new services.

### Concept

Authorino is already running in the cluster as part of the Kuadrant stack and implements the `envoy.service.auth.v3.Authorization/Check` RPC. The demo extension registers Authorino as a second, extension-managed auth service via RegisterService — proving the infrastructure works with a real, reachable gRPC service.

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
    err := kCtx.RegisterService(ctx, policy, types.ServiceConfig{
        Name: "demo-authorino",
        URL:  "grpc://authorino-authorino-authorization.kuadrant-system.svc.cluster.local:50051",
    })
    if errors.Is(err, types.ErrServiceUnreachable) {
        return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
    }
    return reconcile.Result{}, err
}
```

### Demo Script

**Part 1: RegisterService infrastructure**

```bash
# 1. Verify Authorino is running
kubectl get pods -n kuadrant-system -l app=authorino
#    Expected: authorino pod Running

# 2. Apply the DemoPolicy
kubectl apply -f examples/demo-policy/demo-policy.yaml

# 3. Verify the Envoy cluster was created
#    (Istio)
istioctl proxy-config cluster deploy/istio-ingressgateway -n istio-system | grep ext-demo-authorino
#    Expected: ext-demo-authorino   STRICT_DNS   ...

#    (Envoy Gateway)
kubectl get envoyPatchPolicy -n envoy-gateway-system -o yaml | grep ext-demo-authorino

# 4. Verify the service appears in the wasm config
#    (Istio)
kubectl get wasmplugin -n istio-system -o jsonpath='{.items[0].spec.pluginConfig}' | jq '.services'
#    Expected: "ext-demo-authorino": { "endpoint": "ext-demo-authorino", "type": "auth", "failureMode": "deny", "timeout": "100ms" }
#    Note: the built-in "auth-service" entry also remains — both coexist

# 5. Delete the DemoPolicy and verify cleanup
kubectl delete demopolicy demo-auth
istioctl proxy-config cluster deploy/istio-ingressgateway -n istio-system | grep ext-demo-authorino
#    Expected: (empty — cluster removed, built-in auth cluster unaffected)
```

**Part 2: Traffic flow (manual action wiring)**

```bash
# 6. Re-apply the DemoPolicy
kubectl apply -f examples/demo-policy/demo-policy.yaml

# 7. Patch the WasmPlugin to add an action referencing the extension-registered service
#    This adds an action for all requests to the HTTPRoute, calling ext-demo-authorino
kubectl patch wasmplugin kuadrant-istio-ingressgateway -n istio-system --type merge -p '
  ... (add action with service: "ext-demo-authorino") ...'

# 8. Send traffic — Authorino evaluates the request via the extension-registered service
curl -v http://my-api.example.com/anything
#    Authorino processes the request through the ext-demo-authorino cluster

# 9. Check Authorino logs for the request
kubectl logs deploy/authorino -n kuadrant-system
```

### What the demo proves

- **Part 1**: RegisterService creates a new Envoy cluster and wasm service entry pointing to an already-running service. The built-in `auth-service` is unaffected — both coexist. Cleanup works when the policy is deleted.
- **Part 2**: The extension-registered service is callable from the data plane. The wasm-shim routes traffic to Authorino via the `ext-demo-authorino` cluster, independent of the built-in auth flow.
- **No new services deployed**: The demo uses only infrastructure already present in a standard Kuadrant installation.

**Note**: Part 2 requires manual action wiring. A future `RegisterAction` method on `KuadrantCtx` would allow extensions to automate this step.

## Execution

### Todo

- [ ] Extend RegisteredDataStore with service storage
  - [ ] Unit tests
- [ ] Add RegisterService RPC to gRPC proto and regenerate
  - [ ] Unit tests
- [ ] Implement server-side RegisterService handler
  - [ ] Unit tests
- [ ] Extend IstioExtensionReconciler with registered service support
  - [ ] Unit tests
  - [ ] Integration tests
- [ ] Extend EnvoyGatewayExtensionReconciler with registered service support
  - [ ] Unit tests
  - [ ] Integration tests
- [ ] Implement client-side RegisterService on ExtensionController
  - [ ] Unit tests
- [ ] Add RegisterService to KuadrantCtx interface and ServiceConfig type
- [ ] Create demo entities and interactive demo script
  - [ ] DemoPolicy CRD and extension reconciler (`cmd/extensions/demo-policy/`)
  - [ ] DemoPolicy manifest (`examples/demo-policy/demo-policy.yaml`)
  - [ ] Interactive demo script (`examples/demo-policy/demo.sh`) that walks through Part 1 and Part 2, pausing at each step for discussion

### Completed

## Change Log

### 2026-03-04 — Initial design

- Chose policy-scoped lifecycle (consistent with AddDataTo pattern)
- Chose to extend existing IstioExtensionReconciler and EnvoyGatewayExtensionReconciler rather than creating new reconcilers
- Chose extension-provided name as cluster name and wasm service key
- ServiceConfig contains only URL; Type hardcoded to `auth`, will change to `dynamic` once wasm-shim supports it
- No wasm-shim changes needed for initial implementation
- Added gRPC dial reachability check — returns `ErrServiceUnreachable` sentinel error via gRPC `Unavailable` status
- Deferred `ServiceReachable` (data plane reachability via wasm-shim metrics) as future work — blocked on wasm-shim per-service metric labels and Prometheus endpoint configuration in Kuadrant CR

## References

- [wasm-shim envoy.yaml example (cluster config)](https://github.com/Kuadrant/wasm-shim/blob/main/e2e/basic/envoy.yaml#L118-L135)
