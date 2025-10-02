# Kuadrant Extension SDK

The Kuadrant Extension SDK provides a framework for building custom policy extensions that extend Kuadrant's functionality 
beyond the core policies (AuthPolicy, RateLimitPolicy, TLSPolicy, and DNSPolicy). The SDK enables developers to create 
specialized policy controllers that integrate seamlessly with the Kuadrant ecosystem while maintaining consistency with 
Gateway API standards.

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

### 1. Project Structure

Create your extension with the following structure:

```
my-extension/
├── main.go
├── api/
│   └── v1alpha1/
│       ├── groupversion_info.go
│       ├── mypolicy_types.go
│       └── zz_generated.deepcopy.go
├── config/
│   ├── crd/
│   │   ├── kustomization.yaml
│   │   └── bases/
│   │       └── extensions.kuadrant.io_mypolicies.yaml
│   ├── deploy/
│   │   └── kustomization.yaml
│   └── rbac/
│       ├── kustomization.yaml
│       └── role.yaml
└── internal/
    └── controller/
        └── mypolicy_reconciler.go
```

### 2. Define Your Policy Type

Create your policy CRD following Gateway API policy patterns:

```go
// api/v1alpha1/mypolicy_types.go
package v1alpha1

import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// MyPolicySpec defines the desired state of MyPolicy
type MyPolicySpec struct {
    // TargetRefs identifies the Gateway API resources to apply the policy to
    TargetRefs []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRefs"`
    
    // Your custom policy configuration
    CustomConfig string `json:"customConfig,omitempty"`
}

// MyPolicyStatus defines the observed state of MyPolicy
type MyPolicyStatus struct {
    // Standard Gateway API policy status
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// MyPolicy is the Schema for the mypolicies API
type MyPolicy struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   MyPolicySpec   `json:"spec,omitempty"`
    Status MyPolicyStatus `json:"status,omitempty"`
}

// Implement the Policy interface
func (p *MyPolicy) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
    return p.Spec.TargetRefs
}
```

### 3. Implement the Reconciler

Create a reconciler that implements the extension pattern:

```go
// internal/controller/mypolicy_reconciler.go
package controller

import (
    "context"
    
    "sigs.k8s.io/controller-runtime/pkg/reconcile"
    
    "github.com/kuadrant/kuadrant-operator/pkg/extension/types"
    "your-module/api/v1alpha1"
)

type MyPolicyReconciler struct {
    types.ExtensionBase
}

func NewMyPolicyReconciler() *MyPolicyReconciler {
    return &MyPolicyReconciler{}
}

func (r *MyPolicyReconciler) Reconcile(ctx context.Context, req reconcile.Request, kuadrant types.KuadrantCtx) (reconcile.Result, error) {
    // Get the policy
    policy := &v1alpha1.MyPolicy{}
    if err := r.Client.Get(ctx, req.NamespacedName, policy); err != nil {
        // Handle not found and other errors
        return reconcile.Result{}, client.IgnoreNotFound(err)
    }

    // Example: Resolve a CEL expression using Kuadrant topology functions
    // Get all gateway addresses attached to this policy
    addresses, err := kuadrant.Resolve(ctx, policy,
        `self.findGateways().map(g, g.status.addresses.map(a, a.value)).flatten()`,
        false,
    )
    if err != nil {
        return reconcile.Result{}, err
    }

    // Example: Create or update related resources
    desiredResource := r.buildDesiredResource(policy, addresses)
    _, err = kuadrant.ReconcileObject(ctx, desiredResource, desiredResource, r.mutateFn)
    if err != nil {
        return reconcile.Result{}, err
    }

    // Example: Add data for other policies to consume
    err = kuadrant.AddDataTo(ctx, policy, types.DomainAuth, "custom.binding", "value")
    if err != nil {
        return reconcile.Result{}, err
    }

    return reconcile.Result{}, nil
}

func (r *MyPolicyReconciler) mutateFn(existing, desired client.Object) (bool, error) {
    // Implement mutation logic
    return true, nil
}
```

### 4. Create the Main Function

Wire everything together in your main function:

```go
// main.go
package main

import (
    "os"
    
    corev1 "k8s.io/api/core/v1"
    k8sruntime "k8s.io/apimachinery/pkg/runtime"
    utilruntime "k8s.io/apimachinery/pkg/util/runtime"
    ctrl "sigs.k8s.io/controller-runtime"
    gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
    
    kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
    extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
    "your-module/api/v1alpha1"
    "your-module/internal/controller"
)

var scheme = k8sruntime.NewScheme()

func init() {
    utilruntime.Must(corev1.AddToScheme(scheme))
    utilruntime.Must(gatewayapiv1.Install(scheme))
    utilruntime.Must(kuadrantv1.AddToScheme(scheme))
    utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
    reconciler := controller.NewMyPolicyReconciler()
    builder, logger := extcontroller.NewBuilder("my-policy-controller")
    
    extController, err := builder.
        WithScheme(scheme).
        WithReconciler(reconciler.Reconcile).
        For(&v1alpha1.MyPolicy{}).
        Owns(&corev1.ConfigMap{}). // Example owned resource
        Build()
    if err != nil {
        logger.Error(err, "unable to create controller")
        os.Exit(1)
    }

    if err = extController.Start(ctrl.SetupSignalHandler()); err != nil {
        logger.Error(err, "unable to start extension controller")
        os.Exit(1)
    }
}
```

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

### Cross-Policy Data Sharing

Extensions can share data with other policies through the data binding system:

```go
// Add authentication data for rate limiting policies
err = kuadrant.AddDataTo(ctx, policy, types.DomainAuth, "user.tier", "premium")

// Add request metadata
err = kuadrant.AddDataTo(ctx, policy, types.DomainRequest, "custom.header", headerValue)
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
- **Features**: Metric definition, trace sampling, log aggregation

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
- Example invocation: `./my-policy-controller /var/run/kuadrant/extensions.sock`.

In Kubernetes, pass the socket path as a container arg and mount the socket accordingly.

## Deployment

Extensions are typically deployed as separate controllers in the Kuadrant namespace:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-policy-controller
  namespace: kuadrant-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: my-policy-controller
  template:
    metadata:
      labels:
        app: my-policy-controller
    spec:
      containers:
      - name: controller
        image: my-org/my-policy-controller:latest
        args:
        - /var/run/kuadrant/extensions.sock # Unix socket path provided by the operator
        env:
        - name: LOG_LEVEL
          value: info
        volumeMounts:
        - name: kuadrant-socket
          mountPath: /var/run/kuadrant
        ports:
        - containerPort: 8080
          name: metrics
      volumes:
      - name: kuadrant-socket
        hostPath:
          path: /var/run/kuadrant
          type: Directory
```

## Troubleshooting

### Common Issues

1. **gRPC Connection Failures**
   - Verify the Kuadrant operator is running
   - Check network connectivity between extension and operator
   - Ensure correct gRPC server address configuration

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
