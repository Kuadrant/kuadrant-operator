# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kuadrant is a Kubernetes operator that extends Gateway API providers (Istio, Envoy Gateway) with additional policies for TLS, DNS, authentication/authorization, and rate limiting. It leverages Gateway API Policy Attachment (GEP-713) to provide a consistent policy-driven approach to traffic management.

## Build and Test Commands

### Building
```bash
# Build operator binary
make build

# Build Docker image
make docker-build

# Build with specific version
make build VERSION=1.0.0
```

### Testing
```bash
# Run unit tests
make test-unit

# Run specific unit test
make test-unit TEST_NAME=TestLimitIndexEquals

# Run specific subtest
make test-unit TEST_NAME=TestLimitIndexEquals/empty_indexes_are_equal

# Run with verbose output
make test-unit VERBOSE=1

# Run integration tests for specific environment
make test-integration GATEWAYAPI_PROVIDER=istio
make test-integration GATEWAYAPI_PROVIDER=envoygateway

# Run bare k8s integration tests (no gateway provider)
make test-bare-k8s-integration

# Run gatewayapi integration tests (GatewayAPI CRDs only)
make test-gatewayapi-env-integration

# Run Istio-specific integration tests
make test-istio-env-integration

# Run EnvoyGateway-specific integration tests
make test-envoygateway-env-integration
```

### Linting and Code Quality
```bash
# Run linter
make run-lint

# Format code
make fmt

# Run go vet
make vet

# Run all verification checks
make verify-all

# Run comprehensive pre-commit checks
make pre-commit

# Run pre-commit with integration tests
make pre-commit INTEGRATION_TEST_ENV=istio
make pre-commit INTEGRATION_TEST_ENV=all
```

### Local Development
```bash
# Setup local Kind cluster with all dependencies and operator
make local-setup

# Setup local environment without running operator (for local development)
make local-env-setup

# Run operator locally (after local-env-setup)
make run

# Cleanup local environment
make local-cleanup

# Install CRDs
make install

# Uninstall CRDs
make uninstall

# Deploy operator to existing cluster
make deploy

# Deploy dependencies only
make deploy-dependencies
```

### Code Generation
```bash
# Generate manifests (CRDs, RBAC, webhooks)
make manifests

# Generate code (DeepCopy methods)
make generate

# Generate extension manifests
make extensions-manifests

# Update dependency manifests
make dependencies-manifests
```

## Architecture

### Policy System

Kuadrant implements a sophisticated policy attachment system based on Gateway API's Policy Attachment (GEP-713). The operator manages five core policies:

- **AuthPolicy** (`api/v1/authpolicy_types.go`): Authentication and authorization via Authorino
- **RateLimitPolicy** (`api/v1/ratelimitpolicy_types.go`): Rate limiting via Limitador
- **DNSPolicy** (`api/v1/dnspolicy_types.go`): DNS and load balancing configuration
- **TLSPolicy** (`api/v1/tlspolicy_types.go`): TLS certificate management via cert-manager
- **TokenRateLimitPolicy** (`api/v1alpha1/tokenratelimitpolicy_types.go`): Token-based rate limiting for AI/LLM workloads

Policies attach to Gateway API resources (Gateway, HTTPRoute) using `targetRef` fields and are reconciled through a workflow-based controller system.

### Extensions System

The operator supports out-of-process (OOP) extensions via gRPC over Unix domain sockets. Extensions are separate controller processes that communicate with the main operator to:

1. **Evaluate CEL expressions** with access to Kuadrant's topology (Gateway, Routes, Policies)
2. **Publish data bindings** that influence downstream policy configurations (AuthConfig, Limitador, Envoy wasm)
3. **Subscribe to cluster events** for reactive reconciliation

**Extension Architecture:**
- Extensions live in `cmd/extensions/*/` (e.g., `oidc-policy`, `plan-policy`, `telemetry-policy`)
- They connect to the operator via Unix socket (path provided as first CLI arg)
- Communication uses gRPC protocol defined in `pkg/extension/`
- Extensions use the SDK in `pkg/extension/` for controller building

**Data Flow:**
```
Extension Controller → kuadrant.Resolve(CEL) → Operator Policy Machinery
                    ↓
                kuadrant.AddDataTo(bindings) → Managed Resources (AuthConfig/Limitador/Wasm)
```

**Bindings and Domains:**
- `DomainAuth`: Bindings consumed by Authorino (e.g., dynamic metadata, claims)
- `DomainRequest`: Bindings consumed by Envoy wasm/Limitador (e.g., request labels, headers)

Extensions publish ephemeral key-value bindings that the operator uses to augment managed resources. Values can be literals (evaluated at reconcile time) or CEL programs (evaluated at request time by data plane).

### Controller Architecture

The operator uses a workflow-based reconciliation pattern rather than traditional Kubernetes controller reconcile loops:

1. **PolicyMachineryController** (`internal/controller/state_of_the_world.go`):
   - Manages the "state of the world" for all policies
   - Dynamically discovers and watches policy types
   - Coordinates reconciliation across multiple policy types
   - Uses a DAG (Directed Acyclic Graph) to represent topology relationships

2. **Workflow-based Reconcilers**:
   - Controllers define workflows with preconditions, tasks, and postconditions
   - Example: `internal/controller/data_plane_policies_workflow.go` for data plane policies
   - Workflows compose smaller reconcilers for specific resources

3. **Specialized Reconcilers**:
   - `authorino_reconciler.go`: Manages Authorino resources (AuthConfig)
   - `limitador_reconciler.go`: Manages Limitador deployments and configurations
   - `envoy_gateway_extension_reconciler.go`: Integrates with Envoy Gateway
   - `limitador_istio_integration_reconciler.go`: Integrates Limitador with Istio

### Dependency Integration

The operator dynamically detects and integrates with dependent operators:

- **Authorino Operator**: Authentication/authorization engine
- **Limitador Operator**: Rate limiting engine
- **DNS Operator**: DNS management for multi-cluster scenarios
- **Cert-Manager**: TLS certificate provisioning

Detection happens at startup via CRD availability checks. Controllers are only registered if their dependencies are present.

### API Versioning

- **v1** (`api/v1/`): Stable APIs (AuthPolicy, RateLimitPolicy, DNSPolicy, TLSPolicy)
- **v1beta1** (`api/v1beta1/`): Beta APIs (Kuadrant CRD)
- **v1alpha1** (`api/v1alpha1/`): Alpha APIs (TokenRateLimitPolicy)

Extensions define their own API versions under `cmd/extensions/*/api/`.

## Key Packages

### `internal/controller/`
Contains all reconciliation logic:
- Workflow definitions for policy reconciliation
- Integration reconcilers for Authorino, Limitador, Envoy Gateway, Istio
- Status updaters and validators
- Gateway provider-specific logic

### `internal/policymachinery/`
Core policy machinery for topology management:
- Policy attachment resolution
- Target reference validation
- Policy conflict detection

### `internal/extension/`
Extension manager for OOP extensions:
- gRPC server for extension communication
- Extension discovery and lifecycle management
- CEL evaluation context provider

### `internal/gatewayapi/`
Gateway API utilities and helpers:
- HTTPRoute matching and selection
- Gateway status management
- Policy attachment patterns

### `internal/authorino/`, `internal/istio/`, `internal/envoygateway/`
Provider-specific integrations with platform-specific APIs and CRDs.

### `internal/wasm/`
WebAssembly integration for Envoy-based rate limiting and policy enforcement.

### `pkg/extension/`
Extension SDK for building OOP extensions:
- Controller builder API
- gRPC client/server definitions
- Utilities for CEL evaluation and resource management

### `pkg/cel/`
CEL (Common Expression Language) extensions:
- Topology query functions (`findGateways()`, `findAuthPolicies()`)
- Kuadrant-specific CEL functions
- Expression evaluation utilities

## Development Workflow

### Making Policy Changes

When modifying policy types:

1. Edit API types in `api/v1/*.go` or extension types in `cmd/extensions/*/api/`
2. Run `make generate` to update DeepCopy methods
3. Run `make manifests` to regenerate CRDs
4. Run `make bundle` to generate the Operator Lifecycle Manager manifest bundle
5. Run `make helm-build` to generate the Helm charts
6. Update controller logic in `internal/controller/` or extension reconcilers
7. Run `make test-unit` to verify changes
8. Run `make pre-commit` before committing

### Adding New Reconcilers

New reconcilers should:
- Implement the workflow pattern with preconditions, tasks, postconditions
- Use `internal/reconcilers/` utilities for common operations
- Follow the naming convention `*_reconciler.go`
- Include unit tests in `*_reconciler_test.go`

### Working with Extensions

Extensions are independent controller processes:
- Each extension has its own `main.go` in `cmd/extensions/*/`
- Extensions receive the Unix socket path as the first CLI argument
- Use `pkg/extension/controller.NewBuilder()` to construct controllers
- Extensions must implement the Policy interface and define CRDs

To develop a new extension:
1. Create directory structure under `cmd/extensions/my-extension/`
2. Define API types in `api/v1alpha1/`
3. Implement reconciler in `internal/controller/`
4. Create `main.go` using the SDK builder pattern
5. Update `WITH_EXTENSIONS` and extension manifests

### Gateway Provider Support

The operator supports multiple gateway providers (Istio, Envoy Gateway). When adding provider-specific logic:
- Use `internal/istio/` for Istio-specific code
- Use `internal/envoygateway/` for Envoy Gateway-specific code
- Ensure logic is conditionally enabled based on CRD availability
- Test with both providers using `make test-integration GATEWAYAPI_PROVIDER=istio|envoygateway`

## Important Notes

- **Controller registration is dynamic**: Controllers are only registered if their dependency CRDs are detected
- **Extensions communicate via Unix sockets**: The socket path is mounted in-cluster and passed as the first CLI arg
- **CEL evaluation happens in two places**:
  - Reconcile-time: via `kuadrant.Resolve()` in extension reconcilers
  - Request-time: CEL programs published as bindings are evaluated by data plane (Authorino/Envoy wasm)
- **Bindings are ephemeral**: They are cleared when policies are deleted
- **Test environments vary**: Integration tests require different setups (bare k8s, gatewayapi, istio, envoygateway)
- **Makefile is the source of truth**: Use `make help` to see all available targets

## Common Pitfalls

1. **Forgetting to run code generation**: Always run `make generate manifests` after API changes
2. **Forgetting to update the manifest bundles**: Always run `make bundle` and `make helm-build` after API changes
3. **Testing without proper environment**: Integration tests need `make local-env-setup` first
4. **Mixing API versions**: Policies and extensions must use compatible API versions
5. **Not handling missing dependencies**: Controllers must gracefully handle missing CRDs
6. **CEL expression errors**: Validate CEL syntax and available context variables carefully
7. **Socket path configuration**: Extensions require the Unix socket path as first CLI argument
