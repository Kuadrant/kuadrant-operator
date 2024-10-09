package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

type TLSPolicyStatusUpdaterReconciler struct {
	Client *dynamic.DynamicClient
}

func NewTLSPolicyStatusUpdaterReconciler(client *dynamic.DynamicClient) *TLSPolicyStatusUpdaterReconciler {
	return &TLSPolicyStatusUpdaterReconciler{Client: client}
}

func (t *TLSPolicyStatusUpdaterReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1alpha1.TLSPolicyGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1alpha1.TLSPolicyGroupKind, EventType: ptr.To(controller.UpdateEvent)},
			{Kind: &CertManagerCertificateKind},
			{Kind: &CertManagerIssuerKind},
			{Kind: &CertManagerClusterIssuerKind},
		},
		ReconcileFunc: t.UpdateStatus,
	}
}

func (t *TLSPolicyStatusUpdaterReconciler) UpdateStatus(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, s *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("TLSPolicyStatusUpdaterReconciler").WithName("Reconcile")

	policies := lo.FilterMap(topology.Policies().Items(), func(item machinery.Policy, index int) (*kuadrantv1alpha1.TLSPolicy, bool) {
		p, ok := item.(*kuadrantv1alpha1.TLSPolicy)
		return p, ok
	})

	for _, policy := range policies {
		if policy.DeletionTimestamp != nil {
			logger.V(1).Info("tls policy is marked for deletion, skipping", "name", policy.GetName(), "namespace", policy.GetNamespace(), "uid", policy.GetUID())
			continue
		}

		newStatus := &kuadrantv1alpha1.TLSPolicyStatus{
			// Copy initial conditions. Otherwise, status will always be updated
			Conditions:         slices.Clone(policy.Status.Conditions),
			ObservedGeneration: policy.Status.ObservedGeneration,
		}

		_, err := IsPolicyValid(ctx, s, policy)
		meta.SetStatusCondition(&newStatus.Conditions, *kuadrant.AcceptedCondition(policy, err))

		// Do not set enforced condition if Accepted condition is false
		if meta.IsStatusConditionFalse(newStatus.Conditions, string(gatewayapiv1alpha2.PolicyReasonAccepted)) {
			meta.RemoveStatusCondition(&newStatus.Conditions, string(kuadrant.PolicyConditionEnforced))
		} else {
			enforcedCond := t.enforcedCondition(ctx, policy, topology)
			meta.SetStatusCondition(&newStatus.Conditions, *enforcedCond)
		}

		// Nothing to do
		equalStatus := equality.Semantic.DeepEqual(newStatus, policy.Status)
		if equalStatus && policy.Generation == policy.Status.ObservedGeneration {
			logger.V(1).Info("policy status unchanged, skipping update")
			continue
		}
		newStatus.ObservedGeneration = policy.Generation
		policy.Status = *newStatus

		resource := t.Client.Resource(kuadrantv1alpha1.TLSPoliciesResource).Namespace(policy.GetNamespace())
		un, err := controller.Destruct(policy)
		if err != nil {
			logger.Error(err, "unable to destruct policy")
			continue
		}

		_, err = resource.UpdateStatus(ctx, un, metav1.UpdateOptions{})
		if err != nil {
			logger.Error(err, "unable to update status for TLSPolicy", "name", policy.GetName(), "namespace", policy.GetNamespace(), "uid", policy.GetUID())
		}
	}

	return nil
}

func (t *TLSPolicyStatusUpdaterReconciler) enforcedCondition(ctx context.Context, tlsPolicy *kuadrantv1alpha1.TLSPolicy, topology *machinery.Topology) *metav1.Condition {
	if err := t.isIssuerReady(ctx, tlsPolicy, topology); err != nil {
		return kuadrant.EnforcedCondition(tlsPolicy, kuadrant.NewErrUnknown(tlsPolicy.Kind(), err), false)
	}

	if err := t.isCertificatesReady(tlsPolicy, topology); err != nil {
		return kuadrant.EnforcedCondition(tlsPolicy, kuadrant.NewErrUnknown(tlsPolicy.Kind(), err), false)
	}

	return kuadrant.EnforcedCondition(tlsPolicy, nil, true)
}

func (t *TLSPolicyStatusUpdaterReconciler) isIssuerReady(ctx context.Context, tlsPolicy *kuadrantv1alpha1.TLSPolicy, topology *machinery.Topology) error {
	logger := controller.LoggerFromContext(ctx).WithName("TLSPolicyStatusUpdaterReconciler").WithName("isIssuerReady")

	// Get all gateways
	gws := lo.FilterMap(topology.Targetables().Items(), func(item machinery.Targetable, index int) (*machinery.Gateway, bool) {
		gw, ok := item.(*machinery.Gateway)
		return gw, ok
	})

	// Find gateway defined by target ref
	gw, ok := lo.Find(gws, func(item *machinery.Gateway) bool {
		if item.GetName() == string(tlsPolicy.GetTargetRef().Name) && item.GetNamespace() == tlsPolicy.GetNamespace() {
			return true
		}
		return false
	})

	if !ok {
		return fmt.Errorf("unable to find target ref %s for policy %s in ns %s in topology", tlsPolicy.GetTargetRef(), tlsPolicy.Name, tlsPolicy.Namespace)
	}

	var conditions []certmanagerv1.IssuerCondition

	switch tlsPolicy.Spec.IssuerRef.Kind {
	case "", certmanagerv1.IssuerKind:
		objs := topology.Objects().Children(gw)
		obj, ok := lo.Find(objs, func(o machinery.Object) bool {
			return o.GroupVersionKind().GroupKind() == CertManagerIssuerKind && o.GetNamespace() == tlsPolicy.GetNamespace() && o.GetName() == tlsPolicy.Spec.IssuerRef.Name
		})
		if !ok {
			err := fmt.Errorf("%s \"%s\" not found", tlsPolicy.Spec.IssuerRef.Kind, tlsPolicy.Spec.IssuerRef.Name)
			logger.Error(err, "error finding object in topology")
			return err
		}

		issuer := obj.(*controller.RuntimeObject).Object.(*certmanagerv1.Issuer)

		conditions = issuer.Status.Conditions
	case certmanagerv1.ClusterIssuerKind:
		objs := topology.Objects().Children(gw)
		obj, ok := lo.Find(objs, func(o machinery.Object) bool {
			return o.GroupVersionKind().GroupKind() == CertManagerClusterIssuerKind && o.GetName() == tlsPolicy.Spec.IssuerRef.Name
		})
		if !ok {
			err := fmt.Errorf("%s \"%s\" not found", tlsPolicy.Spec.IssuerRef.Kind, tlsPolicy.Spec.IssuerRef.Name)
			logger.Error(err, "error finding object in topology")
			return err
		}

		issuer := obj.(*controller.RuntimeObject).Object.(*certmanagerv1.ClusterIssuer)
		conditions = issuer.Status.Conditions
	default:
		return fmt.Errorf(`invalid value %q for issuerRef.kind. Must be empty, %q or %q`, tlsPolicy.Spec.IssuerRef.Kind, certmanagerv1.IssuerKind, certmanagerv1.ClusterIssuerKind)
	}

	transformedCond := utils.Map(conditions, func(c certmanagerv1.IssuerCondition) metav1.Condition {
		return metav1.Condition{Reason: c.Reason, Status: metav1.ConditionStatus(c.Status), Type: string(c.Type), Message: c.Message}
	})

	if !meta.IsStatusConditionTrue(transformedCond, string(certmanagerv1.IssuerConditionReady)) {
		return fmt.Errorf("%s not ready", tlsPolicy.Spec.IssuerRef.Kind)
	}

	return nil
}

func (t *TLSPolicyStatusUpdaterReconciler) isCertificatesReady(p machinery.Policy, topology *machinery.Topology) error {
	tlsPolicy, ok := p.(*kuadrantv1alpha1.TLSPolicy)
	if !ok {
		return errors.New("invalid policy")
	}

	// Get all listeners where the gateway contains this
	// TODO: Update when targeting by section name is allowed, the listener will contain the policy rather than the gateway
	listeners := lo.FilterMap(topology.Targetables().Items(), func(t machinery.Targetable, index int) (*machinery.Listener, bool) {
		l, ok := t.(*machinery.Listener)
		return l, ok && lo.Contains(l.Gateway.Policies(), p)
	})

	if len(listeners) == 0 {
		return errors.New("no valid gateways found")
	}

	for i, l := range listeners {
		// Not valid - so no need to check if cert is ready since there should not be one created
		err := validateGatewayListenerBlock(field.NewPath("").Index(i), *l.Listener, l.Gateway).ToAggregate()
		if err != nil {
			continue
		}

		expectedCertificates := expectedCertificatesForListener(l, tlsPolicy)

		for _, cert := range expectedCertificates {
			objs := topology.Objects().Children(l)
			obj, ok := lo.Find(objs, func(o machinery.Object) bool {
				return o.GroupVersionKind().GroupKind() == CertManagerCertificateKind && o.GetNamespace() == cert.GetNamespace() && o.GetName() == cert.GetName()
			})

			if !ok {
				return errors.New("certificate not found")
			}

			c := obj.(*controller.RuntimeObject).Object.(*certmanagerv1.Certificate)

			conditions := utils.Map(c.Status.Conditions, func(c certmanagerv1.CertificateCondition) metav1.Condition {
				return metav1.Condition{Reason: c.Reason, Status: metav1.ConditionStatus(c.Status), Type: string(c.Type), Message: c.Message}
			})

			if !meta.IsStatusConditionTrue(conditions, string(certmanagerv1.CertificateConditionReady)) {
				return fmt.Errorf("certificate %s not ready", cert.Name)
			}
		}
	}

	return nil
}
