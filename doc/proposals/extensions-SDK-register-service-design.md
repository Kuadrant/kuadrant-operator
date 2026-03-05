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
Phase 1: Extension registers a service
──────────────────────────────────────

Extension              SDK Client           Operator (gRPC server)
   │                       │                        │
   │── RegisterService  ──►│                        │
   │   (policy, url,       │── RegisterService ────►│
   │    ServiceConfig)     │   RPC (unix socket)    │
   │                       │                        │── Dial url (reachability check)
   │                       │                        │── Parse url → host + port
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

type ServiceType int

const (
    ServiceTypeRateLimit ServiceType = iota
    ServiceTypeAuth
)

type ServiceConfig struct {
    Type ServiceType
}

// Defaults applied by the operator:
//   FailureMode: "deny"
//   Timeout:     "100ms"

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

enum ServiceType {
  SERVICE_TYPE_RATELIMIT = 0;
  SERVICE_TYPE_AUTH = 1;
}

message RegisterServiceRequest {
  Policy policy = 1;
  string url = 2;               // e.g. "grpc://my-service:8081"
  ServiceType service_type = 3;
  // failure_mode and timeout are reserved for future use.
  // Defaults: failure_mode = "deny", timeout = "100ms"
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
        Type: types.ServiceTypeRateLimit,
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
    ServiceType ServiceType
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
timeout := "100ms"
wasmConfig.Services[entry.ClusterName] = wasm.Service{
    Endpoint:    entry.ClusterName,
    Type:        wasm.ServiceType(entry.ServiceType),
    FailureMode: wasm.FailureModeDeny,
    Timeout:     &timeout,
}
```

#### 4. IstioExtensionReconciler (internal/controller/istio_extension_reconciler.go)

Extend the existing reconciler to handle registered services:
- In `buildWasmConfigs`: read registered services from `RegisteredDataStore` and add them to the `ServiceBuilder` via `WithService(clusterName, service)`
- In `Reconcile`: for each gateway, also create/update an `EnvoyFilter` with cluster patches for all registered services using `buildClusterPatch(clusterName, host, port, false, true)` (HTTP/2 enabled)
- Cleanup: delete cluster EnvoyFilters when registered services are removed

#### 5. EnvoyGatewayExtensionReconciler (internal/controller/envoy_gateway_extension_reconciler.go)

Same changes as the Istio variant:
- In `buildWasmConfigs`: add registered services to the `ServiceBuilder`
- In `Reconcile`: create/update `EnvoyPatchPolicy` resources with cluster patches for registered services using `BuildEnvoyPatchPolicyClusterPatch`

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
4. Extend `IstioExtensionReconciler` to add registered services to `ServiceBuilder` and create cluster EnvoyFilters
5. Extend `EnvoyGatewayExtensionReconciler` to add registered services to `ServiceBuilder` and create cluster EnvoyPatchPolicies
6. Implement client-side `RegisterService` on `ExtensionController`
7. Add `RegisterService` to `KuadrantCtx` interface and `ServiceConfig` type

## Testing Strategy

- **Unit tests**: URL parsing, cluster name generation, `RegisteredDataStore` service CRUD, `ClearPolicyData` cleanup, wasm config mutation, gRPC dial failure → `ErrServiceUnreachable` mapping
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
    RegisterService(ctx context.Context, policy Policy, url string, svc ServiceConfig) error
    ServiceReachable(ctx context.Context, policy Policy, url string, svc ServiceConfig) (bool, error)
}
```

Extension authors would use it to check data plane connectivity before relying on a service:

```go
reachable, err := kCtx.ServiceReachable(ctx, policy, "grpc://my-service:8081", svcConfig)
if err != nil {
    return reconcile.Result{}, err
}
if !reachable {
    // Service registered but wasm-shim cannot reach it yet
    return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
}
```

## Demo: Echo/Debug Extension

A two-part demo showing RegisterService in action with a minimal echo server.

### Echo Server

A small Go binary (~100 lines) implementing the Envoy ext_authz `Check` RPC. It logs every request it receives and returns `OK` with debug headers:

```go
// cmd/echo-debug-server/main.go

package main

import (
    "log"
    "net"

    authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
    typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
    "google.golang.org/grpc"
    rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
    corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

type echoServer struct {
    authv3.UnimplementedAuthorizationServer
}

func (s *echoServer) Check(ctx context.Context, req *authv3.CheckRequest) (*authv3.CheckResponse, error) {
    httpReq := req.GetAttributes().GetRequest().GetHttp()
    log.Printf("[echo] %s %s from %s | headers: %v",
        httpReq.GetMethod(), httpReq.GetPath(), httpReq.GetHost(), httpReq.GetHeaders())

    return &authv3.CheckResponse{
        Status: &rpcstatus.Status{Code: int32(0)}, // OK
        HttpResponse: &authv3.CheckResponse_OkResponse{
            OkResponse: &authv3.OkHttpResponse{
                Headers: []*corev3.HeaderValueOption{
                    {Header: &corev3.HeaderValue{
                        Key:   "x-echo-debug",
                        Value: fmt.Sprintf("method=%s path=%s host=%s", httpReq.GetMethod(), httpReq.GetPath(), httpReq.GetHost()),
                    }},
                },
            },
        },
    }, nil
}

func main() {
    lis, _ := net.Listen("tcp", ":9001")
    srv := grpc.NewServer()
    authv3.RegisterAuthorizationServer(srv, &echoServer{})
    log.Println("echo-debug-server listening on :9001")
    srv.Serve(lis)
}
```

### DebugPolicy Extension

A minimal extension with a single-field CRD:

```yaml
# DebugPolicy CRD instance
apiVersion: extensions.kuadrant.io/v1alpha1
kind: DebugPolicy
metadata:
  name: echo-debug
  namespace: default
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: my-api
  echoServiceURL: "grpc://echo-debug-server.default.svc.cluster.local:9001"
```

Extension reconciler:

```go
func (r *DebugPolicyReconciler) Reconcile(ctx context.Context, req reconcile.Request, kCtx types.KuadrantCtx) (reconcile.Result, error) {
    policy := &v1alpha1.DebugPolicy{}
    r.Client.Get(ctx, req.NamespacedName, policy)

    err := kCtx.RegisterService(ctx, policy, policy.Spec.EchoServiceURL, types.ServiceConfig{
        Type: types.ServiceTypeAuth,
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
# 1. Deploy the echo server
kubectl apply -f examples/echo-debug-server/deployment.yaml

# 2. Apply the DebugPolicy
kubectl apply -f examples/echo-debug-server/debug-policy.yaml

# 3. Verify the Envoy cluster was created
#    (Istio)
istioctl proxy-config cluster deploy/istio-ingressgateway -n istio-system | grep ext-auth
#    Expected: ext-auth-echo-debug-server   STRICT_DNS   ...

#    (Envoy Gateway)
kubectl get envoyPatchPolicy -n envoy-gateway-system -o yaml | grep ext-auth

# 4. Verify the service appears in the wasm config
#    (Istio)
kubectl get wasmplugin -n istio-system -o jsonpath='{.items[0].spec.pluginConfig}' | jq '.services'
#    Expected: { "ext-auth-echo-debug-server": { "endpoint": "ext-auth-echo-debug-server", "type": "auth", "failureMode": "deny", "timeout": "100ms" } }

# 5. Delete the DebugPolicy and verify cleanup
kubectl delete debugpolicy echo-debug
istioctl proxy-config cluster deploy/istio-ingressgateway -n istio-system | grep ext-auth
#    Expected: (empty — cluster removed)
```

**Part 2: Traffic flow (manual action wiring)**

```bash
# 6. Re-apply the DebugPolicy
kubectl apply -f examples/echo-debug-server/debug-policy.yaml

# 7. Patch the WasmPlugin to add an action referencing the echo service
#    This adds an action for all requests to the HTTPRoute, calling ext-auth-echo-debug-server
kubectl patch wasmplugin kuadrant-istio-ingressgateway -n istio-system --type merge -p '
  ... (add action with service: "ext-auth-echo-debug-server") ...'

# 8. Send traffic and observe
curl -v http://my-api.example.com/anything
#    Response includes header: x-echo-debug: method=GET path=/anything host=my-api.example.com

# 9. Check echo server logs
kubectl logs deploy/echo-debug-server
#    [echo] GET /anything from my-api.example.com | headers: {host: my-api.example.com, ...}
```

### What the demo proves

- **Part 1**: RegisterService creates the correct Envoy cluster and wasm service entry. Cleanup works when the policy is deleted. The `ErrServiceUnreachable` error is returned if the echo server isn't running.
- **Part 2**: The registered service is callable from the data plane. The wasm-shim routes traffic to it via the ext_authz protocol. Debug headers appear in responses.

**Note**: Part 2 requires manual action wiring. A future `RegisterAction` method on `KuadrantCtx` would allow extensions to automate this step, completing the full self-service extension story.

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

### Completed

## Change Log

### 2026-03-04 — Initial design

- Chose policy-scoped lifecycle (consistent with AddDataTo pattern)
- Chose to extend existing IstioExtensionReconciler and EnvoyGatewayExtensionReconciler rather than creating new reconcilers
- Chose cluster name = wasm service name (derived from type + host)
- Extensions specify existing ServiceType values; no wasm-shim changes needed
- URL + ServiceConfig struct as RegisterService parameters
- Added gRPC dial reachability check — returns `ErrServiceUnreachable` sentinel error via gRPC `Unavailable` status
- Deferred `ServiceReachable` (data plane reachability via wasm-shim metrics) as future work — blocked on wasm-shim per-service metric labels and Prometheus endpoint configuration in Kuadrant CR

## References

- [wasm-shim envoy.yaml example (cluster config)](https://github.com/Kuadrant/wasm-shim/blob/main/e2e/basic/envoy.yaml#L118-L135)
