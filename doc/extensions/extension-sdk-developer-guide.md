# Kuadrant Extension SDK — Developer Guide

This guide shows how to build a custom extension policy using the Kuadrant Extension SDK. It covers the project structure, the SDK API, and three reconciler patterns you can choose from depending on your use case.

## Project structure

    my-extension/
    ├── main.go
    ├── go.mod
    ├── Dockerfile
    ├── Makefile
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

## Policy type (CRD)

Every extension must define a policy type that implements the `types.Policy` interface. The policy attaches to Gateway API resources via a `targetRef` field.

```go
package v1alpha1

import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

type MyPolicy struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   MyPolicySpec   `json:"spec,omitempty"`
    Status MyPolicyStatus `json:"status,omitempty"`
}

type MyPolicySpec struct {
    // +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
    // +kubebuilder:validation:XValidation:rule="self.kind == 'HTTPRoute' || self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'"
    TargetRef    gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`
    CustomConfig string `json:"customConfig,omitempty"`
}

type MyPolicyStatus struct {
    // +optional
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`

    // +patchMergeKey=type
    // +patchStrategy=merge
    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

func (p *MyPolicy) GetName() string      { return p.Name }
func (p *MyPolicy) GetNamespace() string  { return p.Namespace }
func (p *MyPolicy) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
    return []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{p.Spec.TargetRef}
}
```

Key points:
- The SDK supports both singular and plural target references. All built-in extensions use `TargetRef` (singular) with `GetTargetRefs()` wrapping it in a single-element slice, but you can use `TargetRefs` (plural, a slice) if your policy needs to target multiple resources.
- Include `ObservedGeneration` in the status for proper status tracking.
- Add kubebuilder validation markers on the `TargetRef` to restrict to supported kinds.

## Main

The entry point uses the SDK builder to wire everything together.

```go
package main

import (
    "os"

    k8sruntime "k8s.io/apimachinery/pkg/runtime"
    utilruntime "k8s.io/apimachinery/pkg/util/runtime"
    ctrl "sigs.k8s.io/controller-runtime"

    "your-module/api/v1alpha1"
    "your-module/internal/controller"
    extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
)

var scheme = k8sruntime.NewScheme()

func init() {
    utilruntime.Must(v1alpha1.AddToScheme(scheme))
    // Only register additional schemes if your reconciler interacts with those types:
    // utilruntime.Must(gwapiv1.Install(scheme))          // if you read Gateway/HTTPRoute objects
    // utilruntime.Must(kuadrantv1.AddToScheme(scheme))   // if you create RateLimitPolicy/AuthPolicy
    // utilruntime.Must(corev1.AddToScheme(scheme))       // if you interact with Secrets/ConfigMaps
}

func main() {
    r := controller.NewMyPolicyReconciler()
    b, logger := extcontroller.NewBuilder("my-policy-controller")
    c, err := b.
        WithScheme(scheme).
        WithReconciler(r.Reconcile).
        For(&v1alpha1.MyPolicy{}).
        // Watches(&gwapiv1.HTTPRoute{}).  // watch additional types (enqueues reconcile for your policy)
        // Owns(&kuadrantv1.AuthPolicy{}). // watch owned types (owner ref resolution)
        Build()
    if err != nil {
        logger.Error(err, "unable to create controller")
        os.Exit(1)
    }
    if err := c.Start(ctrl.SetupSignalHandler()); err != nil {
        logger.Error(err, "unable to start extension controller")
        os.Exit(1)
    }
}
```

### Builder methods

| Method | Purpose |
|--------|---------|
| `WithScheme(scheme)` | Sets the runtime scheme |
| `WithReconciler(fn)` | Sets the reconcile function |
| `For(obj)` | Sets the primary policy type (required, call once) |
| `Watches(obj)` | Registers additional types to watch (enqueues reconcile for your policy) |
| `Owns(obj)` | Registers owned types (reconcile triggered via owner references) |
| `Build()` | Validates config, connects to operator via gRPC, returns `*ExtensionController` |

### Scheme registration

Only register what your reconciler actually uses:

| Scheme | When to register |
|--------|-----------------|
| Your `v1alpha1` | Always (your own CRD types) |
| `gwapiv1` | If you read Gateway or HTTPRoute objects via `r.Client.Get()` |
| `kuadrantv1` | If you create/manage RateLimitPolicy or AuthPolicy resources |
| `corev1` | If you interact with Secrets, ConfigMaps, or other core resources |

## SDK API reference

### ExtensionBase

Embed `types.ExtensionBase` in your reconciler struct to get `Logger`, `Client`, and `Scheme` fields. You **must** call `Configure(ctx)` at the start of every reconcile invocation to populate these fields from the context:

```go
type MyPolicyReconciler struct {
    types.ExtensionBase
}

func (r *MyPolicyReconciler) Reconcile(ctx context.Context, req reconcile.Request, kctx types.KuadrantCtx) (reconcile.Result, error) {
    if err := r.Configure(ctx); err != nil {
        return reconcile.Result{}, fmt.Errorf("failed to configure extension: %w", err)
    }
    // r.Logger, r.Client, r.Scheme are now available
    // ...
}
```

### KuadrantCtx interface

`KuadrantCtx` is passed to your reconcile function and provides access to the operator:

| Method | Signature | Purpose |
|--------|-----------|---------|
| `Resolve` | `(ctx, policy, celExpr string, subscribe bool) (ref.Val, error)` | Evaluate a CEL expression against the topology DAG |
| `ResolvePolicy` | `(ctx, policy, celExpr string, subscribe bool) (Policy, error)` | Same as Resolve but returns a Policy |

**The `subscribe` parameter:** When `subscribe` is `true`, the operator stores the CEL expression result and monitors the topology for changes. If a subsequent topology update causes the expression to produce a different result, the operator automatically re-triggers reconciliation for the extension. When `subscribe` is `false`, the expression is evaluated once (one-shot) with no further notifications. Use `subscribe: true` for expressions whose results depend on topology state that may change (e.g., gateway listeners, attached routes).
| `AddDataTo` | `(ctx, policy, domain Domain, binding string, celExpr string) error` | Register a data binding (CEL expression) for auth or request domain |
| `ReconcileObject` | `(ctx, existing, desired client.Object, mutateFn) (client.Object, error)` | Create or update a Kubernetes resource |
| `RegisterActionMethod` | `(ctx, policy, config ActionMethodConfig) error` | Register an external gRPC service for data-plane dispatch |
| `NewPipeline` | `(policy) Pipeline` | Create an action pipeline builder |

### Domains

Used with `AddDataTo` to specify where the binding is consumed:

| Domain | Consumed by | Use case |
|--------|-------------|----------|
| `types.DomainAuth` | Authorino (AuthConfig) | Authentication/authorization enrichment |
| `types.DomainRequest` | Envoy wasm-shim | Request/traffic data, metric labels |

### Action types

Used with the Pipeline API:

| Action | Purpose | Key fields |
|--------|---------|------------|
| `GRPCMethodAction` | Call a registered gRPC service | `Method`, `Var` (store response), `Predicate` |
| `DenyAction` | Deny the request/response | `WithStatus`, `WithHeaders`, `WithBody`, `Predicate` (all CEL) |
| `FailAction` | Log error and terminate action chain | `LogMessage`, `Predicate` |
| `AddHeadersAction` | Add headers to request/response | `HeadersToAdd` (CEL), `Predicate` |

### Status condition helpers

The SDK provides helpers for setting standard Gateway API policy conditions:

```go
import extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"

status := &v1alpha1.MyPolicyStatus{
    ObservedGeneration: pol.Generation,
    Conditions:         slices.Clone(pol.Status.Conditions),
}

// Policy was accepted (or rejected with error)
meta.SetStatusCondition(&status.Conditions, *extcontroller.AcceptedCondition(pol, err))

// Policy is enforced (fully or partially)
meta.SetStatusCondition(&status.Conditions, *extcontroller.EnforcedCondition(pol, err, true))

// Compare conditions for equality
marshaledJSON, _ := extcontroller.ConditionMarshal(status.Conditions)
```

### Generic Resolve helper

For type-safe CEL resolution without manual type conversion:

```go
import extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"

result, err := extcontroller.Resolve[MyStruct](ctx, kctx, policy, celExpression, subscribe)
```

## Reconciler patterns

### Pattern 1: Data bindings (simplest)

Inject data into the auth or request domain via CEL expressions. No external service needed.

**Use case:** Enrich requests with computed metadata, add custom metric labels, inject auth context.

**SDK methods used:** `AddDataTo`

**Built-in example:** TelemetryPolicy — publishes CEL label expressions to the request domain for request-time metrics.

```go
func (r *MyPolicyReconciler) Reconcile(ctx context.Context, req reconcile.Request, kctx types.KuadrantCtx) (reconcile.Result, error) {
    if err := r.Configure(ctx); err != nil {
        return reconcile.Result{}, fmt.Errorf("failed to configure extension: %w", err)
    }

    pol := &v1alpha1.MyPolicy{}
    if err := r.Client.Get(ctx, req.NamespacedName, pol); err != nil {
        return reconcile.Result{}, client.IgnoreNotFound(err)
    }
    if pol.GetDeletionTimestamp() != nil {
        return reconcile.Result{}, nil
    }

    // Publish a CEL expression to the request domain.
    // The expression is evaluated at request time by the data plane.
    if err := kctx.AddDataTo(ctx, pol, types.DomainRequest, "my-label", `request.headers["x-custom-id"]`); err != nil {
        return reconcile.Result{}, err
    }

    return reconcile.Result{}, nil
}
```

**Note:** The last argument to `AddDataTo` is a **CEL expression string**, not a resolved value. The expression is evaluated at request time by the data plane (Authorino or wasm-shim), not at reconcile time.

See: `cmd/extensions/telemetry-policy/internal/controller/telemetrypolicy_reconciler.go`

### Pattern 2: Pipeline with external gRPC service

Register an external gRPC backend and build an action pipeline that calls it at request time, then acts on the response (deny, fail, add headers).

**Use case:** Call an external service (threat assessment, fraud detection, AI moderation) and make routing decisions based on the response.

**SDK methods used:** `RegisterActionMethod`, `NewPipeline`, `Pipeline.OnHTTPRequest`, `Pipeline.OnHTTPResponse`, `Pipeline.Commit`

```go
func (r *MyPolicyReconciler) Reconcile(ctx context.Context, req reconcile.Request, kctx types.KuadrantCtx) (reconcile.Result, error) {
    if err := r.Configure(ctx); err != nil {
        return reconcile.Result{}, fmt.Errorf("failed to configure extension: %w", err)
    }

    pol := &v1alpha1.MyPolicy{}
    if err := r.Client.Get(ctx, req.NamespacedName, pol); err != nil {
        return reconcile.Result{}, client.IgnoreNotFound(err)
    }
    if pol.GetDeletionTimestamp() != nil {
        return reconcile.Result{}, nil
    }

    // Register an external gRPC service with the data plane.
    // The operator validates the service is reachable via gRPC reflection.
    if err := kctx.RegisterActionMethod(ctx, pol, types.ActionMethodConfig{
        Name:            "my-check",
        URL:             "grpc://my-service.my-namespace.svc.cluster.local:8080",
        Service:         "mypackage.v1.MyService",
        Method:          "Check",
        MessageTemplate: `mypackage.v1.CheckRequest{path: request.path, source_ip: source.address}`,
    }); err != nil {
        return reconcile.Result{}, err
    }

    // Build a pipeline of actions executed at request time.
    pipeline := kctx.NewPipeline(pol)

    if err := pipeline.OnHTTPRequest(
        // Call the gRPC service, store the response in a variable
        types.GRPCMethodAction{
            Method: "my-check",
            Var:    "checkResponse",
        },
        // Deny the request if the score exceeds a threshold
        types.DenyAction{
            Predicate:  `checkResponse.score > 80`,
            WithStatus: 403,
            WithBody:   "'Request blocked'",
        },
    ); err != nil {
        return reconcile.Result{}, err
    }

    if err := pipeline.OnHTTPResponse(
        // Add response headers with the check result
        types.AddHeadersAction{
            HeadersToAdd: `[["x-check-score", string(checkResponse.score)]]`,
        },
    ); err != nil {
        return reconcile.Result{}, err
    }

    // Commit atomically replaces all pipeline actions for this policy.
    if err := pipeline.Commit(ctx); err != nil {
        return reconcile.Result{}, err
    }

    return reconcile.Result{}, nil
}
```

**Important:** The external gRPC service must:
- Be deployed separately (its own Deployment + Service in the cluster)
- Support gRPC server reflection (the operator uses it to validate the service)
- Be reachable from the Envoy proxy (not from the extension — Envoy calls it at request time)

### Pattern 3: Topology queries and owned resources

Query the gateway topology via CEL and create/manage Kubernetes resources owned by your policy.

**Use case:** Dynamically create other Kuadrant policies (AuthPolicy, RateLimitPolicy) or Kubernetes resources based on the current gateway/route topology.

**SDK methods used:** `Resolve` (or generic `Resolve[T]`), `ResolvePolicy`, `ReconcileObject`, `AddDataTo`

**Built-in examples:**
- OIDCPolicy — resolves gateway info via CEL, creates AuthPolicy and HTTPRoute resources.
- PlanPolicy — creates RateLimitPolicy resources and publishes auth bindings.

```go
func (r *MyPolicyReconciler) Reconcile(ctx context.Context, req reconcile.Request, kctx types.KuadrantCtx) (reconcile.Result, error) {
    if err := r.Configure(ctx); err != nil {
        return reconcile.Result{}, fmt.Errorf("failed to configure extension: %w", err)
    }

    pol := &v1alpha1.MyPolicy{}
    if err := r.Client.Get(ctx, req.NamespacedName, pol); err != nil {
        return reconcile.Result{}, client.IgnoreNotFound(err)
    }
    if pol.GetDeletionTimestamp() != nil {
        return reconcile.Result{}, nil
    }

    // Query the topology via CEL — find the gateway this policy targets.
    // subscribe=true means the extension is re-reconciled when the result changes.
    gatewayInfo, err := extcontroller.Resolve[GatewayInfo](
        ctx, kctx, pol,
        `{"name": self.findGateways()[0].metadata.name,
          "namespace": self.findGateways()[0].metadata.namespace}`,
        true,
    )
    if err != nil {
        return reconcile.Result{}, err
    }

    // Create or update an owned resource.
    // The mutateFn determines if an existing resource needs updating.
    desired := buildDesiredResource(pol, gatewayInfo)
    if err := controllerutil.SetControllerReference(pol, desired, r.Scheme); err != nil {
        return reconcile.Result{}, err
    }
    _, err = kctx.ReconcileObject(ctx, &MyResource{}, desired, func(existing, desired client.Object) (bool, error) {
        e := existing.(*MyResource)
        d := desired.(*MyResource)
        if !reflect.DeepEqual(e.Spec, d.Spec) {
            e.Spec = d.Spec
            return true, nil
        }
        return false, nil
    })
    if err != nil {
        return reconcile.Result{}, err
    }

    // Optionally publish data bindings
    if err := kctx.AddDataTo(ctx, pol, types.DomainAuth, "my-binding", pol.BuildCelExpression()); err != nil {
        return reconcile.Result{}, err
    }

    return reconcile.Result{}, nil
}
```

**Available CEL functions for topology queries:**
- `self.findGateways()` — find Gateways targeted by the policy
- `self.findAuthPolicies()` — find AuthPolicies attached to the same targets
- `targetRef.findGateways()` — find Gateways from a specific target ref

See:
- `cmd/extensions/oidc-policy/internal/controller/oidcpolicy_reconciler.go`
- `cmd/extensions/plan-policy/internal/controller/planpolicy_reconciler.go`

### Combining patterns

The patterns above are not mutually exclusive. You can use any combination of SDK methods in a single reconciler. For example, a reconciler could query the topology via `Resolve` to discover gateway information, create owned resources via `ReconcileObject`, register an external gRPC service via `RegisterActionMethod`, build a pipeline, and publish data bindings via `AddDataTo` — all in the same `Reconcile` function.

The built-in PlanPolicy already combines patterns — it uses `ReconcileObject` to create a RateLimitPolicy and `AddDataTo` to publish auth bindings in the same reconcile cycle.

## Status management

All patterns should include proper status management. The SDK provides helpers that follow Gateway API conventions:

```go
func calculateErrorStatus(pol *v1alpha1.MyPolicy, specErr error) *v1alpha1.MyPolicyStatus {
    newStatus := &v1alpha1.MyPolicyStatus{
        ObservedGeneration: pol.Generation,
        Conditions:         slices.Clone(pol.Status.Conditions),
    }
    meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.AcceptedCondition(pol, specErr))
    meta.RemoveStatusCondition(&newStatus.Conditions, string(types.PolicyConditionEnforced))
    return newStatus
}

func calculateEnforcedStatus(pol *v1alpha1.MyPolicy, enforcedErr error) *v1alpha1.MyPolicyStatus {
    newStatus := &v1alpha1.MyPolicyStatus{
        ObservedGeneration: pol.Generation,
        Conditions:         slices.Clone(pol.Status.Conditions),
    }
    meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.AcceptedCondition(pol, nil))
    meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.EnforcedCondition(pol, enforcedErr, true))
    return newStatus
}
```

## RBAC

Add kubebuilder RBAC markers to your reconciler. At minimum, every extension needs:

```go
//+kubebuilder:rbac:groups=extensions.kuadrant.io,resources=mypolicies,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=extensions.kuadrant.io,resources=mypolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=extensions.kuadrant.io,resources=mypolicies/finalizers,verbs=update
```

Add additional markers if your reconciler interacts with other resources:

```go
// If creating RateLimitPolicy or AuthPolicy
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=create;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=authpolicies,verbs=get;create;list;watch;update;patch

// If reading Gateway API resources
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch
```

Run `make manifests` to generate the ClusterRole YAML from these markers.

## Concrete examples in this repo

| Extension | Pattern | SDK methods | Description |
|-----------|---------|-------------|-------------|
| [telemetry-policy](../../cmd/extensions/telemetry-policy/) | Data bindings | `AddDataTo` (DomainRequest) | Publishes CEL label expressions for request-time metrics |
| [plan-policy](../../cmd/extensions/plan-policy/) | Owned resources + bindings | `ReconcileObject`, `AddDataTo` (DomainAuth) | Creates RateLimitPolicy and publishes plan CEL expression for Authorino |
| [oidc-policy](../../cmd/extensions/oidc-policy/) | Topology queries + owned resources | `Resolve[T]`, `ReconcileObject` | Resolves gateway info via CEL, creates AuthPolicy and HTTPRoute |
