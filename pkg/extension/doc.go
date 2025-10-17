// Package extension provides the public surface for writing Kuadrant policy
// extensions.
//
// An extension is a lightweight controller process which connects (via the
// grpc sub‑package) to the Kuadrant extension service over a unix domain
// socket. Through this connection an extension can:
//  1. Resolve CEL expressions against an in‑memory view of policies and
//     gateway topology (see the cel/ext package for available functions like
//     findGateways and findAuthPolicies).
//  2. Register mutators that enrich policy documents with computed data.
//  3. Subscribe to evaluation or object change events and trigger
//     reconciliation.
//  4. Reconcile and persist Kubernetes objects related to a custom policy
//     kind.
//
// Typical Usage
//
//	b, log := controller.NewBuilder("my-policy")
//	b.WithScheme(scheme).
//	  For(&myv1.MyPolicy{}).
//	  Watches(&corev1.Secret{}).
//	  WithReconciler(func(ctx context.Context, req reconcile.Request, kctx types.KuadrantCtx) (reconcile.Result, error) {
//	      // 1. Fetch the policy instance using kctx.Client() (available via context)
//	      // 2. Resolve CEL expressions:
//	      //    v, err := kctx.Resolve(ctx, policy, "self.findGateways()", false)
//	      // 3. Create/update owned objects:
//	      //    obj, err := kctx.ReconcileObject(ctx, existing, desired, mutateFn)
//	      // 4. Return standard controller-runtime reconcile.Result
//	      return reconcile.Result{}, err
//	  })
//	ctrl, _ := b.Build()
//	_ = ctrl.Start(ctx)
//
// Sub‑packages
//
//	controller - Builder + runtime controller that wires watches, reconcile
//	             invocation, finalizers, gRPC subscription and event caching.
//	types      - Interfaces (Policy, KuadrantCtx, ReconcileFn) and helpers
//	             shared between implementations and the controller.
//	utils      - Context helper accessors for logger, client and scheme.
//	grpc       - Protobuf generated types and small adapters (do not edit).
//
// See also the cel/ext package which defines CEL functions and type registry
// exposed to policy CEL expressions evaluated through KuadrantCtx.Resolve.
package extension
