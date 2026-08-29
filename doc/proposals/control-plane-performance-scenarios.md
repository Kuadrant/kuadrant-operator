# Control Plane Performance Scenarios

**Status:** Proposal
**Authors:** TBD
**Date:** 2026-04-01

## Purpose

Establish performance benchmarks for the Kuadrant control plane to ensure reliability, stability, and predictable behavior under varying operational conditions. Currently, we have no baseline performance data or understanding of how the operator behaves under different loads and scenarios. 

The purpose of this document is to define what to measure and how to measure it. The SLOs and targets specified here are initial estimates based on user expectations and system requirements. Actual SLOs will be determined from empirical data gathered through benchmark testing. We need to discover what realistic performance characteristics are before we can commit to defensible service level objectives.

## Scenario 1: Policy Enforcement Latency

### Description

When a user applies a policy (`kubectl apply`), it goes through several stages before becoming active: the operator reconciles the policy, updates its status to Enforced, creates necessary data plane resources (AuthConfig, WasmPlugin/EnvoyExtensionPolicy), and the wasm-shim loads the configuration. This end-to-end process takes time, and understanding how long it takes under different conditions is critical for setting user expectations and identifying bottlenecks.

We need to measure how this latency is influenced by policy type, target resources, system load, and gateway provider.

### Metrics

**Measurement points:**
- **T0:** Policy resource created (`metadata.creationTimestamp`)
- **T1:** Policy status reaches `Enforced=True` (`status.conditions[type=Enforced].lastTransitionTime`)
- **T2:** Wasm-shim acknowledges configuration


**Primary metric: Control Plane Enforcement Time (T0 → T1)**

Time from policy creation until `status.conditions[type=Enforced,status=True]`.

**What `Enforced=True` actually means:**

- **AuthPolicy (EnvoyGateway)**: Authorino ready + AuthConfig ready + EnvoyPatchPolicy programmed + EnvoyExtensionPolicy accepted (via `gatewayapiv1alpha2.PolicyConditionAccepted` in ancestor status)
- **AuthPolicy (Istio)**: Authorino ready + AuthConfig ready + WasmPlugin resource created (Istio does not populate status)
- **RateLimitPolicy (EnvoyGateway)**: Limitador ready + Limitador limits configured + EnvoyExtensionPolicy accepted
- **RateLimitPolicy (Istio)**: Limitador ready + Limitador limits configured + WasmPlugin resource created
- **DNSPolicy**: DNSRecord created and ready (status semantics differ from data plane policies)
- **TLSPolicy**: Certificate created via cert-manager (status semantics differ from data plane policies)

For **EnvoyGateway**, T1 includes data plane acknowledgment via Gateway API ancestor status, making T0→T1 the true end-to-end enforcement time.

For **Istio**, T1 only confirms `WasmPlugin` resource creation (Istio doesn't populate status), so T2 verification may be needed.

**Secondary metrics:**

1. **Wasm-shim Configuration Acknowledgment (T1 → T2, Istio only)**
   - Time from Enforced=True to wasm-shim config loaded
   - Validates actual enforcement when status unavailable
   - Not needed for EnvoyGateway (already captured in T1)

2. **Pending Events Count (at T0)**
   - Number of events buffered by the policy-machinery controller when policy is created
   - Explains variance: if 50 events are pending, policy waits for those to process first
   - **Note:** The policy-machinery controller does not use traditional workqueues. It processes batches of events through a single workflow. Standard controller-runtime `workqueue_depth` metrics may not be emitted.
   - Metric source: Custom instrumentation or OpenTelemetry span analysis (event batch size)
   - High pending event count = longer wait before reconciliation starts

3. **Breakdown by Stage (identifies bottlenecks)**

   **Note:** `Accepted` and `Enforced` conditions are set in the same reconciliation cycle and often have identical `lastTransitionTime` values. They cannot be used to measure distinct stages. Instead, measure actual reconciliation boundaries:

   - **T0 → First reconciliation triggered:** Event delivery latency from API server to operator
     - Metric source: OpenTelemetry trace span start time for reconciliation
   
   - **Reconciliation start → Dependent resources created:** Controller execution time
     - For AuthPolicy: AuthConfig resource creation timestamp - reconciliation span start
     - For RateLimitPolicy: Limitador limit POST completion - reconciliation span start
     - Measures reconciler workflow execution time
   
   - **Dependent resources created → Dependent resources ready:** External system processing time
     - AuthConfig created → AuthConfig ready (Authorino processing)
     - Limitador limits sent → Limitador confirms configuration
     - Measures external dependency responsiveness
   
   - **Dependent resources ready → Gateway resources created:** Data plane resource provisioning time
     - WasmPlugin/EnvoyExtensionPolicy creation timestamp - dependent resource ready time
     - Measures gateway provider resource creation latency
   
   - **Gateway resources created → Gateway resources acknowledged (EnvoyGateway only):** Data plane acknowledgment time
     - `PolicyConditionAccepted` in ancestor status - WasmPlugin/EnvoyExtensionPolicy creation
     - EnvoyGateway processing and status update latency
   
   - **All components ready → Status update (T1):** Status updater execution time
     - `conditions[type=Enforced].lastTransitionTime` - last component ready time
     - Measures final status update latency
   
   **Metric source:** OpenTelemetry tracing spans (already instrumented in `state_of_the_world.go` lines 678-699). Each workflow step is traced with named spans. Controller-runtime `workqueue_*` metrics may not be emitted by the policy-machinery controller and should not be relied upon.
   
   Identifies which stage dominates T0→T1 latency: event delivery, reconciliation logic, external dependencies, or data plane provisioning.

### Service Level Objectives (SLOs)

| Metric | p50 | p95 | p99 | Rationale |
|--------|-----|-----|-----|-----------|
| **End-to-End Enforcement Time (T0→T1)** | < 3s | < 5s | < 10s | User expectation: policy should work within seconds of creation |
| Control Plane Reconciliation (T0→T1) | < 2s | < 3s | < 5s | Includes data plane resource acknowledgment (EnvoyGateway) |
| Additional Verification (T1→T2, Istio only) | < 1s | < 2s | < 5s | Verify wasm plugin loaded when status unavailable |

**Note:** For EnvoyGateway, T1 already includes data plane acknowledgment via `EnvoyExtensionPolicy.Status`, so T0→T1 represents true end-to-end time. For Istio, T1→T2 may still be needed to verify actual wasm plugin loading.

**Failure conditions:**
- Any enforcement time > 30 seconds is unacceptable (user will file bug report)
- Status=Enforced but policy not working for > 5 seconds (observability lie)

### Test Variations

**Vary to understand behavior:**

1. **Policy type:**
   - AuthPolicy: different identity providers (API key vs OIDC vs mTLS)
   - RateLimitPolicy: different limit complexity (1 limit vs 10 limits)
   - DNSPolicy: different DNS provider (AWS vs GCP vs Azure)
   - TLSPolicy: different cert issuer (self-signed vs Let's Encrypt)

2. **Target type:**
   - Gateway-level policy vs HTTPRoute-level policy
   - Policy with overrides vs defaults
   - Policy targeting shared gateway vs dedicated gateway

3. **System load:**
   - Clean system (no existing policies) vs loaded system (100 existing policies)
   - Single policy creation vs batch creation (10 policies simultaneously)
   - Operator under load (other reconciliations in progress)

4. **Gateway provider:**
   - **EnvoyGateway**: T0→T1 includes full data plane acknowledgment (most accurate)
   - **Istio**: T0→T1 only confirms WasmPlugin creation, may need T1→T2 verification
   - Compare enforcement latency between providers

**Expected insights:**
- Does enforcement latency degrade with policy count? (should be O(1), not O(n))
- Which policy type is slowest to enforce?
- Is there a queue backlog issue under burst load?
- What is the delta between EnvoyGateway (with status) and Istio (without status) enforcement timing?
- For Istio, what is the T1→T2 gap (WasmPlugin created vs. actually loaded)?

---

## Scenario 2: Operator Restart with Existing Resources

### Description

When the operator restarts (upgrade, crash, or node failure), it must re-initialize its cache by listing and watching all relevant resources from the Kubernetes API server. This includes all policies (AuthPolicy, RateLimitPolicy, DNSPolicy, TLSPolicy, TokenRateLimitPolicy), gateways, HTTPRoutes, and dependent resources like AuthConfigs, WasmPlugins, EnvoyExtensionPolicies, and Limitador configurations. The operator loads these resources into memory, builds the topology DAG, establishes watches, and must become ready to reconcile new changes. This startup process consumes memory and time, and the operator must successfully complete it without OOMing.

**Note:** The kuadrant-operator does NOT watch secrets directly - they are not part of the topology. Secrets are consumed by dependency operators (cert-manager, Authorino) but are not cached by the kuadrant-operator itself.

We need to measure how startup performance and memory consumption scale with cluster resource count, and identify resource limits that prevent successful operator restarts.

### Metrics

**Measurement points:**
- **T0:** Operator pod starts (container started timestamp)
- **T1:** Cache synchronized (watches established, initial list/watch complete)
- **T2:** First reconciliation completes successfully

**Primary metric: Time to Operational (T0 → T2)**

Time from pod start until operator can successfully reconcile a new policy. Measures recovery time after restart.

**Secondary metrics:**

1. **Cache Initialization Time (T0 → T1)**
   - Time to complete initial list/watch for all resource types
   - Metric: Log timestamp analysis or custom instrumentation
   - Scales with resource count in cluster

2. **Peak Memory During Startup (T0 → T1)**
   - Maximum memory usage during cache initialization
   - Metric: `container_memory_working_set_bytes` max value during T0→T1
   - Critical for preventing OOM crashes

3. **Steady-State Memory After Startup (at T2)**
   - Memory usage after cache initialization and first GC
   - Metric: `container_memory_working_set_bytes` at T2
   - Baseline for detecting memory leaks

4. **Memory Growth per Resource Type**
   - Memory delta per additional resource loaded into cache
   - Measured by varying resource counts and observing peak memory
   - Example: (Peak memory with 5000 secrets - Peak memory with 1000 secrets) / 4000
   - Enables capacity planning and resource limit setting

5. **Goroutine Count (at T2)**
   - Number of goroutines after startup stabilizes
   - Metric: `go_goroutines` at T2
   - Detect goroutine leaks (should not grow unbounded)

6. **API Server Request Burst (T0 → T1)**
   - Number of API requests during cache initialization
   - Metric: count of requests to kube-apiserver during T0→T1
   - Should use efficient list/watch, not individual GETs

### Service Level Objectives (SLOs)

| Metric | Target | Max Acceptable | Rationale |
|--------|--------|----------------|-----------|
| **Time to Operational** | < 30s | < 60s | Fast recovery after crash/upgrade |
| Cache Initialization Time | < 20s | < 45s | API server load should be manageable |
| **Peak Memory (1000 policies + 1000 routes)** | **< 1GB** | **< 1.5GB** | **Prevent OOM during startup** |
| Steady-State Memory | < 800MB | < 1GB | After GC, memory should drop |
| Memory per Policy | < 100KB | < 200KB | Enable capacity planning |
| Memory per HTTPRoute | < 50KB | < 100KB | Enable capacity planning |

**Resource count scaling (combined policy + route counts):**

| Total Resources (Policies + HTTPRoutes + Gateways) | Peak Memory Target | Max Acceptable |
|----------------------------------------------------|-------------------|----------------|
| 500 resources (400 policies + 80 routes + 20 gateways) | < 500MB | < 750MB |
| 2,000 resources (1600 policies + 320 routes + 80 gateways) | < 1.5GB | < 2GB |
| 5,000 resources (4000 policies + 800 routes + 200 gateways) | < 3GB | < 4GB |

**Failure conditions:**
- Operator OOMs during startup with < 5,000 total resources (unacceptable)
- Time to operational > 2 minutes (users will assume operator is broken)
- Memory leak: steady-state memory at T+10m > steady-state memory at T+1m by more than 20% (memory growing over time, not being garbage collected)

### Test Variations

**Vary to understand behavior:**

1. **Resource types and counts (resources actually watched by the operator):**
   - **Policies** (AuthPolicy, RateLimitPolicy, DNSPolicy, TLSPolicy, TokenRateLimitPolicy): 0, 100, 500, 1000, 2000
   - **HTTPRoutes**: 0, 100, 500, 1000, 2000
   - **Gateways**: 0, 10, 50, 100, 200
   - **Provider-specific resources** (WasmPlugins, EnvoyExtensionPolicies, AuthConfigs): varies with policy count
   - **ConfigMaps** (filtered by label): 0, 50, 100, 500
   - **Deployments** (filtered by label): 0, 10, 50, 100

2. **Restart scenarios:**
   - **Cold start:** Operator starts with resources already present (most common)
   - **Hot restart:** Operator crashes and restarts (no cache warmup)
   - **Upgrade:** New operator version deployed (may have different cache logic)
   - **Node failure:** Operator pod rescheduled to different node

3. **Operator variations:**
   - Test each operator separately (kuadrant, authorino, dns, limitador)
   - Identify which operator has highest memory usage
   - Prioritize fixing the most problematic operator

**Expected insights:**
- Which operator consumes most memory on startup?
- Is memory scaling linear, sub-linear, or super-linear with resource count?
- Which resource types cause highest memory usage? (policies vs HTTPRoutes vs provider-specific resources)
- Does the topology DAG size grow linearly with resource count?
- Does operator OOM with realistic cluster state?

### Implementation Requirements

Several metrics require instrumentation not currently exposed by the operator:

1. **Cache Initialization Time (T1):**
   - Requires custom instrumentation to track cache sync completion
   - Controller-runtime exposes `workqueue_*` metrics but not cache sync events
   - Possible approaches: custom metric on informer sync, log timestamp analysis

2. **Peak Memory During Startup:**
   - Requires either Prometheus container metrics collection or custom instrumentation
   - Can use `/metrics` endpoint memory stats or Kubernetes metrics-server
   - May require memory profiling (pprof) for detailed per-resource analysis

3. **Time to Operational (T2):**
   - Composite metric requiring coordination between startup time and first successful reconciliation
   - May require custom readiness probe or lifecycle event tracking

4. **Memory Growth per Resource:**
   - Requires controlled testing with incremental resource addition
   - Best measured via load testing with memory profiling enabled

**Existing infrastructure:**
- Prometheus metrics already exposed at `/metrics` endpoint
- Controller-runtime provides workqueue and reconciliation metrics
- Grafana dashboards exist in [examples/dashboards/](examples/dashboards/)

---

## Scenario 3: Reconciliation Performance Under Load

### Description

As policy count grows in a cluster, reconciliation time will increase due to the operator's architecture. Reconciliation involves building the full topology DAG, computing effective policies for all policy types, and creating dependent resources. The policy-machinery controller rebuilds the topology from scratch on every event and computes effective policies by iterating over relevant topology objects. This is **inherently O(N)** where N is the total number of policies, routes, and gateways.

The goal is not to achieve O(1) (which is architecturally impossible), but to ensure:
1. **Linear scaling with low slope**: marginal reconciliation time grows slowly as policy count increases
2. **No quadratic degradation**: reconciliation should not exhibit O(N²) or worse behavior
3. **Acceptable absolute performance**: even at high policy counts (1000+), reconciliation completes within seconds

We need to measure marginal reconciliation time as policy count increases, identify algorithmic bottlenecks that could cause super-linear scaling, and validate the system can handle bursts of policy creation.

### Metrics

**Measurement approach:**
- Given: N policies already exist and are Enforced
- Action: Create policy N+1
- Measure: Time from creation to Enforced=True for policy N+1
- Repeat for N = 0, 10, 50, 100, 250, 500, 1000

**Primary metric: Marginal Reconciliation Time(N)**

Time to reconcile one additional policy when N existing policies are present. Measured as T0→T1 (creation to Enforced=True) for the new policy.

**Note:** The policy-machinery controller processes events in batches through a single workflow that rebuilds the full topology and computes effective policies for all policy types. Each reconciliation iteration processes ALL pending events, not individual policies. This means reconciliation time depends on both the number of existing resources (topology size) and the number of concurrent events.

Metric source: OpenTelemetry trace span duration for the reconciliation workflow, OR p95 of end-to-end latency (policy creation to Enforced=True). Standard controller-runtime `workqueue_work_duration_seconds_bucket` may not be emitted by the policy-machinery controller.

**Secondary metrics:**

1. **Reconciliation Time Scaling Coefficient**
   - Regression slope of Marginal Reconciliation Time vs. N
   - Expected: Small positive slope (O(N) behavior with low constant factor)
   - Acceptable: < 10% growth per 100 policies (linear with manageable slope)
   - Unacceptable: > 20% growth per 100 policies (indicates O(N²) or algorithmic inefficiency)
   - Indicates whether reconciliation logic exhibits linear, quadratic, or worse scaling

2. **Pending Event Count Under Burst**
   - Create 100 policies simultaneously
   - Measure: Max pending events buffered by the controller, time to process all events
   - **Note:** Policy-machinery controller does not use workqueues. It batches events and processes them through a single workflow.
   - Metric source: Custom instrumentation or OpenTelemetry span analysis (event batch sizes over time)
   - Identifies batching/throttling behavior and event processing capacity

3. **Reconciliation Throughput**
   - Policies reconciled per second under sustained load
   - Metric: rate(`controller_runtime_reconcile_total`) during policy creation burst
   - Maximum reconciliation capacity of the operator

4. **Time to Steady State After Batch Operations**
   - Create 100 policies simultaneously
   - Measure: time until all 100 reach Enforced=True
   - Indicates system recovery time after load spike

5. **Cross-Policy Reconciliation Interference**
   - Update existing policy A while creating new policy B
   - Measure: impact on policy B's reconciliation time
   - Determines if reconciliations compete for shared resources

6. **API Server Request Rate During Reconciliation**
   - API requests/second during policy reconciliation burst
   - Should remain below API server rate limits
   - Metric: count of requests to kube-apiserver during burst period

**Metric sources:**

**OpenTelemetry tracing (primary source):**
- Reconciliation workflow span duration - end-to-end reconciliation time
- Individual workflow step spans - breakdown by stage (topology rebuild, effective policy computation, resource creation)
- Already instrumented in `state_of_the_world.go` lines 678-699

**Controller-runtime metrics (may not be emitted by policy-machinery controller):**
- `controller_runtime_reconcile_total` - total reconciliations (verify availability)
- `controller_runtime_reconcile_errors_total` - reconciliation errors (verify availability)
- `workqueue_work_duration_seconds_bucket` - NOT RELIABLE (policy-machinery does not use traditional workqueues)
- `workqueue_depth` - NOT RELIABLE (policy-machinery batches events differently)
- `workqueue_adds_total` - NOT RELIABLE
- `workqueue_retries_total` - NOT RELIABLE

**Custom Kuadrant metrics:**
- `kuadrant_policies_total{kind}` - total policies by type
- `kuadrant_policies_enforced{kind,status}` - enforcement status tracking

**Recommendation:** Use OpenTelemetry tracing as the primary metric source. Verify controller-runtime metrics empirically before relying on them.

### Service Level Objectives (SLOs)

| Metric | Target | Max Acceptable | Rationale |
|--------|--------|----------------|-----------|
| **Reconciliation Time Scaling** | **Linear (O(N)) with slope < 5ms/policy** | **< 10% growth per 100 policies** | Scalability: linear growth is acceptable, quadratic is not |
| Marginal Reconciliation Time (absolute) | < 3s (p95) at N=0 | < 6s (p95) at N=1000 | Absolute performance matters even with scaling |
| Event Batch Processing Time (100 policies) | < 60s | < 120s | Burst should be handled quickly |
| Reconciliation Throughput | > 2 workflows/sec | > 1 workflow/sec | Each workflow processes all pending events |
| API Request Rate | < 50 req/sec | < 100 req/sec | Don't overwhelm API server |

**Scaling behavior validation:**

| Existing Policy Count | Target Reconciliation Time (p95) | Max Acceptable |
|-----------------------|----------------------------------|----------------|
| 0 | < 2s | < 3s |
| 100 | < 2.5s | < 4s |
| 500 | < 4s | < 6s |
| 1000 | < 5s | < 8s |
| 2000 | < 7s | < 12s |

**Target: ~0.5s growth per 100 policies (linear with low slope)**
**Acceptable: < 10% growth per 100 policies (0.2-1s depending on baseline)**
**Unacceptable: > 20% growth per 100 policies (indicates super-linear scaling or O(N²) behavior)**

**Failure conditions:**
- Reconciliation time more than triples from 0 to 1000 policies (super-linear or O(N²) scaling)
- Reconciliation time > 10s at any policy count (absolute performance failure)
- Pending event count grows unbounded (no backpressure, eventual OOM)
- API server rate limit hit (operator needs throttling)

### Test Variations

**Vary to understand behavior:**

1. **Existing policy count (N):**
   - Test points: N = 0, 10, 50, 100, 250, 500, 1000
   - Plot marginal reconciliation time vs N
   - Identify scaling characteristic

2. **Burst intensity:**
   - Create 1 policy (baseline)
   - Create 10 policies simultaneously
   - Create 50 policies simultaneously
   - Create 100 policies simultaneously
   - Measure queue depth, drain time

3. **Policy complexity:**
   - Simple policy (1 rule) vs complex policy (50 rules)
   - Does complex policy slow down reconciliation?

4. **Policy type mix:**
   - 1000 RateLimitPolicies only
   - 500 AuthPolicies + 500 RateLimitPolicies (mixed)
   - Different operators, different reconciliation paths

5. **Namespace distribution:**
   - 1000 policies in 1 namespace (single-tenant)
   - 1000 policies across 100 namespaces (multi-tenant)
   - Does cross-namespace watching affect performance?

6. **Target resource distribution:**
   - 1000 policies targeting 1 gateway (shared gateway)
   - 1000 policies targeting 1000 different gateways (dedicated gateways)
   - Does target fanout affect reconciliation?

**Expected insights:**
- Is reconciliation time O(N) with low slope, or O(N²) or worse?
- What is the slope coefficient (ms per additional policy)?
- Which stage dominates reconciliation time: topology rebuild, effective policy computation, or resource creation?
- Does the single-workflow architecture cause unnecessary re-processing of unchanged policies?
- What is the maximum policy count before absolute performance becomes unacceptable (> 10s)?
- What is the maximum burst size the system can handle before event batching causes delays?
- Which operator has the worst scaling characteristics?

---

## Scenario 4: Policy Update and Deletion Latency

### Description

Policy lifecycle includes not just creation but also updates and deletions. These operations have different performance characteristics:

- **Updates**: Trigger re-evaluation of effective policies, may require updating dependent resources (AuthConfig, Limitador limits, WasmPlugin configuration), and could trigger cascading reconciliations if the policy's status changes affect other policies.

- **Deletions**: Require cleanup of dependent resources (AuthConfig, WasmPlugin, EnvoyExtensionPolicy, Limitador limits), may trigger re-evaluation of effective policies as other policies become effective in the absence of the deleted policy, and must handle finalizers.

Understanding update and deletion latency is critical for operational workflows like rolling updates, policy tuning, and incident response.

### Metrics

**Measurement points (Update):**
- **T0:** Policy update applied (`metadata.generation` increments)
- **T1:** Updated policy status reaches `Enforced=True` with new generation (`status.observedGeneration == metadata.generation`)
- **T2:** Dependent resources reflect the update (AuthConfig, WasmPlugin configuration updated)

**Measurement points (Deletion):**
- **T0:** Policy deletion requested (`metadata.deletionTimestamp` set)
- **T1:** Dependent resources cleaned up (AuthConfig deleted, Limitador limits removed, WasmPlugin updated)
- **T2:** Policy finalizers removed and policy fully deleted from API server

**Primary metrics:**

1. **Policy Update Latency (T0 → T1)**
   - Time from update to Enforced=True with new observed generation
   - Measures how quickly changes take effect

2. **Policy Deletion Latency (T0 → T2)**
   - Time from deletion request to policy fully removed
   - Measures cleanup efficiency and finalizer processing

**Secondary metrics:**

3. **Dependent Resource Update Latency**
   - Time from policy update to dependent resource updated
   - Identifies bottlenecks in external system updates (Authorino, Limitador)

4. **Cascading Reconciliation Impact**
   - When deleting policy A causes policy B to become effective, measure time until policy B's Enforced status updates
   - Identifies cross-policy reconciliation dependencies

### Service Level Objectives (SLOs)

| Metric | p50 | p95 | p99 | Rationale |
|--------|-----|-----|-----|-----------|
| Policy Update Latency | < 3s | < 5s | < 10s | Similar to creation - users expect quick updates |
| Policy Deletion Latency | < 2s | < 4s | < 8s | Cleanup should be faster than creation |
| Cascading Reconciliation | < 5s | < 10s | < 20s | Secondary effect, can tolerate slightly higher latency |

### Test Variations

1. **Update types:**
   - Spec-only update (no status change)
   - Update that changes enforcement (e.g., changing target reference)
   - Update to already-enforced policy vs. not-yet-enforced policy

2. **Deletion scenarios:**
   - Delete policy with no dependents (simple cleanup)
   - Delete policy causing another policy to become effective (cascading)
   - Delete policy with finalizers

3. **System load:**
   - Update/delete with 0 existing policies vs. 1000 existing policies
   - Batch updates (10 policies updated simultaneously)
   - Batch deletions (10 policies deleted simultaneously)

---

## Scenario 5: Extension Reconciliation Overhead

### Description

The operator supports out-of-process (OOP) extensions via gRPC over Unix domain sockets. Extensions introduce additional latency to reconciliation through:

1. **Event delivery**: Extension manager must forward topology events to extension processes
2. **CEL evaluation**: Extensions call back to the operator via `kuadrant.Resolve(CEL)` for topology queries, which requires gRPC round-trips
3. **Binding publication**: Extensions publish key-value bindings via `kuadrant.AddDataTo()`, which are aggregated into downstream resources

With extensions enabled, reconciliation has additional latency that must be measured separately from core policy reconciliation.

### Metrics

**Primary metric: Extension Overhead**

Compare end-to-end reconciliation time (T0 → T1) with and without extensions enabled:
- Baseline: Reconciliation time with extensions disabled
- With extensions: Reconciliation time with extensions enabled
- Extension overhead = (With extensions) - (Baseline)

**Secondary metrics:**

1. **gRPC Call Latency**
   - Time per `kuadrant.Resolve()` call from extension to operator
   - Identifies socket communication overhead

2. **CEL Evaluation Time**
   - Time to evaluate CEL expressions in extension context
   - Measures topology query performance

3. **Binding Aggregation Time**
   - Time to aggregate bindings from multiple extensions into downstream resources (AuthConfig, WasmPlugin)
   - Measures binding processing overhead

4. **Extension Count Scaling**
   - Measure overhead with 1, 2, 3, 5 extensions enabled
   - Determine if overhead is per-extension or constant

### Service Level Objectives (SLOs)

| Metric | Target | Max Acceptable | Rationale |
|--------|--------|----------------|-----------|
| Extension Overhead | < 500ms | < 1s | Extensions should not double reconciliation time |
| gRPC Call Latency (p95) | < 50ms | < 100ms | Unix socket should be fast |
| CEL Evaluation Time (p95) | < 100ms | < 200ms | Topology queries should be efficient |
| Overhead per Extension | < 200ms | < 500ms | Linear scaling with extension count |

### Test Variations

1. **Extension types:**
   - Simple extension (no CEL evaluation, minimal bindings)
   - Complex extension (heavy CEL evaluation, many bindings)
   - Multiple extensions (1, 2, 3, 5 extensions enabled)

2. **CEL evaluation complexity:**
   - Simple queries (`findGateways()`)
   - Complex queries with filtering
   - Queries over large topology (1000 policies)

3. **Binding count:**
   - Few bindings (1-5 per policy)
   - Many bindings (50+ per policy)

---

## Scenario 6: Topology Rebuild Performance

### Description

The policy-machinery controller rebuilds the topology DAG from scratch on every reconciliation event. The topology includes all policies, gateways, HTTPRoutes, and dependent resources, organized into a directed acyclic graph representing relationships (policy → target, route → gateway, etc.).

Topology rebuild time is the foundation of all reconciliation operations. If topology rebuild scales poorly, all other metrics will degrade. This scenario measures topology rebuild cost as a function of total object count.

### Metrics

**Primary metric: Topology Rebuild Time**

Time to construct the topology DAG from API server state. Measured via OpenTelemetry span for the `TopologyReconciler.Reconcile` step.

**Secondary metrics:**

1. **Topology Size Metrics**
   - Node count (policies + gateways + routes + dependent resources)
   - Edge count (relationships between nodes)
   - DAG depth (longest path from root to leaf)

2. **Rebuild Time Scaling**
   - Topology rebuild time vs. node count
   - Expected: O(N) or O(N log N) with low constant factor
   - Unacceptable: O(N²) or worse

3. **API Server List Call Latency**
   - Time to list all resources during topology rebuild
   - Dominated by API server response time
   - May be the bottleneck for large clusters

### Service Level Objectives (SLOs)

| Topology Size (Nodes) | Target Rebuild Time | Max Acceptable | Rationale |
|-----------------------|---------------------|----------------|-----------|
| 100 nodes | < 100ms | < 200ms | Small cluster, should be very fast |
| 500 nodes | < 500ms | < 1s | Medium cluster |
| 1000 nodes | < 1s | < 2s | Large cluster |
| 5000 nodes | < 5s | < 10s | Very large cluster (stress test) |

**Scaling target: Linear (O(N)) with slope < 1ms per node**

### Test Variations

1. **Resource distribution:**
   - Topology with many policies, few routes
   - Topology with few policies, many routes
   - Topology with many gateways (high fanout)

2. **Topology complexity:**
   - Flat topology (policies targeting few gateways)
   - Deep topology (policies → routes → gateways with many hops)
   - Wide topology (policies targeting many routes)

3. **Dependency resource count:**
   - Minimal dependent resources (new cluster)
   - Many dependent resources (mature cluster with AuthConfigs, WasmPlugins, etc.)

---

## Summary

### Six Critical Control Plane Scenarios

| Scenario | Primary Metric | Key SLO | Why Critical |
|----------|---------------|---------|--------------|
| **1. Policy Enforcement Latency** | End-to-End Enforcement Time (creation) | < 5s (p95) | User-facing: policies must work quickly after creation |
| **2. Operator Restart** | Peak Memory with 2000 resources | < 1.5GB | Prevents OOM during startup |
| **3. Reconciliation Under Load** | Reconciliation Time Scaling | O(N) with slope < 5ms/policy | Ensures system scales linearly, not quadratically |
| **4. Policy Update/Deletion** | Update/Deletion Latency | < 5s (p95) | Operational workflows: updates and cleanup must be fast |
| **5. Extension Overhead** | Extension Reconciliation Overhead | < 1s | Extensions should not significantly slow reconciliation |
| **6. Topology Rebuild** | Topology Rebuild Time | O(N) with slope < 1ms/node | Foundation of all reconciliation - must be efficient |

### Architectural Reality: O(N) is Expected

**The policy-machinery controller architecture is inherently O(N):**
- Every event triggers full topology rebuild (all policies, routes, gateways)
- Effective policy computation iterates over all policies for each policy type
- Status updates iterate over all policies to check enforcement status
- WasmPlugin/EnvoyExtensionPolicy configuration aggregates rules from all relevant policies

**This means:**
- O(1) (constant time) reconciliation is **architecturally impossible**
- The goal is **O(N) with low slope** (linear scaling with small constant factor)
- We must detect and prevent **O(N²) or worse** behavior (quadratic/exponential scaling)

**Future optimization potential:**
- Incremental topology updates (delta-based instead of full rebuild)
- Caching effective policy computations
- Selective reconciliation (only re-process affected policies)

### Metrics Philosophy

**We measure operational characteristics, not just resources:**
- "How long until policy is enforced?" (operational)
- "Does reconciliation scale linearly or quadratically?" (scalability - architectural constraint)
- "Can operator restart without OOM?" (stability)
- "What is the constant factor in O(N) scaling?" (efficiency within architectural constraints)

**Rather than:**
- "CPU usage" (trivial, not actionable)
- "Memory usage" (without context, not meaningful)
- "O(1) reconciliation time" (architecturally impossible with current design)

### SLO Rationale

**SLOs are based on:**
1. **User expectations:** Policies should work within seconds of creation
2. **Production stability:** Prevent OOM and crash loop conditions during normal operations
3. **Scalability requirements:** System must handle 1000+ policies
4. **Operational requirements:** Fast recovery after restarts

**SLOs are derived from:**
- User workflows and pain points
- Production incidents and failure modes
- Realistic usage patterns

### Next Steps

**With these scenarios defined:**
1. Agreement on scenarios and SLOs (this document)
2. Design measurement methodology (how to collect these metrics)
3. Design test infrastructure (cluster topology, tooling)
4. Implement tests
5. Establish baselines
6. Monitor and alert on SLO violations

**Implementation comes after scenario/metric/SLO agreement.**

---

## Existing Infrastructure

The Kuadrant operator already includes relevant monitoring infrastructure that can be leveraged:

### Resource Watch Scope

**Resources watched by kuadrant-operator** (verified from `state_of_the_world.go`):
- Kuadrant CR
- Policies: AuthPolicy, RateLimitPolicy, DNSPolicy, TLSPolicy, TokenRateLimitPolicy
- Gateway API: Gateways, HTTPRoutes, GatewayClasses
- ConfigMaps (filtered by label)
- Deployments (filtered by label)
- Provider-specific resources: WasmPlugin, EnvoyFilter (Istio), EnvoyExtensionPolicy, EnvoyPatchPolicy (EnvoyGateway), AuthConfig (Authorino)

**Resources NOT watched by kuadrant-operator:**
- Secrets (consumed by dependency operators like cert-manager and Authorino, not cached by kuadrant-operator)

Memory benchmarks should focus on resources actually watched and cached by the operator.

### Metrics Instrumentation

**Policy metrics** ([internal/controller/policy_metrics.go](internal/controller/policy_metrics.go)):
- `kuadrant_policies_total{kind}` - Total number of policies by kind
- `kuadrant_policies_enforced{kind,status}` - Enforcement status tracking
- Automatically discovers all policy types in the topology
- Currently tracks: AuthPolicy, RateLimitPolicy, DNSPolicy, TLSPolicy, TokenRateLimitPolicy

**Controller-runtime metrics** (may not all be available):

⚠️ **IMPORTANT:** The policy-machinery controller does not use traditional controller-runtime workqueues. It processes events in batches through a single workflow. Many standard controller-runtime metrics may not be emitted or may have different semantics than expected.

**Metrics to verify empirically before relying on:**
- `controller_runtime_reconcile_total` - Reconciliation count (likely available)
- `controller_runtime_reconcile_errors_total` - Error count (likely available)
- `workqueue_work_duration_seconds_bucket` - **May not be emitted** (policy-machinery does not use workqueues)
- `workqueue_depth` - **May not be emitted**
- `workqueue_adds_total` - **May not be emitted**
- `workqueue_retries_total` - **May not be emitted**
- `workqueue_queue_duration_seconds_bucket` - **May not be emitted**

**Recommended metric source: OpenTelemetry tracing** (already instrumented in `state_of_the_world.go`)

### Monitoring Dashboards

**Grafana dashboards** ([examples/dashboards/](examples/dashboards/)):
- `controller-runtime-metrics.json` - Reconciliation and workqueue metrics
- Additional dashboards in [config/observability/openshift/grafana/](config/observability/openshift/grafana/)

### Test Infrastructure

**Integration tests** ([tests/](tests/)):
- `tests/commons.go` - Common test utilities including enforcement validation:
  - `RLPIsEnforced()` - Checks RateLimitPolicy enforcement status
  - `IsAuthPolicyEnforced()` - Checks AuthPolicy enforcement status
  - `TokenRateLimitPolicyIsEnforced()` - Checks TokenRateLimitPolicy enforcement status
- Test environments: bare k8s, gatewayapi, istio, envoygateway
- `Eventually()` patterns already validate policy enforcement timing

### Status Tracking

**Policy conditions** ([internal/kuadrant/conditions.go](internal/kuadrant/conditions.go)):
- `PolicyConditionEnforced` - Standard enforcement condition across all policies
- All policies use `metav1.Condition` with `lastTransitionTime` for T0/T1 measurement
- Conditions include: Enforced, Accepted, with standard reasons

**Data plane readiness checks** ([internal/controller/auth_policy_status_updater.go](internal/controller/auth_policy_status_updater.go)):
- Enforced condition includes multiple component status checks:
  - **Authorino readiness**: Operator checks Authorino is running and ready (lines 260-265)
  - **AuthConfig readiness**: Checks per-HTTPRouteRule AuthConfig ready status (lines 270-277)
  - **EnvoyPatchPolicy status**: Checks `PolicyConditionProgrammed` (lines 300-303)
  - **EnvoyExtensionPolicy status (EnvoyGateway)**: Checks `PolicyConditionAccepted` via Gateway API ancestor status (lines 305-308)
  - **WasmPlugin resource (Istio)**: Verifies resource existence, but Istio does not populate status (lines 292-296)
- This means `status.conditions[type=Enforced,status=True]` represents a **composite signal** spanning control plane (Authorino) and data plane (EnvoyExtensionPolicy/WasmPlugin) readiness
- **EnvoyGateway**: T1 (Enforced=True) includes full data plane acknowledgment - T2 measurement not needed
- **Istio**: T1 only confirms WasmPlugin created - may need T2 verification to confirm wasm-shim actually loaded the configuration

**RateLimitPolicy status** ([internal/controller/ratelimit_policy_status_updater.go](internal/controller/ratelimit_policy_status_updater.go)):
- Enforced condition checks Limitador readiness (lines 248-259) AND gateway resource status (WasmPlugin/EnvoyExtensionPolicy)
- Similar composite signal: control plane (Limitador) + data plane (gateway resources)

This existing infrastructure provides a foundation for implementing the performance scenarios with minimal additional instrumentation. The key finding is that `Enforced=True` already represents multi-component readiness, not just policy acceptance.
