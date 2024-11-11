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

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/pkg/kuadrant"
)

type AuthPolicyValidator struct {
	isGatewayAPIInstalled        bool
	isGatewayProviderInstalled   bool
	isAuthorinoOperatorInstalled bool
}

// AuthPolicyValidator subscribes to events with potential to flip the validity of auth policies
func (r *AuthPolicyValidator) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Validate,
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1.AuthPolicyGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1.AuthPolicyGroupKind, EventType: ptr.To(controller.UpdateEvent)},
		},
	}
}

func (r *AuthPolicyValidator) Validate(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("AuthPolicyValidator")

	policies := topology.Policies().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == kuadrantv1.AuthPolicyGroupKind
	})

	logger.V(1).Info("validating auth policies", "policies", len(policies))
	defer logger.V(1).Info("finished validating auth policies")

	state.Store(StateAuthPolicyValid, lo.SliceToMap(policies, func(policy machinery.Policy) (string, error) {
		if !r.isGatewayAPIInstalled {
			return policy.GetLocator(), kuadrant.MissingGatewayAPIError()
		}

		if !r.isGatewayProviderInstalled {
			return policy.GetLocator(), kuadrant.MissingGatewayProviderError()
		}

		if !r.isAuthorinoOperatorInstalled {
			return policy.GetLocator(), kuadrant.MissingAuthorinoOperatorError()
		}

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
			err = kuadrant.NewErrPolicyTargetNotFound(kuadrantv1.AuthPolicyGroupKind.Kind, ref, apierrors.NewNotFound(res, ref.GetName()))
		}
		return policy.GetLocator(), err
	}))

	return nil
}
