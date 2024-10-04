package controllers

import (
	"context"
	"errors"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

func NewValidateTLSPoliciesValidatorReconciler() *ValidateTLSPoliciesValidatorReconciler {
	return &ValidateTLSPoliciesValidatorReconciler{}
}

type ValidateTLSPoliciesValidatorReconciler struct{}

func (t *ValidateTLSPoliciesValidatorReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1alpha1.TLSPolicyGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1alpha1.TLSPolicyGroupKind, EventType: ptr.To(controller.UpdateEvent)},
			{Kind: &CertManagerCertificateKind},
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

	isCertManagerInstalled := false
	installed, ok := s.Load(IsCertManagerInstalledKey)
	if ok {
		isCertManagerInstalled = installed.(bool)
	} else {
		logger.V(1).Error(errors.New("isCertManagerInstalled was not found in sync map, defaulting to false"), "sync map error")
	}

	for _, p := range policies {
		if p.DeletionTimestamp != nil {
			logger.V(1).Info("tls policy is marked for deletion, skipping", "name", p.Name, "namespace", p.Namespace)
			continue
		}

		if !isCertManagerInstalled {
			isPolicyValidErrorMap[p.GetLocator()] = kuadrant.NewErrDependencyNotInstalled("Cert Manager")
			continue
		}

		// TODO: What should happen if multiple target refs is supported in the future in terms of reporting in log and policy status?
		// Policies are already linked to their targets, if is target ref length and length of targetables by this policy is the same
		if len(p.GetTargetRefs()) != len(topology.Targetables().Children(p)) {
			logger.V(1).Info("tls policy cannot find target ref", "name", p.Name, "namespace", p.Namespace)
			isPolicyValidErrorMap[p.GetLocator()] = kuadrant.NewErrTargetNotFound(p.Kind(), p.GetTargetRef(), apierrors.NewNotFound(kuadrantv1alpha1.TLSPoliciesResource.GroupResource(), p.GetName()))
			continue
		}

		isPolicyValidErrorMap[p.GetLocator()] = nil
	}

	s.Store(TLSPolicyAcceptedKey, isPolicyValidErrorMap)

	return nil
}
