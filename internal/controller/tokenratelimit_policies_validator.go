package controllers

import (
	"context"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

type TokenRateLimitPolicyValidator struct {
	isLimitadorOperatorInstalled bool
	isGatewayAPIInstalled        bool
	isGatewayProviderInstalled   bool
}

// TokenRateLimitPolicyValidator subscribes to events with potential to flip the validity of token rate limit policies
func (r *TokenRateLimitPolicyValidator) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Validate,
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1alpha1.TokenRateLimitPolicyGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1alpha1.TokenRateLimitPolicyGroupKind, EventType: ptr.To(controller.UpdateEvent)},
		},
	}
}

func (r *TokenRateLimitPolicyValidator) Validate(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("TokenRateLimitPolicyValidator")

	policies := topology.Policies().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == kuadrantv1alpha1.TokenRateLimitPolicyGroupKind
	})

	logger.V(1).Info("validating token rate limit policies", "policies", len(policies))
	defer logger.V(1).Info("finished validating token rate limit policies")

	missingDependencyErr := r.isMissingDependency()

	state.Store(StateTokenRateLimitPolicyValid, lo.SliceToMap(policies, func(policy machinery.Policy) (string, error) {
		if missingDependencyErr != nil {
			return policy.GetLocator(), missingDependencyErr
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
			err = kuadrant.NewErrPolicyTargetNotFound(kuadrantv1alpha1.TokenRateLimitPolicyGroupKind.Kind, ref, apierrors.NewNotFound(res, ref.GetName()))
		}
		return policy.GetLocator(), err
	}))

	return nil
}

func (r *TokenRateLimitPolicyValidator) isMissingDependency() error {
	isMissingDependency := false
	var missingDependencies []string

	if !r.isGatewayAPIInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "Gateway API")
	}
	if !r.isGatewayProviderInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "Gateway API provider (istio / envoy gateway)")
	}
	if !r.isLimitadorOperatorInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "Limitador Operator")
	}

	if isMissingDependency {
		return kuadrant.NewErrDependencyNotInstalled(missingDependencies...)
	}

	return nil
}

const StateTokenRateLimitPolicyValid = "TokenRateLimitPolicyValid"

func tokenRateLimitPolicyAcceptedStatusFunc(state *sync.Map) func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) (bool, error) {
	stateMap, _ := state.Load(StateTokenRateLimitPolicyValid)
	m, ok := stateMap.(map[string]error)
	if !ok {
		return func(_ *kuadrantv1alpha1.TokenRateLimitPolicy) (bool, error) {
			return false, kuadrant.NewErrDependencyNotInstalled("token rate limit policy validation has not finished")
		}
	}

	return func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) (bool, error) {
		err := m[policy.GetLocator()]
		return err == nil || apierrors.IsNotFound(err), err
	}
}

func isTokenRateLimitPolicyAcceptedAndNotDeletedFunc(state *sync.Map) func(machinery.Policy) bool {
	f := isTokenRateLimitPolicyAcceptedFunc(state)
	return func(policy machinery.Policy) bool {
		p, object := policy.(metav1.Object)
		return object && f(policy) && p.GetDeletionTimestamp() == nil
	}
}

func isTokenRateLimitPolicyAcceptedFunc(state *sync.Map) func(machinery.Policy) bool {
	f := tokenRateLimitPolicyAcceptedStatusFunc(state)
	return func(policy machinery.Policy) bool {
		if tokenPolicy, ok := policy.(*kuadrantv1alpha1.TokenRateLimitPolicy); ok {
			accepted, _ := f(tokenPolicy)
			return accepted
		}
		return false
	}
}
