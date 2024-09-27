package controllers

import (
	"context"
	"sync"

	"github.com/samber/lo"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
)

type rateLimitPolicyValidator struct{}

func (r *rateLimitPolicyValidator) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Validate,
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1beta3.RateLimitPolicyGroupKind},
		},
	}
}

func (r *rateLimitPolicyValidator) Validate(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("rateLimitPolicyValidator")

	policies := topology.Policies().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == kuadrantv1beta3.RateLimitPolicyGroupKind
	})

	logger.V(1).Info("validating rate limit policies", "policies", len(policies))

	state.Store(StateRateLimitPolicyValid, lo.SliceToMap(policies, func(policy machinery.Policy) (string, error) {
		var err error
		if len(policy.GetTargetRefs()) > 0 && len(topology.Targetables().Children(policy)) == 0 {
			err = ErrMissingTarget
		}
		return policy.GetLocator(), err
	}))

	logger.V(1).Info("finished validating rate limit policies")

	return nil
}
