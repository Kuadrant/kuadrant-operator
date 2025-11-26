package controllers

import (
	"context"
	"errors"
	"reflect"
	"sync"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

func NewTLSPoliciesValidator(isGatewayAPIInstalled, isCertManagerInstalled bool) *TLSPoliciesValidator {
	return &TLSPoliciesValidator{
		isGatewayAPIInstalled:  isGatewayAPIInstalled,
		isCertManagerInstalled: isCertManagerInstalled,
	}
}

type TLSPoliciesValidator struct {
	isGatewayAPIInstalled  bool
	isCertManagerInstalled bool
}

func (r *TLSPoliciesValidator) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1.TLSPolicyGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1.TLSPolicyGroupKind, EventType: ptr.To(controller.UpdateEvent)},
			{Kind: &CertManagerIssuerKind},
			{Kind: &CertManagerClusterIssuerKind},
		},
		ReconcileFunc: r.Validate,
	}
}

func (r *TLSPoliciesValidator) Validate(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("TLSPoliciesValidator").WithName("Validate").WithValues("context", ctx)

	policies := lo.Filter(topology.Policies().Items(), filterForTLSPolicies)
	logger.V(1).Info("validating tls policies", "policies", len(policies))
	defer logger.V(1).Info("finished validating tls policies")

	state.Store(TLSPolicyAcceptedKey, lo.SliceToMap(policies, func(p machinery.Policy) (string, error) {
		if err := r.isMissingDependency(); err != nil {
			return p.GetLocator(), err
		}

		policy := p.(*kuadrantv1.TLSPolicy)
		// Validate target ref
		if err := r.isTargetRefsFound(topology, policy); err != nil {
			return p.GetLocator(), err
		}

		// Validate if there's a conflicting policy
		if err := r.isConflict(policies, policy); err != nil {
			return p.GetLocator(), err
		}

		// Validate Issuer is present on cluster through the topology
		if err := r.isIssuerFound(topology, policy); err != nil {
			return p.GetLocator(), err
		}

		return p.GetLocator(), nil
	}))

	return nil
}

func (r *TLSPoliciesValidator) isMissingDependency() error {
	isMissingDependency := false
	var missingDependencies []string

	if !r.isGatewayAPIInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "Gateway API")
	}
	if !r.isCertManagerInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "Cert Manager")
	}

	if isMissingDependency {
		return kuadrant.NewErrDependencyNotInstalled(missingDependencies...)
	}

	return nil
}

// isTargetRefsFound Policies are already linked to their targets. If the target ref length and length of targetables by this policy is not the same,
// then the policy could not find the target
// TODO: What should happen if multiple target refs is supported in the future in terms of reporting in log and policy status?
func (r *TLSPoliciesValidator) isTargetRefsFound(topology *machinery.Topology, p *kuadrantv1.TLSPolicy) error {
	if len(p.GetTargetRefs()) != len(topology.Targetables().Children(p)) {
		return kuadrant.NewErrTargetNotFound(kuadrantv1.TLSPolicyGroupKind.Kind, p.Spec.TargetRef.LocalPolicyTargetReference, apierrors.NewNotFound(controller.GatewaysResource.GroupResource(), p.GetName()))
	}

	return nil
}

// isConflict Validates if there's already an older policy with the same target ref
func (r *TLSPoliciesValidator) isConflict(policies []machinery.Policy, p *kuadrantv1.TLSPolicy) error {
	conflictingP, ok := lo.Find(policies, func(item machinery.Policy) bool {
		conflictTLSPolicy := item.(*kuadrantv1.TLSPolicy)
		return p != conflictTLSPolicy && conflictTLSPolicy.DeletionTimestamp == nil &&
			conflictTLSPolicy.CreationTimestamp.Before(&p.CreationTimestamp) &&
			reflect.DeepEqual(conflictTLSPolicy.GetTargetRefs(), p.GetTargetRefs())
	})

	if ok {
		return kuadrant.NewErrConflict(kuadrantv1.TLSPolicyGroupKind.Kind, client.ObjectKeyFromObject(conflictingP.(*kuadrantv1.TLSPolicy)).String(), errors.New("conflicting policy"))
	}

	return nil
}

// isIssuerFound Validates that the Issuer specified can be found in the topology
func (r *TLSPoliciesValidator) isIssuerFound(topology *machinery.Topology, p *kuadrantv1.TLSPolicy) error {
	_, ok := lo.Find(topology.Objects().Children(p), func(item machinery.Object) bool {
		runtimeObj, ok := item.(*controller.RuntimeObject)
		if !ok {
			return false
		}

		issuer, ok := runtimeObj.Object.(certmanv1.GenericIssuer)
		if !ok {
			return false
		}

		nameMatch := issuer.GetName() == p.Spec.IssuerRef.Name
		if lo.Contains([]string{"", certmanv1.IssuerKind}, p.Spec.IssuerRef.Kind) {
			return nameMatch && issuer.GetNamespace() == p.GetNamespace() &&
				issuer.GetObjectKind().GroupVersionKind().Kind == certmanv1.IssuerKind
		}

		return nameMatch && issuer.GetObjectKind().GroupVersionKind().Kind == certmanv1.ClusterIssuerKind
	})

	if !ok {
		return kuadrant.NewErrInvalid(kuadrantv1.TLSPolicyGroupKind.Kind, errors.New("unable to find issuer"))
	}

	return nil
}
