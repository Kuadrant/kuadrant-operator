package controllers

import (
	"context"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	kuadrant "github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

type rateLimitPolicyValidator struct{}

func (r *rateLimitPolicyValidator) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Validate,
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1beta3.RateLimitPolicyGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1beta3.RateLimitPolicyGroupKind, EventType: ptr.To(controller.UpdateEvent)},
		},
	}
}

func (r *rateLimitPolicyValidator) Validate(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("rateLimitPolicyValidator")

	policies := topology.Policies().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == kuadrantv1beta3.RateLimitPolicyGroupKind
	})

	logger.V(1).Info("validating rate limit policies", "policies", len(policies))
	defer logger.V(1).Info("finished validating rate limit policies")

	state.Store(StateRateLimitPolicyValid, lo.SliceToMap(policies, func(policy machinery.Policy) (string, error) {
		var err error
		if len(policy.GetTargetRefs()) > 0 && len(topology.Targetables().Children(policy)) == 0 {
			ref := policy.GetTargetRefs()[0]
			var res schema.GroupResource
			switch ref.GroupVersionKind().Kind {
			case machinery.GatewayGroupKind.Kind:
				res = controller.GatewaysResource.GroupResource()
			case machinery.HTTPRouteGroupKind.Kind:
				res = controller.HTTPRoutesResource.GroupResource()
			}
			err = kuadrant.NewErrPolicyTargetNotFound(kuadrantv1beta3.RateLimitPolicyGroupKind.Kind, ref, apierrors.NewNotFound(res, ref.GetName()))
		}
		return policy.GetLocator(), err
	}))

	return nil
}
