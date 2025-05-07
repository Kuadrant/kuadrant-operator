package types

import (
	"context"

	celref "github.com/google/cel-go/common/types/ref"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type Policy interface {
	client.Object
	GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName
}

type KuadrantCtx interface {
	Resolve(context.Context, Policy, string, bool) (celref.Val, error)
}

type ReconcileFn func(ctx context.Context, request reconcile.Request, kuadrant KuadrantCtx) (reconcile.Result, error)
