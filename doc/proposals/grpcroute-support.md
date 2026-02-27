# Feature: GRPCRoute Support for Kuadrant Policies

## Summary

This design document outlines the implementation of GRPCRoute support in the Kuadrant ecosystem. GRPCRoute is a Gateway API resource (GA since v1.1.0) that provides gRPC-native routing semantics. This work enables AuthPolicy and RateLimitPolicy to target GRPCRoute resources, and ensures DNSPolicy and TLSPolicy correctly resolve when applied to Gateways with attached GRPCRoutes.

## Goals

1. Enable AuthPolicy and RateLimitPolicy to target GRPCRoute resources via the standard `targetRef` field
2. Generate correct CEL predicates from GRPCRouteMatch (service/method matching)
3. Integrate GRPCRoutes into the existing topology graph
4. Provide example applications and documentation for gRPC use cases
5. Maintain full backward compatibility with existing HTTPRoute-based workflows

## Non-Goals

1. **LLM/AI token-based rate limiting** - TokenRateLimitPolicy is a separate feature that works independently of route types
2. **Changes to backend services** - Authorino, Limitador, and WASM shim require no modifications
3. **New Well-Known Attributes** - RFC 0002 already supports HTTP/2; gRPC uses standard `request.url_path`
4. **gRPC streaming support for token limiting** - Out of scope for this feature (TokenRateLimitPolicy is separate)
5. **gRPC-specific convenience attributes** - Optional future enhancement (`grpc.service`, `grpc.method`)
6. **Extension policies** - OIDCPolicy, PlanPolicy, TelemetryPolicy have separate reconcilers and will be addressed in follow-up work
7. **APIProduct CRD** - Developer portal feature, separate domain from traffic policy

## Requirements

- **Minimum Gateway API version:** v1.1.0 (GRPCRoute reached GA in this release)
- **sectionName targeting** requires Gateway API v1.2.0+ (GRPCRouteRule `name` field was added in v1.2.0)
- The operator should log a warning if GRPCRoute CRD is not available at startup

## Design

### Backwards Compatibility

All changes are additive. Existing HTTPRoute-based policies continue to work unchanged. The core change is extending the topology to include GRPCRoute resources alongside HTTPRoutes.

### Architecture Changes

#### Key Insight: gRPC = HTTP/2

gRPC runs over HTTP/2, not alongside it. From Envoy's perspective, a gRPC request is an HTTP/2 request:

| Attribute | HTTP Request | gRPC Request |
|-----------|--------------|--------------|
| `request.method` | `GET`, `POST`, etc. | Always `POST` |
| `request.url_path` | `/api/users/123` | `/package.Service/Method` |
| `request.headers` | Standard headers | HTTP/2 headers + gRPC metadata |
| Protocol | HTTP/1.1 or HTTP/2 | HTTP/2 |

This means:
- **No changes needed to WASM shim** - CEL predicates against HTTP attributes work for gRPC
- **No changes needed to Authorino** - Receives AuthConfig CRDs with standard predicates
- **No changes needed to Limitador** - Counts requests by descriptors, protocol-agnostic

#### Data Flow

```
                            KUADRANT OPERATOR (changes here)
  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐
  │ GRPCRoute   │    │ AuthPolicy/ │    │ CEL         │    │ AuthConfig/ │
  │ Match       │ +  │ RateLimit   │ →  │ Predicates  │ →  │ Limitador   │
  │             │    │ Policy      │    │             │    │ Limits      │
  │ service: X  │    │             │    │ url_path == │    │             │
  │ method: Y   │    │             │    │ '/X/Y'      │    │             │
  └─────────────┘    └─────────────┘    └─────────────┘    └─────────────┘
                                                                 │
                                                                 ▼
                                         ┌───────────────────────────────┐
                                         │     ENVOY + WASM (no changes) │
                                         │  - Evaluates CEL predicates   │
                                         │  - Works for HTTP and gRPC    │
                                         └───────────────────────────────┘
                                                │                  │
                                                ▼                  ▼
                                         ┌─────────────┐    ┌─────────────┐
                                         │ AUTHORINO   │    │ LIMITADOR   │
                                         │ (no changes)│    │ (no changes)│
                                         └─────────────┘    └─────────────┘
```

#### Predicate Generation

GRPCRouteMatch translates to CEL predicates using standard HTTP attributes:

| GRPCRouteMatch | CEL Predicate |
|----------------|---------------|
| `service: "UserService", method: "GetUser"` | `request.url_path == '/UserService/GetUser'` |
| `service: "UserService"` | `request.url_path.startsWith('/UserService/')` |
| `headers: [{name: "x-tenant", value: "acme"}]` | `request.headers['x-tenant'] == 'acme'` |

### API Changes

#### Policy TargetRef Validation

AuthPolicy and RateLimitPolicy `targetRef` fields will accept `GRPCRoute` as a valid kind. The policy structure is identical to HTTPRoute — only the `targetRef` changes.

**Example - RateLimitPolicy targeting HTTPRoute (existing):**
```yaml
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: toystore-ratelimit
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  defaults:
    limits:
      global:
        rates:
          - limit: 10
            window: 10s
```

**For GRPCRoute, only the targetRef changes:**
```yaml
  targetRef:
    group: gateway.networking.k8s.io
    kind: GRPCRoute  # Only this changes
    name: grpcstore
```

The rest of the policy spec (rules, limits, authentication config, etc.) is identical. This applies to both AuthPolicy and RateLimitPolicy. Policies operate on the generated CEL predicates, not the route type directly.

#### Example GRPCRoute Configuration

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: GRPCRoute
metadata:
  name: grpcstore
spec:
  parentRefs:
    - name: kuadrant-ingressgateway
      namespace: gateway-system
  hostnames: ["grpc.example.com"]
  rules:
    - matches:
        - method:
            service: "talker.TalkerService"
            method: "Echo"
      backendRefs:
        - name: grpcstore
          port: 9000
```

### Component Changes

#### Repositories Affected

| Component | Repository | Change Type |
|-----------|------------|-------------|
| policy-machinery | `kuadrant/policy-machinery` | Controller-layer wiring (machinery types/topology already exist) |
| kuadrant-operator | `kuadrant/kuadrant-operator` | Watchers, topology, predicates, reconcilers |
| authorino-examples | `kuadrant/authorino-examples` | Dual-protocol talker-api (optional) |

#### Repositories NOT Affected

| Component | Why No Changes |
|-----------|----------------|
| Authorino / Authorino Operator | Receives AuthConfig CRDs - protocol agnostic |
| Limitador / Limitador Operator | Receives limit definitions - protocol agnostic |
| WASM Shim | Evaluates CEL against HTTP/2 attributes - works for gRPC |
| DNS Operator | Operates on Gateway listeners - route type irrelevant |
| RFC 0002 (Well-Known Attributes) | Already supports HTTP/2 |

#### Kuadrant Operator Changes

1. **Watcher Registration**: Register GRPCRoute watcher in state_of_the_world.go
2. **Topology Building**: Include GRPCRoutes via `machinery.WithGRPCRoutes()` and `machinery.ExpandGRPCRouteRules()`
3. **Path Extraction**: Abstract `ObjectsInRequestPath()` to handle both HTTP and gRPC routes
4. **Predicate Generation**: New `PredicatesFromGRPCRouteMatch()` function
5. **Policy Reconcilers**: Update effective policy reconcilers to iterate over GRPCRouteRules
6. **Extension Reconcilers**: Add GRPCRouteGroupKind subscriptions to Istio and Envoy Gateway reconcilers (WasmPlugin, EnvoyExtensionPolicy, auth/ratelimit cluster configs)
7. **Discoverability Reconcilers**: Define a `GRPCRoutePolicyDiscoverabilityReconciler` reconciler

### Security Considerations

No additional security considerations. GRPCRoute support uses the same policy enforcement mechanisms as HTTPRoute. All authentication, authorization, and rate limiting policies apply identically - only the predicate generation differs.

## Implementation Plan

Work is organized into 9 tasks across 3 repositories, designed to be reviewed and merged independently while respecting dependencies.

### Task Dependency Graph

```
  policy-machinery          authorino-examples
  ┌──────────────┐          ┌──────────────┐
  │   Task 1     │          │   Task 2     │
  │  GRPCRoutes  │          │  talker-api  │
  │  Resource    │          │  (parallel)  │
  └──────┬───────┘          └───────┬──────┘
         │                          │
         ▼                          │
  kuadrant-operator                 │
  ┌──────────────┐                  │
  │   Task 3     │                  │
  │  Core Infra  │                  │
  └──────┬───────┘                  │
         │                          │
         ▼                          │
  ┌──────────────┐                  │
  │   Task 4     │                  │
  │  Predicates  │                  │
  └──────┬───────┘                  │
         │                          │
    ┌────┴────┐                     │
    ▼         ▼                     │
┌───────┐ ┌───────┐                 │
│Task 5 │ │Task 6 │                 │
│Auth   │ │Rate   │                 │
│Policy │ │Limit  │                 │
└───┬───┘ └───┬───┘                 │
    │         │                     │
    └────┬────┘                     │
         ▼                          │
  ┌──────────────┐                  │
  │   Task 7     │                  │
  │  Reconcilers │                  │
  └──────┬───────┘                  │
         │                          │
         └────────────┬─────────────┘
                      │
              ┌───────┴───────┐
              ▼               ▼
       ┌──────────────┐ ┌──────────────┐
       │   Task 8     │ │   Task 9     │
       │  Coverage &  │ │  Examples &  │
       │  E2E Tests   │ │  Docs        │
       └──────────────┘ └──────────────┘
```

- Tasks 5 & 6 can run in parallel
- Tasks 8 & 9 can run in parallel
- Task 2 runs in parallel with Tasks 1-7

## Testing Strategy

### Unit Tests

- Predicate generation for all GRPCRouteMatch types
- Path extraction with HTTP and gRPC routes
- Action set building for gRPC paths
- Sorting behavior for GRPCRouteMatchConfigs

### Integration Tests

- AuthPolicy targeting GRPCRoute
- RateLimitPolicy targeting GRPCRoute
- Mixed HTTPRoute and GRPCRoute on same Gateway
- Policy inheritance (Gateway → GRPCRoute)
- Both Istio and Envoy Gateway providers

### E2E Tests

- Real gRPC traffic with AuthPolicy enforcement
- Real gRPC traffic with RateLimitPolicy enforcement
- gRPC method-specific policies

## Open Questions

1. **Go vs Ruby for talker-api**: Should the example app be rewritten in Go for ecosystem alignment, or extended in Ruby to preserve existing knowledge?
2. **GRPCRoute GA verification**: Confirm Gateway API version in use includes GA GRPCRoute (v1.1.0+)
3. **Gateway provider gRPC support**: Verify Istio and Envoy Gateway gRPC routing behavior is consistent
4. **TokenRateLimitPolicy scope**: Should TokenRateLimitPolicy support GRPCRoute targets? This would require protobuf response body parsing in the WASM shim (gRPC responses are protobuf-encoded, not JSON), which is additional work beyond this proposal's scope.
5. **Extension policies scope**: Should extension policies (OIDCPolicy, PlanPolicy, TelemetryPolicy) be updated as part of this work or in a separate follow-up?

## Execution

### Todo

#### Task 1: policy-machinery - GRPCRoute Controller Wiring
**Repository:** `kuadrant/policy-machinery`
**Note:** The machinery layer already has full GRPCRoute support (types, link functions, topology options) via PR #16. This task adds the controller-layer integration.

- [ ] Add `GRPCRoutesResource` constant to `controller/resources.go`
- [ ] Update `controller/topology_builder.go` to include GRPCRoutes in topology building
- [ ] Add unit tests for topology building with GRPCRoutes

---

#### Task 2: authorino-examples - Dual-Protocol talker-api
**Repository:** `kuadrant/authorino-examples`
**Note:** Team decision required on Go vs Ruby implementation

- [ ] Create proto definition (`talker.proto`) with Echo and SayHello RPCs
- [ ] Implement dual-protocol server (HTTP on :3000, gRPC on :9000)
- [ ] Create multi-stage Dockerfile
- [ ] Update Kubernetes manifests with both ports
- [ ] Enable gRPC reflection for grpcurl testing
- [ ] Publish to `quay.io/kuadrant/authorino-examples:talker-api`

---

#### Task 3: kuadrant-operator - Core GRPCRoute Infrastructure
**Repository:** `kuadrant/kuadrant-operator`
**Depends on:** Task 1

- [ ] Register GRPCRoute watcher in `internal/controller/state_of_the_world.go`
- [ ] Update topology building with `machinery.WithGRPCRoutes()` and `machinery.ExpandGRPCRouteRules()`
- [ ] Abstract `ObjectsInRequestPath()` to return `RequestPathObjects` supporting both route types
- [ ] Create helper methods: `IsHTTPRoute()`, `IsGRPCRoute()`, `RouteNamespace()`, `RouteName()`
- [ ] Update existing callers of `ObjectsInRequestPath()` for new signature
- [ ] Add unit tests for path extraction with both route types

**RequestPathObjects struct design:**

```go
// RequestPathObjects holds extracted objects from a topology path,
// supporting both HTTPRoute and GRPCRoute paths.
type RequestPathObjects struct {
    GatewayClass *machinery.GatewayClass
    Gateway      *machinery.Gateway
    Listener     *machinery.Listener
    Route        machinery.Targetable  // Either HTTPRoute or GRPCRoute
    RouteRule    machinery.Targetable  // Either HTTPRouteRule or GRPCRouteRule
}

// Type checking helpers
func (r *RequestPathObjects) IsHTTPRoute() bool
func (r *RequestPathObjects) IsGRPCRoute() bool

// Type assertion helpers
func (r *RequestPathObjects) AsHTTPRoute() (*machinery.HTTPRoute, *machinery.HTTPRouteRule)
func (r *RequestPathObjects) AsGRPCRoute() (*machinery.GRPCRoute, *machinery.GRPCRouteRule)

// Common accessors
func (r *RequestPathObjects) RouteNamespace() string
func (r *RequestPathObjects) RouteName() string
func (r *RequestPathObjects) Hostnames() []string
```

---

#### Task 4: kuadrant-operator - GRPCRoute Predicate Generation
**Repository:** `kuadrant/kuadrant-operator`
**Depends on:** Task 3

- [ ] Implement `PredicatesFromGRPCRouteMatch()` in `internal/wasm/utils.go`
- [ ] Implement `predicateFromGRPCMethod()` for service/method → url_path conversion
- [ ] Implement `predicateFromGRPCHeader()` (can reuse HTTP header logic)
- [ ] Create `GRPCRouteMatchConfig` struct in `internal/gatewayapi/types.go`
- [ ] Implement `SortableGRPCRouteMatchConfigs` with Gateway API precedence rules (see below)
- [ ] Implement `BuildActionSetsForGRPCPath()` in `internal/wasm/utils.go`
- [ ] Verify topology correctly links Gateway → GRPCRoute (prerequisite for DNS/TLS policy inheritance)
- [ ] Integration test: DNSPolicy on Gateway with attached GRPCRoutes resolves correctly
- [ ] Integration test: TLSPolicy on Gateway with attached GRPCRoutes resolves correctly
- [ ] Add unit tests for predicate generation (all match types, edge cases)
- [ ] Add unit tests for sorting behavior

**GRPCRouteMatch sorting precedence ([per Gateway API spec](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.GRPCRouteRule)):**

1. Largest number of characters in a matching non-wildcard hostname
2. Largest number of characters in a matching hostname
3. Largest number of characters in a matching service
4. Largest number of characters in a matching method
5. Largest number of header matches
6. Oldest Route based on creation timestamp (tie-breaker)
7. Alphabetical order by `{namespace}/{name}` (tie-breaker)

**GRPCMethodMatch predicate patterns (all combinations to implement):**

| Pattern | CEL Predicate |
|---------|---------------|
| Exact service + exact method | `request.url_path == '/Service/Method'` |
| Exact service only | `request.url_path.startsWith('/Service/')` |
| Exact method only (no service) | `request.url_path.matches('^/[^/]+/Method$')` |
| Regex service + regex method | `request.url_path.matches('^/ServicePattern/MethodPattern$')` |
| Regex service only | `request.url_path.matches('^/ServicePattern/.*$')` |
| Regex method only | `request.url_path.matches('^/[^/]+/MethodPattern$')` |
| Mixed: exact service + regex method | `request.url_path.matches('^/Service/MethodPattern$')` |

**Note:** Per Gateway API spec, "at least one of Service and Method MUST be a non-empty string"

**Empty match handling:**

```go
// Empty GRPCRouteMatch = match all gRPC requests (consistent with HTTPRoute behavior)
if match.Method == nil && len(match.Headers) == 0 {
    return []string{} // Empty predicates = match all
}
```

---

#### Task 5: kuadrant-operator - AuthPolicy GRPCRoute Support
**Repository:** `kuadrant/kuadrant-operator`
**Depends on:** Task 4

- [ ] Update `api/v1/authpolicy_types.go` targetRef validation to accept GRPCRoute
- [ ] Update `effective_auth_policies_reconciler.go` to iterate over GRPCRouteRules
- [ ] Update `auth_workflow_helpers.go` for GRPCRoute path handling
- [ ] Create `LinkGRPCRouteRuleToAuthConfig()` in `internal/authorino/utils.go`
- [ ] Update `authconfigs_reconciler.go` to handle GRPCRouteRule paths
- [ ] Add `AuthConfigGRPCRouteRuleAnnotation` constant
- [ ] Add unit tests for AuthPolicy with GRPCRoute targets
- [ ] Add integration tests for AuthConfig generation from GRPCRoutes
- [ ] Run `make generate manifests bundle helm-build` after API changes

**Iteration strategy:** Use separate iteration loops for HTTPRouteRules and GRPCRouteRules (rather than combined iteration with type switching). This provides clearer separation and easier debugging.

---

#### Task 6: kuadrant-operator - RateLimitPolicy GRPCRoute Support
**Repository:** `kuadrant/kuadrant-operator`
**Depends on:** Task 4

- [ ] Update `api/v1/ratelimitpolicy_types.go` targetRef validation to accept GRPCRoute
- [ ] Update `effective_ratelimit_policies_reconciler.go` to iterate over GRPCRouteRules
- [ ] Update `ratelimit_workflow_helpers.go` for GRPCRoute path handling
- [ ] Update `limitador_limits_reconciler.go` to handle GRPCRouteRule paths
- [ ] Create `LimitsNamespaceFromGRPCRoute()` helper function
- [ ] Add unit tests for RateLimitPolicy with GRPCRoute targets
- [ ] Add integration tests for Limitador config generation from GRPCRoutes
- [ ] Run `make generate manifests bundle helm-build` after API changes

**Iteration strategy:** Use separate iteration loops for HTTPRouteRules and GRPCRouteRules (same pattern as Task 5).

---

#### Task 7: kuadrant-operator - GRPCRoute Reconciler Updates
**Repository:** `kuadrant/kuadrant-operator`
**Depends on:** Task 5, Task 6

- [ ] Create `grpcroute_policy_discoverability_reconciler.go` (mirror HTTPRoute pattern)
- [ ] Create `FindGRPCRouteParentStatusFunc` helper (mirrors `FindRouteParentStatusFunc`)
- [ ] Use `controller.GRPCRoutesResource` for status updates (requires Task 1)
- [ ] Register GRPCRoute policy discoverability reconciler in workflow
- [ ] Update all effective policy reconciler subscriptions with `GRPCRouteGroupKind`
- [ ] Update all effective policy reconciler subscriptions with `GRPCRouteRuleGroupKind`
- [ ] Update `istio_extension_reconciler.go` for GRPCRoute events
- [ ] Update `envoy_gateway_extension_reconciler.go` for GRPCRoute events
- [ ] Update auth cluster reconcilers (Istio, Envoy Gateway)
- [ ] Update ratelimit cluster reconcilers (Istio, Envoy Gateway)
- [ ] Add unit tests for reconciler subscriptions

---

#### Task 8: kuadrant-operator - Test Coverage & E2E Tests
**Repository:** `kuadrant/kuadrant-operator`
**Depends on:** Task 2, Task 7

This task ensures comprehensive test coverage and adds E2E tests. Unit and integration tests for specific components are included in Tasks 3-7.

- [ ] Verify test coverage for all new code meets project standards
- [ ] Add any missing unit tests identified during coverage review
- [ ] Integration tests: Mixed HTTPRoute and GRPCRoute on same Gateway
- [ ] Integration tests: Policy inheritance (Gateway → GRPCRoute)
- [ ] Integration tests: Both Istio and Envoy Gateway providers
- [ ] E2E tests: gRPC service with AuthPolicy enforcement
- [ ] E2E tests: gRPC service with RateLimitPolicy enforcement
- [ ] E2E tests: Method-specific policies (target specific service/method)

---

#### Task 9: kuadrant-operator - GRPCRoute Examples & Documentation
**Repository:** `kuadrant/kuadrant-operator`
**Depends on:** Task 2, Task 7 (can run in parallel with Task 8)

- [ ] Create `examples/grpcstore/grpcstore.yaml` (Deployment + Service)
- [ ] Create `examples/grpcstore/grpcroute.yaml`
- [ ] Create `examples/grpcstore/authpolicy.yaml`
- [ ] Create `examples/grpcstore/ratelimitpolicy.yaml`
- [ ] Create `examples/grpcstore/README.md` with usage instructions
- [ ] Write `doc/user-guides/ratelimiting/grpc-rl-for-app-developers.md`
- [ ] Write `doc/user-guides/auth/grpc-auth-for-app-developers.md`
- [ ] Include grpcurl commands for verification
- [ ] Include gRPC-specific `when` clause examples in rate limiting guide

**Example gRPC-specific `when` clause for documentation:**

```yaml
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: grpc-method-limit
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: GRPCRoute
    name: grpcstore
  limits:
    per-grpc-method:
      when:
        - predicate: "request.url_path == '/talker.TalkerService/Echo'"
      rates:
        - limit: 100
          window: 1m
```

---

### In Progress

_(No tasks in progress)_

---

### Completed

_(No tasks completed yet)_

---

## Alternatives Considered

### Alternative 1: GRPCRoute as HTTPRoute Adapter

Translate GRPCRoute to HTTPRoute internally instead of parallel code paths:

```go
func GRPCRouteMatchToHTTPRouteMatch(grpcMatch gwapiv1.GRPCRouteMatch) gwapiv1.HTTPRouteMatch {
    return gwapiv1.HTTPRouteMatch{
        Path: &gwapiv1.HTTPPathMatch{
            Type:  ptr(gwapiv1.PathMatchExact),
            Value: ptr("/" + grpcMatch.Method.Service + "/" + grpcMatch.Method.Method),
        },
        Method: ptr(gwapiv1.HTTPMethodPost), // gRPC is always POST
    }
}
```

| Pros | Cons |
|------|------|
| Reuses all existing HTTPRoute logic | Loses GRPCRoute semantics |
| Simpler maintenance - one code path | Harder to add gRPC-specific features later |
| | Obscures what's actually happening |

**Verdict:** Not recommended. The parallel approach is cleaner and more extensible.

### Alternative 2: Generic Route Interface

Create a common interface for both route types:

```go
type RouteWrapper interface {
    Hostnames() []string
    ParentRefs() []gwapiv1.ParentReference
    Rules() []RouteRuleWrapper
}
```

| Pros | Cons |
|------|------|
| Truly unified handling | Significant refactor of existing code |
| Extensible to TCPRoute, UDPRoute | Over-engineering for current needs |

**Verdict:** Not recommended for initial implementation. Could be considered as a future refactor if TCPRoute/UDPRoute support is needed.

---

## Appendix: Common Misconceptions

### "gRPC needs separate protocol handling"

**Incorrect.** gRPC is built on HTTP/2. Envoy sees gRPC requests as HTTP/2 requests with:
- `:method: POST`
- `:path: /package.Service/Method`
- `content-type: application/grpc`

All existing `request.*` Well-Known Attributes apply directly.

### "We need gRPC-specific Well-Known Attributes"

**Incorrect.** RFC 0002 explicitly supports HTTP/2. The same `request.url_path` attribute works for both:
- HTTP: `request.url_path == '/api/users'`
- gRPC: `request.url_path == '/UserService/GetUser'`

Optional future enhancement: `grpc.service` and `grpc.method` convenience attributes could parse the path for better UX, but are not required for GRPCRoute support.

### "Authorino/Limitador need changes for gRPC"

**Incorrect.** These components receive generated artifacts (AuthConfig CRDs, limit definitions) that are protocol-agnostic. The WASM filter evaluates CEL predicates against HTTP/2 attributes - this works identically for gRPC because gRPC IS HTTP/2.
