# Kuadrant Operator Integration Tests

Integration test suites for kuadrant-operator. Each suite tests a different combination of gateway provider and functionality.

## Test Suites

### bare_k8s — Bare Kubernetes (no gateway provider)
Tests core Kuadrant functionality on plain Kubernetes without any Gateway API provider installed.

```bash
make local-k8s-env-setup test-bare-k8s-integration
```

### gatewayapi — Gateway API (no provider)
Tests Gateway API CRD integration without a specific provider (Istio/Envoy Gateway).

```bash
make local-gatewayapi-env-setup test-gatewayapi-env-integration
```

### controlplane — Control Plane
Tests Kuadrant control plane components and their reconciliation logic.

```bash
make local-gatewayapi-env-setup test-controlplane-integration
```

### istio — Istio Integration
Tests Istio-specific functionality with Kuadrant policies. Supports two installation methods:

**Using istioctl (default):**
```bash
make local-istio-env-setup test-istio-env-integration
```

**Using Sail (Kubernetes operator):**
```bash
make local-istio-env-setup test-istio-env-integration ISTIO_INSTALL_SAIL=true
```

### envoygateway — Envoy Gateway Integration
Tests Envoy Gateway provider integration with Kuadrant policies.

```bash
make local-envoygateway-env-setup test-envoygateway-env-integration
```

### controllers — Controller-level Integration (internal/controller tests)
Tests the internal controller implementations across multiple gateway providers:

**Istio (istioctl):**
```bash
make local-env-setup test-integration GATEWAYAPI_PROVIDER=istio ISTIO_INSTALL_SAIL=false
```

**Istio (Sail):**
```bash
make local-env-setup test-integration GATEWAYAPI_PROVIDER=istio ISTIO_INSTALL_SAIL=true
```

**Envoy Gateway:**
```bash
make local-env-setup test-integration GATEWAYAPI_PROVIDER=envoygateway
```

## Running Tests

### Setup and Run in One Command
Each test suite can be created and run locally in a single `make` command:

```bash
# Bare K8s
make local-k8s-env-setup test-bare-k8s-integration

# Gateway API
make local-gatewayapi-env-setup test-gatewayapi-env-integration

# Control Plane
make local-gatewayapi-env-setup test-controlplane-integration

# Istio (istioctl)
make local-istio-env-setup test-istio-env-integration

# Istio (Sail)
make local-istio-env-setup test-istio-env-integration ISTIO_INSTALL_SAIL=true

# Envoy Gateway
make local-envoygateway-env-setup test-envoygateway-env-integration

# Controllers – Istio (istioctl)
make local-env-setup test-integration GATEWAYAPI_PROVIDER=istio ISTIO_INSTALL_SAIL=false

# Controllers – Istio (Sail)
make local-env-setup test-integration GATEWAYAPI_PROVIDER=istio ISTIO_INSTALL_SAIL=true

# Controllers – Envoy Gateway
make local-env-setup test-integration GATEWAYAPI_PROVIDER=envoygateway
```

### Adding Test Options
To add options like `--repeat=5`, `--flake-attempts=2`, `--focus` etc..., use `INTEGRATION_TESTS_EXTRA_ARGS`:

```bash
make local-gatewayapi-env-setup test-gatewayapi-env-integration INTEGRATION_TESTS_EXTRA_ARGS="--repeat=5 --flake-attempts=2"
make local-k8s-env-setup test-bare-k8s-integration INTEGRATION_TESTS_EXTRA_ARGS="--focus=TestSomething"
```

### Manual Setup (separate commands)
If you need to iterate on tests without recreating the cluster:

```bash
# Setup cluster once
make local-env-setup

# Run tests multiple times
make test-integration
make test-integration INTEGRATION_TESTS_EXTRA_ARGS="--focus=TestFoo"
```

### Testing Against Existing Cluster

To run tests against a cluster that already has Kuadrant installed (instead of starting an in-process operator):

```bash
export USE_EXISTING_OPERATOR=true
make test-bare-k8s-integration
make test-gatewayapi-env-integration
make test-istio-env-integration
# etc.
```

When `USE_EXISTING_OPERATOR=true`:
- Skips in-process manager startup and CRD bootstrapping
- Uses the cluster's existing Kuadrant installation
- Tests connect via kubeconfig and verify against live operators
- Useful for validating fixes without cluster churn or testing against production-like setups

**Requirement:** The target cluster must have Kuadrant and all dependencies (Authorino, Limitador, etc.) already installed and running.

## Common Targets

- `make local-<suite>-env-setup` — Create Kind cluster and install all dependencies
- `make test-<suite>-integration` — Run tests against an existing cluster

Run `make help` to see all available targets.

## Cleanup

Remove the local Kind cluster:

```bash
make local-cleanup
```

## CI/CD

The GitHub Actions workflow (`.github/workflows/test.yaml`) runs all test suites in parallel across multiple environments. See that file for the exact cluster configuration and environment variables used.
