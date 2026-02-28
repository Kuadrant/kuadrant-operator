# Authoring Metapolicies with the Kuadrant Extensions Framework

> **Note**: The Extensions Framework is a preview feature under active development. APIs and deployment models may evolve as we refine the architecture.

## Introduction

**Metapolicies** are higher-level policy abstractions built on Kuadrant's core policies (AuthPolicy, RateLimitPolicy). They let you hide complex multi-policy configurations behind simple, domain-specific interfaces.

Instead of manually wiring together authentication flows or rate limiting logic, metapolicies package these workflows into purpose-built resources. You define a simple CRD, and your extension handles the orchestration behind the scenes.

### Why Build Metapolicies?

You might build a metapolicy when:

- A workflow requires coordinating multiple Kuadrant policies behind a single interface (e.g., OIDCPolicy creates AuthPolicies + HTTPRoutes for OAuth)
- You need information from the kuadrant topology to configure your implementation (e.g., extracting Gateway listener details, HTTPRoute configurations, or other policy states)
- You need to influence Kuadrant's data plane (Authorino, Limitador) with dynamic data that adapts to topology changes

### Examples in This Repository

We've built three metapolicies that demonstrate different patterns:

- **PlanPolicy**: Maps user tiers to rate limits using CEL expressions evaluated at request time
- **OIDCPolicy**: Orchestrates the OAuth Authorization Code Flow by creating HTTPRoutes and AuthPolicies
- **TelemetryPolicy**: Publishes metric label bindings for request-time observability data

## Architecture Overview

### How Metapolicies Work

Metapolicies are implemented as **extensions** that run as separate controller processes and communicate with the main Kuadrant operator via gRPC over Unix domain sockets. Each metapolicy extension:

1. **Defines a Custom Resource Definition (CRD)** with a user-friendly spec
2. **Runs as a separate controller process** (out-of-process from the operator) that reconciles instances of that CRD
3. **Creates and manages underlying resources** - This can include:
   - **Kuadrant policies**: AuthPolicy, RateLimitPolicy, DNSPolicy, TLSPolicy
   - **Gateway API resources**: HTTPRoute, TCPRoute, etc.
   - **Any Kubernetes resource**: ConfigMaps, Secrets, Services, etc.
4. **Publishes data bindings** that influence downstream policy configurations
5. **Evaluates CEL expressions** with access to Kuadrant's topology (Gateways, Routes, Policies)

### Understanding the Topology

The kuadrant-operator maintains an in-memory graph of Gateway API resources and Kuadrant policies - we call this the **topology**. Your extension can query it via CEL expressions to discover relationships and extract configuration:

- `self.findGateways()` - which Gateways does my policy attach to?
- `self.findAuthPolicies()` - what other AuthPolicies are related to my targets?
- Access Gateway spec/status directly (listeners, addresses, protocols, etc.)

This lets you build context-aware metapolicies that adapt to cluster state instead of requiring configuration to be duplicated in multiple places.

#### Topology Access Without Direct Kubernetes API Calls

A key architectural feature: **extensions access the topology through the gRPC connection to the operator, not by querying the Kubernetes API server directly**.

When you call `kuadrantCtx.Resolve()` with a CEL expression, the extension:
1. Sends the CEL expression to the operator over gRPC
2. The operator evaluates it against its in-memory topology
3. Returns the result back to the extension

This means:
- **No RBAC needed for Gateway/Policy resources**: Extensions don't need permissions to read Gateways, HTTPRoutes, or other policies
- **Reduced API server load**: Topology queries don't create additional API calls
- **Consistent view**: All extensions see the same topology state maintained by the operator
- **Deployment flexibility**: Extensions can run in separate pods/containers and still access topology via gRPC

**Current Deployment Model**:

The PlanPolicy and OIDCPolicy examples in this repository run as **out-of-process extensions** within the same container as the operator, communicating over Unix domain sockets:

```
┌─────────────────────────────────────────────────────────┐
│  Kuadrant Operator Pod (same container)                 │
│                                                         │
│  ┌──────────────────────┐       ┌────────────────────┐  │
│  │ Operator Process     │       │ Extension Process  │  │
│  │ ┌─────────────────┐  │       │ ┌────────────────┐ │  │
│  │ │ Topology        │  │◄──────┤►│ MyPolicy       │ │  │
│  │ │ (in-memory)     │  │ Unix  │ │ Reconciler     │ │  │
│  │ │ - Gateways      │  │ Socket│ └────────────────┘ │  │
│  │ │ - HTTPRoutes    │  │ gRPC  │                    │  │
│  │ │ - Policies      │  │       │                    │  │
│  │ └─────────────────┘  │       │                    │  │
│  └──────────────────────┘       └────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

**Future: Separate Container/Pod Deployment** (work in progress):

The architecture is designed to support extensions running in separate containers or pods, though this is not yet fully production-ready:

```
┌─────────────────────────────┐          ┌──────────────────────────┐
│  Kuadrant Operator Pod      │          │  Extension Pod           │
│  ┌───────────────────────┐  │          │  ┌────────────────────┐  │
│  │ Topology (in-memory)  │  │          │  │ MyPolicy Reconciler│  │
│  │ - Gateways            │  │          │  └─────────┬──────────┘  │
│  │ - HTTPRoutes          │  │◄─────────┤            │             │
│  │ - Policies            │  │  gRPC    │  ┌─────────▼──────────┐  │
│  └───────────────────────┘  │          │  │ kuadrantCtx.Resolve│  │
│                             │          │  │ (sends CEL expr)   │  │
└─────────────────────────────┘          │  └────────────────────┘  │
                                         └──────────────────────────┘
```

**Key point**: Regardless of deployment model, extensions query topology via gRPC - no direct Kubernetes API calls needed. Extensions only need RBAC permissions for resources they directly manage (creating AuthPolicies, HTTPRoutes, etc.), but **not** for reading the topology.

### Key Concepts

#### 1. Data Bindings and Domains

Extensions can publish ephemeral key-value bindings that augment managed resources. These bindings are consumed by the data plane at request time:

- **DomainAuth**: Bindings consumed by Authorino (authentication/authorization service)
  - Example: PlanPolicy publishes a `plan` binding that evaluates CEL to determine user tier
- **DomainRequest**: Bindings consumed by Envoy wasm/Limitador (rate limiting service)
  - Example: TelemetryPolicy publishes metric label bindings

Bindings can contain:
- **Literals**: Evaluated at reconcile time by the controller
- **CEL programs**: Evaluated at request time by the data plane

#### 2. CEL Evaluation

Extensions use the Common Expression Language (CEL) to:
- **Query topology**: Find related Gateways, HTTPRoutes, and Policies using functions like `findGateways()`, `findHTTPRoutes()`
- **Extract runtime data**: Access Gateway status, listener configurations, policy specifications
- **Define request-time logic**: Create expressions that the data plane evaluates per-request

CEL evaluation happens at two stages:
1. **Reconcile-time**: Via `kuadrantCtx.Resolve()` - controller evaluates CEL to make decisions
2. **Request-time**: CEL programs in bindings are evaluated by Authorino/Envoy wasm for each request

#### 3. Resource Reconciliation

Extensions create and manage Kubernetes resources using `kuadrantCtx.ReconcileObject()`:
- Creates resources if they don't exist
- Updates existing resources to match desired state
- Sets owner references for automatic cleanup

## Extension SDK Reference

The Extensions Framework provides key functions through the `KuadrantCtx` interface. These differentiate extensions from standard Kubernetes controllers.

### kuadrantCtx.Resolve()

Query the topology and extract structured data via CEL—without making Kubernetes API calls.

**What it does**: Evaluates a CEL expression against the in-memory topology and returns the result. The operator handles evaluation via gRPC.

**Signature**:
```go
Resolve(ctx context.Context, policy Policy, celExpression string, asJSON bool) (celref.Val, error)
```

**Available CEL functions**:
- `self.findGateways()` - Gateways this policy attaches to (via targetRef)
- `self.findAuthPolicies()` - AuthPolicies related to this policy's targets  
- `targetRef.findGateways()` - Gateways for a specific targetRef

You can access any field: `.metadata.name`, `.spec.listeners[0].hostname`, `.status.addresses`, etc.

**Example**:
```go
type GatewayInfo struct {
    Name     string `json:"name"`
    Hostname string `json:"hostname"`
    Protocol string `json:"protocol"`
}

// Extract gateway details as structured data
gwInfo, err := extcontroller.Resolve[GatewayInfo](ctx, kCtx, policy,
    `{"name": self.findGateways()[0].metadata.name,
      "hostname": self.findGateways()[0].spec.listeners[0].hostname,
      "protocol": self.findGateways()[0].spec.listeners[0].protocol}`,
    true)
if err != nil {
    return reconcile.Result{}, err
}

// Use it to configure resources
redirectURL := fmt.Sprintf("%s://%s/callback", 
    strings.ToLower(gwInfo.Protocol), gwInfo.Hostname)
```

### kuadrantCtx.AddDataTo()

Publish data bindings injected into downstream resources and evaluated at request time by the data plane.

**What it does**: Registers a key-value binding that gets added to AuthConfigs (DomainAuth) or Limitador/Envoy wasm configs (DomainRequest). Values can be literals or CEL expressions evaluated per-request.

**Signature**:
```go
AddDataTo(ctx context.Context, policy Policy, domain Domain, key string, value string) error
```

**Domains**:
- `types.DomainAuth` - Consumed by Authorino (authentication/authorization)
- `types.DomainRequest` - Consumed by Limitador/Envoy wasm (rate limiting)

**Example**:
```go
// Publish a CEL expression evaluated by Authorino per-request
celExpr := `auth.identity.metadata.annotations["plan-tier"]`
if err := kCtx.AddDataTo(ctx, policy, types.DomainAuth, "plan", celExpr); err != nil {
    return err
}

// Now available in AuthPolicies as `auth.kuadrant.plan`
// Can be used in rate limit when conditions, authorization rules, etc.
```

For metrics:
```go
kCtx.AddDataTo(ctx, policy, types.DomainRequest, 
    types.KuadrantMetricBinding("user_tier"), 
    `request.headers["x-user-tier"]`)
```

### kuadrantCtx.ReconcileObject()

Create or update Kubernetes resources with three-way merge semantics.

**What it does**: Similar to `controllerutil.CreateOrUpdate()` but tailored for the extension SDK. Creates the resource if missing, updates if changed based on your mutator function.

**Signature**:
```go
ReconcileObject(ctx context.Context, emptyObj client.Object, desired client.Object, mutateFn MutateFn) (client.Object, error)
```

**Example**:
```go
desired := &kuadrantv1.AuthPolicy{
    ObjectMeta: metav1.ObjectMeta{Name: policy.Name, Namespace: policy.Namespace},
    Spec: buildSpec(policy),
}
controllerutil.SetControllerReference(policy, desired, r.Scheme)

obj, err := kCtx.ReconcileObject(ctx, &kuadrantv1.AuthPolicy{}, desired, mutatorFn)
```

## Building a Reconciler

Here's a minimal reconciler showing how to use the SDK functions:

```go
func (r *MyPolicyReconciler) Reconcile(ctx context.Context, req reconcile.Request, kCtx types.KuadrantCtx) (reconcile.Result, error) {
    if err := r.Configure(ctx); err != nil {
        return reconcile.Result{}, err
    }

    pol := &v1alpha1.MyPolicy{}
    if err := r.Client.Get(ctx, req.NamespacedName, pol); err != nil {
        return reconcile.Result{}, client.IgnoreNotFound(err)
    }

    if pol.GetDeletionTimestamp() != nil {
        return reconcile.Result{}, nil
    }

    // 1. Query topology
    gwInfo, err := extcontroller.Resolve[GatewayInfo](ctx, kCtx, pol,
        `{"hostname": self.findGateways()[0].spec.listeners[0].hostname}`, true)
    if err != nil {
        return reconcile.Result{}, err
    }

    // 2. Publish bindings
    if err := kCtx.AddDataTo(ctx, pol, types.DomainAuth, "gateway.host", gwInfo.Hostname); err != nil {
        return reconcile.Result{}, err
    }

    // 3. Reconcile managed resources
    desired := buildAuthPolicy(pol, gwInfo)
    controllerutil.SetControllerReference(pol, desired, r.Scheme)
    
    _, err = kCtx.ReconcileObject(ctx, &kuadrantv1.AuthPolicy{}, desired, authPolicyMutator)
    if err != nil {
        return reconcile.Result{}, err
    }

    // 4. Update status (standard controller-runtime)
    return r.reconcileStatus(ctx, pol)
}
```

The reconciler signature is `Reconcile(ctx context.Context, req reconcile.Request, kCtx types.KuadrantCtx)`. Note the `kCtx` parameter - that's your access to the Extension SDK functions.

### Wiring Up the Extension

Create `main.go` to bootstrap your extension:

```go
func main() {
    reconciler := controller.NewMyPolicyReconciler()
    builder, logger := extcontroller.NewBuilder("my-policy-controller")
    
    ctrl, err := builder.
        WithScheme(scheme).
        WithReconciler(reconciler.Reconcile).
        For(&v1alpha1.MyPolicy{}).
        Owns(&kuadrantv1.AuthPolicy{}).
        Build()
    if err != nil {
        logger.Error(err, "unable to create controller")
        os.Exit(1)
    }
    
    if err = ctrl.Start(ctrl.SetupSignalHandler()); err != nil {
        logger.Error(err, "unable to start extension controller")
        os.Exit(1)
    }
}
```

Note that you're using `extcontroller.NewBuilder()` from the Extension SDK (`pkg/extension/controller`), not controller-runtime's builder. The API is designed to look similar to controller-runtime for familiarity, but it wires up the gRPC connection and passes the `KuadrantCtx` to your reconciler.

The Unix socket path is automatically passed as `os.Args[1]` by the operator.

## Development Workflow

### Project Structure

```
cmd/extensions/my-policy/
├── main.go
├── api/
│   └── v1alpha1/
│       ├── groupversion_info.go
│       ├── mypolicy_types.go
│       └── zz_generated.deepcopy.go
└── internal/
    └── controller/
        └── mypolicy_reconciler.go
```

### Deployment Options

#### Current Approach: Same-Pod Deployment

To deploy your extension alongside the Kuadrant operator:

1. **Build your extension container image** with your extension binary
2. **Install your extension's CRD** in the cluster
3. **Update the operator deployment** to add:
   - An init container or sidecar running your extension image
   - A shared volume mount at `/extensions` for your extension binary
   - The existing `extensions-socket-volume` mounted at `/tmp/kuadrant` for Unix socket communication
4. **Update RBAC**: Add a ClusterRole with permissions for:
   - Your extension's policy CRD (read/write/status)
   - Resources your extension creates (e.g., AuthPolicy, RateLimitPolicy, HTTPRoute)

The operator watches the `/extensions` directory (configured via `EXTENSIONS_DIR` env var) and automatically starts any extension binaries it finds there, passing the Unix socket path as the first argument.

**Reference**: See how the built-in extensions are deployed in `config/extensions/extensions-patch.yaml` - your deployment would follow a similar pattern but with your own extension image.

#### Future: Separate Container/Pod Deployment

> **Note**: Support for deploying extensions in separate containers/pods is under development and not yet production-ready.

For extensions developed outside the Kuadrant operator repository, the architecture is designed to support:

1. **Standalone extension images**: Package your extension as a separate container
2. **Independent deployment**: Deploy your extension controller separately from the operator
3. **Network-based gRPC**: Connect to the operator's gRPC endpoint over the network
4. **Minimal RBAC**: Extensions only need permissions for resources they create/manage
   - **No RBAC needed** for reading Gateways, HTTPRoutes, or policies—topology queries happen via gRPC
5. **CRD installation**: Install your metapolicy CRD independently

This model would enable extensions to be developed, versioned, and deployed independently while accessing the full Kuadrant topology without requiring extensive cluster read permissions. Watch the Kuadrant repository for updates on separate-container deployment support.

## Design Considerations

### Targeting and Attachment

Metapolicies use the Gateway API Policy Attachment pattern (GEP-713):

- **Target Gateway API resources**: Your metapolicy attaches to Gateways or HTTPRoutes via `targetRef`
- **Not tied to other policies**: While metapolicies often *create* Kuadrant policies (AuthPolicy, RateLimitPolicy), they don't attach to them—they attach to Gateway API resources
- **Topology discovery**: Use `findGateways()` to discover which Gateway your policy applies to, then extract configuration like hostnames, protocols, listener details

**Example**: OIDCPolicy targets an HTTPRoute. It uses `self.findGateways()[0]` to discover the parent Gateway, extracts the hostname and protocol, and uses that information to build OAuth redirect URLs.

### Resource Ownership

- **Set owner references** on all managed resources using `controllerutil.SetControllerReference()`
- This ensures automatic garbage collection when the metapolicy is deleted
- Both Kuadrant policies and Gateway API resources can be owned by your metapolicy

### Reconciliation Patterns

**Separate spec and status reconciliation**:
```go
// Reconcile spec (create/update resources, publish bindings)
newStatus, specErr := r.reconcileSpec(ctx, pol, context)

// Reconcile status (update conditions)
statusResult, statusErr := r.reconcileStatus(ctx, pol, newStatus)
```

**Check managed resource status** before reporting success:
```go
func isAuthPolicyEnforced(authPolicy *kuadrantv1.AuthPolicy) error {
    cond := meta.FindStatusCondition(authPolicy.Status.Conditions, string(types.PolicyConditionEnforced))
    if cond == nil || cond.Status == metav1.ConditionFalse {
        return fmt.Errorf("AuthPolicy %s is not enforced", authPolicy.Name)
    }
    return nil
}
```

### Leveraging the Topology

The topology gives you context about the Gateway API resources and policies in your cluster:

**Available CEL functions**:
- `self.findGateways()` - Find Gateways that this policy attaches to (based on targetRef)
- `self.findAuthPolicies()` - Find AuthPolicies related to this policy's targets
- `targetRef.findGateways()` - Find Gateways for a specific targetRef

**What you can access**:
- Gateway spec: listeners, addresses, gateway class
- Gateway status: assigned addresses, listener status, conditions
- Policy spec and status: configuration and enforcement state

**Pattern**: Query once at reconcile-time, use the data to configure managed resources:
```go
// Resolve gateway info via CEL topology query
gwData, _ := extcontroller.Resolve[GatewayInfo](ctx, kCtx, policy,
    `{"hostname": self.findGateways()[0].spec.listeners[0].hostname,
      "protocol": self.findGateways()[0].spec.listeners[0].protocol}`,
    true)

// Use in resource construction
redirectURL := fmt.Sprintf("%s://%s/callback", 
    strings.ToLower(string(gwData.Protocol)), gwData.Hostname)
```

## Debugging Extensions

### Logging

Extensions use structured logging via `logr`:

```go
r.Logger.Info("reconciling policy", "name", pol.Name, "namespace", pol.Namespace)
r.Logger.V(1).Info("debug details", "gatewayInfo", gwInfo)
r.Logger.Error(err, "failed to reconcile AuthPolicy")
```

Set log level via environment variable:
```bash
LOG_LEVEL=debug  # debug, info, warn, error
LOG_MODE=development  # development or production
```

## Resources

### Code References

- **Extension SDK**: `pkg/extension/`
- **PlanPolicy Example**: `cmd/extensions/plan-policy/`
- **OIDCPolicy Example**: `cmd/extensions/oidc-policy/`
- **TelemetryPolicy Example**: `cmd/extensions/telemetry-policy/`
- **CEL Functions**: `pkg/cel/`
- **Developer Guide**: `doc/extensions/extension-sdk-developer-guide.md`

### External Documentation

- [Gateway API Policy Attachment](https://gateway-api.sigs.k8s.io/geps/gep-713/)
- [CEL Language Definition](https://github.com/google/cel-spec)
- [Kuadrant's Introduction to CEL](../cel/introduction.md)
- [Authorino Documentation](https://docs.kuadrant.io/authorino/)
- [Limitador Documentation](https://docs.kuadrant.io/limitador/)

## Conclusion

The Extensions Framework lets you wrap complex workflows (OAuth flows, tiered rate limiting, custom traffic rules) in simple, purpose-built CRDs. When someone applies one of these CRDs, your extension orchestrates the underlying resources.

**What you get**:

- Query Gateway and policy topology via CEL without touching the Kubernetes API
- Inject request-time logic through data bindings (DomainAuth for Authorino, DomainRequest for Limitador)
- Manage multiple resources from a single metapolicy CR
- Minimal RBAC footprint—extensions only need permissions for resources they create

**Next steps**:

Start by looking at **PlanPolicy**, **OIDCPolicy**, and **TelemetryPolicy** in this repository to see these in action. When you're ready to build your own:

1. Pick a workflow that would benefit from a simpler interface
2. Use `kuadrantCtx.Resolve()` to query the topology via CEL
3. Use `kuadrantCtx.AddDataTo()` to publish bindings for request-time evaluation
4. The rest is standard controller-runtime—build your reconciler, manage resources, update status

As we continue developing the framework, we're working toward support for separate-container deployments and expanding the topology query capabilities. The core patterns you learn now will carry forward.
