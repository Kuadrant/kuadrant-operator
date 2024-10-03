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
			{Kind: &kuadrantv1alpha1.TLSPolicyKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1alpha1.TLSPolicyKind, EventType: ptr.To(controller.UpdateEvent)},
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

	// Get all gateways
	gws := lo.FilterMap(topology.Targetables().Items(), func(item machinery.Targetable, index int) (*machinery.Gateway, bool) {
		gw, ok := item.(*machinery.Gateway)
		return gw, ok
	})

	isCertManagerInstalled := false
	installed, ok := s.Load(IsCertManagerInstalledKey)
	if ok {
		isCertManagerInstalled = installed.(bool)
	} else {
		logger.V(1).Error(errors.New("isCertManagerInstalled was not found in sync map, defaulting to false"), "sync map error")
	}

	for _, policy := range policies {
		if policy.DeletionTimestamp != nil {
			logger.V(1).Info("tls policy is marked for deletion, skipping", "name", policy.Name, "namespace", policy.Namespace)
			continue
		}

		if !isCertManagerInstalled {
			s.Store(TLSPolicyAcceptedKey(policy.GetUID()), kuadrant.NewErrDependencyNotInstalled("Cert Manager"))
			continue
		}

		// TODO: This should be only one target ref for now, but what should happen if multiple target refs is supported in the future?
		targetRefs := policy.GetTargetRefs()
		for _, targetRef := range targetRefs {
			// Find gateway defined by target ref
			_, ok := lo.Find(gws, func(item *machinery.Gateway) bool {
				if item.GetName() == targetRef.GetName() && item.GetNamespace() == targetRef.GetNamespace() {
					return true
				}
				return false
			})

			// Can't find gateway target ref
			if !ok {
				logger.V(1).Info("tls policy cannot find target ref", "name", policy.Name, "namespace", policy.Namespace)
				s.Store(TLSPolicyAcceptedKey(policy.GetUID()), kuadrant.NewErrTargetNotFound(policy.Kind(), policy.GetTargetRef(), apierrors.NewNotFound(kuadrantv1alpha1.TLSPoliciesResource.GroupResource(), policy.GetName())))
				continue
			}

			logger.V(1).Info("tls policy found target ref", "name", policy.Name, "namespace", policy.Namespace)
			s.Store(TLSPolicyAcceptedKey(policy.GetUID()), nil)
		}
	}

	return nil
}
