# Performance Benchmarking and Regression Detection — Design Doc

## Context

Users report slow reconciliation times and unresponsiveness when creating many resources. The operator currently has no systematic way to detect performance regressions before they ship, and limited visibility into where time is spent during reconciliation. The existing metrics are operational health gauges (policy counts, readiness) — none track timing of internal operations.

The goal is a two-layer system:
- **Layer 1 (Eng):** Go microbenchmarks in the operator repo, gating PRs via CI
- **Layer 2 (QE):** Existing kube-burner scale tests in `Kuadrant/testsuite`, run per release, with new operator metrics providing granular visibility. QE owns results, dashboards, and regression reporting.

This doc covers eng's deliverables: new metrics, benchmark functions, and the CI gate.

---

## Layer 1: Go Benchmarks as CI Gate

### New Makefile Target

Add `test-bench` target alongside existing `test-unit`:

```makefile
.PHONY: test-bench
test-bench:
	go test $(UNIT_DIRS) -bench=. -benchmem -run='^$$' -count=6 -timeout 30m -tags unit | tee benchmark-results.txt
```

- `-run='^$$'` skips unit tests, runs only `Benchmark*` functions
- `-count=6` gives `benchstat` enough samples for statistical significance
- `-benchmem` tracks allocations (critical for GC pressure at scale)
- Uses same `UNIT_DIRS` (`./pkg/... ./api/... ./internal/...`) and `-tags unit` as existing tests

### Benchmark Functions to Write

Based on the identified hot paths, 5 benchmark functions ordered by expected impact:

**1. `BenchmarkEffectiveAuthPolicyCalculation`** — `internal/controller/effective_auth_policies_reconciler_test.go`
- The `Paths()` call in the nested `gatewayClasses × routeRules` loop is O(G*R*P)
- Sub-benchmarks: `b.Run("1gc-10routes")`, `b.Run("1gc-100routes")`, `b.Run("1gc-300routes")`, `b.Run("3gc-100routes")`
- Build a topology in-memory with the specified number of routes and gateway classes, then benchmark the `Reconcile()` method
- Tests the same code path as `BenchmarkEffectiveRateLimitPolicyCalculation` (they share the same loop structure), so one benchmark covers both

**2. `BenchmarkTopologyToDot`** — `internal/controller/topology_reconciler_test.go`
- `ToDot()` is called on every reconciliation cycle (topology_reconciler.go:44)
- Full DAG traversal + string serialization
- Sub-benchmarks by topology size: 10, 100, 300, 1000 objects
- Construct topology with N routes attached to a gateway, call `ToDot()`

**3. `BenchmarkAuthConfigBuilding`** — `internal/controller/authconfigs_reconciler_test.go`
- The 1-policy-to-N-AuthConfigs fan-out
- Benchmark `buildDesiredAuthConfig()` and `reflect.DeepEqual()` comparison
- Sub-benchmarks: 1, 10, 100, 300 route rules (each produces an AuthConfig)

**4. `BenchmarkLimitadorLimitsBuilding`** — `internal/controller/limitador_limits_reconciler_test.go`
- `buildLimitadorLimits()` merges all effective policies into a single resource
- Less expensive than AuthConfig (no fan-out), but `processPolicyRules()` iterates all rules
- Sub-benchmarks by policy count: 10, 100, 300

**5. `BenchmarkPathsCalculation`** — `internal/policymachinery/` (or wherever `Paths()` is accessible)
- Direct benchmark of the `targetables.Paths(gatewayClass, routeRule)` call
- This is the single most expensive operation called from both effective policy reconcilers
- Sub-benchmarks by topology depth and breadth

### GitHub Actions Workflow

New workflow `.github/workflows/benchmark.yaml`:

```yaml
name: Benchmark
on:
  pull_request:
    branches: [main]
    paths:
      - 'internal/**'
      - 'pkg/**'
      - 'api/**'
      - 'go.mod'
      - 'go.sum'

jobs:
  benchmark:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod

      # Run benchmarks on PR branch
      - name: Run benchmarks (PR)
        run: make test-bench
      - name: Save PR results
        run: mv benchmark-results.txt pr-bench.txt

      # Run benchmarks on base branch
      - uses: actions/checkout@v4
        with:
          ref: ${{ github.base_ref }}
          clean: false
          path: base
      - name: Run benchmarks (base)
        run: cd base && make test-bench
      - name: Save base results
        run: mv base/benchmark-results.txt base-bench.txt

      # Compare with benchstat
      - name: Install benchstat
        run: go install golang.org/x/perf/cmd/benchstat@latest

      - name: Compare results
        id: benchstat
        run: |
          echo '## Benchmark Results' >> benchmark-report.md
          echo '```' >> benchmark-report.md
          benchstat base-bench.txt pr-bench.txt >> benchmark-report.md 2>&1 || true
          echo '```' >> benchmark-report.md

          # Check for significant regressions (>10% slower)
          if benchstat -filter '.unit/sec' base-bench.txt pr-bench.txt | grep -q '+[1-9][0-9]\+\.\|+[1-9][0-9]*%'; then
            echo "regression=true" >> $GITHUB_OUTPUT
          fi

      - name: Post PR comment
        uses: marocchino/sticky-pull-request-comment@v2
        with:
          path: benchmark-report.md

      - name: Upload artifacts
        uses: actions/upload-artifact@v4
        with:
          name: benchmark-results
          path: |
            pr-bench.txt
            base-bench.txt
            benchmark-report.md
```

**Key design decisions:**
- **Inform, don't gate (initially).** Post a PR comment with the benchstat diff but don't fail the build. Once baselines stabilize and noise is understood, add a failure condition.
- **Only runs when relevant code changes** — path filter on `internal/`, `pkg/`, `api/`, `go.mod`
- **Results as artifacts** — stored per-run, downloadable for deeper analysis
- **Sticky PR comment** — updated on each push, not spamming multiple comments
- **`-count=6`** gives benchstat enough data points; CI runners are noisy so more samples help

### Adding to Required Checks

Once the workflow is stable and baselines are established (after ~2 weeks of data), add `benchmark` to the `required-checks` job in `test.yaml` to make it a merge gate. Start with a regression threshold of 20% to avoid false positives on noisy CI runners, then tighten.

---

## Layer 2: New Operator Metrics for QE Scale Tests

### New Prometheus Histograms

Add to a new file `internal/metrics/reconciliation.go`:

| Metric | Type | Labels | What it measures |
|--------|------|--------|-----------------|
| `kuadrant_reconciliation_duration_seconds` | Histogram | `workflow` (init, dns, tls, data_plane, observability, finalize) | End-to-end duration of each top-level workflow phase |
| `kuadrant_effective_policy_duration_seconds` | Histogram | `policy_type` (auth, ratelimit, tokenratelimit) | Time to calculate effective policies (the `Paths()` loop) |
| `kuadrant_topology_rebuild_duration_seconds` | Histogram | — | Time for `ToDot()` + ConfigMap write |
| `kuadrant_authconfig_generation_duration_seconds` | Histogram | — | Time for full AuthConfig reconciliation (all paths) |
| `kuadrant_limitador_limits_generation_duration_seconds` | Histogram | — | Time for Limitador limits build + update |

Plus two gauges for topology size tracking:

| Metric | Type | Labels | What it measures |
|--------|------|--------|-----------------|
| `kuadrant_topology_objects_total` | Gauge | `kind` (Gateway, HTTPRoute, AuthPolicy, etc.) | DAG size — correlates with reconciliation cost |
| `kuadrant_authconfigs_generated_total` | Gauge | — | Number of AuthConfigs generated per cycle |

### Where to Instrument

**`internal/controller/state_of_the_world.go`** — Wrap each workflow phase in timing:
- Lines ~718-750 in `Reconciler()`: wrap `initWorkflow`, `dataPlanePoliciesWorkflow`, `dnsWorkflow`, `tlsWorkflow`, `finalizeWorkflow` with `prometheus.NewTimer()`

**`internal/controller/topology_reconciler.go`** — Wrap `ToDot()` call:
- Line 44: timer around `topology.ToDot()` through ConfigMap update

**`internal/controller/effective_auth_policies_reconciler.go`** — Wrap the full `Reconcile()`:
- Lines 53-105: timer around the gateway class × route rule loop

**`internal/controller/effective_ratelimit_policies_reconciler.go`** — Same pattern

**`internal/controller/authconfigs_reconciler.go`** — Wrap full `Reconcile()`:
- Lines 53-104: timer around the AuthConfig generation loop

**`internal/controller/limitador_limits_reconciler.go`** — Wrap `buildLimitadorLimits()`:
- Line 60: timer around the build + comparison

### pprof Endpoint — Existing Work (Mike Nairn)

Mike already has a coordinated set of draft PRs adding `--pprof-bind-address` via controller-runtime's `PprofBindAddress` option across all Kuadrant Go components. These use the proper controller-runtime integration (not a manual `http.ListenAndServe`), enabled by default on `:8082`, matching the pattern already shipped in dns-operator.

| Repo | PR | Status |
|------|-----|--------|
| **kuadrant-operator** | [#1979](https://github.com/Kuadrant/kuadrant-operator/pull/1979) | Draft — includes profiling guide at `doc/observability/profiling.md` |
| **authorino** | [#612](https://github.com/Kuadrant/authorino/pull/612) | Draft — adds flag to auth server + webhook server |
| **authorino-operator** | [#307](https://github.com/Kuadrant/authorino-operator/pull/307) | Draft — adds flag to operator |
| **limitador-operator** | [#249](https://github.com/Kuadrant/limitador-operator/pull/249) | Draft — adds flag to operator |
| **dns-operator** | [#767](https://github.com/Kuadrant/dns-operator/pull/767) | Draft — docs only (pprof already enabled) |

**Dependency chain:** PR #1979 (kuadrant-operator) is blocked on the component PRs merging first, since it includes a profiling guide covering all components.

**Action:** Review and merge Mike's PRs rather than implementing pprof separately. This covers our pprof requirement completely and is the proper controller-runtime approach (flag-based, not env-var).

---

## Interface Between Eng and QE

### Eng Delivers

1. New Prometheus histograms (listed above) — merged to operator
2. Review and merge Mike's pprof PRs ([#1979](https://github.com/Kuadrant/kuadrant-operator/pull/1979) + dependencies) — pprof across all components via controller-runtime `PprofBindAddress`
3. Documentation of new metrics (names, labels, what they measure)

### QE Delivers

1. Update `testsuite/scale_test/metrics.yaml` to scrape the new histograms
2. Add new panels to `testsuite/scale_test/dashboard.json` (Grafana)
3. Update `testsuite/scale_test/compare.sh` to include new metrics in comparison reports
4. Run scale tests per release and report regressions to eng
5. Wire scale tests into release workflow (their existing Tekton pipelines or manual process)

### Handoff

Eng provides a list of metric names and their semantics. QE adds them to their existing kube-burner `metrics.yaml` scrape config and Grafana dashboards. No code sharing needed — the interface is Prometheus metrics over the existing `/metrics` endpoint.

---

## Implementation Order

1. **Review and merge Mike's pprof PRs** — unblock [#1979](https://github.com/Kuadrant/kuadrant-operator/pull/1979) by merging dependencies first: [authorino#612](https://github.com/Kuadrant/authorino/pull/612), [authorino-operator#307](https://github.com/Kuadrant/authorino-operator/pull/307), [limitador-operator#249](https://github.com/Kuadrant/limitador-operator/pull/249), [dns-operator#767](https://github.com/Kuadrant/dns-operator/pull/767)
2. **New metrics file + histogram registration** — `internal/metrics/reconciliation.go`
3. **Instrument the 6 hot paths** — add `prometheus.NewTimer()` calls in reconcilers
4. **Add topology size gauges** — in topology reconciler
5. **Write 5 benchmark functions** — in corresponding `*_test.go` files
6. **Makefile target** — `test-bench`
7. **GitHub Actions workflow** — `.github/workflows/benchmark.yaml`
8. **Coordinate with QE** — share metric names, they update their scrape config

Step 1 (pprof) is independent. Steps 2-4 (metrics) and 5-7 (benchmarks) are independent of each other and can be done in parallel as separate PRs.

---

## Verification

**Metrics:**
- `make local-env-setup && make run` — start operator locally
- `curl localhost:8080/metrics | grep kuadrant_reconciliation` — verify new histograms appear
- Create a few policies — verify histograms populate with values
- Run `make test-unit` — ensure no regressions

**Benchmarks:**
- `make test-bench` — should produce output like:
  ```
  BenchmarkEffectiveAuthPolicyCalculation/1gc-300routes-8    50    23456789 ns/op    1234567 B/op    12345 allocs/op
  ```
- Run twice, pipe to two files, run `benchstat file1.txt file2.txt` — should show no significant difference (validates reproducibility)

**CI:**
- Open a test PR that touches `internal/` — verify the benchmark workflow triggers
- Check the PR comment for the benchstat comparison table

**QE handoff:**
- Share the metric list with QE team
- After QE updates their `metrics.yaml`, run a scale test on a local Kind cluster and verify new metrics appear in their Elasticsearch index
