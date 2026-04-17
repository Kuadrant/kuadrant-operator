# Feature: Extension SDK Action Pipeline

## Summary

Add an Action pipeline API to the extension SDK so that extension authors can register named gRPC action methods and compose them into ordered pipelines with request and response phases. This builds on Phase 1 (`RegisterUpstreamMethod`) and Phase 2 (gRPC reflection / ProtoCache) to provide the control-flow layer that tells the wasm-shim *what* to call, *when* to call it, and *how* to interpret the result.

Reference: [Kuadrant/kuadrant-operator#1889](https://github.com/Kuadrant/kuadrant-operator/issues/1889)

## Goals

- Extension authors can register a named gRPC action method and invoke it from the data plane during request processing
- Extension authors can compose ordered pipelines of actions that run on inbound requests and outbound responses
- Request-phase actions can call a gRPC service and allow or deny the request based on the response, or allow/deny based on request attributes alone
- Response-phase actions can add headers or override the HTTP status code
- Invalid CEL expressions and schema mismatches are caught when registered, not at request time
- Demo: a ThreatPolicy extension registers a threat-scoring gRPC service, calls it on each request, denies requests that exceed a threshold, and adds a response header confirming the check ran

## Non-Goals

- Inter-action data flow: response data from one action is not available to subsequent actions (future work)
- New wasm `ServiceType` values beyond the existing `dynamic` type from Phase 2
- Multi-cluster pipeline orchestration
- Branching or conditional control flow beyond per-action predicates

## Design

### Deviations from Issue Metacode

The [issue metacode](https://github.com/Kuadrant/kuadrant-operator/issues/1889) outlines the ideal API. This design deviates in two places; each deviation is documented below with the reason it was required.

#### 1. `NewPipeline()` becomes `NewPipeline(policy)`

The issue uses a no-arg constructor:
```text
pipeline = kCtx.NewPipeline()
```

This design requires a policy:
```go
pipeline := kCtx.NewPipeline(policy)
```

**Why:** Every `OnRequest` and `OnResponse` call sends a gRPC message to the operator that must include the policy identity. Rather than requiring the policy on every action call, the pipeline captures it once at construction time. `NewPipeline` itself performs no I/O (no `ctx` needed) — it simply creates a `PipelineImpl` bound to the policy and the underlying gRPC client.

#### 2. `OnRequest` / `OnResponse` return `error` and take `ctx`

The issue uses fire-and-forget calls:
```text
pipeline.OnRequest(AllowAction{...})
pipeline.OnResponse(AddHeadersAction{...})
```

This design uses Go error handling:
```go
if err := pipeline.OnRequest(ctx, GRPCMethodAction{...}, AllowAction{...}); err != nil {
    return reconcile.Result{}, err
}
```

**Why:** Each `OnRequest`/`OnResponse` call sends a gRPC message to the operator immediately with all supplied actions. The operator may reject the batch (e.g. invalid CEL expression, unknown action method name). Go idiom requires surfacing these errors to the caller rather than silently swallowing them. The `ctx` parameter carries cancellation and deadline semantics for the gRPC call. There is no separate `Register()` step — each call is immediately effective.

### Backwards Compatibility

**Breaking change**: `RegisterUpstreamMethod` is renamed to `RegisterActionMethod` and its config type changes from `UpstreamConfig` to `ActionMethodConfig`. Extensions using the Phase 1 API must update their calls. Since the extension SDK is pre-1.0, this is acceptable. The gRPC proto RPC is also renamed; the old RPC is removed.

### Architecture Changes

```text
Extension Controller                    Operator (gRPC server)
   │                                       │
   │── RegisterActionMethod ──────────────►│── Validate name uniqueness (per-policy)
   │   (policy, ActionMethodConfig)        │── Validate CEL message template
   │                                       │── Store in ActionMethodStore
   │                                       │── Trigger reconciliation
   │◄── nil / error ───────────────────────│
   │                                       │
   │── NewPipeline(policy) ─── (local) ───►│  (no I/O — creates PipelineImpl)
   │                                       │
   │── pipeline.OnRequest(ctx, actions...) ►│── Validate each action (predicates, intention CEL)
   │                                       │── Validate intentions against proto schema (ProtoCache)
   │                                       │── Store pipeline actions for policy
   │                                       │── Trigger reconciliation
   │◄── nil / error ───────────────────────│
   │                                       │
   │── pipeline.OnResponse(ctx, actions...)►│── Same validation and storage
   │◄── nil / error ───────────────────────│
   │                                       │

Reconciliation:
   ActionMethodStore + PipelineActionStore
      │
      └──► Extension Reconcilers (Istio / EnvoyGateway)
              │
              ├── Wasm Config: Action entries with ActionType discriminator
              │   (grpc_method, allow, add_headers, with_response_code)
              │
              └── Envoy Clusters: created by Phase 1 infrastructure
```

### API Changes

#### ActionMethodConfig (replaces UpstreamConfig)

```go
// pkg/extension/types/types.go

// ActionMethodConfig holds the configuration for registering a named gRPC
// action method that can be invoked from pipeline actions.
type ActionMethodConfig struct {
    Name            string // Unique per-policy identifier, e.g. "checkThreatLevel"
    URL             string // e.g. "grpc://threat-service:8080"
    Service         string // gRPC service name, e.g. "threat.ThreatService"
    Method          string // gRPC method name, e.g. "CheckThreatLevel"
    MessageTemplate string // CEL/JSON template for building the request message
}
```

The `Name` field is new compared to Phase 1's `UpstreamConfig`. It provides a human-readable identifier that pipeline actions use to reference this method (via `GRPCMethodAction.Method`). Names are unique per-policy — two different policies can both register a method named `"checkThreatLevel"` without collision (see Name Scoping below).

#### KuadrantCtx Interface

```go
// pkg/extension/types/types.go

type KuadrantCtx interface {
    Resolve(context.Context, Policy, string, bool) (celref.Val, error)
    ResolvePolicy(context.Context, Policy, string, bool) (Policy, error)
    AddDataTo(context.Context, Policy, Domain, string, string) error
    ReconcileObject(context.Context, client.Object, client.Object, MutateFn) (client.Object, error)
    RegisterActionMethod(ctx context.Context, policy Policy, cfg ActionMethodConfig) error // renamed from RegisterUpstreamMethod
    NewPipeline(policy Policy) Pipeline // new
}
```

#### Pipeline Interface

```go
// pkg/extension/types/types.go

// Pipeline provides a builder for composing ordered actions on request
// and response phases. Each OnRequest/OnResponse call sends a single gRPC
// message to the operator containing all supplied actions.
type Pipeline interface {
    OnRequest(ctx context.Context, actions ...RequestAction) error
    OnResponse(ctx context.Context, actions ...ResponseAction) error
}
```

#### Action Types

```go
// pkg/extension/types/actions.go

// RequestAction is the interface implemented by actions that can be used
// in the request phase of a pipeline.
type RequestAction interface {
    actionType() ActionType
}

// ResponseAction is the interface implemented by actions that can be used
// in the response phase of a pipeline.
type ResponseAction interface {
    actionType() ActionType
}

// ActionType discriminates how the wasm-shim dispatches an action.
type ActionType string

const (
    ActionTypeGRPCMethod       ActionType = "grpc_method"
    ActionTypeAllow            ActionType = "allow"
    ActionTypeAddHeaders       ActionType = "add_headers"
    ActionTypeWithResponseCode ActionType = "with_response_code"
)

// GRPCMethodAction invokes a registered gRPC action method and evaluates
// the response. Implements RequestAction.
type GRPCMethodAction struct {
    Predicate []string // CEL predicates — if any is false, skip this action
    Intention string   // CEL expression evaluated against the gRPC response
    Method    string   // Name of a registered ActionMethod
}

// AllowAction permits or denies the request based on request attributes only.
// No gRPC call is made. Implements RequestAction.
type AllowAction struct {
    Predicate []string // CEL predicates — if any is false, skip this action
    Intention string   // CEL expression — if false, deny the request
}

// AddHeadersAction adds headers to the response. Implements ResponseAction.
type AddHeadersAction struct {
    Predicate    []string // CEL predicates — if any is false, skip this action
    HeadersToAdd string   // CEL expression evaluating to a map of headers to add
}

// WithResponseCodeAction modifies the HTTP response code. Implements ResponseAction.
type WithResponseCodeAction struct {
    Predicate       []string // CEL predicates — if any is false, skip this action
    NewResponseCode int      // HTTP status code to set on the response
}
```

**Why separate `RequestAction` and `ResponseAction` interfaces?** Request-phase actions can deny the request — a failed `Intention` expression stops the request from reaching the backend. Response-phase actions cannot deny; the response has already been produced by the backend, so actions can only modify it (add headers, change status code). This fundamental difference in gating semantics means the two phases accept different action types. Separate interfaces enforce this at compile time. An action type that makes sense in both phases can implement both interfaces.

#### gRPC Proto Changes

The gRPC proto (`pkg/extension/grpc/v1/kuadrant.proto`) requires the following changes:

- **Rename** `RegisterUpstreamMethod` RPC to `RegisterActionMethod` — request message gains `name` and `message_template` fields
- **New** `PipelineOnRequest` RPC — accepts a policy and a repeated list of request action entries (each with `action_type`, `predicates`, `intention`, `method`)
- **New** `PipelineOnResponse` RPC — accepts a policy and a repeated list of response action entries (each with `action_type`, `predicates`, `headers_to_add`, `new_response_code`)
- **New** `ActionType` enum — `GRPC_METHOD`, `ALLOW`, `ADD_HEADERS`, `WITH_RESPONSE_CODE`

### Action Method Name Scoping

Action method names (the `Name` in `ActionMethodConfig`) are unique **per-policy**, not globally. This means two different policies can each register a method called `"checkThreatLevel"` without collision.

This is safe because at every point in the data flow, the name is scoped to the owning policy:

1. **ActionMethodStore** (operator): keyed by `{PolicyResourceID, Name}` — no cross-policy collision possible
2. **Wasm services map**: keyed by hash of service config values (Phase 1 design) — the action method name is not part of this key
3. **Wasm Action entries**: anonymous items in an ordered `Actions []Action` list within an `ActionSet` — they carry `SourcePolicyLocators` to track provenance but do not share a namespace
4. **CEL response variables**: scoped to the action that produced them (e.g. `checkThreatLevelResponse.HeatLevel` is only available within the `Intention` field of the same `GRPCMethodAction`) — no cross-action data flow in this phase

Two `ThreatPolicy` instances targeting the same Gateway both register `"checkThreatLevel"`. Each produces its own wasm `Action` entry in the `ActionSet` for that gateway's route. The actions reference the same wasm service (deduplicated by hash), but each action is an independent list item with its own predicates, intention, and source policy locator. There is no shared key where the name `"checkThreatLevel"` could collide.

### Wasm Changes

#### New ActionType Field

The wasm `Action` struct (`internal/wasm/types.go`) gains an explicit `ActionType` discriminator field. The wasm-shim dispatches based on the `ActionType` value, not by inspecting which fields are present (duck-typing). New fields are added for pipeline actions:

- `ActionType` — one of `grpc_method`, `allow`, `add_headers`, `with_response_code`
- `Intention` — CEL expression (for `grpc_method` and `allow`)
- `ActionMethod` — registered method name (for `grpc_method`)
- `HeadersToAdd` — CEL expression evaluating to a map of headers (for `add_headers`)
- `NewResponseCode` — HTTP status code (for `with_response_code`)

**Why an explicit discriminator?** The wasm-shim needs to know unambiguously how to process each action. With duck-typing (checking which fields are set), adding a new action type in the future could create ambiguity if its fields overlap with an existing type. An explicit `ActionType` field makes dispatch deterministic and extensible.

**Backwards compatibility:** Existing actions (auth, ratelimit, tracing) do not set `ActionType`. The wasm-shim falls back to the existing `ServiceName`-based dispatch when `ActionType` is empty, so all current behaviour is preserved.

#### Example Wasm Config

```yaml
services:
  ext-a1b2c3d4:
    type: dynamic
    endpoint: ext-threat-service-security-svc-cluster-local-8080
    failureMode: deny
    timeout: 100ms
    grpcService: "threat.ThreatService"
    grpcMethod: "CheckThreatLevel"

actionSets:
  - name: "abc123-hash"
    routeRuleConditions:
      hostnames: ["api.example.com"]
    actions:
      # GRPCMethodAction from ThreatPolicy
      - service: ext-a1b2c3d4
        scope: request
        actionType: grpc_method
        predicates: ["request.headers['check_threat'] == '1'"]
        actionMethod: checkThreatLevel
        intention: "checkThreatLevelResponse.HeatLevel == 5"
        sources: ["ThreatPolicy/default/my-threat-policy"]

      # AllowAction — no gRPC call
      - service: ""
        scope: request
        actionType: allow
        predicates: ["request.headers['x-bypass'] == 'true'"]
        intention: "request.auth.identity.admin == true"
        sources: ["ThreatPolicy/default/my-threat-policy"]
```

### CEL Intention Validation

When a `GRPCMethodAction` is registered via `OnRequest`, the operator validates the `Intention` CEL expression against the gRPC response proto schema at registration time. This uses the Phase 2 ProtoCache:

1. `OnRequest` receives a `GRPCMethodAction` with `Method: "checkThreatLevel"` and `Intention: "checkThreatLevelResponse.HeatLevel == 5"`
2. The operator looks up the `ActionMethodConfig` for `"checkThreatLevel"` in the `ActionMethodStore`
3. From the config's `Service` and the generated cluster name, the operator retrieves the `FileDescriptorSet` from the `ProtoCache`
4. The operator parses the `Intention` expression, extracts the response variable prefix (`checkThreatLevelResponse`), and resolves it to the method's output message type
5. The operator validates that `HeatLevel` is a valid field on the response message type
6. If validation fails, the `OnRequest` call returns an error immediately — the action is not stored

This catches typos and schema mismatches at reconcile time rather than at request time in the wasm-shim.

### Component Changes

#### 1. ActionMethodStore (internal/extension/registry.go)

Replaces the `registeredUpstreams` map from Phase 1. Stores action method registrations keyed by `{PolicyResourceID, Name}`. Each entry holds the generated cluster name, parsed host/port, target ref, gRPC service/method names, message template, failure mode, and timeout. `ClearPolicyData` clears all entries matching the policy's `ResourceID`, consistent with Phase 1 behaviour.

#### 2. PipelineActionStore (internal/extension/registry.go)

New store for pipeline actions, ordered per-policy. Keyed by `{PolicyResourceID, Phase, Index}` where Phase is `request` or `response` and Index is the insertion order. Each entry holds the action type, predicates, intention, action method name, headers-to-add CEL expression, or response code as appropriate for the action type.

Index allocation is atomic per `(Policy, Phase)` pair — the store maintains a mutex-protected counter so that concurrent `OnRequest`/`OnResponse` calls cannot produce duplicate or out-of-order indices.

#### 3. Server-Side Handlers (internal/extension/manager.go)

**RegisterActionMethod handler:**
- Validates `Name` is non-empty and unique for this policy
- Validates `URL`, `Service`, `Method` are set
- Performs gRPC dial reachability check (from Phase 1)
- Triggers gRPC reflection and caches descriptors (from Phase 2)
- Stores `ActionMethodEntry` in `ActionMethodStore`
- Triggers reconciliation

**PipelineOnRequest handler:**
- Iterates over the `actions` list in order
- For each action, validates the action type is a valid request-phase type (`grpc_method`, `allow`)
- For `grpc_method`: validates `Method` references a registered action method for this policy
- For `grpc_method`: validates `Intention` CEL against the response proto schema (ProtoCache)
- Validates predicate CEL expressions
- If any action fails validation, the entire request is rejected (no partial writes)
- Appends all actions to `PipelineActionStore` with sequential indices for this policy's request phase
- Triggers reconciliation

**PipelineOnResponse handler:**
- Iterates over the `actions` list in order
- For each action, validates the action type is a valid response-phase type (`add_headers`, `with_response_code`)
- Validates predicate CEL expressions
- If any action fails validation, the entire request is rejected (no partial writes)
- Appends all actions to `PipelineActionStore` with sequential indices for this policy's response phase
- Triggers reconciliation

#### 4. Client-Side Implementation (pkg/extension/controller/controller.go)

- **`RegisterActionMethod`** on `ExtensionController` — converts `ActionMethodConfig` to the proto request, sends the RPC, and translates gRPC `Unavailable` status to `types.ErrUpstreamUnreachable`
- **`NewPipeline`** on `ExtensionController` — creates a `PipelineImpl` bound to the policy and gRPC client. Performs no I/O.
- **`PipelineImpl`** — `OnRequest` converts each `RequestAction` to a proto `RequestActionEntry`, sends them all in a single `PipelineOnRequestRequest` RPC. `OnResponse` does the same with `ResponseActionEntry` and `PipelineOnResponseRequest`. Unknown action types return an error before the RPC is sent.

#### 5. Extension Reconcilers (internal/controller/)

The existing `IstioExtensionReconciler` and `EnvoyGatewayExtensionReconciler` are extended to read from both `ActionMethodStore` and `PipelineActionStore` when building wasm configs:

- Action method entries provide the wasm service (same as Phase 1 upstream registration)
- Pipeline action entries are translated into wasm `Action` structs with the `ActionType` field set
- Actions are ordered: request-phase actions first, then response-phase actions, preserving insertion order within each phase
- `SourcePolicyLocators` is populated from the policy identity

#### Extension Author Usage

```go
func (r *ThreatPolicyReconciler) reconcileSpec(ctx context.Context, pol *v1alpha1.ThreatPolicy, kCtx types.KuadrantCtx) (*v1alpha1.ThreatPolicyStatus, error) {
    // Register the gRPC action method (replaces RegisterUpstreamMethod)
    err := kCtx.RegisterActionMethod(ctx, pol, types.ActionMethodConfig{
        Name:            "checkThreatLevel",
        URL:             threatServiceURL,
        Service:         "threat.ThreatService",
        Method:          "CheckThreatLevel",
        MessageTemplate: `{"values": "request.headers['phil']"}`,
    })
    if errors.Is(err, types.ErrUpstreamUnreachable) {
        return calculateErrorStatus(pol, err), err
    }
    if err != nil {
        return calculateErrorStatus(pol, err), err
    }

    // Build the action pipeline
    pipeline := kCtx.NewPipeline(pol)

    // Request phase: call the threat service and evaluate the response
    if err := pipeline.OnRequest(ctx,
        types.GRPCMethodAction{
            Predicate: []string{"request.headers['check_threat'] == '1'"},
            Method:    "checkThreatLevel",
            Intention: "checkThreatLevelResponse.HeatLevel == 5",
        },
        types.AllowAction{
            Predicate: []string{"request.headers['x-bypass'] == 'true'"},
            Intention: "request.auth.identity.admin == true",
        },
    ); err != nil {
        return calculateErrorStatus(pol, err), err
    }

    // Response phase: add a header (using a static value for now —
    // cross-action data flow is future work)
    if err := pipeline.OnResponse(ctx,
        types.AddHeadersAction{
            Predicate:    []string{"response.headers['check_threat'] == ''"},
            HeadersToAdd: "{'x-threat-checked': 'true'}",
        },
    ); err != nil {
        return calculateErrorStatus(pol, err), err
    }

    return calculateEnforcedStatus(pol, nil), nil
}
```

### Security Considerations

- **Name validation**: Action method names are validated to contain only alphanumeric characters, hyphens, and underscores, preventing injection of crafted identifiers into wasm config
- **CEL validation at registration time**: Intention expressions are validated against proto schemas before being stored, preventing malformed CEL from reaching the wasm-shim
- **Policy-scoped lifecycle**: Action methods and pipeline actions are tied to their owning policy and cleaned up via `ClearPolicyData`
- **No user-controlled names in Envoy/wasm service config**: Cluster names and wasm service keys remain operator-generated (Phase 1 design)
- **ActionType validation**: The operator rejects unknown action types; the wasm-shim ignores actions with unrecognised types

## Implementation Plan

1. Rename `RegisterUpstreamMethod` to `RegisterActionMethod` in proto, SDK types, server handler, and client — add `Name` and `MessageTemplate` fields to the config
2. Add action type definitions (`GRPCMethodAction`, `AllowAction`, `AddHeadersAction`, `WithResponseCodeAction`) and `Pipeline` interface to `pkg/extension/types/`
3. Add `PipelineOnRequest` and `PipelineOnResponse` RPCs to the gRPC proto and regenerate
4. Implement `ActionMethodStore` (rename/extend from `RegisteredDataStore` upstream storage)
5. Implement `PipelineActionStore` for ordered pipeline actions
6. Implement server-side `PipelineOnRequest` and `PipelineOnResponse` handlers with CEL intention validation
7. Implement client-side `NewPipeline` and `PipelineImpl` on `ExtensionController`
8. Add `ActionType` discriminator field to wasm `Action` struct
9. Extend extension reconcilers to translate pipeline actions into wasm `Action` entries
10. Update wasm-shim to dispatch based on `ActionType` field

## Testing Strategy

- **Unit tests**: ActionMethodConfig validation, name uniqueness per-policy, CEL intention validation against proto schemas, PipelineActionStore ordering, wasm Action translation with ActionType field, client-side PipelineImpl serialization
- **Integration tests**: End-to-end RegisterActionMethod + Pipeline flow — verify wasm config contains Action entries with correct ActionType, predicates, intention, and method fields; verify cleanup on policy deletion; verify two policies with same action method name do not collide
- **E2E tests**: Deploy ThreatPolicy extension with full pipeline, send HTTP requests, verify gRPC upstream is invoked when predicates match, verify request is denied when intention evaluates to false

## Open Questions

- None currently

## Execution

### Todo

- [ ] [Rename `RegisterUpstreamMethod` to `RegisterActionMethod`](https://github.com/Kuadrant/kuadrant-operator/issues/1899)
  - [ ] Unit tests
- [ ] [Add `ActionMethodConfig` with `Name` and `MessageTemplate` fields](https://github.com/Kuadrant/kuadrant-operator/issues/1900)
  - [ ] Unit tests
- [ ] [Define action types and `Pipeline` interface](https://github.com/Kuadrant/kuadrant-operator/issues/1901)
  - [ ] Unit tests
- [ ] [Add `PipelineOnRequest` and `PipelineOnResponse` RPCs to gRPC proto](https://github.com/Kuadrant/kuadrant-operator/issues/1902)
  - [ ] Unit tests
- [ ] [Implement `PipelineActionStore`](https://github.com/Kuadrant/kuadrant-operator/issues/1903)
  - [ ] Unit tests
- [ ] [Implement server-side pipeline handlers with CEL intention validation](https://github.com/Kuadrant/kuadrant-operator/issues/1904)
  - [ ] Unit tests
  - [ ] Integration tests
- [ ] [Implement client-side `NewPipeline` and `PipelineImpl`](https://github.com/Kuadrant/kuadrant-operator/issues/1905)
  - [ ] Unit tests
- [ ] [Add `ActionType` discriminator to wasm `Action` struct](https://github.com/Kuadrant/kuadrant-operator/issues/1906)
  - [ ] Unit tests
- [ ] [Extend extension reconcilers for pipeline action → wasm Action translation](https://github.com/Kuadrant/kuadrant-operator/issues/1907)
  - [ ] Unit tests
  - [ ] Integration tests
- [ ] [Update wasm-shim to dispatch on `ActionType`](https://github.com/Kuadrant/kuadrant-operator/issues/1908)
  - [ ] Unit tests

### Completed

## Change Log

### 2026-04-16 — OnRequest/OnResponse accept multiple actions

- Changed `OnRequest` and `OnResponse` from single-action to variadic (`...RequestAction`, `...ResponseAction`)
- Proto messages use `repeated RequestActionEntry` / `repeated ResponseActionEntry` sub-messages
- All actions in a single call are validated atomically — if any fails, the entire batch is rejected
- `HeadersToAdd` changed from `map[string]string` to `string` (CEL expression evaluating to a map)
- Standardized cleanup API name to `ClearPolicyData`
- Added atomic index allocation guarantee for `PipelineActionStore`

### 2026-04-15 — Initial design

- Designed Action pipeline API for extension SDK (Kuadrant/kuadrant-operator#1889)
- `RegisterUpstreamMethod` renamed to `RegisterActionMethod` with `Name` and `MessageTemplate` fields
- `Pipeline` API with `OnRequest`/`OnResponse` phases — each call sends gRPC immediately (no Register step)
- Four action types with explicit `ActionType` discriminator: `grpc_method`, `allow`, `add_headers`, `with_response_code`
- Action method names scoped per-policy, not globally — traced through all data flow layers to confirm no collision
- CEL intention validation at registration time using Phase 2 ProtoCache
- Cross-action response data flow deferred to future work
- Documented deviations from issue metacode API with rationale

## References

- [Phase 1: RegisterUpstreamMethod Design](extensions-SDK-register-upstream-method-design.md)
- [Phase 2: gRPC Reflection and Dynamic Messages Design](grpc-reflection-dynamic-messages-design.md)
- [Kuadrant/kuadrant-operator#1889 — SDK API to orchestrate custom policies' Actions](https://github.com/Kuadrant/kuadrant-operator/issues/1889)
