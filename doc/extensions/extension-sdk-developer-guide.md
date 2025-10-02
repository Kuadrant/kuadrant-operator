# Kuadrant Extension SDK — Developer Guide

This guide shows the minimal scaffold to build an extension: CRD, reconciler, and main wiring.

## Project structure

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

## Policy type (CRD)

```go
package v1alpha1

import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type MyPolicySpec struct {
    TargetRefs []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRefs"`
    CustomConfig string `json:"customConfig,omitempty"`
}

type MyPolicyStatus struct {
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

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

## Reconciler

```go
package controller

import (
    "context"

    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"
    "github.com/kuadrant/kuadrant-operator/pkg/extension/types"
    "your-module/api/v1alpha1"
)

type MyPolicyReconciler struct { types.ExtensionBase }

func NewMyPolicyReconciler() *MyPolicyReconciler { return &MyPolicyReconciler{} }

func (r *MyPolicyReconciler) Reconcile(ctx context.Context, req reconcile.Request, kctx types.KuadrantCtx) (reconcile.Result, error) {
    pol := &v1alpha1.MyPolicy{}
    if err := r.Client.Get(ctx, req.NamespacedName, pol); err != nil { return reconcile.Result{}, client.IgnoreNotFound(err) }

    // Compute via CEL
    addrs, err := kctx.Resolve(ctx, pol, `self.findGateways().map(g, g.status.addresses.map(a, a.value)).flatten()`, false)
    if err != nil { return reconcile.Result{}, err }

    // Publish a binding for request-time evaluation downstream
    _ = kctx.AddDataTo(ctx, pol, types.DomainRequest, "gateway.addresses", addrs)

    return reconcile.Result{}, nil
}
```

## Main

```go
package main

import (
    "os"

    corev1 "k8s.io/api/core/v1"
    k8sruntime "k8s.io/apimachinery/pkg/runtime"
    utilruntime "k8s.io/apimachinery/pkg/util/runtime"
    ctrl "sigs.k8s.io/controller-runtime"
    gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

    kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
    extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
    "your-module/api/v1alpha1"
    "your-module/internal/controller"
)

var scheme = k8sruntime.NewScheme()

func init() {
    utilruntime.Must(corev1.AddToScheme(scheme))
    utilruntime.Must(gatewayapiv1alpha2.Install(scheme))
    utilruntime.Must(kuadrantv1.AddToScheme(scheme))
    utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
    r := controller.NewMyPolicyReconciler()
    b, logger := extcontroller.NewBuilder("my-policy-controller")
    c, err := b.WithScheme(scheme).WithReconciler(r.Reconcile).For(&v1alpha1.MyPolicy{}).Owns(&corev1.ConfigMap{}).Build()
    if err != nil { logger.Error(err, "unable to create controller"); os.Exit(1) }
    if err = c.Start(ctrl.SetupSignalHandler()); err != nil { logger.Error(err, "unable to start extension controller"); os.Exit(1) }
}
```

## Concrete examples in this repo

- Plan policy: publishes a CEL program to DomainAuth (`plan`) that is evaluated by Authorino at request time.
  - See: `cmd/extensions/plan-policy/internal/controller/planpolicy_reconciler.go` (AddDataTo with `types.DomainAuth`, key `plan`).
- Telemetry policy: publishes CEL label expressions to DomainRequest for request-time metrics.
  - See: `cmd/extensions/telemetry-policy/internal/controller/telemetrypolicy_reconciler.go` (AddDataTo with `types.DomainRequest`, `types.KuadrantMetricBinding(...)`).
