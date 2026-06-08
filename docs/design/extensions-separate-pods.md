# Extensions to Separate Pods — Design & Implementation Plan

## Status: Draft
## Date: 2026-06-08

## Problem Statement

Extensions currently run as child processes inside the operator pod (OOP — out-of-process, same pod). They are discovered via filesystem scanning (`/extensions/{name}/{name}`), spawned with `exec.Command`, and communicate over Unix domain sockets at `/tmp/kuadrant/{name}/.grpc.sock`. The operator is the gRPC server; extensions are clients.

This model couples extension deployment to the operator image and pod lifecycle. Adding or updating an extension requires rebuilding the operator image and restarting the pod. We want extensions to run in their own pods so they can be deployed, scaled, and updated independently.

## Architecture Overview

### Current Model

```
┌──────────────────────────────────────────────────┐
│                 Operator Pod                      │
│                                                   │
│  ┌─────────────┐    Unix Socket    ┌───────────┐ │
│  │  Operator    │◄────────────────►│ Extension  │ │
│  │  (gRPC srv)  │  /tmp/kuadrant/  │ (subprocess│ │
│  │              │   {name}/.sock   │  gRPC cli) │ │
│  └─────────────┘                   └───────────┘ │
│        │                                          │
│        ▼                                          │
│  BlockingDAG (in-memory, atomic pointer)          │
│  RegisteredDataStore (in-memory, mutex-protected) │
└──────────────────────────────────────────────────┘
```

### Target Model

```
┌─────────────────────────┐       ┌─────────────────────┐
│      Operator Pod       │       │   Extension Pod A    │
│                         │       │                      │
│  ┌───────────────────┐  │  TCP  │  ┌────────────────┐ │
│  │ Operator          │◄─┼───────┼──│ Extension      │ │
│  │ (gRPC server)     │  │  TLS  │  │ (gRPC client)  │ │
│  └───────────────────┘  │       │  └────────────────┘ │
│        │                │       └─────────────────────┘
│        ▼                │       ┌─────────────────────┐
│  BlockingDAG            │       │   Extension Pod B    │
│  RegisteredDataStore    │  TCP  │                      │
│  Extension CR watcher ──┼───────┼──► ...              │
└─────────────────────────┘       └─────────────────────┘
```

## Key Concepts Explained

### RegisteredDataStore

The `RegisteredDataStore` (`internal/extension/registry.go`) is the operator's in-memory record of everything extensions have told it. It holds four categories of data, each protected by its own `sync.RWMutex`:

1. **Data Providers** (`dataProviders map[DataProviderKey]DataProviderEntry`): CEL expressions that extensions want injected into Authorino AuthConfigs or Envoy wasm configs. Keyed by (policy + targetRef + domain + binding name). Example: "For ThreatPolicy X targeting HTTPRoute Y, in the auth domain, add binding `x-threat-score` with expression `request.headers['x-score']`."

2. **Subscriptions** (`subscriptions map[SubscriptionKey]Subscription`): CEL expressions the extension asked to watch via `Resolve(subscribe=true)`. The operator re-evaluates these on every topology change and pushes a notification via the `Subscribe` stream only when the result differs from the cached value.

3. **Registered Upstreams** (`registeredUpstreams map[RegisteredUpstreamKey]RegisteredUpstreamEntry`): gRPC services that extensions registered via `RegisterActionMethod`. Includes host, port, service/method names, and cached protobuf descriptors for wasm-shim.

4. **Pipeline Actions** (`pipelineActions map[pipelineKey][]PipelineActionEntry`): Ordered lists of request/response-phase actions committed via `PipelineCommit`. Actions include gRPC calls, deny, add-headers, and fail. Stored per-policy per-phase.

During reconciliation, the operator calls `ApplyAuthConfigMutators` and `ApplyWasmConfigMutators`, which iterate over relevant entries in the store and inject them into the managed resources (AuthConfig, wasm Config).

### BlockingDAG

The `BlockingDAG` (`internal/extension/reconciler.go`) is a global atomic pointer to a `StateAwareDAG` (topology + sync.Map of state). It provides:

- `set(dag)`: Atomically swaps the pointer and broadcasts to all update channels.
- `newUpdateChannel()`: Returns a Go channel that receives every future DAG update.
- `getWait()`: Blocks until the DAG is first set (used at startup).

The `Subscribe` RPC reads from an update channel in a loop. On each DAG update, it re-evaluates all CEL subscriptions for the requesting extension's policy kind. If any expression's result changed, it sends a `SubscribeResponse` to trigger the extension's reconciler.

### Data Flow (end to end)

```
1. Extension reconciler fires (new/updated policy CR)
        │
        ▼
2. Extension calls Resolve(expression, subscribe=true)
   ──► Operator evaluates CEL against topology DAG
   ◄── Returns result; stores subscription
        │
        ▼
3. Extension calls RegisterMutator / RegisterActionMethod
   ──► Operator stores in RegisteredDataStore
   ──► changeNotifier fires
        │
        ▼
4. Operator annotates Kuadrant CR (TriggerTimeAnnotation)
   ──► Main reconciliation loop triggers
        │
        ▼
5. Reconciliation builds AuthConfig / wasm Config
   ──► ApplyAuthConfigMutators / ApplyWasmConfigMutators
   ──► Reads from RegisteredDataStore, injects extension data
        │
        ▼
6. Extension calls PipelineCommit (ordered actions)
   ──► Stored in RegisteredDataStore.pipelineActions
   ──► changeNotifier fires again ──► another reconcile
        │
        ▼
7. End of reconcile: Reconcile() updates BlockingDAG
   ──► Update channels fire
   ──► Subscribe loop re-evaluates CEL subscriptions
   ──► If changed, sends SubscribeResponse to extension
   ──► Extension reconciler fires again (back to step 1)
```

### What Stays the Same

- The gRPC proto service definition (`ExtensionService` in `kuadrant.proto`) — only a `Register` RPC is added
- `RegisteredDataStore` internal logic (maps, mutexes, mutation application)
- `BlockingDAG` / `StateAwareDAG` / topology-based `Reconcile()` function
- CEL evaluation, topology traversal, pipeline validation
- The extension SDK builder pattern (`pkg/extension/controller/builder.go`)
- `DescriptorService` for wasm-shim protobuf descriptor serving
- `MutatorRegistry` and how mutators are applied during reconciliation

---

## Phase 1: Transport & Discovery (Minimum Viable Separate Pods)

### 1.1 Extension CRD for Discovery

**Goal:** Replace filesystem-based extension discovery with a Kubernetes-native mechanism.

**New CRD:** `Extension` (group: `kuadrant.io`, version: `v1alpha1`)

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: Extension
metadata:
  name: threat-policy
  namespace: kuadrant-system
spec:
  # The policy kind this extension manages
  policyKind: ThreatPolicy
  # Optional: timeout for registration after operator startup
  registrationTimeout: 30s
status:
  conditions:
    - type: Connected
      status: "True"
      lastTransitionTime: "2026-06-08T10:00:00Z"
    - type: Registered
      status: "True"
      lastTransitionTime: "2026-06-08T10:00:01Z"
```

**Changes:**
- New API type in `api/v1alpha1/extension_types.go`
- Operator watches `Extension` CRs instead of scanning `/extensions/` directory
- `Manager.discoverExtensions()` replaced with a controller watch
- Extension pods are deployed independently (their own Deployment + ServiceAccount)

**Files affected:**
- `api/v1alpha1/extension_types.go` — new
- `internal/extension/manager.go` — replace `discoverExtensions()`, remove subprocess logic
- `internal/controller/state_of_the_world.go` — update `getExtensionsOptions()`

### 1.2 TCP Transport with Registration Handshake

**Goal:** Extensions connect to the operator over TCP instead of Unix sockets.

**Operator side:**
- Expose a gRPC Service (e.g., `kuadrant-operator-extensions:9443`) for extension connections
- The existing per-extension Unix socket listener in `oop.go` is replaced with a single shared TCP listener
- Add a `Register` RPC to `ExtensionService`:

```protobuf
message RegisterRequest {
  string name = 1;           // Must match an Extension CR name
  string policy_kind = 2;    // Must match Extension CR spec.policyKind
}

message RegisterResponse {
  bool accepted = 1;
  string error = 2;
}

rpc Register(RegisterRequest) returns (RegisterResponse) {}
```

**Extension side:**
- SDK `client.go` changes dialer from `unix://` to `dns:///` or `kubernetes:///`
- Operator address provided via environment variable (e.g., `KUADRANT_OPERATOR_GRPC_ADDRESS`)
- After dialing, extension sends `Register` RPC before any other call
- Operator validates against known Extension CRs and tracks the connection

**Files affected:**
- `pkg/extension/grpc/v1/kuadrant.proto` — add `Register` RPC
- `internal/extension/oop.go` — delete entirely or gut to just connection tracking (no subprocess management)
- `pkg/extension/controller/client.go` — change dialer, add registration call
- `internal/extension/manager.go` — new connection tracking logic, single TCP listener

### 1.3 Remove Subprocess Lifecycle Management

**Goal:** Delete all process management code. Extension pods manage their own lifecycle via Kubernetes.

**Delete:**
- `exec.Command` spawning in `oop.go`
- SIGTERM / SIGKILL shutdown in `OOPExtension.Stop()`
- stderr monitoring in `monitorStderr()`
- Socket file cleanup in `cleanupSocket()`
- `completionWg` / `monitorWg` process wait groups

**Replace with:**
- Connection-level lifecycle: track active gRPC connections per extension
- Health checking: periodic `Ping` RPC or gRPC health protocol
- Status updates on Extension CR (`Connected` condition)

**Files affected:**
- `internal/extension/oop.go` — major rewrite or replace entirely

### 1.4 Connection-Aware Data Cleanup

**Goal:** When an extension disconnects, clean up its data from `RegisteredDataStore`.

**Problem today:** `ClearPolicy` is only called explicitly by the extension's finalizer when a policy CR is deleted. If an extension pod crashes, its data lingers forever.

**Solution:** Track which extension owns which data. On disconnect:
1. Mark Extension CR status as `Connected: False`
2. Start a grace period (configurable, e.g., 30s) for reconnection
3. If grace period expires without reconnect, call `ClearPolicy` for all policies managed by that extension

**Implementation:**
- Add `extensionName` field to `RegisteredDataStore` keys (or a parallel ownership index)
- New method: `ClearExtension(name string)` that removes all data for an extension
- Connection tracking in the manager maps extension name → active gRPC stream

**Files affected:**
- `internal/extension/registry.go` — add ownership tracking, `ClearExtension()`
- `internal/extension/manager.go` — connection lifecycle, grace period timer

### 1.5 Startup Ordering / Readiness Gates

**Goal:** Prevent the operator from reconciling extension-affected resources before extensions have registered.

**Problem:** If the operator starts and reconciles before extensions connect, it produces AuthConfigs and wasm configs with no extension data — briefly nuking extension-driven policy.

**Solution:**
- On startup, operator reads all Extension CRs
- For each Extension CR, wait for a `Register` RPC (or until `spec.registrationTimeout`)
- Only mark the extension reconciler as "synced" once all known extensions have registered or timed out
- Use existing `HasSynced()` mechanism (currently returns `true` unconditionally)

**Files affected:**
- `internal/extension/manager.go` — `HasSynced()` logic, registration tracking

---

## Phase 2: Resilience (Handle Network Failure Modes)

### 2.1 DAG Versioning

Add a monotonic version counter to `StateAwareDAG`. Every `BlockingDAG.set()` increments it. Extensions receive the version in `SubscribeResponse`. When calling `RegisterMutator` / `PipelineCommit`, extensions include the DAG version they observed. The operator rejects registrations from versions older than N-K (configurable staleness window).

This prevents split-brain scenarios where two extensions register mutators based on different topology views.

**Files affected:**
- `internal/extension/reconciler.go` — version counter in `StateAwareDAG`
- `pkg/extension/grpc/v1/kuadrant.proto` — version field in `SubscribeResponse`, `RegisterMutatorRequest`, `PipelineCommitRequest`
- `internal/extension/manager.go` — version validation in RPC handlers

### 2.2 Subscription Resume

Add `last_seen_version` to `SubscribeRequest`. On reconnect, the operator replays all subscription change events from `last_seen_version` to current. This prevents extensions from missing topology changes that occurred during a brief disconnect.

**Alternative (simpler):** On reconnect, force a full re-evaluation of all subscriptions for that extension and send notifications for any that differ from the extension's last known state. No replay log needed.

**Files affected:**
- `pkg/extension/grpc/v1/kuadrant.proto` — `last_seen_version` in `SubscribeRequest`
- `internal/extension/manager.go` — `Subscribe` handler reconnect logic

### 2.3 Extension-Side State Caching for Replay

Extensions must remember what they registered so they can replay on reconnect. Today they are stateless — the SDK calls `RegisterMutator` / `PipelineCommit` during reconciliation and forgets.

Add a local cache in the SDK (`pkg/extension/controller/`) that stores all registrations. On reconnect, the SDK automatically replays all cached registrations before resuming normal operation.

**Files affected:**
- `pkg/extension/controller/controller.go` — add registration cache, replay logic
- `pkg/extension/controller/client.go` — reconnection with replay

---

## Phase 3: Persistence (Survive Operator Restarts)

### 3.1 Persistent RegisteredDataStore

Back the `RegisteredDataStore` with a Kubernetes resource (ConfigMap or dedicated CRD) so the operator can restore extension state after a restart without waiting for all extensions to re-register.

**Options:**
- **ConfigMap per extension:** Serialize each extension's data (mutators, upstreams, pipeline actions) as JSON. Operator reads on startup.
- **Dedicated CRD (`ExtensionState`):** More structured, versioned, but heavier.
- **Annotate the policies themselves:** Each policy CR carries its extension-derived data as annotations. Most Kubernetes-native, but size-limited.

This eliminates the "operator restart → brief policy nuke" problem entirely.

### 3.2 Operator-Side Reconciliation Hold-Off (Enhanced)

Even with persistent state, add a configurable hold-off period after startup. The operator loads persisted state and reconciles with it immediately (no policy nuke), but waits for extensions to confirm their registrations match before considering the state fully consistent.

---

## Phase 4: Security

### 4.1 mTLS

Secure the gRPC connection between extensions and operator with mutual TLS. Use cert-manager to provision per-extension certificates, or leverage the service mesh's mTLS if available.

### 4.2 Extension Identity Verification

After mTLS, verify the extension's certificate CN/SAN matches the Extension CR it claims in the `Register` RPC. Prevents a rogue pod from impersonating an extension.

### 4.3 Upstream URL Validation

`RegisterActionMethod` accepts arbitrary gRPC URLs. Validate that URLs resolve to known services (e.g., Services in allowed namespaces). Consider an allowlist in the Extension CR spec.

---

## Risks & Open Questions

1. **RegisteredDataStore ownership granularity:** Currently keyed by policy, not by extension. Multiple extensions could theoretically register mutators for the same policy. Need to decide: is that valid? If so, cleanup on disconnect must be per-extension, not per-policy.

2. **DescriptorService location:** Currently runs on TCP port 50051 inside the operator pod. Wasm-shim (running in Envoy sidecars) calls it. This doesn't change — the descriptor service stays in the operator pod. But if extensions register upstream methods, the operator still needs to reach those upstream gRPC services for reflection. Network policies must allow operator → extension-registered-upstream traffic.

3. **Extension CRD vs. labels/annotations:** An Extension CRD is the cleanest approach, but an alternative is to discover extension pods via label selectors (e.g., `kuadrant.io/extension=threat-policy`). CRD is preferred because it gives a place for spec (policyKind, timeout) and status (Connected, Registered).

4. **Single vs. multiple operator replicas:** If the operator runs with leader election (multiple replicas), only the leader should accept extension connections. Extensions must handle leader failover (reconnect to new leader). This is an existing constraint that doesn't change, but worth noting.

5. **gRPC load balancing:** If the operator is behind a Kubernetes Service, gRPC's HTTP/2 connection reuse means all RPCs from one extension go to one operator replica. This is desired (stateful connection), but must be understood when scaling.

---

## Implementation Order (Suggested)

```
Phase 1.1  Extension CRD ──────────────────────────┐
Phase 1.2  TCP transport + Register RPC ────────────┤
Phase 1.3  Remove subprocess management ────────────┤── MVP: extensions in separate pods
Phase 1.4  Connection-aware cleanup ────────────────┤
Phase 1.5  Startup readiness gates ─────────────────┘
                        │
Phase 2.1  DAG versioning ─────────────────────────┐
Phase 2.2  Subscription resume ────────────────────┤── Resilience
Phase 2.3  Extension-side state caching ───────────┘
                        │
Phase 3.1  Persistent RegisteredDataStore ─────────┐── Persistence
Phase 3.2  Enhanced hold-off ──────────────────────┘
                        │
Phase 4.1  mTLS ───────────────────────────────────┐
Phase 4.2  Identity verification ──────────────────┤── Security
Phase 4.3  Upstream URL validation ────────────────┘
```
