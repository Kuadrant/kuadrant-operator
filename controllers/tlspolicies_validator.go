package controllers

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/kuadrant/kuadrant-operator/pkg/kuadrant"
)

func NewTLSPoliciesValidator(isCertManagerInstalled bool) *TLSPoliciesValidator {
	return &TLSPoliciesValidator{
		isCertManagerInstalled: isCertManagerInstalled,
	}
}

type TLSPoliciesValidator struct {
	isCertManagerInstalled bool
}

func (t *TLSPoliciesValidator) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1.TLSPolicyGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1.TLSPolicyGroupKind, EventType: ptr.To(controller.UpdateEvent)},
			{Kind: &CertManagerIssuerKind},
			{Kind: &CertManagerClusterIssuerKind},
		},
		ReconcileFunc: t.Validate,
	}
}

func (t *TLSPoliciesValidator) Validate(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, s *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("TLSPoliciesValidator").WithName("Validate")

	policies := lo.Filter(topology.Policies().Items(), filterForTLSPolicies)

	isPolicyValidErrorMap := make(map[string]error, len(policies))

	for _, policy := range policies {
		p := policy.(*kuadrantv1.TLSPolicy)
		if p.DeletionTimestamp != nil {
			logger.V(1).Info("tls policy is marked for deletion, skipping", "name", p.Name, "namespace", p.Namespace)
			continue
		}

		if !t.isCertManagerInstalled {
			isPolicyValidErrorMap[p.GetLocator()] = kuadrant.NewErrDependencyNotInstalled("Cert Manager")
			continue
		}

		// Validate target ref
		if err := t.isTargetRefsFound(topology, p); err != nil {
			isPolicyValidErrorMap[p.GetLocator()] = err
			continue
		}

		// Validate if there's a conflicting policy
		if err := t.isConflict(policies, p); err != nil {
			isPolicyValidErrorMap[p.GetLocator()] = err
			continue
		}

		// Validate IssuerRef kind is correct
		if err := t.isValidIssuerKind(p); err != nil {
			isPolicyValidErrorMap[p.GetLocator()] = err
			continue
		}

		// Validate Issuer is present on cluster through the topology
		if err := t.isIssuerFound(topology, p); err != nil {
			isPolicyValidErrorMap[p.GetLocator()] = err
			continue
		}

		isPolicyValidErrorMap[p.GetLocator()] = nil
	}

	s.Store(TLSPolicyAcceptedKey, isPolicyValidErrorMap)

	return nil
}

// isTargetRefsFound Policies are already linked to their targets. If the target ref length and length of targetables by this policy is not the same,
// then the policy could not find the target
// TODO: What should happen if multiple target refs is supported in the future in terms of reporting in log and policy status?
func (t *TLSPoliciesValidator) isTargetRefsFound(topology *machinery.Topology, p *kuadrantv1.TLSPolicy) error {
	if len(p.GetTargetRefs()) != len(topology.Targetables().Children(p)) {
		return kuadrant.NewErrTargetNotFound(kuadrantv1.TLSPolicyGroupKind.Kind, p.GetTargetRef(), apierrors.NewNotFound(controller.GatewaysResource.GroupResource(), p.GetName()))
	}

	return nil
}

// isConflict Validates if there's already an older policy with the same target ref
func (t *TLSPoliciesValidator) isConflict(policies []machinery.Policy, p *kuadrantv1.TLSPolicy) error {
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

// isValidIssuerKind Validates that the Issuer Ref kind is either empty, Issuer or ClusterIssuer
func (t *TLSPoliciesValidator) isValidIssuerKind(p *kuadrantv1.TLSPolicy) error {
	if !lo.Contains([]string{"", certmanv1.IssuerKind, certmanv1.ClusterIssuerKind}, p.Spec.IssuerRef.Kind) {
		return kuadrant.NewErrInvalid(kuadrantv1.TLSPolicyGroupKind.Kind, fmt.Errorf(`invalid value %q for issuerRef.kind. Must be empty, %q or %q`,
			p.Spec.IssuerRef.Kind, certmanv1.IssuerKind, certmanv1.ClusterIssuerKind))
	}

	return nil
}

// isIssuerFound Validates that the Issuer specified can be found in the topology
func (t *TLSPoliciesValidator) isIssuerFound(topology *machinery.Topology, p *kuadrantv1.TLSPolicy) error {
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
