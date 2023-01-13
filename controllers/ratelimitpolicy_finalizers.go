package controllers

import (
	"context"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

const (
	// RateLimitPolicy finalizer
	rateLimitPolicyFinalizer = "ratelimitpolicy.kuadrant.io/finalizer"
)

func (r *RateLimitPolicyReconciler) finalizeRLP(ctx context.Context, rlp *kuadrantv1beta1.RateLimitPolicy) error {
	logger, _ := logr.FromContext(ctx)
	logger.V(1).Info("Handling removal of ratelimitpolicy object")

	gatewayDiffObj, err := r.ComputeFinalizeGatewayDiff(ctx, rlp, &common.KuadrantRateLimitPolicyRefsConfig{})
	if err != nil {
		return err
	}
	if gatewayDiffObj == nil {
		logger.V(1).Info("finalizeRLP: gatewayDiffObj is nil")
		return nil
	}

	if err := r.ReconcileGatewayPolicyReferences(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	if err := r.reconcileWASMPluginConf(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	if err := r.reconcileRateLimitingClusterEnvoyFilter(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	if err := r.reconcileLimits(ctx, rlp, gatewayDiffObj); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	if err := r.deleteTargetBackReference(ctx, rlp); err != nil {
		return err
	}

	return nil
}

func (r *RateLimitPolicyReconciler) deleteTargetBackReference(ctx context.Context, rlp *kuadrantv1beta1.RateLimitPolicy) error {
	return r.DeleteTargetBackReference(ctx, client.ObjectKeyFromObject(rlp), rlp.Spec.TargetRef, rlp.Namespace, common.RateLimitPolicyBackRefAnnotation)
}
