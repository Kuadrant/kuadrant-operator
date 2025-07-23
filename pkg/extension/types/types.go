package types

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	celref "github.com/google/cel-go/common/types/ref"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type Policy interface {
	GetName() string
	GetNamespace() string
	GetObjectKind() schema.ObjectKind
	GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName
}

type KuadrantCtx interface {
	Resolve(context.Context, Policy, string, bool) (celref.Val, error)
	ResolvePolicy(context.Context, Policy, string, bool) (Policy, error)
	AddDataTo(context.Context, Policy, Policy, string, string) error
	ClearPolicy(context.Context, Policy) error
	GetClient() client.Client
	GetScheme() *runtime.Scheme
	ReconcileKuadrantResource(context.Context, client.Object, client.Object, MutateFn) error
}

type ReconcileFn func(ctx context.Context, request reconcile.Request, kuadrant KuadrantCtx) (reconcile.Result, error)

// MutateFn is a function which mutates the existing object into it's desired state.
type MutateFn func(existing, desired client.Object) (bool, error)
