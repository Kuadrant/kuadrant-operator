package controllers

import (
	"context"
	"sync"

	"github.com/samber/lo"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

func NewDNSPoliciesValidator() *DNSPoliciesValidator {
	return &DNSPoliciesValidator{}
}

type DNSPoliciesValidator struct{}

func (r *DNSPoliciesValidator) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.validate,
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1.DNSPolicyGroupKind},
		},
	}
}

func (r *DNSPoliciesValidator) validate(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("DNSPoliciesValidator")

	policies := lo.FilterMap(topology.Policies().Items(), dnsPolicyTypeFilterFunc())

	logger.V(1).Info("validating dns policies", "policies", len(policies))

	state.Store(StateDNSPolicyAcceptedKey, lo.SliceToMap(policies, func(policy *kuadrantv1.DNSPolicy) (string, error) {
		if len(policy.GetTargetRefs()) == 0 || len(topology.Targetables().Children(policy)) == 0 {
			return policy.GetLocator(), kuadrant.NewErrTargetNotFound(kuadrantv1.DNSPolicyGroupKind.Kind, policy.GetTargetRef(),
				apierrors.NewNotFound(controller.GatewaysResource.GroupResource(), policy.GetName()))
		}
		return policy.GetLocator(), policy.Validate()
	}))

	logger.V(1).Info("finished validating dns policies")

	return nil
}
