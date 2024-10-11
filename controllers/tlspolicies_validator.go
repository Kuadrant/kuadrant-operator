package controllers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

func NewValidateTLSPoliciesValidatorReconciler(isCertManagerInstalled bool) *ValidateTLSPoliciesValidatorReconciler {
	return &ValidateTLSPoliciesValidatorReconciler{
		isCertManagerInstalled: isCertManagerInstalled,
	}
}

type ValidateTLSPoliciesValidatorReconciler struct {
	isCertManagerInstalled bool
}

func (t *ValidateTLSPoliciesValidatorReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1alpha1.TLSPolicyGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1alpha1.TLSPolicyGroupKind, EventType: ptr.To(controller.UpdateEvent)},
			{Kind: &CertManagerIssuerKind},
			{Kind: &CertManagerClusterIssuerKind},
		},
		ReconcileFunc: t.Validate,
	}
}

func (t *ValidateTLSPoliciesValidatorReconciler) Validate(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, s *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("ValidateTLSPolicyTask").WithName("Reconcile")

	// Get all TLS Policies
	policies := lo.FilterMap(topology.Policies().Items(), func(item machinery.Policy, index int) (*kuadrantv1alpha1.TLSPolicy, bool) {
		p, ok := item.(*kuadrantv1alpha1.TLSPolicy)
		return p, ok
	})

	isPolicyValidErrorMap := make(map[string]error, len(policies))

	for _, p := range policies {
		if p.DeletionTimestamp != nil {
			logger.V(1).Info("tls policy is marked for deletion, skipping", "name", p.Name, "namespace", p.Namespace)
			continue
		}

		if !t.isCertManagerInstalled {
			isPolicyValidErrorMap[p.GetLocator()] = kuadrant.NewErrDependencyNotInstalled("Cert Manager")
			continue
		}

		// TODO: What should happen if multiple target refs is supported in the future in terms of reporting in log and policy status?
		// Policies are already linked to their targets. If the target ref length and length of targetables by this policy is not the same,
		// then the policy could not find the target
		if len(p.GetTargetRefs()) != len(topology.Targetables().Children(p)) {
			logger.V(1).Info("tls policy cannot find target ref", "name", p.Name, "namespace", p.Namespace)
			isPolicyValidErrorMap[p.GetLocator()] = kuadrant.NewErrTargetNotFound(p.Kind(), p.GetTargetRef(),
				apierrors.NewNotFound(kuadrantv1alpha1.TLSPoliciesResource.GroupResource(), p.GetName()))
			continue
		}

		// Validate IssuerRef is correct
		if !lo.Contains([]string{"", certmanv1.IssuerKind, certmanv1.ClusterIssuerKind}, p.Spec.IssuerRef.Kind) {
			isPolicyValidErrorMap[p.GetLocator()] = kuadrant.NewErrInvalid(p.Kind(),
				fmt.Errorf(`invalid value %q for issuerRef.kind. Must be empty, %q or %q`,
					p.Spec.IssuerRef.Kind, certmanv1.IssuerKind, certmanv1.ClusterIssuerKind))
			continue
		}

		// Validate Issuer is present on cluster through the topology
		_, ok := lo.Find(topology.Objects().Items(), func(item machinery.Object) bool {
			runtimeObj, ok := item.(*controller.RuntimeObject)
			if !ok {
				return false
			}

			issuer, ok := runtimeObj.Object.(certmanv1.GenericIssuer)
			if !ok {
				return false
			}

			match := issuer.GetName() == p.Spec.IssuerRef.Name
			if lo.Contains([]string{"", certmanv1.IssuerKind}, p.Spec.IssuerRef.Kind) {
				match = match && issuer.GetNamespace() == p.GetNamespace() &&
					issuer.GetObjectKind().GroupVersionKind().Kind == certmanv1.IssuerKind
			} else {
				match = match && issuer.GetObjectKind().GroupVersionKind().Kind == certmanv1.ClusterIssuerKind
			}

			return match
		})

		if !ok {
			isPolicyValidErrorMap[p.GetLocator()] = kuadrant.NewErrInvalid(p.Kind(), errors.New("unable to find issuer"))
			continue
		}

		isPolicyValidErrorMap[p.GetLocator()] = nil
	}

	s.Store(TLSPolicyAcceptedKey, isPolicyValidErrorMap)

	return nil
}
