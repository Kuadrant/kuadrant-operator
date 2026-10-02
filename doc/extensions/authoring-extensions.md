# Authoring Extensions with the Kuadrant Extensions Framework

> **Note**: The deployment model and SDK described here are the supported way to build an extension. The framework is not yet stable, so expect details to change on the way to GA.

## Introduction

An **extension** adds a new policy kind to Kuadrant. You define a CRD that expresses something in your own terms, and your extension turns each instance of it into whatever that implies: Kuadrant policies, Gateway API resources, or behaviour applied to live traffic.

An extension is an ordinary Kubernetes controller that you build, deploy and operate yourself. What makes it an extension is a gRPC connection to the Kuadrant operator, which gives it three things a plain controller cannot easily get:

- A view of the Gateway API topology the operator already maintains, queryable with CEL.
- A way to inject request-time logic into the data plane, meaning Authorino and Limitador.
- A way to add actions to the HTTP request and response path, including calls to your own gRPC service.

You do not fork or rebuild the Kuadrant operator to do this. Your extension is a separate image, deployed separately, running against a stock operator.

### Why build one

Consider an extension when:

- A workflow needs several Kuadrant policies coordinated behind a single interface. An OIDC policy, for example, might create an AuthPolicy and an HTTPRoute to implement an OAuth flow.
- Configuring what you create requires facts from the cluster, such as a gateway's listener hostname or assigned addresses.
- The decision you want to express depends on the request, not on the reconcile. Rate limits that vary by user tier, or metric labels drawn from a header, have to be evaluated per request.
- You want your own service consulted on the request path, and its answer to decide whether the request proceeds.

If none of those apply and you only need to create Kubernetes resources, write a normal controller. The SDK earns its place when you need the topology or the data plane.

### Reference examples

The [Kuadrant/example-extensions](https://github.com/Kuadrant/example-extensions) repository holds runnable extensions, including their deployment manifests. They are reference material rather than a library or a framework: read them, copy what is useful into a repository of your own, and build and release your extension from there. This guide refers to [`threat-policy`](https://github.com/Kuadrant/example-extensions/tree/main/threat-policy), which calls an external gRPC service on every request and blocks the ones it rates as dangerous.

## How extensions work

```
┌────────────────────────────┐                     ┌──────────────────────────────┐
│  Your extension pod        │                     │  Kuadrant operator pod       │
│                            │                     │                              │
│  MyPolicy reconciler       │   gRPC :50052       │  Topology                    │
│                            ├────────────────────>│    Gateways, Routes,         │
│  projected token           │   handshake,        │    Policies                  │
│    audience:               │   CEL queries,      │                              │
│    kuadrant-extensions     │   bindings,         │  Managed resources           │
│                            │   pipeline actions  │    AuthConfig, Limitador,    │
│  watches MyPolicy CRs      │                     │    wasm config               │
└────────────────────────────┘                     └──────────────────────────────┘
```

On startup the SDK connects to the operator and completes a handshake, where your extension proves its identity and claims the policy kind it manages. The SDK does this for you; what you provide is the configuration behind it, covered under [Deploying](#deploying). From then on your reconciler runs for every instance of your CRD exactly as a controller-runtime reconciler would, with one extra argument: a `KuadrantCtx` carrying the SDK methods.

Everything your extension tells the operator is scoped to the policy that told it. When a policy is deleted, the bindings and pipeline actions it published go with it.

### The topology

The operator maintains an in-memory graph of Gateway API resources and the policies attached to them. Your extension queries that graph by sending a CEL expression over the existing gRPC connection, so it needs no RBAC for Gateways, Routes or other policies, and adds no load to the API server.

That graph is also the limit of what the query functions can see. Anything else your extension needs to read, including its own CRs, is an ordinary client call with ordinary RBAC behind it.

## What the SDK gives you

These four methods are what distinguish an extension from a plain controller. Each is covered in full in the [Extension SDK developer guide](extension-sdk-developer-guide.md); this section is a tour of what they are for.

### Query the topology

`Resolve` evaluates a CEL expression against the topology. The generic helper converts the result into a Go type:

```go
type GatewayInfo struct {
    Hostname string `json:"hostname"`
    Protocol string `json:"protocol"`
}

gw, err := extcontroller.Resolve[GatewayInfo](ctx, kCtx, pol,
    `{"hostname": self.findGateways()[0].spec.listeners[0].hostname,
      "protocol": self.findGateways()[0].spec.listeners[0].protocol}`,
    true)
```

The final argument is `subscribe`. Pass `true` and the operator watches for the expression's result changing and re-triggers your reconcile when it does, which is what you want whenever the answer depends on cluster state that can move. Pass `false` for a one-off read.

`self` is the policy being reconciled. From there, `findGateways()` follows its target refs, and `findAuthPolicies()` finds AuthPolicies attached to the same targets.

### Publish data bindings

`AddDataTo` publishes a named CEL expression that the data plane evaluates on every request. The domain decides who receives it:

```go
// Authorino evaluates this per request; the result is available to later
// evaluators and downstream as dynamic metadata.
kCtx.AddDataTo(ctx, pol, types.DomainAuth, "plan",
    `auth.identity.metadata.annotations["plan-tier"]`)

// A metrics label in both Limitador and Authorino.
kCtx.AddDataTo(ctx, pol, types.DomainRequest,
    types.KuadrantMetricBinding("user_tier"),
    `request.headers["x-user-tier"]`)
```

The value is an expression, not a value you resolved at reconcile time. This is how an extension expresses a decision that has to be made per request.

### Manage resources

`ReconcileObject` creates or updates a resource your policy owns, in the role `controllerutil.CreateOrUpdate` plays in a plain controller:

```go
desired := buildAuthPolicy(pol, gw)
controllerutil.SetControllerReference(pol, desired, r.Scheme)

obj, err := kCtx.ReconcileObject(ctx, &kuadrantv1.AuthPolicy{}, desired, authPolicyMutator)
```

Set the owner reference and deleting your policy garbage-collects what it created. Note that the call returns a nil object when it had to create the resource rather than update one.

### Act on request traffic

A pipeline is an ordered list of actions the data plane runs on the request and response phases for the traffic your policy targets. Actions can call out to a gRPC service of your own, deny a request, add headers, or store a value for a later action to use.

Register the service first, then describe what should happen:

```go
kCtx.RegisterActionMethod(ctx, pol, types.ActionMethodConfig{
    Name:            "assess-threat",
    URL:             "grpc://threat-service.my-namespace:8081",
    Service:         "threat.v1.ThreatAssessmentService",
    Method:          "AssessRequest",
    MessageTemplate: `threat.v1.ThreatRequest{uri: request.path, source_ip: source.address}`,
})

pipeline := kCtx.NewPipeline(pol)
pipeline.OnHTTPRequest(
    types.GRPCAction{
        Method: "assess-threat",
        Var:    "threatResponse",
    },
    types.DenyAction{
        Predicate:   fmt.Sprintf("threatResponse.threat_level >= %d", pol.Spec.Threshold),
        WithStatus:  403,
        WithHeaders: `[["x-threat-blocked", "true"]]`,
        WithBody:    "'Request blocked: threat level exceeds threshold'",
    },
)
pipeline.Commit(ctx)
```

Nothing takes effect until `Commit`, which replaces the policy's actions as a unit and validates them. A reference to a variable no action produces, or a field that is not on the gRPC response message, fails the commit rather than misbehaving quietly on live traffic. The operator learns the response message by gRPC reflection, so your service needs reflection enabled.

## Building an extension

### 1. Define the CRD

An extension policy attaches to Gateway API resources using the Policy Attachment pattern, so your spec carries a `targetRef` alongside whatever your policy actually configures:

```go
type MyPolicySpec struct {
    TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`
    Threshold int                                                         `json:"threshold"`
}
```

Your type needs to satisfy the SDK's `Policy` interface, which is mostly `GetTargetRefs()` on top of the usual object methods. Generate deepcopy functions and CRD manifests as you would for any operator.

The Kind name matters beyond Kubernetes here: it is what your extension claims at handshake, and what an administrator grants it permission to register. Keep it unique across the cluster.

### 2. Write the reconciler

The signature is controller-runtime's with one addition:

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

    gw, err := extcontroller.Resolve[GatewayInfo](ctx, kCtx, pol,
        `{"hostname": self.findGateways()[0].spec.listeners[0].hostname}`, true)
    if err != nil {
        return reconcile.Result{}, err
    }

    desired := buildAuthPolicy(pol, gw)
    controllerutil.SetControllerReference(pol, desired, r.Scheme)
    if _, err := kCtx.ReconcileObject(ctx, &kuadrantv1.AuthPolicy{}, desired, authPolicyMutator); err != nil {
        return reconcile.Result{}, err
    }

    return r.reconcileStatus(ctx, pol)
}
```

Embedding `types.ExtensionBase` gives you `Configure`, which populates the logger, client and scheme from the context.

### 3. Wire up main.go

Use the SDK's builder rather than controller-runtime's. It looks deliberately similar, but it also establishes the gRPC connection and passes `KuadrantCtx` into your reconciler:

```go
func main() {
    reconciler := controller.NewMyPolicyReconciler()
    builder, logger := extcontroller.NewBuilder("my-policy-controller")

    extController, err := builder.
        WithScheme(scheme).
        WithReconciler(reconciler.Reconcile).
        For(&v1alpha1.MyPolicy{}).
        Owns(&kuadrantv1.AuthPolicy{}).
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

The type you pass to `For()` determines the policy kind your extension claims at handshake, taken from the Go type name.

For a complete, compiling version of all three steps, read [`threat-policy`](https://github.com/Kuadrant/example-extensions/tree/main/threat-policy) end to end.

## Deploying

Your extension is a Deployment you own, running as its own ServiceAccount, in any namespace you choose. It needs a Kuadrant operator v1.6.0 or later already running, with its extensions service reachable at `kuadrant-operator-extensions.kuadrant-system.svc:50052`.

Four things have to be in place.

**1. Permission to register the policy kind.** Which kind an extension may manage is a Kubernetes authorization decision, expressed as the `register` verb on the virtual `policyregistrations` resource, scoped by name to one kind:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-policy
  namespace: my-namespace
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: my-policy-register
rules:
  - apiGroups: ["extensions.kuadrant.io"]
    resources: ["policyregistrations"]
    resourceNames: ["MyPolicy"] # must match the Kind passed to For()
    verbs: ["register"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: my-policy-register
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: my-policy-register
subjects:
  - kind: ServiceAccount
    name: my-policy
    namespace: my-namespace
```

Nothing is ever stored under `policyregistrations`. It exists so that "may this ServiceAccount claim kind X" can be written as an ordinary RBAC rule. Revoking an extension means deleting this binding, which takes effect at its next handshake: a session already established keeps running until its connection drops or the operator restarts.

**2. Ordinary controller RBAC.** Your extension no longer inherits the operator's permissions, so it needs its own: its CRD and status, plus anything it creates such as AuthPolicies or HTTPRoutes. It does not need read access to Gateways or Routes if it only reaches them through topology queries.

**3. The endpoint and a token.** The SDK reads both from the environment. The token is a projected ServiceAccount token scoped to the `kuadrant-extensions` audience, which the kubelet rotates for you:

```yaml
spec:
  serviceAccountName: my-policy
  containers:
    - name: extension
      env:
        - name: KUADRANT_EXTENSION_ADDRESS
          value: kuadrant-operator-extensions.kuadrant-system.svc:50052
        - name: KUADRANT_EXTENSION_TOKEN_FILE
          value: /var/run/secrets/kuadrant/token
      volumeMounts:
        - name: kuadrant-token
          mountPath: /var/run/secrets/kuadrant
          readOnly: true
  volumes:
    - name: kuadrant-token
      projected:
        sources:
          - serviceAccountToken:
              path: token
              audience: kuadrant-extensions
              expirationSeconds: 3600
```

The audience matters. A token minted for anything else is rejected at handshake, which is also what stops a token from elsewhere being replayed against this endpoint.

**4. Network reachability.** The operator namespace ships with a default-deny ingress NetworkPolicy, so you need one permitting your extension pods to reach the controller manager on port 50052. The example repository's overlay includes one.

Install your CRD, apply the above, and watch the extension's logs for the handshake. The [standalone overlay](https://github.com/Kuadrant/example-extensions/tree/main/threat-policy) in the example repository assembles all four pieces and is applied with `kubectl apply -k config/deploy/standalone`.

To check it end to end, create an instance of your CRD targeting a Gateway or HTTPRoute and read its `status.conditions` for `Accepted` and `Enforced`.

## Design considerations

### Targeting and attachment

Your policy attaches to Gateway API resources, not to other policies. Extensions frequently create AuthPolicies and RateLimitPolicies, but they target Gateways, HTTPRoutes and GRPCRoutes. Use `findGateways()` to discover which gateway you ended up under and read what you need from its spec and status.

### Ownership

Set a controller reference on everything you create. Garbage collection then removes managed resources when the policy goes away, and you do not have to write cleanup logic.

### Status

Reconcile spec and status separately, and check the resources you created before claiming success. A policy whose AuthPolicy exists but is not enforced has not done its job:

```go
func isAuthPolicyEnforced(authPolicy *kuadrantv1.AuthPolicy) error {
    cond := meta.FindStatusCondition(authPolicy.Status.Conditions, string(types.PolicyConditionEnforced))
    if cond == nil || cond.Status == metav1.ConditionFalse {
        return fmt.Errorf("AuthPolicy %s is not enforced", authPolicy.Name)
    }
    return nil
}
```

### Reconnection

Connections drop and operators restart. The SDK reconnects and re-handshakes on its own, and re-reconciles your CRs afterwards so the operator's view is rebuilt. Write your reconciler to be idempotent, as you would any controller.

## Debugging

Extensions log through `logr`:

```go
r.Logger.Info("reconciling policy", "name", pol.Name, "namespace", pol.Namespace)
r.Logger.V(1).Info("resolved gateway", "gatewayInfo", gwInfo)
r.Logger.Error(err, "failed to reconcile AuthPolicy")
```

Set the level and format with environment variables:

```bash
LOG_LEVEL=debug       # debug, info, warn, error
LOG_MODE=development  # development or production
```

If nothing is reconciling at all, the handshake is the first thing to check. The operator logs a rejection with its reason, and the usual causes are a token with the wrong audience, a missing `register` grant, a `resourceNames` entry that does not match your Kind, or a NetworkPolicy blocking port 50052.

## Resources

- [Extension SDK developer guide](extension-sdk-developer-guide.md): full reference for the SDK API, reconciler patterns and project layout
- [Kuadrant/example-extensions](https://github.com/Kuadrant/example-extensions): reference extensions to read and copy from, with their deployment manifests
- [Gateway API Policy Attachment](https://gateway-api.sigs.k8s.io/geps/gep-713/)
- [Kuadrant's introduction to CEL](../cel/introduction.md)
- [CEL language definition](https://github.com/google/cel-spec)
- [Authorino documentation](https://docs.kuadrant.io/authorino/)
- [Limitador documentation](https://docs.kuadrant.io/limitador/)
