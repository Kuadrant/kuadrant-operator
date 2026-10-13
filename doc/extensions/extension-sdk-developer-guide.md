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
    │   │   └── kustomization.yaml       # kustomize bases for deploying the extension
    │   └── rbac/
    │       ├── kustomization.yaml
    │       └── role.yaml
    └── internal/
        └── controller/
            └── mypolicy_reconciler.go

This is the generic shape — for a complete, runnable extension you can deploy
as its own workload (`Deployment`, `ServiceAccount`, RBAC, `NetworkPolicy`
under `config/deploy/standalone/`), see the [Kuadrant/example-extensions](https://github.com/Kuadrant/example-extensions)
repo's README and the `threat-policy` example.

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

- Your CRD's API group (declared via `+groupName` in `groupversion_info.go`) must be `extensions.kuadrant.io` — this is required by the SDK, not just a convention; the operator's extension registration looks for policy types under that group.
- The SDK supports both singular and plural target references: use `TargetRef` (singular) with `GetTargetRefs()` wrapping it in a single-element slice for the common case, or `TargetRefs` (plural, a slice) if your policy needs to target multiple resources.
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

The builder mirrors controller-runtime's own (`ctrl.NewControllerManagedBy(mgr).For(...).Watches(...).Owns(...)`) — `WithScheme`, `For`, `Watches`, and `Owns` behave exactly as they do in any kubebuilder project.

What's different:

| Method               | Purpose                                                                                                                                       |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `WithReconciler(fn)` | Sets the reconcile function — its signature takes an extra `types.KuadrantCtx` argument (see [KuadrantCtx interface](#kuadrantctx-interface)) |
| `Build()`            | Validates config and connects to the operator over gRPC instead of just assembling a `ctrl.Manager` — returns `*ExtensionController`          |

### Scheme registration

Registering schemes works exactly like any controller-runtime project — nothing here is specific to the extension SDK. Register whatever your reconciler's client needs, e.g.:

| Scheme          | When to register                                                  |
| --------------- | ----------------------------------------------------------------- |
| Your `v1alpha1` | Always (your own CRD types)                                       |
| `gwapiv1`       | If you read Gateway or HTTPRoute objects via `r.Client.Get()`     |
| `kuadrantv1`    | If you create/manage RateLimitPolicy or AuthPolicy resources      |
| `corev1`        | If you interact with Secrets, ConfigMaps, or other core resources |

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

| Method                 | Signature                                                                 | Purpose                                                                                                                       |
| ---------------------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `Resolve`              | `(ctx, policy, celExpr string, subscribe bool) (ref.Val, error)`          | Evaluate a CEL expression against the topology DAG                                                                            |
| `ResolvePolicy`        | `(ctx, policy, celExpr string, subscribe bool) (Policy, error)`           | Same as Resolve but returns a Policy                                                                                          |
| `AddDataTo`            | `(ctx, policy, domain Domain, binding string, celExpr string) error`      | Register a data binding (CEL expression) for auth or request domain                                                           |
| `ReconcileObject`      | `(ctx, existing, desired client.Object, mutateFn) (client.Object, error)` | Create or update a Kubernetes resource                                                                                        |
| `RegisterActionMethod` | `(ctx, policy, config ActionMethodConfig) error`                          | Register an external gRPC service for data-plane dispatch (see [Action methods and pipelines](#action-methods-and-pipelines)) |
| `NewPipeline`          | `(policy) Pipeline`                                                       | Create an action pipeline builder (see [Action methods and pipelines](#action-methods-and-pipelines))                         |

### Resolve and ResolvePolicy

`Resolve` evaluates a CEL expression against the operator's topology and returns the result. `ResolvePolicy` is for expressions that yield a single policy object, returning it as a `types.Policy`.

**The `subscribe` parameter:** with `subscribe: true` the operator remembers the result and watches the topology, re-triggering your reconcile whenever the expression would produce something different. With `false` it evaluates once and tells you nothing further. Subscribe when the result depends on topology that can change, such as gateway listeners or attached routes.

`extcontroller.Resolve[T]` wraps the call and converts the result into a Go type of your choosing:

```go
import extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"

gwInfo, err := extcontroller.Resolve[GatewayInfo](ctx, kctx, pol,
    `{"name": self.findGateways()[0].metadata.name}`, true)
```

CEL does the conversion, and errors if the result does not fit `T`.

#### Topology CEL functions

The operator evaluates these expressions against its topology: the graph of Gateways, Routes and attached policies it already maintains. On top of standard CEL, these are available:

| Symbol                     | Type                  | Returns                                                   |
| -------------------------- | --------------------- | --------------------------------------------------------- |
| `self`                     | `kuadrant.v1.Policy`  | The policy you passed to `Resolve`                        |
| `self.findGateways()`      | member on `Policy`    | `[Gateway]` matching the policy's target refs             |
| `targetRef.findGateways()` | member on `TargetRef` | `[Gateway]` matching that one target ref                  |
| `self.findAuthPolicies()`  | member on `Policy`    | `[Policy]`, the AuthPolicies attached to the same targets |

Results are proto objects (`kuadrant.v1.Gateway`, `kuadrant.v1.Policy`), so you can walk the spec and status of what you find:

```go
// Addresses of every gateway this policy attaches to
val, err := kctx.Resolve(ctx, pol,
    `self.findGateways().map(g, g.status.addresses.map(a, a.value)).flatten()`, false)

// Is an AuthPolicy already attached to the same target?
val, err = kctx.Resolve(ctx, pol, `self.findAuthPolicies().size() > 0`, false)

// Gateways for one specific target ref
val, err = kctx.Resolve(ctx, pol, `self.targetRefs[0].findGateways()`, false)
```

The operator answers these from its own topology, so your extension needs no RBAC for anything it finds that way. That is also the limit of what the functions see: only Gateways, Routes and policies the operator already tracks. Reading anything else, including resources of your own, still needs RBAC and a client call. The function set may grow; `pkg/cel/ext/kuadrant.go` is the current source of truth.

### AddDataTo and domains

`AddDataTo` publishes a binding: a named CEL expression the data plane evaluates per request. The domain decides who consumes it:

| Domain                | Consumed by                                            | Use case                                               |
| --------------------- | ------------------------------------------------------ | ------------------------------------------------------ |
| `types.DomainAuth`    | Authorino (AuthConfig)                                 | Authentication/authorization enrichment                |
| `types.DomainRequest` | Limitador, Authorino, and metrics (via the data plane) | Metric labels and per-request values for rate limiting |

`DomainAuth` bindings become metadata evaluators on the managed AuthConfig, so the result is available to later Authorino evaluators and as dynamic metadata downstream. `DomainRequest` bindings go into the managed wasm configuration, so the result is forwarded per request to Authorino and Limitador. The operator re-renders those managed resources when a binding changes; it never modifies user-authored policies such as AuthPolicy or RateLimitPolicy.

Binding names are plain strings; a dotted name is a convention, not a nested structure. Two helpers produce names the data plane treats specially:

| Helper                                 | Produces              | Effect                                      |
| -------------------------------------- | --------------------- | ------------------------------------------- |
| `types.KuadrantMetricBinding("name")`  | `metrics.labels.name` | Authorino emits the value as a metric label |
| `types.KuadrantLoggingBinding("name")` | `logging.fields.name` | Authorino adds the value as a log field     |

Those prefixes only govern Authorino. Limitador labels its metrics with every `DomainRequest` binding it receives, prefixed or not, so a binding you published for some other purpose still shows up there. Keep high-cardinality values out of `DomainRequest`. Logging bindings are the exception: they are only ever sent to Authorino.

### ReconcileObject

Creates or updates a Kubernetes resource your policy manages, in the role `controllerutil.CreateOrUpdate` plays in a plain controller:

```go
desired := buildAuthPolicy(pol)
if err := controllerutil.SetControllerReference(pol, desired, r.Scheme); err != nil {
    return reconcile.Result{}, err
}

obj, err := kctx.ReconcileObject(ctx, &kuadrantv1.AuthPolicy{}, desired, func(existing, desired client.Object) (bool, error) {
    e, d := existing.(*kuadrantv1.AuthPolicy), desired.(*kuadrantv1.AuthPolicy)
    if reflect.DeepEqual(e.Spec, d.Spec) {
        return false, nil
    }
    e.Spec = d.Spec
    return true, nil
})
```

The second argument is an empty object for the lookup to read into; `desired` supplies the name and namespace.

- Nothing at that key: `desired` is created and the call returns a **nil object**, so check before dereferencing.
- Something there: your `MutateFn` runs. Copy what you care about onto `existing` and return `true` to update, `false` to leave it alone.

Set an owner reference on `desired` before calling, so deleting your policy garbage-collects what it created. To delete a managed resource explicitly, annotate `desired` with `kuadrant.io/delete: "true"`.

### Action methods and pipelines

A pipeline is an ordered list of actions the data plane runs on the HTTP request and response phases for the traffic your policy targets. Actions that call out to your own gRPC service need that service registering first.

#### Registering an action method

`RegisterActionMethod` makes an external gRPC service callable from a pipeline. The operator resolves the service by gRPC reflection and creates the Envoy cluster and wasm service entries for it:

```go
err := kctx.RegisterActionMethod(ctx, pol, types.ActionMethodConfig{
    Name:            "assess-threat",
    URL:             "grpc://threat-service.my-namespace.svc.cluster.local:8080",
    Service:         "threat.v1.ThreatAssessmentService",
    Method:          "AssessRequest",
    MessageTemplate: `threat.v1.ThreatRequest{uri: request.path, source_ip: source.address}`,
})
```

`Name` is the handle a `GRPCAction` refers to and only needs to be unique within the policy. `MessageTemplate` constructs the request message: a proto message literal whose field values are CEL expressions over the request attributes.

Envoy calls this service at request time, not your extension, so it must be reachable from the proxy. It must also serve gRPC reflection, which the operator uses to validate the registration and the response fields your actions reference. If the operator cannot reach it, the call returns an error wrapping `types.ErrUpstreamUnreachable`; test for it with `errors.Is` to report a distinct status condition.

#### Action types

| Action             | Purpose                                                                   | Key fields                                                     |
| ------------------ | ------------------------------------------------------------------------- | -------------------------------------------------------------- |
| `GRPCAction`       | Call a registered gRPC service                                            | `Method`, `Var` (store response), `Predicate`                  |
| `DenyAction`       | Deny the request/response                                                 | `WithStatus`, `WithHeaders`, `WithBody`, `Predicate` (all CEL) |
| `FailAction`       | Log error and terminate the action chain                                  | `LogMessage`, `Predicate`                                      |
| `AddHeadersAction` | Add headers to request/response                                           | `HeadersToAdd` (CEL), `Predicate`                              |
| `StoreAction`      | Store a CEL-evaluated value under a path for later actions (or export it) | `Path`, `Value` (CEL), `ExportToHost`, `Predicate`             |

Every action takes an optional `Predicate`, a CEL expression gating whether it runs. With no predicate, it always runs.

`DenyAction` and `AddHeadersAction` mean different things in each phase. In the request phase, a deny responds to the client without the request ever reaching the backend, and added headers are seen by the backend. In the response phase, a deny replaces the backend's response, and added headers are seen by the client.

#### Phases and ordering

```go
pipeline := kctx.NewPipeline(pol)
pipeline.OnHTTPRequest(action, action, ...)
pipeline.OnHTTPResponse(action, ...)
pipeline.Commit(ctx)
```

Actions run in the order you add them, within the phase you add them to. All request-phase actions must be added before any response-phase ones; calling `OnHTTPRequest` after `OnHTTPResponse` is rejected.

#### Variables

`GRPCAction.Var` names a variable holding that call's response; `StoreAction.Path` names one holding the result of any CEL expression. Later actions reference either by name in their own CEL expressions, including across the phase boundary: a response-phase action can read a variable a request-phase gRPC call populated.

The builder validates these references as you add actions, but it only tracks the variables gRPC actions populate. It rejects:

- referencing a variable before the action that populates it
- referencing variables from two different gRPC actions in one expression, since a response variable is only in scope within its own reply hook
- reusing a variable name already populated in the pipeline
- a `FailAction` that references no gRPC response variable, since it exists to handle a bad reply

The paths a `StoreAction` populates are not tracked, so neither referencing one before the store that fills it nor reusing a path name is caught here. Those mistakes reach the data plane instead of failing the commit, so keep your path names distinct and order store actions ahead of whatever reads them.

Setting `ExportToHost: true` on a `StoreAction` additionally makes the stored value visible outside the pipeline, to subsequent Envoy filters in the chain.

#### Commit

`Commit` sends the whole pipeline in one call and atomically replaces whatever that policy had registered before, so a reconcile that rebuilds the pipeline from scratch each time converges with no stale actions left behind. Nothing reaches the operator until you commit; the builder calls only accumulate locally.

The operator validates the committed pipeline and rejects it as a whole. On top of the ordering rules the builder already enforced, it checks that every CEL expression parses, that every `GRPCAction.Method` names an action method registered for this policy, and that `DenyAction.WithStatus` is a valid HTTP status code.

It also checks field accesses on a gRPC response variable against that method's response message, using the proto descriptor it obtained by reflection. So if the service returns a `tier` field and an action reads `planResponse.teir`, the commit fails with that error rather than the misspelled field quietly evaluating to nothing on every request.

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

## Environment

The SDK reads its configuration from the environment; none of it is passed on the command line.

| Variable                        | Default                           | Purpose                                               |
| ------------------------------- | --------------------------------- | ----------------------------------------------------- |
| `KUADRANT_EXTENSION_ADDRESS`    | none, required                    | `host:port` of the operator's extensions gRPC service |
| `KUADRANT_EXTENSION_TOKEN_FILE` | `/var/run/secrets/kuadrant/token` | ServiceAccount token presented when connecting        |
| `LOG_LEVEL`                     | `info`                            | `debug`, `info`, `warn` or `error`                    |
| `LOG_MODE`                      | `production`                      | `development` for human-readable logs                 |

`Build()` fails immediately if `KUADRANT_EXTENSION_ADDRESS` is unset. See [Authoring Extensions](authoring-extensions.md) for the Deployment that supplies these.

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
    if err := kctx.AddDataTo(ctx, pol, types.DomainRequest, types.KuadrantMetricBinding("my-label"), `request.headers["x-custom-id"]`); err != nil {
        return reconcile.Result{}, err
    }

    return reconcile.Result{}, nil
}
```

**Note:** The last argument to `AddDataTo` is a **CEL expression string**, not a resolved value. The expression is evaluated at request time by the data plane (Authorino or wasm-shim), not at reconcile time.

**Note:** Authorino only emits a binding as a metric if its name is qualified with `types.KuadrantMetricBinding(...)`. Limitador ignores the prefix and labels its metrics with every `DomainRequest` binding it receives. See [AddDataTo and domains](#adddatato-and-domains).

See: [`cmd/extensions/telemetry-policy/internal/controller/telemetrypolicy_reconciler.go`](https://github.com/Kuadrant/kuadrant-operator/blob/main/cmd/extensions/telemetry-policy/internal/controller/telemetrypolicy_reconciler.go)

### Pattern 2: Pipeline with external gRPC service

Register an external gRPC service and build an action pipeline that calls it on matching requests, then acts on that service's reply (deny, fail, add headers).

**Use case:** Call an external service (threat assessment, fraud detection, AI moderation) and let its reply influence the request flow.

**SDK methods used:** `RegisterActionMethod`, `NewPipeline`, `Pipeline.OnHTTPRequest`, `Pipeline.OnHTTPResponse`, `Pipeline.Commit`

**Example:** [ThreatPolicy](https://github.com/Kuadrant/example-extensions/tree/main/threat-policy), in the [Kuadrant/example-extensions](https://github.com/Kuadrant/example-extensions) repo — registers an external threat-assessment gRPC service and denies requests whose returned threat level crosses a configurable threshold.

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
        types.GRPCAction{
            Method: "my-check",
            Var:    "checkResponse",
        },
        // Deny the request if the score exceeds a threshold
        types.DenyAction{
            Predicate:  `checkResponse.score > 80`,
            WithStatus: 403,
            WithBody:   "'Request blocked'",
        },
        // Persist the score under a path so it survives past this gRPC call
        // and, with ExportToHost, is visible outside the pipeline (e.g. to
        // subsequent Envoy filters in the chain).
        types.StoreAction{
            Path:         "threat.score",
            Value:        "checkResponse.score",
            ExportToHost: true,
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

The external gRPC service is deployed separately, as its own Deployment and Service. See [Action methods and pipelines](#action-methods-and-pipelines) for what it must support and for the rules the pipeline is validated against.

See: [`threat-policy/internal/controller/threatpolicy_reconciler.go`](https://github.com/Kuadrant/example-extensions/blob/main/threat-policy/internal/controller/threatpolicy_reconciler.go)

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

See [Topology CEL functions](#topology-cel-functions) for what the expression can query.

See:

- [`cmd/extensions/oidc-policy/internal/controller/oidcpolicy_reconciler.go`](https://github.com/Kuadrant/kuadrant-operator/blob/main/cmd/extensions/oidc-policy/internal/controller/oidcpolicy_reconciler.go)
- [`cmd/extensions/plan-policy/internal/controller/planpolicy_reconciler.go`](https://github.com/Kuadrant/kuadrant-operator/blob/main/cmd/extensions/plan-policy/internal/controller/planpolicy_reconciler.go)

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

## Concrete examples in the kuadrant-operator repo

| Extension                                                                                                   | Pattern                            | SDK methods                                 | Description                                                             |
| ----------------------------------------------------------------------------------------------------------- | ---------------------------------- | ------------------------------------------- | ----------------------------------------------------------------------- |
| [telemetry-policy](https://github.com/Kuadrant/kuadrant-operator/tree/main/cmd/extensions/telemetry-policy) | Data bindings                      | `AddDataTo` (DomainRequest)                 | Publishes CEL label expressions for request-time metrics                |
| [plan-policy](https://github.com/Kuadrant/kuadrant-operator/tree/main/cmd/extensions/plan-policy)           | Owned resources + bindings         | `ReconcileObject`, `AddDataTo` (DomainAuth) | Creates RateLimitPolicy and publishes plan CEL expression for Authorino |
| [oidc-policy](https://github.com/Kuadrant/kuadrant-operator/tree/main/cmd/extensions/oidc-policy)           | Topology queries + owned resources | `Resolve[T]`, `ReconcileObject`             | Resolves gateway info via CEL, creates AuthPolicy and HTTPRoute         |

For an example of the pipeline pattern (Pattern 2), see [ThreatPolicy](https://github.com/Kuadrant/example-extensions/tree/main/threat-policy) in the standalone [Kuadrant/example-extensions](https://github.com/Kuadrant/example-extensions) repo.
