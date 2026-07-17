# Profiling

All Go-based Kuadrant components support runtime profiling via Go's built-in [pprof](https://pkg.go.dev/net/http/pprof) tooling. This uses controller-runtime's `PprofBindAddress` option, enabled by default on port `8084`.

Profiling is useful for diagnosing performance issues such as slow reconciliation, high CPU usage, or excessive memory allocation at scale.

## Connecting to a component

Port-forward to the component you want to profile:

```bash
# Kuadrant Operator
kubectl port-forward -n kuadrant-system deploy/kuadrant-operator-controller-manager 8084:8084

# Authorino (namespace may differ depending on where the Kuadrant CR is installed)
kubectl port-forward -n kuadrant-system deploy/authorino 8084:8084

# Authorino Operator
kubectl port-forward -n kuadrant-system deploy/authorino-operator 8084:8084

# Limitador Operator
kubectl port-forward -n kuadrant-system deploy/limitador-operator-controller-manager 8084:8084

# DNS Operator
kubectl port-forward -n kuadrant-system deploy/dns-operator-controller-manager 8084:8084
```

To profile multiple components simultaneously, use different local ports (e.g., 8085, 8086, etc.) to avoid conflicts.

> **Note:** All commands in the sections below assume a single component is being profiled on `localhost:8084`. If profiling multiple components, adjust the port number accordingly.

## Capturing profiles

### CPU profile

Captures a CPU profile for a specified duration (default 30 seconds):

```bash
go tool pprof http://localhost:8084/debug/pprof/profile?seconds=30
```

### Heap profile

Captures a snapshot of current memory allocations:

```bash
go tool pprof http://localhost:8084/debug/pprof/heap
```

### Goroutine dump

Lists all goroutines and their stack traces, useful for diagnosing stuck reconciliation:

```bash
curl http://localhost:8084/debug/pprof/goroutine?debug=2
```

## Analysing profiles

### Interactive web UI

The most useful way to view profiles — opens a browser with flame graphs, call graphs, and source annotation:

```bash
go tool pprof -http=:8080 http://localhost:8084/debug/pprof/profile?seconds=30
```

### Save and view later

```bash
# Save to disk
curl -o cpu.prof "http://localhost:8084/debug/pprof/profile?seconds=30"
curl -o heap.prof "http://localhost:8084/debug/pprof/heap"

# View saved profiles
go tool pprof -http=:8080 cpu.prof
```

### Compare two profiles

Useful for validating that a change improved performance:

```bash
go tool pprof -http=:8080 -diff_base=before.prof after.prof
```

Functions that got faster appear in green, slower in red.

### Terminal-only (no browser)

```bash
go tool pprof -top cpu.prof              # top functions by flat CPU
go tool pprof -top -cum cpu.prof         # top functions by cumulative CPU
go tool pprof -top heap.prof             # top memory allocators
```

## Configuration

Each component accepts a `--pprof-bind-address` flag:

| Component | Flag | Default |
|---|---|---|
| kuadrant-operator | `--pprof-bind-address` | `:8084` |
| authorino | `--pprof-bind-address` | `:8084` |
| authorino-operator | `--pprof-bind-address` | `:8084` |
| limitador-operator | `--pprof-bind-address` | `:8084` |
| dns-operator | `--pprof-bind-address` | `:8084` |

Set to an empty string to disable: `--pprof-bind-address=""`.
