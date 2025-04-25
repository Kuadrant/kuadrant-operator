package types

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type KuadrantCtx interface{}

type ReconcileFn func(ctx context.Context, request reconcile.Request, kuadrant *KuadrantCtx) (reconcile.Result, error)
