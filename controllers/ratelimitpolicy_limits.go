package controllers

import (
	"context"

	"github.com/go-logr/logr"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
)

func (r *RateLimitPolicyReconciler) reconcileLimits(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, targetNetworkObject client.Object) error {
	rlpRefs, err := r.TargetRefReconciler.GetAllGatewayPolicyRefs(ctx, rlp)
	if err != nil {
		return err
	}
	return r.reconcileLimitador(ctx, rlp, append(rlpRefs, client.ObjectKeyFromObject(rlp)), targetNetworkObject)
}

func (r *RateLimitPolicyReconciler) deleteLimits(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, targetNetworkObject client.Object) error {
	rlpRefs, err := r.TargetRefReconciler.GetAllGatewayPolicyRefs(ctx, rlp)
	if err != nil {
		return err
	}

	rlpRefsWithoutRLP := utils.Filter(rlpRefs, func(rlpRef client.ObjectKey) bool {
		return rlpRef.Name != rlp.Name || rlpRef.Namespace != rlp.Namespace
	})

	return r.reconcileLimitador(ctx, rlp, rlpRefsWithoutRLP, targetNetworkObject)
}

func (r *RateLimitPolicyReconciler) reconcileLimitador(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, rlpRefs []client.ObjectKey, targetNetworkObject client.Object) error {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("reconcileLimitador").WithValues("rlp refs", utils.Map(rlpRefs, func(ref client.ObjectKey) string { return ref.String() }))

	rateLimitIndex, err := r.buildRateLimitIndex(ctx, rlpRefs, targetNetworkObject)
	if err != nil {
		return err
	}

	// get the current limitador cr for the kuadrant instance so we can compare if it needs to be updated
	logger.V(1).Info("get kuadrant namespace")
	var kuadrantNamespace string
	kuadrantNamespace, isSet := kuadrant.GetKuadrantNamespaceFromPolicy(rlp)
	if !isSet {
		var err error
		kuadrantNamespace, err = kuadrant.GetKuadrantNamespaceFromPolicyTargetRef(ctx, r.Client(), rlp)
		if err != nil {
			logger.Error(err, "failed to get kuadrant namespace")
			return err
		}
		kuadrant.AnnotateObject(rlp, kuadrantNamespace)
		err = r.UpdateResource(ctx, rlp) // @guicassolato: not sure if this belongs to here
		if err != nil {
			logger.Error(err, "failed to update policy, re-queuing")
			return err
		}
	}
	limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantNamespace}
	limitador := &limitadorv1alpha1.Limitador{}
	err = r.Client().Get(ctx, limitadorKey, limitador)
	logger.V(1).Info("get limitador", "limitador", limitadorKey, "err", err)
	if err != nil {
		return err
	}

	// return if limitador is up to date
	if rlptools.Equal(rateLimitIndex.ToRateLimits(), limitador.Spec.Limits) {
		logger.V(1).Info("limitador is up to date, skipping update")
		return nil
	}

	// update limitador
	limitador.Spec.Limits = rateLimitIndex.ToRateLimits()
	err = r.UpdateResource(ctx, limitador)
	logger.V(1).Info("update limitador", "limitador", limitadorKey, "err", err)
	if err != nil {
		return err
	}

	return nil
}

func (r *RateLimitPolicyReconciler) buildRateLimitIndex(ctx context.Context, rlpRefs []client.ObjectKey, targetNetworkObject client.Object) (*rlptools.RateLimitIndex, error) {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("buildRateLimitIndex").WithValues("ratelimitpolicies", rlpRefs)

	rateLimitIndex := rlptools.NewRateLimitIndex()

	for _, rlpKey := range rlpRefs {
		if _, ok := rateLimitIndex.Get(rlpKey); ok {
			continue
		}

		rlp := &kuadrantv1beta2.RateLimitPolicy{}
		err := r.Client().Get(ctx, rlpKey, rlp)
		logger.V(1).Info("get rlp", "ratelimitpolicy", rlpKey, "err", err)
		if err != nil {
			return nil, err
		}

		if err := r.applyOverrides(ctx, rlp, targetNetworkObject); err != nil {
			return nil, err
		}

		rateLimitIndex.Set(rlpKey, rlptools.LimitadorRateLimitsFromRLP(rlp))
	}

	return rateLimitIndex, nil
}

// applyOverrides checks for any overrides set for the RateLimitPolicy.
// It iterates through the RateLimitPolicyList to find overrides for the provided target HTTPRoute.
// If an override is found, it updates the limits in the RateLimitPolicySpec in accordingly.
func (r *RateLimitPolicyReconciler) applyOverrides(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, targetNetworkObject client.Object) error {
	if route, ok := targetNetworkObject.(*gatewayapiv1.HTTPRoute); ok {
		rlpList := &kuadrantv1beta2.RateLimitPolicyList{}
		if err := r.Client().List(ctx, rlpList); err != nil {
			return err
		}

		for _, p := range rlpList.Items {
			clientKeys := gatewayapi.GetRouteAcceptedGatewayParentKeys(route)
			for _, clientKey := range clientKeys {
				if gatewayapi.IsTargetRefGateway(p.GetTargetRef()) &&
					clientKey.Name == string(p.Spec.TargetRef.Name) && clientKey.Namespace == p.Namespace {
					if p.Spec.Overrides != nil {
						rlp.Spec.CommonSpec().Limits = p.Spec.Overrides.Limits
					}
				}
			}
		}
	}

	return nil
}
