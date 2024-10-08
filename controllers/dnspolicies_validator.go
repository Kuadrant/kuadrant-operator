package controllers

import (
	"context"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

var (
	StateDNSPolicyValid = struct{}{}
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

func (r *DNSPoliciesValidator) validate(_ context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	policies := topology.Policies().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == kuadrantv1alpha1.DNSPolicyGroupKind
	})

	state.Store(StateDNSPolicyValid, lo.SliceToMap(policies, func(policy machinery.Policy) (string, bool) {
		return policy.GetLocator(), len(policy.GetTargetRefs()) == 0 || len(topology.Targetables().Parents(policy)) > 0
	}))

	return nil
}
