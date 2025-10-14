# Kuadrant Extension SDK

The Kuadrant Extension SDK provides a framework for building custom policy extensions that extend Kuadrant's functionality 
beyond the core policies (AuthPolicy, RateLimitPolicy, TLSPolicy, and DNSPolicy). The SDK enables developers to create 
specialized policy controllers that integrate seamlessly with the Kuadrant ecosystem while maintaining consistency with 
Gateway API standards.

## At a glance

- Build policy-focused extensions that attach via Gateway API patterns
- Evaluate CEL with Kuadrant topology/context and publish bindings
- Operator wires bindings into managed resources (AuthConfig, wasm/Limitador)
- Extensions connect to the operator over a Unix socket (first CLI arg)

## Key concepts

- Policy: Your CRD implementing Gateway API policy attachment (targetRefs) and the SDK `Policy` interface.
- KuadrantCtx: Context the reconciler receives for CEL evaluation, data publishing, and reconciliation helpers.
- Binding: A policy-scoped key/value published via `AddDataTo`. Values are literals or CEL programs evaluated at request time.
- Domain: Logical channel for bindings: `DomainAuth` (identity/auth) and `DomainRequest` (request/topology).
- Topology DAG: Operator-maintained view of Gateways, Routes, and attached policies; exposed to CEL via helpers like `self.findGateways()`.

## Overview

The Extension SDK allows developers to build policy extensions that:

- Follow Gateway API policy attachment patterns
- Integrate with the Kuadrant control plane via gRPC
- Leverage Common Expression Language (CEL) for dynamic configuration
- Subscribe to cluster events and react to changes
- Access the Kuadrant context for cross-policy coordination

## Architecture

The Extension SDK consists of several key components:

### Core Components

1. **Extension Base** (`pkg/extension/types`): Provides common functionality for all extensions
2. **Controller Builder** (`pkg/extension/controller/builder.go`): Fluent API for constructing extension controllers
3. **gRPC Interface** (`pkg/extension/grpc/v1`): Communication protocol between extensions and Kuadrant
4. **Utilities** (`pkg/extension/utils`): Helper functions for common operations

### Communication Flow

```
Extension Controller → gRPC Client → Kuadrant Operator → Kubernetes API
                    ↓
                CEL Evaluation ← Context Resolution ← Policy Machinery
```

Extensions communicate with the main Kuadrant operator through a gRPC interface that provides:

- **Event Subscription**: React to policy and resource changes
- **Context Resolution**: Access shared Kuadrant context and evaluate CEL expressions
- **Data Sharing (Bindings)**: Share computed data with other policies via bindings
- **Policy Coordination**: Clear subscriptions and bindings when policies are deleted

## Building an Extension

Looking for a step-by-step scaffold (CRD, reconciler, main)? See the Developer Guide:

- doc/extensions/extension-sdk-developer-guide.md

That guide includes concrete code and minimal wiring. This overview focuses on concepts, CEL helpers, domains, and data-plane materialization.

## Extension Features

### CEL Expression Evaluation

Extensions can evaluate CEL expressions using the Kuadrant context:

```go
// Resolve gateway addresses for this policy
addresses, err := kuadrant.Resolve(ctx, policy,
    `self.findGateways().map(g, g.status.addresses.map(a, a.value)).flatten()`,
    false,
)

// Resolve with subscription to changes (re-evaluate when gateways change)
firstAddress, err := kuadrant.Resolve(ctx, policy,
    `self.findGateways().map(g, g.status.addresses.map(a, a.value)).flatten().first()`,
    true,
)
```

Note: `subscribe=true` registers interest in changes to referenced topology/context and can increase reconcile frequency. Use it only when the value must track live changes.

### Publishing Data Bindings

Extensions commonly derive values via CEL using `kuadrant.Resolve(...)` and then publish them using the Go API. Bindings are consumed by the operator to update managed resources (e.g., Authorino AuthConfig, Envoy/wasm/Limitador configuration); they do not directly modify user-authored Kuadrant policy CRs (AuthPolicy, RateLimitPolicy, TLSPolicy, DNSPolicy).

```go
// Publish CEL-derived value
addrs, err := kuadrant.Resolve(ctx, policy,
    `self.findGateways().map(g, g.status.addresses.map(a, a.value)).flatten()`,
    false,
)
if err == nil {
    _ = kuadrant.AddDataTo(ctx, policy, types.DomainRequest, "gateway.addresses", addrs)
}

// Publish a literal value
_ = kuadrant.AddDataTo(ctx, policy, types.DomainAuth, "user.tier", "premium")

// Publish a CEL program to be evaluated at request time downstream
_ = kuadrant.AddDataTo(ctx, policy, types.DomainRequest, "labels.user", `request.headers["x-user"] ?? "anonymous"`)
```

### Kuadrant Topology via CEL

The SDK exposes CEL functions that let extension controllers query Kuadrant's topology (gateways and attached policies) 
without hand-rolling Kubernetes queries. These functions are provided by the Kuadrant CEL library and are available to
expressions evaluated via `kuadrant.Resolve`.

- `self`: The current policy object in proto form (`kuadrant.v1.Policy`).
- `findGateways(...)`:
    - As a member on `Policy`: `self.findGateways()` → `[Gateway]` associated with the policy's `targetRefs`.
    - As a member on `TargetRef`: `targetRef.findGateways()` → `[Gateway]` that match the target reference.
- `findAuthPolicies(...)`:
    - As a member on `Policy`: `self.findAuthPolicies()` → `[Policy]` of kind `AuthPolicy` that attach to the same `targetRefs`.

Example usages:

```go
// Get all gateways for this policy via CEL
val, err := kuadrant.Resolve(ctx, policy, `self.findGateways()`, false)

// Get hostnames of attached gateways (strings)
val, err = kuadrant.Resolve(ctx, policy, `self.findGateways().map(g, g.status.addresses.map(a, a.value)).flatten()`, false)

// Check if any AuthPolicy already attaches to the same targets
val, err = kuadrant.Resolve(ctx, policy, `self.findAuthPolicies().size() > 0`, false)

// For a specific targetRef from the policy, discover gateways
val, err = kuadrant.Resolve(ctx, policy, `self.targetRefs[0].findGateways()`, false)
```

Notes:
- These functions rely on Kuadrant's internal DAG of the topology and return strongly-typed proto objects (`kuadrant.v1.Gateway`, `kuadrant.v1.Policy`).
- A special constant `__KUADRANT_VERSION` is available to CEL expressions for compatibility checks (e.g., "0_dev", "1_dev").
- The available functions may evolve; consult the source in `pkg/cel/ext/kuadrant.go` for the current set.

### Domains reference

| Domain                | Typical consumers                     | Typical keys                          | Evaluated where                   |
|-----------------------|---------------------------------------|---------------------------------------|-----------------------------------|
| `types.DomainAuth`    | Authorino (dynamic metadata)          | `user.tier`, `plan`, `claims.sub`     | Request-time (AuthConfig)         |
| `types.DomainRequest` | Envoy wasm/Limitador, Authorino       | `gateway.addresses`, `labels.user`, `headers.x-foo` | Request-time (wasm/ratelimiting) |

Notes:
- Keys are plain strings; dotted keys are a naming convention only.
- Bindings are ephemeral and cleared on policy deletion.
- See definitions in `pkg/extension/types/types.go`.

### How bindings are materialized in the data plane

Bindings are consumed by the operator to augment managed configurations so CEL is evaluated at request time by the data plane:

- DomainAuth
    - Consumed by the Authorino integration path.
    - Operator effect: updates managed AuthConfig with metadata evaluators that run your CEL and expose results as dynamic metadata for subsequent policies/filters.

- DomainRequest
    - Consumed by the Envoy/wasm/Limitador path.
    - Operator effect: updates managed Envoy wasm configuration and related resources so your CEL is evaluated per request and forwarded as request attributes/labels to Authorino/Limitador.

Notes
- `AddDataTo` publishes ephemeral, policy-scoped keys; the operator re-renders managed resources when these bindings change, and clears them on policy deletion.
- Values can be literals (evaluated at reconcile) or CEL programs (evaluated at request time by data plane components).

### Event Subscription

Extensions automatically subscribe to relevant cluster events through the gRPC interface. The extension controller handles:

- Policy creation, updates, and deletion
- Gateway and HTTPRoute changes
- Related resource modifications

### Resource Management

Extensions can manage Kubernetes resources using the reconciliation pattern:

```go
desired := &corev1.ConfigMap{
    ObjectMeta: metav1.ObjectMeta{
        Name:      "my-config",
        Namespace: policy.Namespace,
    },
    Data: map[string]string{
        "config": configData,
    },
}

actual, err := kuadrant.ReconcileObject(ctx, desired, desired, mutateFn)
```

## Example Extensions

The Kuadrant repository includes several example extensions:

### OIDC Policy Extension
- **Location**: `cmd/extensions/oidc-policy/`
- **Purpose**: Provides OpenID Connect authentication with automatic discovery
- **Features**: JWKS endpoint discovery, issuer validation, claim extraction

### Plan Policy Extension  
- **Location**: `cmd/extensions/plan-policy/`
- **Purpose**: Implements usage plan management with quotas and limits
- **Features**: Plan selection, quota enforcement, usage tracking

### Telemetry Policy Extension
- **Location**: `cmd/extensions/telemetry-policy/`
- **Purpose**: Configures observability and metrics collection
- **Features**: Publishes CEL expressions for request-time metric labels

## Configuration

Extensions can be configured through environment variables and command-line flags:

```bash
# Logging configuration
export LOG_LEVEL=info
export LOG_MODE=production

# Kubernetes client configuration
export KUBECONFIG=/path/to/kubeconfig
export NAMESPACE=kuadrant-system
```

gRPC connectivity

- The extension controller connects to the Kuadrant operator via a Unix domain socket.
- The socket path is passed as the first command-line argument to your controller binary (required by the SDK builder).

In Kubernetes, pass the socket path as a container arg and mount the socket accordingly.

## Troubleshooting

### Common Issues

1. **gRPC Connection Failures**
    - Verify the Kuadrant operator is running and exposing the extensions Unix socket
    - Check the socket path, mount, and file permissions inside your controller Pod
    - Ensure your controller is invoked with the correct socket argument (first CLI arg)

2. **CEL Evaluation Errors**
    - Validate CEL expressions syntax
    - Check available context variables
    - Review error logs for specific evaluation failures

3. **Resource Reconciliation Issues**
    - Verify RBAC permissions for extension controller
    - Check owner reference configuration
    - Review resource creation/update errors

### Debugging

Enable debug logging to troubleshoot issues:

```bash
export LOG_LEVEL=debug
export LOG_MODE=development
```
