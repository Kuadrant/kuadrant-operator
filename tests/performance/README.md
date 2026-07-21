# Performance Tests

This directory contains tests for measuring and preventing performance regressions in the kuadrant-operator reconciliation paths.

There are two types of tests, each serving a different purpose.

## Unit Benchmarks

Go benchmark tests in `internal/controller/reconciliation_bench_test.go` and `internal/wasm/types_bench_test.go`. These construct topologies in-memory and measure specific code paths without needing a cluster.

### What they test

- `BenchmarkCalculateEffectiveAuthPolicies` — effective policy calculation at varying route/listener/gateway scale
- `BenchmarkEffectiveRateLimitPolicies` / `BenchmarkEffectiveTokenRateLimitPolicies` — same for other policy types
- `BenchmarkEffectiveAuthPoliciesListenerFanout` — listener scaling (1–64 listeners)
- `BenchmarkEffectiveAuthPoliciesMultiGateway` — multi-gateway scaling
- `BenchmarkGetKuadrantFromTopology` — Kuadrant CR lookup cost
- `BenchmarkMultiReconcilerCycle` — all three policy reconcilers running together
- `BenchmarkFullReconciliationCycle` — combined topology build + lookups + effective policies
- `TestEqualToNonDeterminism` — reproduces the WasmPlugin comparison bug (Issue #1934)

### Running

```bash
# Run all benchmarks
go test -tags=unit -bench=. -benchmem -timeout=600s -run='^$' ./internal/controller/

# Run a specific benchmark
go test -tags=unit -bench='BenchmarkCalculateEffectiveAuthPolicies' -benchmem -run='^$' ./internal/controller/

# Run with multiple iterations for benchstat comparison
go test -tags=unit -bench=. -benchmem -count=6 -timeout=1800s -run='^$' ./internal/controller/ > results.txt
```

### Profiling

```bash
# Capture CPU and memory profiles
go test -tags=unit -bench='BenchmarkCalculateEffectiveAuthPolicies/gw=1_listen=1_routes=300' \
  -cpuprofile=cpu.prof -memprofile=mem.prof -run='^$' ./internal/controller/

# View as interactive flame graph
go tool pprof -http=:8080 cpu.prof

# Terminal-only top functions
go tool pprof -top -cum cpu.prof
```

### Comparing before/after

```bash
# Before your change
go test -tags=unit -bench=. -benchmem -count=6 -run='^$' ./internal/controller/ > before.txt

# After your change
go test -tags=unit -bench=. -benchmem -count=6 -run='^$' ./internal/controller/ > after.txt

# Compare with statistical analysis
benchstat before.txt after.txt

# Compare pprof profiles (red = slower, green = faster)
go tool pprof -http=:8080 -diff_base=before-cpu.prof after-cpu.prof
```

### When to use

- Developing and validating performance fixes
- Identifying which code paths are expensive (via pprof)
- CI regression detection
- Fast iteration — runs in seconds, no cluster needed

## Cluster Performance Tests

Ginkgo tests in `tests/performance/` that create real resources on a Kubernetes cluster and measure end-to-end reconciliation time. The operator runs in-process via `SetupKuadrantOperatorForTest`.

### What they test

- Creates a Gateway, then N HTTPRoutes each with an AuthPolicy
- Measures wall-clock time until all policies reach `Enforced: True`
- Captures the full operator lifecycle: event handling, topology rebuild, effective policy calculation, AuthConfig creation, Authorino reconciliation, status updates, and API server I/O

### Running

```bash
# Default scale levels (10, 50, 100, 300)
GATEWAYAPI_PROVIDER=istio \
go test -tags=performance -v -timeout=60m ./tests/performance/ -ginkgo.v

# Custom scale levels
PERF_SCALE_LEVELS=100,300,600,1000 \
GATEWAYAPI_PROVIDER=istio \
go test -tags=performance -v -timeout=120m ./tests/performance/ -ginkgo.v

# With error logs visible
PERF_LOG_ERRORS=true \
GATEWAYAPI_PROVIDER=istio \
go test -tags=performance -v -timeout=60m ./tests/performance/ -ginkgo.v
```

### Profiling

Since the operator runs in-process, Go's built-in profiling captures the reconciliation code:

```bash
GATEWAYAPI_PROVIDER=istio \
go test -tags=performance -v -timeout=60m \
  -cpuprofile=perf-cpu.prof -memprofile=perf-mem.prof \
  ./tests/performance/ -ginkgo.v

go tool pprof -http=:8080 perf-cpu.prof
```

### Configuration

| Environment variable | Description | Default |
|---|---|---|
| `GATEWAYAPI_PROVIDER` | Gateway provider (required) | — |
| `PERF_SCALE_LEVELS` | Comma-separated route counts | `10,50,100,300` |
| `PERF_LOG_ERRORS` | Show operator error logs | `false` |

### When to use

- Validating fixes end-to-end with real API server I/O
- Measuring impact across multiple reconciliation cycles
- Finding bottlenecks in downstream components (Authorino, Limitador)
- Final validation before merging performance-sensitive changes

## Which to use?

| Question | Unit benchmarks | Cluster tests |
|---|---|---|
| "Is this code path fast enough?" | ✅ | |
| "Where is the CPU time going?" | ✅ (pprof) | ✅ (pprof) |
| "Did my PR regress performance?" | ✅ (benchstat) | |
| "How long does reconciliation take at 300 routes?" | | ✅ |
| "Does this fix help in a real cluster?" | | ✅ |
| "Can I run this without a cluster?" | ✅ | |
| "Does this capture API server overhead?" | | ✅ |

Start with unit benchmarks for development, use cluster tests for validation.
