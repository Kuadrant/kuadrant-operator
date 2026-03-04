# Feature: Extension SDK RegisterService

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
Extension Reconciler
    │
    ├── kCtx.RegisterService(ctx, policy, "grpc://my-svc:8081", ServiceConfig{...})
    │       │
    │       ▼
    │   gRPC RegisterService RPC ──► extensionService (operator)
    │                                     │
    │                                     ├── Store in RegisteredDataStore (policy-scoped)
    │                                     └── Trigger reconciliation
    │
    ▼
Reconciliation cycle
    │
    ├── ExtensionClusterReconciler (Istio)
    │   └── Creates EnvoyFilter with cluster patch per gateway
    │
    ├── ExtensionClusterReconciler (EnvoyGateway)
    │   └── Creates EnvoyPatchPolicy with cluster patch per gateway
    │
    └── WasmConfig builder
        └── ServiceBuilder picks up registered services → adds to services map
```

A single new `ExtensionClusterReconciler` per gateway provider handles all extension-registered clusters, rather than one reconciler per service type.

### API Changes

#### KuadrantCtx Interface

```go
// pkg/extension/types/types.go

type ServiceConfig struct {
    Type        string // "ratelimit", "auth", "ratelimit-check", "ratelimit-report", "tracing"
    FailureMode string // "deny" or "allow"
    Timeout     string // e.g. "100ms"
}

type KuadrantCtx interface {
    Resolve(context.Context, Policy, string, bool) (celref.Val, error)
    ResolvePolicy(context.Context, Policy, string, bool) (Policy, error)
    AddDataTo(context.Context, Policy, Domain, string, string) error
    ReconcileObject(context.Context, client.Object, client.Object, MutateFn) (client.Object, error)
    RegisterService(ctx context.Context, policy Policy, url string, svc ServiceConfig) error // new
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
  string url = 2;             // e.g. "grpc://my-service:8081"
  string service_type = 3;    // "ratelimit", "auth", etc.
  string failure_mode = 4;    // "deny" or "allow"
  string timeout = 5;         // "100ms"
}
```

#### Cluster Naming

The cluster name is derived from the service type and a substring of the host, ensuring determinism and avoiding collisions:

```
Format: ext-<service_type>-<host_substring>
Example: ext-ratelimit-my-service
```

The same value is used as both the Envoy cluster name and the wasm config service map key (the `Endpoint` field in the wasm `Service` struct).

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

conn, err := grpc.NewClient(url,
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
if err != nil {
    return nil, status.Errorf(codes.Unavailable, "cannot reach service at %s: %v", url, err)
}
conn.Close()
```

#### Extension Author Usage

```go
func (r *MyReconciler) Reconcile(ctx context.Context, req reconcile.Request, kCtx types.KuadrantCtx) (reconcile.Result, error) {
    policy := &v1alpha1.MyPolicy{}
    r.Client.Get(ctx, req.NamespacedName, policy)

    err := kCtx.RegisterService(ctx, policy, "grpc://my-ratelimiter:8081", types.ServiceConfig{
        Type:        "ratelimit",
        FailureMode: "deny",
        Timeout:     "100ms",
    })
    if errors.Is(err, types.ErrServiceUnreachable) {
        // Service not available yet — requeue and retry later
        return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
    }
    if err != nil {
        return reconcile.Result{}, err
    }

    // Service is now available in wasm config as "ext-ratelimit-my-ratelimiter"
    // Cluster "ext-ratelimit-my-ratelimiter" is created in Envoy config
    // Cleanup happens automatically when ClearPolicy is called for this policy

    return reconcile.Result{}, nil
}
```

### Component Changes

#### 1. RegisteredDataStore (internal/extension/registry.go)

Add a new `registeredServices` map alongside the existing `dataProviders` and `subscriptions` maps:

```go
type RegisteredServiceEntry struct {
    Policy      ResourceID
    URL         string
    ClusterName string
    ServiceType string
    FailureMode string
    Timeout     string
}

type RegisteredServiceKey struct {
    Policy      ResourceID
    ClusterName string
}
```

Services are stored keyed by `(Policy, ClusterName)`. `ClearPolicyData` is extended to also clear registered services for the deleted policy.

#### 2. extensionService (internal/extension/manager.go)

New `RegisterService` handler:
- Parses URL to extract host and port
- Performs a gRPC dial attempt to the URL with a 5-second timeout
- If dial fails, returns gRPC status `Unavailable` with descriptive message
- If dial succeeds, generates cluster name: `ext-<type>-<host>`
- Stores in `RegisteredDataStore`
- Triggers reconciliation via `changeNotifier`

#### 3. MutatorRegistry — WasmConfig mutation (internal/extension/registry.go)

Extend `mutateWasmConfig` (or add a new mutator) to inject registered services into the `Config.Services` map. For each registered service entry, add:

```go
wasmConfig.Services[entry.ClusterName] = wasm.Service{
    Endpoint:    entry.ClusterName,
    Type:        wasm.ServiceType(entry.ServiceType),
    FailureMode: wasm.FailureModeType(entry.FailureMode),
    Timeout:     &entry.Timeout,
}
```

#### 4. ExtensionClusterReconciler — Istio (internal/controller/)

New `istio_extension_cluster_reconciler.go`:
- Subscribes to Kuadrant, Gateway, and extension-related events
- Reads registered services from `RegisteredDataStore`
- For each gateway in scope, builds an `EnvoyFilter` with cluster patches for all registered services
- Uses `buildClusterPatch(clusterName, host, port, false, true)` — HTTP/2 enabled, mTLS configurable
- Creates/updates/deletes `EnvoyFilter` resources named `kuadrant-ext-<gateway>`

#### 5. ExtensionClusterReconciler — EnvoyGateway (internal/controller/)

New `envoy_gateway_extension_cluster_reconciler.go`:
- Same logic as Istio variant but creates `EnvoyPatchPolicy` resources
- Uses `BuildEnvoyPatchPolicyClusterPatch`

#### 6. DataPlanePoliciesWorkflow (internal/controller/data_plane_policies_workflow.go)

Register the two new reconcilers in `NewDataPlanePoliciesWorkflow`:
- `IstioExtensionClusterReconciler` alongside existing Istio reconcilers
- `EnvoyGatewayExtensionClusterReconciler` alongside existing EG reconcilers

#### 7. Extension Controller client side (pkg/extension/controller/controller.go)

Implement `RegisterService` on `ExtensionController`:
- Convert `ServiceConfig` to proto `RegisterServiceRequest`
- Call `ec.extensionClient.client.RegisterService(ctx, request)`
- Translate gRPC `Unavailable` status to `types.ErrServiceUnreachable`

### Security Considerations

- **URL validation**: The operator must validate that the URL is well-formed and uses the `grpc://` scheme. Reject URLs with credentials or query parameters.
- **Cluster name sanitization**: Generated cluster names must be valid Envoy cluster identifiers (alphanumeric + hyphens).
- **No privilege escalation**: Extensions can only register services reachable from the data plane network. The cluster is created with the same access level as existing auth/ratelimit clusters.
- **Policy-scoped cleanup**: Services cannot outlive their owning policy, preventing resource leaks.

## Implementation Plan

1. Extend `RegisteredDataStore` with service registration storage and `ClearPolicyData` cleanup
2. Add `RegisterService` RPC to the gRPC proto and regenerate
3. Implement server-side `RegisterService` handler in `extensionService`
4. Add wasm config mutator to inject registered services into `Config.Services`
5. Create `IstioExtensionClusterReconciler`
6. Create `EnvoyGatewayExtensionClusterReconciler`
7. Register new reconcilers in `DataPlanePoliciesWorkflow`
8. Implement client-side `RegisterService` on `ExtensionController`
9. Add `RegisterService` to `KuadrantCtx` interface and `ServiceConfig` type

## Testing Strategy

- **Unit tests**: URL parsing, cluster name generation, `RegisteredDataStore` service CRUD, `ClearPolicyData` cleanup, wasm config mutation, gRPC dial failure → `ErrServiceUnreachable` mapping
- **Integration tests**: End-to-end RegisterService flow with Istio and EnvoyGateway — verify EnvoyFilter/EnvoyPatchPolicy creation, wasm config services map population, cleanup on policy deletion

## Open Questions

- Should mTLS be configurable per registered service, or always disabled for extension services?
- Should there be a limit on the number of services an extension can register?

## Execution

### Todo

- [ ] Extend RegisteredDataStore with service storage
  - [ ] Unit tests
- [ ] Add RegisterService RPC to gRPC proto and regenerate
  - [ ] Unit tests
- [ ] Implement server-side RegisterService handler
  - [ ] Unit tests
- [ ] Add wasm config service injection mutator
  - [ ] Unit tests
- [ ] Create IstioExtensionClusterReconciler
  - [ ] Unit tests
  - [ ] Integration tests
- [ ] Create EnvoyGatewayExtensionClusterReconciler
  - [ ] Unit tests
  - [ ] Integration tests
- [ ] Register new reconcilers in DataPlanePoliciesWorkflow
- [ ] Implement client-side RegisterService on ExtensionController
  - [ ] Unit tests
- [ ] Add RegisterService to KuadrantCtx interface and ServiceConfig type

### Completed

## Change Log

### 2026-03-04 — Initial design

- Chose policy-scoped lifecycle (consistent with AddDataTo pattern)
- Chose single reconciler per gateway provider over extending existing reconcilers
- Chose cluster name = wasm service name (derived from type + host)
- Extensions specify existing ServiceType values; no wasm-shim changes needed
- URL + ServiceConfig struct as RegisterService parameters
- Added gRPC dial reachability check — returns `ErrServiceUnreachable` sentinel error via gRPC `Unavailable` status

## References

- [wasm-shim envoy.yaml example (cluster config)](https://github.com/Kuadrant/wasm-shim/blob/main/e2e/basic/envoy.yaml#L118-L135)
