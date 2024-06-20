package controllers

import (
	"context"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

func (r *RateLimitPolicyReconciler) reconcileDirectBackReferences(ctx context.Context, topology *kuadrantgatewayapi.Topology) error {
	return nil
}
