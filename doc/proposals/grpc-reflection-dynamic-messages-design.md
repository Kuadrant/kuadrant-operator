# Feature: gRPC Reflection and Dynamic Message Building (Phase 2)

## Summary

Phase 2 builds on the RegisterUpstreamMethod foundation (Phase 1) to enable dynamic gRPC message construction and invocation from the wasm data plane. This phase adds:

1. **Operator**: gRPC reflection client to fetch service descriptors from registered upstreams, proto caching, and gRPC server to serve descriptors to wasm
2. **Wasm**: Dynamic service type that fetches descriptors at configuration time and builds/sends gRPC messages at request time
3. **Integration**: End-to-end flow from extension registration to dynamic upstream invocation

This enables extension authors to register any gRPC upstream endpoint and have the wasm data plane invoke it without requiring compile-time proto dependencies.

## Key Points

- Operator uses gRPC reflection to fetch service descriptors when `RegisterUpstreamMethod` is called
- Descriptors are cached in-memory and served to wasm via a dedicated gRPC service
- Wasm introduces a `Dynamic` service type that fetches descriptors at configuration time
- Message building in Phase 2 is **static only** (no CEL expressions)
- Response handling is basic (capture and log, no extraction)
- Cache invalidation happens only on `RegisterUpstreamMethod` calls (no TTL/scheduling)

## Goals

- **Operator**: Fetch and cache protobuf descriptors via gRPC reflection
- **Operator**: Serve descriptors to wasm clients via a separate gRPC service
- **Wasm**: Add `Dynamic` service type with descriptor-based message building
- **Wasm**: Build static gRPC messages from configuration
- **Wasm**: Dispatch gRPC calls to registered upstreams at request time
- **Integration**: Demonstrate end-to-end flow with a real upstream service
- **Security**: Isolate descriptor service from extension clients (separate gRPC services)

## Non-Goals

- **Not in Phase 2**: CEL-based message field expressions (deferred to Phase 3)
- **Not in Phase 2**: Response data extraction and context injection (deferred to Phase 3)
- **Not in Phase 2**: TTL-based cache invalidation or scheduled re-fetching
- **Not in Phase 2**: Reflection fallback to v1alpha
- **Not in Phase 2**: Proto diff detection for smart cache updates
- **Not in Phase 2**: mTLS for reflection or descriptor service
- **Not in Phase 2**: Proto validation or CEL expression validation

## Design

Phase 2 builds on Phase 1's `RegisterUpstreamMethod` foundation by adding reflection-based descriptor fetching and dynamic message building. This completes the extension SDK's ability to call arbitrary gRPC upstreams without compile-time proto dependencies.

### Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│ Extension Author                                                │
│   - Calls RegisterUpstreamMethod(url, service, method)         │
└────────────────────┬────────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────────┐
│ Kuadrant Operator                                               │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ 1. RegisterUpstreamMethod Handler                          │ │
│ │    - Triggers gRPC reflection client                       │ │
│ │    - Fetches service descriptors from upstream             │ │
│ │    - Stores in ProtoCache (cluster_name + service → proto) │ │
│ │    - Registers upstream in RegisteredDataStore             │ │
│ └─────────────────────────────────────────────────────────────┘ │
│                                                                  │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ 2. ProtoCache (in-memory)                                  │ │
│ │    Key: (cluster_name, service)                            │ │
│ │    Value: FileDescriptorSet (protobuf descriptors)         │ │
│ └─────────────────────────────────────────────────────────────┘ │
│                                                                  │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ 3. gRPC Descriptor Service (TCP, separate from extensions) │ │
│ │    - Exposes GetServiceDescriptors(cluster, service)       │ │
│ │    - Serves cached descriptors to wasm clients             │ │
│ │    - Kubernetes Service: kuadrant-operator-grpc:50051      │ │
│ └─────────────────────────────────────────────────────────────┘ │
└────────────────────┬────────────────────────────────────────────┘
                     │ Wasm config update (includes dynamic service)
                     ▼
┌─────────────────────────────────────────────────────────────────┐
│ Wasm Shim (Envoy)                                               │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ 4. on_configure (FilterRoot)                               │ │
│ │    - Detects Dynamic service type in config                │ │
│ │    - Calls operator GetServiceDescriptors via gRPC         │ │
│ │    - Builds DescriptorPool from FileDescriptorSet          │ │
│ │    - Constructs DynamicService instance                    │ │
│ └─────────────────────────────────────────────────────────────┘ │
│                                                                  │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ 5. Request Processing (on_http_request_headers)            │ │
│ │    - DynamicService builds static message from config      │ │
│ │    - Dispatches gRPC call to upstream                      │ │
│ │    - Awaits response (basic logging, no extraction)        │ │
│ └─────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

**Key Flows**:

1. **Extension Registration** (control plane):
   - Extension calls `RegisterUpstreamMethod` → operator reflects → protos cached → wasm config updated

2. **Wasm Configuration** (data plane bootstrap):
   - Wasm receives config → detects dynamic service → fetches descriptors → builds message builder

3. **Request Time** (data plane hot path):
   - HTTP request arrives → wasm builds static message → dispatches gRPC → logs response

### API Changes

#### Service Separation for Security

Phase 2 introduces a **separate gRPC service** for descriptor serving to isolate wasm clients from extension clients:

- **Extension Service** (Unix socket): `kuadrant.v1.Kuadrant`
  - Used by extension controllers
  - Methods: `Resolve`, `AddDataTo`, `RegisterUpstreamMethod`, etc.

- **Descriptor Service** (TCP): `kuadrant.v1.DescriptorService`
  - Used by wasm clients only
  - Methods: `GetServiceDescriptors`
  - Exposed via Kubernetes Service: `kuadrant-operator-grpc.kuadrant-system.svc.cluster.local:50051`

**Why separate services?**

- Security boundary: wasm should not have access to `Resolve`, `AddDataTo`, or other control plane APIs
- Different transports: Unix socket (extensions) vs TCP (wasm in Envoy)
- Different lifecycle: extension client lifecycle tied to controller, wasm client lifecycle tied to Envoy config

#### gRPC Proto Definition

**New Descriptor Service** (`pkg/extension/grpc/v1/descriptor_service.proto`):

```protobuf
syntax = "proto3";

package kuadrant.v1;

service DescriptorService {
  rpc GetServiceDescriptors(GetServiceDescriptorsRequest) returns (GetServiceDescriptorsResponse) {}
}

message GetServiceDescriptorsRequest {
  repeated ServiceRef services = 1;
}

message ServiceRef {
  string cluster_name = 1;
  string service = 2;
}

message GetServiceDescriptorsResponse {
  repeated ServiceDescriptor descriptors = 1;
}

message ServiceDescriptor {
  string cluster_name = 1;
  string service = 2;
  bytes file_descriptor_set = 3;  // Serialized google.protobuf.FileDescriptorSet
}
```

**Modified RegisterUpstreamMethod** (no proto changes, behavior changes only):

When `RegisterUpstreamMethod` is called:

1. Validate that `UpstreamConfig.Service` and `UpstreamConfig.Method` are set
2. Initiate gRPC reflection to `UpstreamConfig.URL`
3. Fetch service descriptors for `UpstreamConfig.Service`
4. Store in `ProtoCache` with key `(cluster_name, service)`
5. Register upstream in `RegisteredDataStore` with service type `dynamic` (Phase 1 defaulted to `auth`)
6. Return error if reflection fails (fail fast)

### Component Changes

#### Operator Components

##### 1. ProtoCache (new: `internal/extension/proto_cache.go`)

Thread-safe in-memory cache for protobuf descriptors:

```go
// ProtoCacheKey uniquely identifies a service's descriptors
// Uses struct as map key following existing RegisteredDataStore patterns
type ProtoCacheKey struct {
    ClusterName string // Envoy cluster name
    Service     string // Fully qualified service name (e.g., "envoy.service.ratelimit.v3.RateLimitService")
}

// ProtoCache stores FileDescriptorSets fetched via reflection
type ProtoCache struct {
    mu    sync.RWMutex
    cache map[ProtoCacheKey]*descriptorpb.FileDescriptorSet
}

// Interface:
// - Set(key ProtoCacheKey, fds *FileDescriptorSet): Store descriptor
// - Get(key ProtoCacheKey) (*FileDescriptorSet, bool): Retrieve descriptor
// - Delete(key ProtoCacheKey): Remove descriptor
```

##### 2. ReflectionClient (new: `internal/extension/reflection.go`)

gRPC reflection client to fetch service descriptors with recursive dependency resolution:

```go
const reflectionTimeout = 30 * time.Second  // Prevent hanging on slow/unreachable upstreams

type ReflectionClient struct {
    timeout time.Duration
}

// FetchServiceDescriptors fetches descriptors for the given service via reflection
// Returns FileDescriptorSet containing the service and all its transitive dependencies
// Key behaviors:
// - Connects to upstream with 30-second timeout
// - Uses grpc_reflection_v1.ServerReflectionClient
// - Recursively fetches all dependencies with circular dependency protection
// - Returns error if reflection unsupported or service not found
func (rc *ReflectionClient) FetchServiceDescriptors(ctx context.Context, url string, serviceName string) (*descriptorpb.FileDescriptorSet, error)
```

##### 3. Modified RegisterUpstreamMethod Handler (`internal/extension/manager.go`)

Updated to trigger reflection and cache protos:

```go
func (es *extensionService) RegisterUpstreamMethod(ctx context.Context, req *kuadrantv1.RegisterUpstreamMethodRequest) (*kuadrantv1.RegisterUpstreamMethodResponse, error)
```

**Key behaviors**:

1. Validate that `UpstreamConfig.Service` and `UpstreamConfig.Method` are set
2. Call `ReflectionClient.FetchServiceDescriptors(url, service)`
3. Generate cluster name (Phase 1 logic)
4. Store descriptors in `ProtoCache` with key `ProtoCacheKey{ClusterName, Service}`
5. Register upstream in `RegisteredDataStore` (Phase 1 logic)
6. Return error if reflection fails (fail fast)
7. Clean up cache on registration failure

##### 4. New GetServiceDescriptors Handler (`internal/extension/manager.go`)

Serves cached descriptors to wasm clients:

```go
func (es *extensionService) GetServiceDescriptors(ctx context.Context, req *kuadrantv1.GetServiceDescriptorsRequest) (*kuadrantv1.GetServiceDescriptorsResponse, error)
```

**Key behaviors**:

1. Batch fetch: iterate over `req.Services`
2. For each service, lookup in `ProtoCache` using `ProtoCacheKey{ClusterName, Service}`
3. Return error if descriptor not found (fail wasm configuration)
4. Marshal `FileDescriptorSet` to bytes
5. Return list of `ServiceDescriptor` (cluster_name, service, bytes)

##### 5. Cache Cleanup in ClearPolicyData (`internal/extension/registry.go`)

When a policy is deleted, leverage `RegisteredDataStore` (from Phase 1) to determine if descriptors can be safely deleted from the cache.

**Key behaviors**:

- On `RegisterUpstreamMethod`: call `ProtoCache.Set(key, fds)` to store descriptor
- On `ClearPolicyData`:
  1. Get all upstreams registered by the policy being deleted
  2. Remove policy from `RegisteredDataStore`
  3. For each upstream, check if any other policies still reference it (query `RegisteredDataStore`)
  4. If no other policies use the upstream, call `ProtoCache.Delete(key)`
- This prevents premature deletion when multiple policies/extensions register the same upstream service
- Reference tracking is handled by existing `RegisteredDataStore` infrastructure

##### 6. TCP Server Setup (`internal/extension/oop.go` and `manager.go`)

Start descriptor service on TCP port 50051 when extensions are enabled:

```go
func StartDescriptorServer(es *extensionService, port int) error
```

**Key behaviors**:

- Listen on TCP port (default: 50051)
- Register `DescriptorServiceServer` (not full `ExtensionServiceServer`)
- Run in goroutine alongside Unix socket extension service
- Serves only `GetServiceDescriptors` RPC

##### 7. Kubernetes Service Manifest

Deploy a Kubernetes Service to expose the descriptor TCP port:

```yaml
# config/manager/grpc_service.yaml
apiVersion: v1
kind: Service
metadata:
  labels:
    control-plane: controller-manager
  name: grpc
  namespace: system
spec:
  ports:
    - name: grpc
      port: 50051
      protocol: TCP
      targetPort: grpc
  selector:
    control-plane: controller-manager
```

##### 8. EnvoyFilter/EnvoyPatchPolicy Generation

The operator must generate gateway-specific Envoy cluster configuration for the descriptor service.

**Design Note**: Use existing Phase 1 upstream cluster generation code (`istio_extension_reconciler.go`, `envoy_gateway_extension_reconciler.go`). When an extension policy targets a Gateway, ensure both:

1. Upstream cluster for the registered service (Phase 1)
2. Descriptor service cluster: `kuadrant-operator-grpc` → `kuadrant-operator-grpc.kuadrant-system.svc.cluster.local:50051`

**Cluster Requirements**:

- Name: `kuadrant-operator-grpc`
- Target: Kubernetes Service on port 50051
- Protocol: HTTP/2 (gRPC)
- Scope: Gateway-specific

**Implementation**: Leverage existing cluster generation infrastructure from Phase 1. The descriptor service cluster is created alongside upstream clusters as part of gateway-specific reconciliation.

#### Wasm Components

##### 1. Configuration Changes

**New ServiceType: Dynamic** (`src/configuration.rs`):

```rust
pub enum ServiceType {
    Auth,
    RateLimit,
    RateLimitCheck,
    RateLimitReport,
    Tracing,
    Dynamic,  // NEW
}
```

**Extended Service Configuration** (`src/service.rs`):

```rust
pub struct Service {
    pub service_type: ServiceType,
    pub endpoint: String,
    pub failure_mode: FailureMode,
    pub timeout: Duration,

    // NEW fields for Dynamic service type:
    pub grpc_service: Option<String>,  // e.g., "envoy.service.ratelimit.v3.RateLimitService"
    pub grpc_method: Option<String>,   // e.g., "ShouldRateLimit"
}
```

**Example Wasm Configuration**:

```yaml
services:
  limitador-check:
    type: dynamic
    endpoint: kuadrant-ratelimit-service
    failureMode: deny
    timeout: 1s
    grpcService: "envoy.service.ratelimit.v3.RateLimitService"
    grpcMethod: "ShouldRateLimit"

actionSets:
  - name: rate-limit-check
    routeRuleConditions:
      hostnames: ["api.example.com"]
    actions:
      - service: limitador-check
        scope: request
        data:
          domain: "example-api"
          hits_addend: 1
```

##### 2. DynamicService Implementation (`src/service/dynamic.rs`)

New service type for descriptor-based message building:

```rust
pub struct DynamicService {
    upstream_name: String,
    service_name: String,
    method: String,
    timeout: Duration,
    failure_mode: FailureMode,
    descriptor_pool: DescriptorPool,
}

impl DynamicService {
    pub fn new(..., descriptor_pool: DescriptorPool) -> Self
    pub fn dispatch_dynamic(&self, ctx: &mut ReqRespCtx, json_message: &str) -> Result<u32, ServiceError>
}
```

**Key behaviors** (dispatch_dynamic):

1. Lookup method descriptor in `DescriptorPool` using `service_name` and `method`
2. Deserialize JSON string into `DynamicMessage` using `prost-reflect`'s serde support
3. Encode message to bytes using `prost::Message::encode`
4. Dispatch gRPC call via `ctx.dispatch_grpc_call`

##### 3. ServiceInstance Enum Update (`src/service/mod.rs`)

```rust
pub enum ServiceInstance {
    Auth(AuthService),
    RateLimit(RateLimitService),
    RateLimitCheck(RateLimitCheckService),
    RateLimitReport(RateLimitReportService),
    Tracing(TracingService),
    Dynamic(DynamicService),  // NEW
}
```

##### 4. Descriptor Fetching at Configuration Time (`src/lib.rs`)

**Key Flow**:

1. **on_configure**:
   - Identify Dynamic services in configuration
   - Call `dispatch_grpc_call("kuadrant-operator-grpc", "kuadrant.v1.DescriptorService", "GetServiceDescriptors", ...)`
   - Store pending configuration and token
   - Return true (wait for async response)

2. **on_grpc_call_response**:
   - Parse `GetServiceDescriptorsResponse`
   - Build `DescriptorPool` from each `FileDescriptorSet` using `prost_reflect`
   - Store pools in `PluginConfiguration.descriptor_pools` (HashMap<String, DescriptorPool>)
   - Complete configuration with `PipelineFactory::try_from(config)`

3. **PipelineFactory::try_from**:
   - For each Dynamic service, create `DynamicService` instance
   - Inject descriptor pool from `config.descriptor_pools.get(cluster_name)`
   - Fail configuration if descriptor pool not found

### Integration Flow

**End-to-End Flow Example** (using Limitador):

1. **Extension registers upstream**:

   ```go
   ctx.RegisterUpstreamMethod(kuadrant.UpstreamConfig{
       URL:     "limitador-limitador.kuadrant-system.svc.cluster.local:8081",
       Service: "envoy.service.ratelimit.v3.RateLimitService",
       Method:  "ShouldRateLimit",
   })
   ```

2. **Operator performs reflection**:
   - Connects to `limitador-limitador.kuadrant-system.svc.cluster.local:8081`
   - Calls `ServerReflection.ServerReflectionInfo`
   - Fetches descriptors for `envoy.service.ratelimit.v3.RateLimitService`
   - Recursively fetches all dependencies (circular dependency protection)
   - Caches in `ProtoCache` with key `ProtoCacheKey{ClusterName: "kuadrant-ratelimit-service", Service: "envoy.service.ratelimit.v3.RateLimitService"}`

3. **Operator updates wasm config**:
   - Adds dynamic service entry to WasmPlugin configuration
   - Service config includes: `endpoint: kuadrant-ratelimit-service`, `grpcService: envoy.service.ratelimit.v3.RateLimitService`, `grpcMethod: ShouldRateLimit`
   - Wasm reconciler applies updated WasmPlugin → Envoy reconfigures

4. **Wasm reconfigures**:
   - `on_configure` detects `Dynamic` service type
   - Calls `GetServiceDescriptors` to operator via cluster `kuadrant-operator-grpc` (port 50051)
   - Receives serialized `FileDescriptorSet`
   - Builds `DescriptorPool` using `prost-reflect`
   - Initializes `DynamicService` with descriptor pool

5. **Request arrives**:
   - `on_http_request_headers` executes action set
   - `DynamicService.dispatch_dynamic` builds `ShouldRateLimitRequest` from static config data
   - Dispatches gRPC call to `kuadrant-ratelimit-service` cluster
   - Receives `ShouldRateLimitResponse`
   - Logs response bytes (no field extraction in Phase 2)

### Error Handling

**Reflection Failures**:

- **When**: During `RegisterUpstreamMethod` if upstream doesn't support reflection or is unreachable
- **Behavior**: Return error to extension, fail the policy reconciliation
- **Rationale**: Fail fast to alert extension authors of misconfiguration

**Descriptor Fetch Failures (Wasm)**:

- **When**: Wasm calls `GetServiceDescriptors` but descriptor not found in cache
- **Behavior**: Log error, retries on tick
- **Rationale**: Don't crash Envoy; metrics deferred to future work (consistent with Phase 1)

**Upstream Unreachable at Request Time**:

- **When**: Wasm dispatches gRPC but upstream is down
- **Behavior**: Defer to `failureMode` setting (allow/deny)
- **Rationale**: Consistent with existing wasm failure modes

**Descriptor Pool Build Failures**:

- **When**: Wasm receives malformed `FileDescriptorSet`
- **Behavior**: Log error, skip that service, continue with others
- **Rationale**: Partial degradation is better than total failure

### Security Considerations

**Service Separation**:

- Extension service (Unix socket) and descriptor service (TCP) are isolated
- Wasm cannot access control plane APIs (`Resolve`, `AddDataTo`, etc.)
- Only `GetServiceDescriptors` is exposed to wasm

**No mTLS in Phase 2**:

- Reflection calls to upstreams are insecure (future work: mTLS)
- Descriptor service is ClusterIP (future work: mTLS for wasm clients)

**Cache Poisoning**:

- ProtoCache is only written by operator (trusted)
- No external API to inject descriptors

## Implementation Plan

### Phase 2.1: Operator Reflection and Caching

- [x] Implement `ProtoCache` with thread-safe get/set/delete
  - [x] Unit test: concurrent access
  - [x] Unit test: delete by cluster
- [x] Implement `ReflectionClient` with recursive dependency fetching
  - [x] Unit test: reflection failure (unreachable, no service)
  - [x] Unit test: 30-second timeout prevents hanging on slow upstreams
- [x] Update `RegisterUpstreamMethod` handler to trigger reflection
  - [x] Unit test: cache populated on success
  - [x] Unit test: error returned on reflection failure
- [x] Update wasm config generation to use dynamic service type
  - [x] Populate `GrpcService` and `GrpcMethod` fields from registered upstreams
  - [x] Set `DescriptorService` field when dynamic services present
  - [x] Unit test: wasm config includes dynamic services
- [x] Update `ClearPolicyData` to clean up proto cache
  - [x] Unit test: cache entries deleted when policy removed

### Phase 2.2: Operator Descriptor Service

- [x] Define `descriptor_service.proto` with `GetServiceDescriptors` RPC
  - [x] Run `make generate` to generate Go code
- [x] Implement `GetServiceDescriptors` handler
  - [x] Unit test: batch fetch returns multiple descriptors
  - [x] Unit test: missing descriptors return error
- [x] Implement TCP server for descriptor service
  - [x] Start server on port 50051 in Manager.Start()
  - [x] Stop server gracefully in Manager.Stop()
  - [x] Register only DescriptorServiceServer (security boundary)
- [ ] Add Kubernetes Service manifest (`config/manager/grpc_service.yaml`)
- [ ] Update operator deployment to expose port 50051
  - [ ] Integration test: verify service is reachable from pod
- [ ] Implement EnvoyFilter generation for Istio (`istio_extension_reconciler.go`)
  - [ ] Unit test: EnvoyFilter created with correct cluster config
  - [ ] Integration test: verify cluster accessible from gateway pod
- [ ] Implement EnvoyPatchPolicy generation for Envoy Gateway (`envoy_gateway_extension_reconciler.go`)
  - [ ] Unit test: EnvoyPatchPolicy created with correct cluster config
  - [ ] Integration test: verify cluster accessible from gateway pod

### Phase 2.3: Wasm Dynamic Service Type

- [x] Add `Dynamic` variant to `ServiceType` enum
  - [x] Unit test: deserialization of dynamic service config
- [x] Extend `Service` struct with `grpc_service` and `grpc_method` fields
  - [x] Unit test: config parsing
- [x] Implement `DynamicService` struct with descriptor pool
  - [x] Unit test: message building from static data
- [x] Update `ServiceInstance` enum to include `Dynamic`

### Phase 2.4: Wasm Descriptor Fetching

- [x] Implement `fetch_descriptors` in `on_configure`
- [x] Implement descriptor fetch response handling in `on_grpc_call_response`
  - [x] Unit test: graceful handling of missing descriptors
- [x] Update `PipelineFactory::try_from` to inject descriptor pools
  - [x] Unit test: dynamic service initialized with pool
- [x] e2e test: full descriptor fetch and activation flow

### Phase 2.5: Integration and Testing

- [ ] Create end-to-end demo with real upstream service
  - [ ] Deploy test upstream with reflection enabled
  - [ ] Extension registers upstream via `RegisterUpstreamMethod`
  - [ ] Verify wasm fetches descriptors
  - [ ] Send HTTP request, verify gRPC call dispatched
  - [ ] Verify response logged (no extraction)
- [ ] Integration test: full flow from extension registration to request dispatch
- [ ] Load test: verify performance impact of dynamic message building

## Testing Strategy

### Unit Tests (Operator)

- `ProtoCache`: concurrent access, cache hit/miss, delete by cluster
- `ReflectionClient`: successful fetch, network errors, malformed responses
- `RegisterUpstreamMethod`: cache population, error propagation
- `GetServiceDescriptors`: batch fetch, missing descriptors

### Unit Tests (Wasm)

- `DynamicService`: message building from static data, field type handling
- `on_configure`: descriptor fetch trigger, pending state management
- `on_grpc_call_response`: descriptor parsing, pool construction
- `PipelineFactory`: dynamic service initialization

### Integration Tests

- **Operator → Upstream Reflection**:
  - Deploy test gRPC service with reflection
  - Call `RegisterUpstreamMethod`
  - Verify descriptors cached

- **Wasm → Operator Descriptor Fetch**:
  - Configure wasm with dynamic service
  - Verify `GetServiceDescriptors` called
  - Verify DescriptorPool built

- **End-to-End**:
  - Extension registers upstream
  - Wasm fetches descriptors
  - HTTP request triggers gRPC dispatch
  - Verify upstream receives message

### Manual Testing

1. Deploy Kuadrant operator with extensions enabled
2. Deploy test upstream (e.g., Limitador with reflection)
3. Deploy extension that calls `RegisterUpstreamMethod`
4. Verify operator reflects and caches descriptors
5. Configure wasm with dynamic service
6. Send HTTP request, verify gRPC call in upstream logs
7. Verify response logged in Envoy logs

## Open Questions

### Resolved

- **Q**: Do we need to support multiple methods from the same service?
  - **A**: Yes - cache is per-service (not per-method). Multiple RegisterUpstreamMethod calls for the same service reuse cached descriptors

- **Q**: Cache key format: struct vs string?
  - **A**: Use struct. Follows existing codebase patterns (`DataProviderKey`, `SubscriptionKey`, `RegisteredUpstreamKey`) and is type-safe

### Open

- **EnvoyFilter/EnvoyPatchPolicy Generation**: Descriptor service cluster should be gateway-specific (builds on Phase 1's cluster generation)
- **Descriptor Size Limits**: Should we impose a max size on FileDescriptorSet to prevent OOM? (e.g., reject descriptors > 10MB)
- **Concurrent Reflection**: If multiple extensions register the same upstream simultaneously, should we deduplicate reflection calls?

## Future Work

### CEL-Based Message Building

- Support CEL expressions in wasm config for dynamic field values (e.g., extract user ID from JWT)
- Operator validates CEL expressions against proto schema at registration time
- Wasm evaluates CEL at request time using context data (headers, claims, topology)
- Example: `user_id: "request.auth.identity.sub"` (CEL expression evaluated per-request)

### Response Data Extraction

- Define response data extraction rules in wasm config
- Extract fields from gRPC responses using proto descriptors
- Inject extracted data into request context for downstream use (subsequent actions, logging)
- Example: Extract rate limit quota from response and expose as header

### Cache Management

- **TTL-Based Cache Invalidation**: Re-fetch descriptors periodically to detect upstream proto changes
- **Proto Diff Detection**: Compare fetched descriptors with cached version, trigger re-configuration if changed
- **Manual Cache Invalidation**: API to force descriptor refresh for a given upstream
- **Persistent Cache**: Store descriptors on disk to survive operator restarts

### Enhanced Reflection Support

- **Reflection v1alpha Fallback**: Support older reflection protocol for legacy upstreams
- **Method Validation**: Validate that registered method exists in reflected service (fail fast on misconfiguration)
- **Service Discovery**: Enumerate available services/methods from upstream via reflection

### Security Enhancements

- **mTLS for Reflection**: Secure reflection calls to upstreams (mutual TLS authentication)
- **mTLS for Descriptor Service**: Secure wasm → operator communication (prevent unauthorized descriptor access)
- **Access Control**: Limit which extensions can register which upstreams (policy-based authorization)

### Observability

- **Cache Metrics**: Expose Prometheus metrics for cache hit/miss rates, descriptor fetch latencies, cache size
- **Reflection Metrics**: Track reflection call success/failure rates, upstream availability
- **Wasm Descriptor Fetch Metrics**: Track descriptor fetch errors from wasm clients
- **Tracing**: Add OpenTelemetry tracing for reflection and descriptor serving flows

### Performance Optimizations

- **Connection Pooling**: Reuse gRPC connections for reflection (avoid connection overhead)
- **Batch Optimization**: Deduplicate descriptor fetches when multiple extensions register the same upstream
- **Lazy Descriptor Fetching**: Only fetch descriptors when wasm actually requests them (defer until needed)
- **Compression**: Compress FileDescriptorSet bytes in GetServiceDescriptors responses

## Change Log

### 2026-03-12 — Phase 2 Design (Post-PoC)

- Defined Phase 2 scope: static messages only (no CEL), basic response logging (no extraction)
- Clarified backwards compatibility: only affects RegisterUpstreamMethod (core policies unchanged)
- Documented recursive dependency fetching with circular dependency protection from PoC
- Added 30-second reflection timeout to prevent hanging on slow/unreachable upstreams
- Updated proto definition to match PoC implementation (list-based GetServiceDescriptorsResponse)
- Added EnvoyFilter/EnvoyPatchPolicy generation requirements for descriptor service cluster
- Updated examples to use realistic services (Limitador's envoy.service.ratelimit.v3.RateLimitService)
- Reorganized future work section without specific phase assignments
- **Decided**: Use struct for cache keys (follows existing codebase patterns)
- **Decided**: Metrics deferred to future work (consistent with Phase 1 approach)
- **Decided**: Descriptor service cluster generation is gateway-specific (builds on Phase 1 infrastructure)

## References

- [Phase 1: RegisterUpstreamMethod Design](extensions-SDK-register-upstream-method-design.md)
- [Gateway API Policy Attachment (GEP-713)](https://gateway-api.sigs.k8s.io/geps/gep-713/)
- [gRPC Reflection Protocol](https://github.com/grpc/grpc/blob/master/doc/server-reflection.md)
- [prost-reflect Documentation](https://docs.rs/prost-reflect/latest/prost_reflect/)
- [Envoy External Authorization](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/ext_authz/v3/ext_authz.proto)
