# Kuadrant Operator Scale Testing Proposal

## Problem Statement

Reports indicate the Kuadrant operator uses excessive resources and takes too long to reconcile at scale - specifically with "300 HTTPRoutes, 300 AuthPolicies, 300 RateLimitPolicies, and 3000 API-key Secrets." There are also concerns about policy attachment patterns causing combinatorial explosion of generated configs.

This document covers three areas:
1. **Tooling** - what to use for scale testing and why
2. **Measurement** - what metrics exist today and what's missing
3. **Test scenarios** - worst-case scenarios derived from codebase analysis

## Environment

OpenShift cluster with a production-like setup (Istio or Envoy Gateway, Authorino, Limitador, cert-manager).

---

## 1. Tooling

### Alternatives considered

| Tool | Fit | Verdict |
|------|-----|---------|
| **kube-burner** | Scale testing of operators on K8s/OpenShift | **Recommended** |
| Custom scripts (kubectl + bash) | Quick smoke tests | Too much to build for proper scale testing |
| ClusterLoader2 | K8s control plane scalability | Wrong scope - tests API server, not operators |
| envtest / e2e-framework | Functional testing | No realistic scale - minimal API server, no webhooks |
| k6 / Gatling | HTTP endpoint load testing | Wrong layer - tests HTTP, not operator reconciliation |
| KWOK | Simulating nodes at scale | Irrelevant - Kuadrant policies attach to Gateway API resources, not nodes |

### Why kube-burner

The batch-creation part is trivial - scripts can do that. Where kube-burner earns its keep:

**Reconciliation latency measurement.** kube-burner's `customStatusPaths` feature timestamps resource creation, polls your CRD's `.status.conditions` until they flip to Ready, and computes P50/P95/P99 automatically. Building that in bash means writing a polling loop per CRD type, capturing timestamps, handling partial failures, and computing percentiles yourself.

**Rate-controlled creation via client-go.** kubectl in a loop pollutes timing with client-side overhead (retries, serialization, auth). kube-burner uses client-go directly, so you measure actual API server + operator latency.

**Prometheus scraping during the run.** Correlates operator CPU/memory with test phases automatically. Scripts need a separate PromQL query and manual timestamp correlation.

**Threshold alerting.** Can fail a CI run when P99 reconciliation exceeds a threshold. Free regression detection between runs.

**OpenShift native.** CNCF Sandbox project maintained by Red Hat's perf team. First-class OpenShift support. `kube-burner-ocp` is the OpenShift-specific wrapper with pre-built workloads. QE already uses it.

**Existing CRD scale testing patterns.** The KubeVirt CRD scale testing workload is almost exactly the pattern we need - create custom resources at scale, wait for operator-managed status conditions, measure latency.

### What kube-burner does NOT do

- Does not profile Go code or tell you *why* reconciliation is slow - you still need pprof and operator metrics for that
- No pre-built Kuadrant workload - we write the config and object templates ourselves (~1-2 days of work)
- No built-in GitHub Action, though CI integration is trivial (download binary, run, check exit code)

### Setup effort

1. Install kube-burner binary (one command)
2. Write object templates for HTTPRoute, AuthPolicy, RateLimitPolicy, Secrets (standard K8s YAML with `{{.Iteration}}` Go-template variables)
3. Write a config YAML with jobs, QPS/burst settings, and `customStatusPaths` wait conditions for each policy type's status conditions
4. Configure Prometheus metric endpoints to scrape operator CPU/memory during the test
5. Run with local indexer for initial testing, add Elasticsearch/OpenSearch later for trend analysis

Example config structure:

```yaml
jobs:
  - name: kuadrant-policy-density
    jobIterations: 300
    qps: 10
    burst: 20
    namespacedIterations: true
    namespace: scale-test
    waitWhenFinished: true
    objects:
      - objectTemplate: httproute.yml
        replicas: 1
      - objectTemplate: ratelimitpolicy.yml
        replicas: 1
        waitOptions:
          customStatusPaths:
            - key: '(.status.conditions.[] | select(.type == "Accepted")).status'
              value: "True"
      - objectTemplate: secrets.yml
        replicas: 10
```

### References

- [kube-burner GitHub](https://github.com/kube-burner/kube-burner)
- [kube-burner Configuration Reference](https://kube-burner.github.io/kube-burner/latest/reference/configuration/)
- [kube-burner Measurements](https://kube-burner.github.io/kube-burner/latest/measurements/)
- [Red Hat Developer: Test Kubernetes performance and scale with kube-burner](https://developers.redhat.com/articles/2024/03/04/test-kubernetes-performance-and-scale-kube-burner)
- [KubeVirt CRD Support for kube-burner](https://www.redhat.com/en/blog/introducing-kubevirts-crd-support-for-kube-burner-to-benchmark-kubernetes-and-openshift-creation-of-vms)

---

## 2. What to Measure

### Already available (no code changes needed)

**Controller-runtime metrics (automatic):**
- `workqueue_work_duration_seconds` - reconciliation duration per controller
- `workqueue_depth_total` - items currently in the work queue
- `workqueue_adds_total` - total items added to work queues
- `workqueue_queue_duration_seconds` - time items wait in queue before processing
- `reconcile_total` / `reconcile_errors_total` - reconciliation counts and errors

**Kuadrant custom metrics:**
- `kuadrant_policies_total` (GaugeVec) - count of policies by kind (AuthPolicy, RateLimitPolicy, DNSPolicy, TLSPolicy, TokenRateLimitPolicy)
- `kuadrant_policies_enforced` (GaugeVec) - policies by kind and enforcement status
- `kuadrant_dependency_detected` (GaugeVec) - dependencies detected at startup
- `kuadrant_controller_registered` (GaugeVec) - active controllers
- `kuadrant_exists` / `kuadrant_ready` / `kuadrant_component_ready` - operator health
- `kuadrant_dns_policy_ready` (GaugeVec) - DNS policy readiness

**OpenTelemetry tracing (already instrumented):**
- Spans on every reconciler: `policy.AuthPolicy.validate`, `policy.RateLimitPolicy.validate`, `policy.DNSPolicy.effective`, `wasm.MergeAndVerifyActions`, etc.
- Can export to Jaeger/Tempo during scale tests for detailed reconciler-level timing
- Configured via `OTEL_EXPORTER_OTLP_ENDPOINT` environment variable

**Container-level (from cAdvisor/Prometheus):**
- CPU and memory usage of the operator pod
- Container restart counts

**Metrics endpoint:** `:8080/metrics` (configurable via `--metrics-bind-address` flag)

### Missing - would benefit from code changes

| Metric | Why it matters |
|--------|---------------|
| `kuadrant_topology_objects_total` (GaugeVec, by kind) | How big is the in-memory DAG? Correlates with memory usage. |
| `kuadrant_effective_policy_calculation_duration_seconds` (Histogram) | The `Paths()` traversal is acknowledged as expensive in code comments but has no metric. |
| `kuadrant_authconfig_reconcile_duration_seconds` (Histogram) | Per-AuthConfig creation time to identify individual bottlenecks. |
| `kuadrant_topology_rebuild_duration_seconds` (Histogram) | Full DAG rebuild + `ToDot()` serialization happens on every reconciliation. |
| `kuadrant_reconciliation_total_duration_seconds` (Histogram) | End-to-end time for the full workflow (all 10 sub-workflows). |

### Recommended approach

Use OpenTelemetry traces for the initial round of testing - every reconciler already has spans, so you get per-reconciler timing without adding metrics. Add custom Prometheus metrics for the hot paths once you've identified which ones matter most from the trace data.

---

## 3. Codebase Analysis: Why Scale Is a Problem

### The core issue: no batching, no debouncing

The operator uses a workflow-based reconciliation pattern. **Any single resource change (one HTTPRoute, one policy, one Secret reference) triggers the entire reconciliation workflow**:

1. TopologyReconciler - rebuilds DAG, serializes to `ToDot()` (full traversal)
2. DNS workflow - all DNS policy reconcilers
3. TLS workflow - all TLS policy reconcilers
4. **Data plane policies workflow** (the big one):
   - Validators for auth, ratelimit, token-ratelimit policies
   - Effective policy calculation for **all** auth, ratelimit, and token-ratelimit policies
   - AuthConfig creation/update for **every** effective auth policy
   - Limitador limits merge for **all** effective rate limit policies
   - Gateway provider integration (Istio or EnvoyGateway extension reconcilers)
   - Status updaters for all policies
5. Observability workflow
6. Developer Portal workflow
7. Limitador/Authorino operator workflows
8. Console plugin workflow
9. Finalize - policy metrics, gateway/route discoverability

**There is no per-resource or incremental reconciliation.** Creating the 300th HTTPRoute recalculates effective policies for all 300 routes.

### Effective policy calculation complexity

The effective policy reconcilers (`effective_auth_policies_reconciler.go`, `effective_ratelimit_policies_reconciler.go`) calculate effective policies by iterating:

```
for each gatewayClass:
    for each routeRule (HTTP + gRPC):
        paths = targetables.Paths(gatewayClass, routeRule)  // expensive!
        for each path:
            compute effective policy (merge gateway-level + route-level)
```

**Complexity per reconciliation: `O(gatewayClasses × routeRules × pathCalculationCost)`**

The code itself acknowledges this at `effective_ratelimit_policies_reconciler.go:78`:
```go
paths := targetables.Paths(gatewayClass, routeRule) // this may be expensive in clusters with many gateway classes
```

### AuthConfig fan-out

When a policy targets a Gateway with N attached HTTPRoutes, the operator generates **N separate AuthConfig resources** (one per route rule). This means:

- 1 Gateway-level AuthPolicy + 300 routes = 300 AuthConfigs
- 1 Gateway-level AuthPolicy + 300 route-level AuthPolicies + 300 routes = 300 AuthConfigs (with merge)
- Each AuthConfig creation is an individual API server write

Limitador works differently - it generates a **single Limitador resource** with all limits merged, so it doesn't have the same fan-out problem.

### What the operator watches

29 separate resource watchers are configured in `state_of_the_world.go`:
- Gateway API resources: Gateway, GatewayClass, HTTPRoute, GRPCRoute
- All 5 policy types
- Infrastructure: Authorino, Limitador, EnvoyFilter, WasmPlugin, EnvoyPatchPolicy, EnvoyExtensionPolicy, PeerAuthentication
- Deployments (Limitador, DeveloperPortal) with label filters
- ConfigMap (topology only)

**Important: Secrets are NOT watched.** Creating 3000 API-key Secrets does not directly trigger reconciliations. They only matter when referenced by AuthConfig objects that the operator creates.

### Event matching

The data plane policies workflow triggers on changes to **any** of: Kuadrant, GatewayClass, Gateway, HTTPRoute, GRPCRoute, RateLimitPolicy, TokenRateLimitPolicy, AuthPolicy, AuthConfig, EnvoyFilter, WasmPlugin, EnvoyPatchPolicy, EnvoyExtensionPolicy. A change to any one of these runs the full workflow.

---

## 4. Test Scenarios - Worst Cases

Ordered from bad to worst, based on the codebase analysis above.

### Scenario 1: Baseline density

**Setup:** 300 HTTPRoutes, each with its own AuthPolicy + RateLimitPolicy + 10 Secrets. Each policy targets its own route (no gateway-level policies).

**What happens internally:**
- 300 AuthConfigs generated (one per route rule)
- 1 Limitador resource with 300 merged limits
- Each new resource triggers full workflow but effective policy count grows linearly

**Measures:** Raw reconciliation time at density. Steady-state CPU and memory. This is the "best case at scale" - linear growth, no fan-out.

### Scenario 2: Gateway fan-out

**Setup:** 1 Gateway, 300 HTTPRoutes attached. 1 AuthPolicy + 1 RateLimitPolicy targeting the Gateway (not the routes).

**What happens internally:**
- The 2 gateway-level policies cascade to all 300 routes via the topology DAG
- 300 AuthConfigs generated from a single AuthPolicy
- Adding route 301 triggers full re-reconciliation of all 300 existing effective policies
- The DAG must traverse paths from the gateway class through the gateway to each route

**Measures:** Fan-out multiplication effect. Incremental cost of adding one more route when gateway-level policies exist.

### Scenario 3: Mixed hierarchy (worst for effective policy calculation)

**Setup:** 1 Gateway, 300 HTTPRoutes attached. 1 gateway-level AuthPolicy + 300 route-level AuthPolicies (each overriding/extending the gateway-level one).

**What happens internally:**
- Effective policy reconciler must compute `Paths(gatewayClass, routeRule)` for every route rule
- Gateway-level policy merges with each route-level policy (merge logic per path)
- 300 effective policies, each requiring DAG path traversal through both levels
- This is the most expensive path through the effective policy calculation code

**Measures:** The `Paths()` calculation cost when both gateway-level and route-level policies exist simultaneously.

### Scenario 4: Burst creation storm (worst for queue pressure)

**Setup:** Create 300 HTTPRoutes + 300 AuthPolicies + 300 RateLimitPolicies as fast as possible (maximum QPS, no pacing).

**What happens internally:**
- Each creation queues a full reconciliation (no debouncing)
- 900+ full reconciliation cycles queued in rapid succession
- Later reconciliations process a larger topology than earlier ones (the 500th reconciliation has ~166 routes in the DAG, the 900th has all 300)
- Work queue depth grows unboundedly

**Measures:** Queue depth growth rate. Does the operator fall behind? Does it OOM? What's the latency of the last policy becoming Ready vs the first?

### Scenario 5: Incremental growth (best for regression detection)

**Setup:** Start with 10 routes + policies. Add 10 more every 30 seconds until 300.

**What happens internally:**
- Each batch of 10 triggers reconciliation with the full current topology
- Reconciliation time should grow with each batch
- The shape of the growth curve reveals whether scaling is linear, quadratic, or worse

**Measures:** Reconciliation time as a function of total resource count. The slope of this curve is the most important metric - it tells you where the breaking point is and whether optimizations are working.

### Scenario 6: Multiple GatewayClasses (worst for path calculation)

**Setup:** 3 GatewayClasses, each with its own Gateway and 100 HTTPRoutes. Policies at both gateway and route levels.

**What happens internally:**
- Effective policy calculation iterates `for each gatewayClass × for each routeRule`
- With 3 classes × 300 route rules = 900 path calculations per reconciliation
- Code comment at `effective_ratelimit_policies_reconciler.go:78` explicitly flags this as expensive

**Measures:** Multiplicative effect of gateway classes on reconciliation time. Whether the operator handles multi-tenancy (multiple gateway classes) at scale.

### Recommended test execution order

1. **Scenario 5 first** (incremental growth) - cheapest to run, gives you the scaling curve immediately, tells you which other scenarios to prioritize
2. **Scenario 3** (mixed hierarchy) - the combinatorial worst case from the code analysis
3. **Scenario 4** (burst storm) - tests whether the operator survives real-world deployment patterns (e.g., GitOps deploying everything at once)
4. **Scenarios 1, 2, 6** as needed based on findings

---

## 5. Summary

| Decision | Recommendation | Reasoning |
|----------|---------------|-----------|
| **Cluster** | OpenShift | Production-representative, matches customer environments |
| **Tooling** | kube-burner | Rate-controlled creation, CRD wait conditions, P50/P95/P99 latency, Prometheus integration, built by Red Hat perf team |
| **Observability** | OTel traces (existing) + Prometheus (existing) | Every reconciler already has spans; add custom metrics for hot paths after initial findings |
| **First test** | Scenario 5 (incremental growth) | Reveals scaling curve shape with minimal setup |
| **Worst case** | Scenario 3 (mixed hierarchy) | Exercises the most expensive code path identified in the codebase |

### Key code locations for reference

| Component | File | Lines |
|-----------|------|-------|
| Full reconciliation workflow | `internal/controller/state_of_the_world.go` | 726-757 |
| Resource watchers (29 total) | `internal/controller/state_of_the_world.go` | 88-183 |
| Data plane event matchers | `internal/controller/data_plane_policies_workflow.go` | 47-62 |
| Effective auth policy calculation | `internal/controller/effective_auth_policies_reconciler.go` | 53-105 |
| Effective ratelimit policy calculation | `internal/controller/effective_ratelimit_policies_reconciler.go` | 53-105 |
| AuthConfig generation (fan-out) | `internal/controller/authconfigs_reconciler.go` | 76-104 |
| Limitador limits merge | `internal/controller/limitador_limits_reconciler.go` | 105-141 |
| "May be expensive" comment | `internal/controller/effective_ratelimit_policies_reconciler.go` | 78 |
| Topology rebuild + ToDot() | `internal/controller/topology_reconciler.go` | 34-82 |
| Metrics registration | `internal/metrics/operator_health.go` | - |
| Policy metrics | `internal/controller/policy_metrics.go` | - |
| OTel configuration | `internal/otel/config.go` | - |
