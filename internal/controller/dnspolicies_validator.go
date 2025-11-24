package controllers

import (
	"context"
	"errors"
	"reflect"
	"sync"

	"github.com/samber/lo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
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
	logger := controller.LoggerFromContext(ctx).WithName("DNSPoliciesValidator").WithValues("context", ctx)
	tracer := controller.TracerFromContext(ctx)

	policies := lo.Filter(topology.Policies().Items(), func(p machinery.Policy, _ int) bool {
		_, ok := p.(*kuadrantv1.DNSPolicy)
		return ok
	})

	logger.V(1).Info("validating dns policies", "policies", len(policies))

	validationResults := make(map[string]error)

	for _, p := range policies {
		policy := p.(*kuadrantv1.DNSPolicy)

		_, span := tracer.Start(ctx, "policy.DNSPolicy.validate")
		span.SetAttributes(
			attribute.String("policy.name", policy.GetName()),
			attribute.String("policy.namespace", policy.GetNamespace()),
			attribute.String("policy.kind", kuadrantv1.DNSPolicyGroupKind.Kind),
			attribute.String("policy.uid", string(policy.GetUID())),
		)

		var err error
		if missingDepErr := r.isMissingDependency(); missingDepErr != nil {
			err = missingDepErr
			span.RecordError(err)
			span.SetStatus(codes.Error, "missing dependency")
		} else if targetErr := isTargetRefsFound(topology, policy); targetErr != nil {
			err = targetErr
			span.RecordError(err)
			span.SetStatus(codes.Error, "target not found")
		} else if conflictErr := isConflict(policies, policy); conflictErr != nil {
			err = conflictErr
			span.RecordError(err)
			span.SetStatus(codes.Error, "conflicting policy")
		} else if validateErr := policy.Validate(); validateErr != nil {
			err = validateErr
			span.RecordError(err)
			span.SetStatus(codes.Error, "validation failed")
		} else {
			span.AddEvent("policy validated successfully")
			span.SetStatus(codes.Ok, "")
		}

		validationResults[policy.GetLocator()] = err
		span.End()
	}

	state.Store(StateDNSPolicyAcceptedKey, validationResults)

	logger.V(1).Info("finished validating dns policies")

	return nil
}

func (r *DNSPoliciesValidator) isMissingDependency() error {
	isMissingDependency := false
	var missingDependencies []string

	if !r.isGatewayAPIInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "Gateway API")
	}
	if !r.isDNSOperatorInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "DNS Operator")
	}

	if isMissingDependency {
		return kuadrant.NewErrDependencyNotInstalled(missingDependencies...)
	}

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
