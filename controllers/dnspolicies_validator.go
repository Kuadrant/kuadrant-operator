package controllers

import (
	"context"
	"sync"

	"github.com/samber/lo"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
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
			{Kind: &kuadrantv1alpha1.DNSPolicyGroupKind},
		},
	}
}

func (r *DNSPoliciesValidator) validate(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("DNSPoliciesValidator")

	policies := lo.FilterMap(topology.Policies().Items(), func(item machinery.Policy, index int) (*kuadrantv1alpha1.DNSPolicy, bool) {
		p, ok := item.(*kuadrantv1alpha1.DNSPolicy)
		return p, ok
	})

	logger.V(1).Info("validating dns policies", "policies", len(policies))

	state.Store(StateDNSPolicyAcceptedKey, lo.SliceToMap(policies, func(policy *kuadrantv1alpha1.DNSPolicy) (string, error) {
		if len(policy.GetTargetRefs()) == 0 || len(topology.Targetables().Children(policy)) == 0 {
			return policy.GetLocator(), kuadrant.NewErrTargetNotFound(policy.Kind(), policy.GetTargetRef(),
				apierrors.NewNotFound(kuadrantv1alpha1.DNSPoliciesResource.GroupResource(), policy.GetName()))
		}
		return policy.GetLocator(), r.policyValid(policy)
	}))

	logger.V(1).Info("finished validating dns policies")

	return nil
}

func (r *DNSPoliciesValidator) policyValid(p *kuadrantv1alpha1.DNSPolicy) error {
	return p.Spec.ExcludeAddresses.Validate()
}
