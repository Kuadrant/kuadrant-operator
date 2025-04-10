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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

type TLSPolicyStatusUpdater struct {
	Client *dynamic.DynamicClient
}

func NewTLSPolicyStatusUpdater(client *dynamic.DynamicClient) *TLSPolicyStatusUpdater {
	return &TLSPolicyStatusUpdater{Client: client}
}

func (t *TLSPolicyStatusUpdater) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1.TLSPolicyGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1.TLSPolicyGroupKind, EventType: ptr.To(controller.UpdateEvent)},
			{Kind: &CertManagerCertificateKind},
			{Kind: &CertManagerIssuerKind},
			{Kind: &CertManagerClusterIssuerKind},
		},
		ReconcileFunc: t.UpdateStatus,
	}
}

func (t *TLSPolicyStatusUpdater) UpdateStatus(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, s *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("TLSPolicyStatusUpdater").WithName("UpdateStatus")

	policies := lo.Filter(topology.Policies().Items(), filterForTLSPolicies)

	for _, policy := range policies {
		p := policy.(*kuadrantv1.TLSPolicy)
		if p.DeletionTimestamp != nil {
			logger.V(1).Info("tls policy is marked for deletion, skipping", "name", policy.GetName(), "namespace", policy.GetNamespace(), "uid", p.GetUID())
			continue
		}

		newStatus := &kuadrantv1.TLSPolicyStatus{
			// Copy initial conditions. Otherwise, status will always be updated
			Conditions:         slices.Clone(p.Status.Conditions),
			ObservedGeneration: p.Status.ObservedGeneration,
		}

		_, err := IsTLSPolicyValid(ctx, s, p)
		meta.SetStatusCondition(&newStatus.Conditions, *kuadrant.AcceptedCondition(p, err))

		// Do not set enforced condition if Accepted condition is false
		if meta.IsStatusConditionFalse(newStatus.Conditions, string(gatewayapiv1alpha2.PolicyReasonAccepted)) {
			meta.RemoveStatusCondition(&newStatus.Conditions, string(kuadrant.PolicyConditionEnforced))
		} else {
			enforcedCond := t.enforcedCondition(ctx, p, topology)
			meta.SetStatusCondition(&newStatus.Conditions, *enforcedCond)
		}

		// Nothing to do
		equalStatus := equality.Semantic.DeepEqual(newStatus, p.Status)
		if equalStatus && p.Generation == p.Status.ObservedGeneration {
			logger.V(1).Info("policy status unchanged, skipping update")
			continue
		}
		newStatus.ObservedGeneration = p.Generation
		p.Status = *newStatus

		resource := t.Client.Resource(kuadrantv1.TLSPoliciesResource).Namespace(policy.GetNamespace())
		un, err := controller.Destruct(policy)
		if err != nil {
			logger.Error(err, "unable to destruct policy")
			continue
		}

		_, err = resource.UpdateStatus(ctx, un, metav1.UpdateOptions{})
		if err != nil && !apierrors.IsConflict(err) {
			logger.Error(err, "unable to update status for TLSPolicy", "name", policy.GetName(), "namespace", policy.GetNamespace(), "uid", p.GetUID())
		}
	}

	return nil
}

func (t *TLSPolicyStatusUpdater) enforcedCondition(ctx context.Context, policy *kuadrantv1.TLSPolicy, topology *machinery.Topology) *metav1.Condition {
	if err := t.isIssuerReady(ctx, policy, topology); err != nil {
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrUnknown(kuadrantv1.TLSPolicyGroupKind.Kind, err), false)
	}

	if err := t.isCertificatesReady(policy, topology); err != nil {
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrUnknown(kuadrantv1.TLSPolicyGroupKind.Kind, err), false)
	}

	return kuadrant.EnforcedCondition(policy, nil, true)
}

func (t *TLSPolicyStatusUpdater) isIssuerReady(ctx context.Context, policy *kuadrantv1.TLSPolicy, topology *machinery.Topology) error {
	logger := controller.LoggerFromContext(ctx).WithName("TLSPolicyStatusUpdater").WithName("isIssuerReady")

	var conditions []certmanagerv1.IssuerCondition

	switch policy.Spec.IssuerRef.Kind {
	case "", certmanagerv1.IssuerKind:
		objs := topology.Objects().Children(policy)
		obj, ok := lo.Find(objs, func(o machinery.Object) bool {
			return o.GroupVersionKind().GroupKind() == CertManagerIssuerKind && o.GetNamespace() == policy.GetNamespace() && o.GetName() == policy.Spec.IssuerRef.Name
		})
		if !ok {
			issuerRef := policy.Spec.IssuerRef.Kind
			if issuerRef == "" {
				issuerRef = certmanagerv1.IssuerKind
			}
			err := fmt.Errorf("%s \"%s\" not found", issuerRef, policy.Spec.IssuerRef.Name)
			logger.Error(err, "error finding object in topology")
			return err
		}

		issuer := obj.(*controller.RuntimeObject).Object.(*certmanagerv1.Issuer)

		conditions = issuer.Status.Conditions
	case certmanagerv1.ClusterIssuerKind:
		objs := topology.Objects().Children(policy)
		obj, ok := lo.Find(objs, func(o machinery.Object) bool {
			return o.GroupVersionKind().GroupKind() == CertManagerClusterIssuerKind && o.GetName() == policy.Spec.IssuerRef.Name
		})
		if !ok {
			err := fmt.Errorf("%s \"%s\" not found", policy.Spec.IssuerRef.Kind, policy.Spec.IssuerRef.Name)
			logger.Error(err, "error finding object in topology")
			return err
		}

		issuer := obj.(*controller.RuntimeObject).Object.(*certmanagerv1.ClusterIssuer)
		conditions = issuer.Status.Conditions
	default:
		return fmt.Errorf(`invalid value %q for issuerRef.kind. Must be empty, %q or %q`, policy.Spec.IssuerRef.Kind, certmanagerv1.IssuerKind, certmanagerv1.ClusterIssuerKind)
	}

	transformedCond := utils.Map(conditions, func(c certmanagerv1.IssuerCondition) metav1.Condition {
		return metav1.Condition{Reason: c.Reason, Status: metav1.ConditionStatus(c.Status), Type: string(c.Type), Message: c.Message}
	})

	if !meta.IsStatusConditionTrue(transformedCond, string(certmanagerv1.IssuerConditionReady)) {
		return fmt.Errorf("%s not ready", policy.Spec.IssuerRef.Kind)
	}

	return nil
}

func (t *TLSPolicyStatusUpdater) isCertificatesReady(p machinery.Policy, topology *machinery.Topology) error {
	policy, ok := p.(*kuadrantv1.TLSPolicy)
	if !ok {
		return errors.New("invalid policy")
	}

	// Get all listeners where the gateway or listener contains this policy
	listeners := lo.FilterMap(topology.Targetables().Items(), func(t machinery.Targetable, _ int) (*machinery.Listener, bool) {
		l, ok := t.(*machinery.Listener)
		return l, ok && (lo.Contains(l.Policies(), p) || lo.Contains(l.Gateway.Policies(), p))
	})

	if len(listeners) == 0 {
		return errors.New("no valid gateways found")
	}

	for _, l := range listeners {
		expectedCertificates := expectedCertificatesForListener(l, policy)

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

			cond := meta.FindStatusCondition(conditions, string(certmanagerv1.CertificateConditionReady))
			if cond == nil {
				return fmt.Errorf("certificate %s not ready", cert.Name)
			}

			if cond.Status != metav1.ConditionTrue {
				msg := fmt.Sprintf("certificate %s is not ready: %s - %s", cert.Name, cond.Reason, cond.Message)
				if cond.Reason == "IncorrectCertificate" {
					msg = fmt.Sprintf("%s. Shared TLS certificates refs between listeners not supported. Use unique certificates refs in the Gateway listeners to fully enforce policy", msg)
				}
				return errors.New(msg)
			}
		}
	}

	return nil
}

func expectedCertificatesForListener(l *machinery.Listener, tlsPolicy *kuadrantv1.TLSPolicy) []*certmanagerv1.Certificate {
	// Not valid - so no need to check if cert is ready since there should not be one created
	err := validateGatewayListenerBlock(field.NewPath(""), *l.Listener, l.Gateway).ToAggregate()
	if err != nil {
		return []*certmanagerv1.Certificate{}
	}

	certs := make([]*certmanagerv1.Certificate, 0)

	hostname := getListenerHostname(l)

	for _, certRef := range l.TLS.CertificateRefs {
		secretRef := getSecretReference(certRef, l)
		// Gateway API hostname explicitly disallows IP addresses, so this
		// should be OK.
		certs = append(certs, buildCertManagerCertificate(l, tlsPolicy, secretRef, []string{hostname}))
	}

	return certs
}
