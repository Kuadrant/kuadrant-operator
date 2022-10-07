package apim

import (
	"context"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	"github.com/kuadrant/kuadrant-controller/pkg/rlptools"
)

const (
	// RateLimitPolicy finalizer
	rateLimitPolicyFinalizer = "ratelimitpolicy.kuadrant.io/finalizer"
)

func (r *RateLimitPolicyReconciler) finalizeRLP(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger, _ := logr.FromContext(ctx)
	logger.V(1).Info("Handling removal of ratelimitpolicy object")

	gatewayDiffObj, err := r.computeFinalizeGatewayDiff(ctx, rlp)
	if err != nil {
		return err
	}
	if gatewayDiffObj == nil {
		logger.V(1).Info("finalizeRLP: gatewayDiffObj is nil")
		return nil
	}

	if err := r.reconcileGatewayRLPReferences(ctx, rlp, gatewayDiffObj); err != nil {
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

	if err := r.deleteNetworkResourceBackReference(ctx, rlp); err != nil {
		return err
	}

	return nil
}

func (r *RateLimitPolicyReconciler) computeFinalizeGatewayDiff(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) (*gatewayDiff, error) {
	logger, _ := logr.FromContext(ctx)

	// Prepare gatewayDiff object only with LeftGateways list populated.
	// Used for the common reconciliation methods of Limits, EnvoyFilters, WasmPlugins, etc...
	gwDiff := &gatewayDiff{
		NewGateways:  nil,
		SameGateways: nil,
		LeftGateways: nil,
	}

	gwKeys, err := r.TargetedGatewayKeys(ctx, rlp.Spec.TargetRef, rlp.Namespace)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}

	for _, gwKey := range gwKeys {
		gw := &gatewayapiv1alpha2.Gateway{}
		err := r.Client().Get(ctx, gwKey, gw)
		logger.V(1).Info("finalizeRLP", "fetch gateway", gwKey, "err", err)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		gwDiff.LeftGateways = append(gwDiff.LeftGateways, rlptools.GatewayWrapper{Gateway: gw})
	}
	logger.V(1).Info("finalizeRLP", "#left-gw", len(gwDiff.LeftGateways))

	return gwDiff, nil
}

func (r *RateLimitPolicyReconciler) deleteNetworkResourceBackReference(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	return r.DeleteTargetBackReference(ctx, client.ObjectKeyFromObject(rlp), rlp.Spec.TargetRef,
		rlp.Namespace, common.RateLimitPolicyBackRefAnnotation)
}
