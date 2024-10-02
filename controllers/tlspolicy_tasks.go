package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/cert-manager/cert-manager/pkg/apis/certmanager"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var (
	CertManagerCertificatesResource  = certmanagerv1.SchemeGroupVersion.WithResource("certificates")
	CertManagerIssuersResource       = certmanagerv1.SchemeGroupVersion.WithResource("issuers")
	CertMangerClusterIssuersResource = certmanagerv1.SchemeGroupVersion.WithResource("clusterissuers")

	CertManagerCertificateKind   = schema.GroupKind{Group: certmanager.GroupName, Kind: certmanagerv1.CertificateKind}
	CertManagerIssuerKind        = schema.GroupKind{Group: certmanager.GroupName, Kind: certmanagerv1.IssuerKind}
	CertManagerClusterIssuerKind = schema.GroupKind{Group: certmanager.GroupName, Kind: certmanagerv1.ClusterIssuerKind}
)

func NewTLSPolicyWorkflow(client *dynamic.DynamicClient) *controller.Workflow {
	return &controller.Workflow{
		Precondition:  NewValidateTLSPolicyTask().Reconcile,
		Postcondition: NewTLSPolicyStatusTask(client).Reconcile,
	}
}

type ValidateTLSPolicyTask struct{}

func NewValidateTLSPolicyTask() *ValidateTLSPolicyTask {
	return &ValidateTLSPolicyTask{}
}

func (t *ValidateTLSPolicyTask) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1alpha1.TLSPolicyKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1alpha1.TLSPolicyKind, EventType: ptr.To(controller.UpdateEvent)},
			{Kind: &CertManagerCertificateKind},
			{Kind: &CertManagerIssuerKind},
			{Kind: &CertManagerClusterIssuerKind},
		},
		ReconcileFunc: t.Reconcile,
	}
}

func (t *ValidateTLSPolicyTask) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, s *sync.Map) error {
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
				logger.Info("tls policy cannot find target ref", "name", policy.Name, "namespace", policy.Namespace)
				s.Store(TLSPolicyAcceptedKey(policy.GetUID()), kuadrant.NewErrTargetNotFound(policy.Kind(), policy.GetTargetRef(), apierrors.NewNotFound(kuadrantv1alpha1.TLSPoliciesResource.GroupResource(), policy.GetName())))
				continue
			}

			logger.Info("tls policy found target ref", "name", policy.Name, "namespace", policy.Namespace)
			s.Store(TLSPolicyAcceptedKey(policy.GetUID()), nil)
		}
	}

	return nil
}

type TLSPolicyStatusTask struct {
	Client *dynamic.DynamicClient
}

func NewTLSPolicyStatusTask(client *dynamic.DynamicClient) *TLSPolicyStatusTask {
	return &TLSPolicyStatusTask{Client: client}
}

func (t *TLSPolicyStatusTask) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1alpha1.TLSPolicyKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1alpha1.TLSPolicyKind, EventType: ptr.To(controller.UpdateEvent)},
			{Kind: &CertManagerCertificateKind},
			{Kind: &CertManagerIssuerKind},
			{Kind: &CertManagerClusterIssuerKind},
		},
		ReconcileFunc: t.Reconcile,
	}
}

func (t *TLSPolicyStatusTask) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, s *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("TLSPolicyStatusTask").WithName("Reconcile")

	policies := lo.FilterMap(topology.Policies().Items(), func(item machinery.Policy, index int) (*kuadrantv1alpha1.TLSPolicy, bool) {
		p, ok := item.(*kuadrantv1alpha1.TLSPolicy)
		return p, ok
	})

	for _, policy := range policies {
		if policy.DeletionTimestamp != nil {
			logger.V(1).Info("tls policy is marked for deletion, skipping", "name", policy.Name, "namespace", policy.Namespace)
			continue
		}

		newStatus := &kuadrantv1alpha1.TLSPolicyStatus{
			// Copy initial conditions. Otherwise, status will always be updated
			Conditions:         slices.Clone(policy.Status.Conditions),
			ObservedGeneration: policy.Status.ObservedGeneration,
		}

		var err error
		accepted, ok := s.Load(TLSPolicyAcceptedKey(policy.GetUID()))
		if ok && accepted != nil {
			err = accepted.(error)
		}
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
			logger.Info("policy status unchanged, skipping update")
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
			logger.Error(err, "unable to update status for TLSPolicy", "uid", policy.GetUID())
		}
	}

	return nil
}

func (t *TLSPolicyStatusTask) enforcedCondition(ctx context.Context, tlsPolicy *kuadrantv1alpha1.TLSPolicy, topology *machinery.Topology) *metav1.Condition {
	if err := t.isIssuerReady(ctx, tlsPolicy, topology); err != nil {
		return kuadrant.EnforcedCondition(tlsPolicy, kuadrant.NewErrUnknown(tlsPolicy.Kind(), err), false)
	}

	if err := t.isCertificatesReady(ctx, tlsPolicy, topology); err != nil {
		return kuadrant.EnforcedCondition(tlsPolicy, kuadrant.NewErrUnknown(tlsPolicy.Kind(), err), false)
	}

	return kuadrant.EnforcedCondition(tlsPolicy, nil, true)
}

func (t *TLSPolicyStatusTask) isIssuerReady(ctx context.Context, tlsPolicy *kuadrantv1alpha1.TLSPolicy, topology *machinery.Topology) error {
	logger := controller.LoggerFromContext(ctx).WithName("TLSPolicyStatusTask").WithName("isIssuerReady")

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

func (t *TLSPolicyStatusTask) isCertificatesReady(ctx context.Context, p machinery.Policy, topology *machinery.Topology) error {
	tlsPolicy, ok := p.(*kuadrantv1alpha1.TLSPolicy)
	if !ok {
		return errors.New("invalid policy")
	}

	// Get all gateways that contains this policy
	gws := lo.FilterMap(topology.Targetables().Items(), func(item machinery.Targetable, index int) (*machinery.Gateway, bool) {
		gw, ok := item.(*machinery.Gateway)
		return gw, ok && lo.Contains(gw.Policies(), p)
	})

	if len(gws) == 0 {
		return errors.New("no valid gateways found")
	}

	for _, gw := range gws {
		expectedCertificates := expectedCertificatesForGateway(ctx, gw.Gateway, tlsPolicy)

		for _, cert := range expectedCertificates {
			objs := topology.Objects().Children(gw)
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

const IsCertManagerInstalledKey = "IsCertManagerInstalled"

func NewIsCertManagerInstalledTask(restMapper meta.RESTMapper) IsCertManagerInstalledTask {
	return IsCertManagerInstalledTask{
		restMapper: restMapper,
	}
}

type IsCertManagerInstalledTask struct {
	restMapper meta.RESTMapper
}

func (t IsCertManagerInstalledTask) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, s *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("IsCertManagerInstalledTask").WithName("Reconcile")
	isCertManagerInstalled, err := kuadrantgatewayapi.IsCertManagerInstalled(t.restMapper, logger)

	if err != nil {
		logger.Error(err, "error checking IsCertManagerInstalled")
	}

	s.Store(IsCertManagerInstalledKey, isCertManagerInstalled)

	return nil
}

func TLSPolicyAcceptedKey(uid types.UID) string {
	return fmt.Sprintf("TLSPolicyValid:%s", uid)
}
