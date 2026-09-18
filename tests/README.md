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
make local-env-setup test-istio-env-integration GATEWAYAPI_PROVIDER=istio
```

**Using Sail (Kubernetes operator):**
```bash
make local-env-setup test-istio-env-integration GATEWAYAPI_PROVIDER=istio ISTIO_INSTALL_SAIL=true
```

### envoygateway — Envoy Gateway Integration
Tests Envoy Gateway provider integration with Kuadrant policies.

```bash
make local-env-setup test-envoygateway-env-integration GATEWAYAPI_PROVIDER=envoygateway
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
make local-env-setup test-istio-env-integration GATEWAYAPI_PROVIDER=istio ISTIO_INSTALL_SAIL=false

# Istio (Sail)
make local-env-setup test-integration GATEWAYAPI_PROVIDER=istio ISTIO_INSTALL_SAIL=true

# Envoy Gateway
make local-env-setup test-envoygateway-env-integration GATEWAYAPI_PROVIDER=envoygateway
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

### Filtering Tests by Label

All test specs are labeled to allow filtering by suite or policy. Use `--label-filter` to run specific tests:

```bash
# Run all bare_k8s tests
make test-bare-k8s-integration INTEGRATION_TESTS_EXTRA_ARGS="--label-filter=bare_k8s"

# Run all Istio AuthPolicy tests
make test-istio-env-integration INTEGRATION_TESTS_EXTRA_ARGS="--label-filter=authpolicy"

# Run RateLimitPolicy tests across all suites
make test-integration INTEGRATION_TESTS_EXTRA_ARGS="--label-filter=ratelimitpolicy"

# Combine labels (tests matching both labels)
make test-istio-env-integration INTEGRATION_TESTS_EXTRA_ARGS="--label-filter='istio && authpolicy'"
```

**Available Labels:**

| Label | Purpose |
|-------|---------|
| `bare_k8s` | Tests for bare Kubernetes (no gateway provider). Tests core Kuadrant functionality without external dependencies. |
| `common` | Shared policy tests (authpolicy, ratelimitpolicy, dnspolicy, tlspolicy, discoverability). Run across all provider configurations. |
| `gatewayapi` | Tests for Gateway API integration without a specific provider. |
| `controlplane` | Tests for Kuadrant control plane components. |
| `istio` | Tests for Istio provider integration. |
| `envoygateway` | Tests for Envoy Gateway provider integration. |
| `authpolicy` | Tests for AuthPolicy controller logic and functionality. |
| `ratelimitpolicy` | Tests for RateLimitPolicy controller logic and functionality. |
| `dnspolicy` | Tests for DNSPolicy controller logic and functionality. |
| `tlspolicy` | Tests for TLSPolicy controller logic and functionality. |
| `discoverability` | Tests for policy discoverability mechanisms. |

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

**Example: Run AuthPolicy tests across multiple suites on existing cluster:**

```bash
make test-integration \
  USE_EXISTING_OPERATOR=true \
  GATEWAYAPI_PROVIDER=istio \
  INTEGRATION_TEST_PACKAGES="tests/istio/... tests/common/..." \
  INTEGRATION_TESTS_EXTRA_ARGS='--flake-attempts=1 --label-filter="authpolicy"'
```

This command:
- Reuses the existing cluster instead of creating a new one
- Targets the Istio provider
- Runs only istio and common test packages
- Filters to AuthPolicy tests only (across both packages)
- Runs with flake attempt detection enabled

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
