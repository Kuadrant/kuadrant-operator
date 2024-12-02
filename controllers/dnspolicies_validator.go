package controllers

import (
	"context"
	"errors"
	"reflect"
	"sync"

	"github.com/samber/lo"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/pkg/kuadrant"
)

func NewDNSPoliciesValidator(isGatewayAPIInstalled, isDNSOperatorInstalled bool) *DNSPoliciesValidator {
	return &DNSPoliciesValidator{
		isGatewayAPIInstalled:  isGatewayAPIInstalled,
		isDNSOperatorInstalled: isDNSOperatorInstalled,
	}
}

type DNSPoliciesValidator struct {
	isGatewayAPIInstalled  bool
	isDNSOperatorInstalled bool
}

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

	policies := lo.Filter(topology.Policies().Items(), func(p machinery.Policy, _ int) bool {
		_, ok := p.(*kuadrantv1.DNSPolicy)
		return ok
	})

	logger.V(1).Info("validating dns policies", "policies", len(policies))

	state.Store(StateDNSPolicyAcceptedKey, lo.SliceToMap(policies, func(p machinery.Policy) (string, error) {
		if !r.isGatewayAPIInstalled {
			return p.GetLocator(), kuadrant.MissingGatewayAPIError()
		}

		if !r.isDNSOperatorInstalled {
			return p.GetLocator(), kuadrant.MissingDNSOperatorError()
		}

		policy := p.(*kuadrantv1.DNSPolicy)

		if err := isTargetRefsFound(topology, policy); err != nil {
			return policy.GetLocator(), err
		}

		if err := isConflict(policies, policy); err != nil {
			return policy.GetLocator(), err
		}

		return policy.GetLocator(), policy.Validate()
	}))

	logger.V(1).Info("finished validating dns policies")

	return nil
}

// isTargetRefsFound Policies are already linked to their targets.
// If the target ref length and length of targetables by this policy is not the same, then the policy could not find the target.
func isTargetRefsFound(topology *machinery.Topology, p *kuadrantv1.DNSPolicy) error {
	if len(p.GetTargetRefs()) != len(topology.Targetables().Children(p)) {
		return kuadrant.NewErrTargetNotFound(kuadrantv1.DNSPolicyGroupKind.Kind, p.Spec.TargetRef.LocalPolicyTargetReference, apierrors.NewNotFound(controller.GatewaysResource.GroupResource(), p.GetName()))
	}

	return nil
}

// isConflict Validates if there's already an older policy with the same target ref
func isConflict(policies []machinery.Policy, p *kuadrantv1.DNSPolicy) error {
	conflictingP, ok := lo.Find(policies, func(item machinery.Policy) bool {
		policy := item.(*kuadrantv1.DNSPolicy)
		return p != policy && policy.DeletionTimestamp == nil &&
			policy.CreationTimestamp.Before(&p.CreationTimestamp) &&
			reflect.DeepEqual(policy.GetTargetRefs(), p.GetTargetRefs())
	})

	if ok {
		return kuadrant.NewErrConflict(kuadrantv1.DNSPolicyGroupKind.Kind, client.ObjectKeyFromObject(conflictingP.(*kuadrantv1.DNSPolicy)).String(), errors.New("conflicting policy"))
	}

	return nil
}
