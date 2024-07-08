package rlptools

import (
	"github.com/go-logr/logr"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
)

func ApplyOverrides(routePolicy *kuadrantv1beta2.RateLimitPolicy, gwPolicies []*kuadrantv1beta2.RateLimitPolicy, logger logr.Logger) *kuadrantv1beta2.RateLimitPolicy {
	if routePolicy == nil {
		logger.V(1).Info("route policy is null")
		return nil
	}

	if len(gwPolicies) == 0 {
		logger.V(1).Info("no gateway polcies, no override")
		return routePolicy
	}

	// Currently, only supporting one
	gwPolicy := gwPolicies[0]

	if gwPolicy.Spec.Overrides == nil {
		logger.V(1).Info("gateway policy does not override")
		return routePolicy
	}

	logger.V(1).Info("gateway policy override applied")
	overriddenPolicy := routePolicy.DeepCopyObject().(*kuadrantv1beta2.RateLimitPolicy)
	overriddenPolicy.Spec.CommonSpec().Limits = gwPolicy.Spec.Overrides.Limits

	return overriddenPolicy
}
