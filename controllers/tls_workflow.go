package controllers

import (
	"context"
	"sync"

	"github.com/cert-manager/cert-manager/pkg/apis/certmanager"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

const (
	TLSPolicyAcceptedKey = "TLSPolicyValid"
)

var (
	CertManagerCertificatesResource  = certmanagerv1.SchemeGroupVersion.WithResource("certificates")
	CertManagerIssuersResource       = certmanagerv1.SchemeGroupVersion.WithResource("issuers")
	CertMangerClusterIssuersResource = certmanagerv1.SchemeGroupVersion.WithResource("clusterissuers")

	CertManagerCertificateKind   = schema.GroupKind{Group: certmanager.GroupName, Kind: certmanagerv1.CertificateKind}
	CertManagerIssuerKind        = schema.GroupKind{Group: certmanager.GroupName, Kind: certmanagerv1.IssuerKind}
	CertManagerClusterIssuerKind = schema.GroupKind{Group: certmanager.GroupName, Kind: certmanagerv1.ClusterIssuerKind}
)

func NewTLSWorkflow(client *dynamic.DynamicClient, scheme *runtime.Scheme, isCertManagerInstalled bool) *controller.Workflow {
	return &controller.Workflow{
		Precondition: NewValidateTLSPoliciesValidatorReconciler(isCertManagerInstalled).Subscription().Reconcile,
		Tasks: []controller.ReconcileFunc{
			NewEffectiveTLSPoliciesReconciler(client, scheme).Subscription().Reconcile,
		},
		Postcondition: NewTLSPolicyStatusUpdaterReconciler(client).Subscription().Reconcile,
	}
}

// Linking functions

func LinkListenerToCertificateFunc(objs controller.Store) machinery.LinkFunc {
	gateways := lo.Map(objs.FilterByGroupKind(machinery.GatewayGroupKind), controller.ObjectAs[*gwapiv1.Gateway])
	listeners := lo.FlatMap(lo.Map(gateways, func(g *gwapiv1.Gateway, _ int) *machinery.Gateway {
		return &machinery.Gateway{Gateway: g}
	}), machinery.ListenersFromGatewayFunc)

	return machinery.LinkFunc{
		From: machinery.ListenerGroupKind,
		To:   CertManagerCertificateKind,
		Func: func(child machinery.Object) []machinery.Object {
			o := child.(*controller.RuntimeObject)
			cert := o.Object.(*certmanagerv1.Certificate)

			if len(listeners) == 0 {
				return nil
			}

			linkedListeners := lo.Filter(listeners, func(l *machinery.Listener, index int) bool {
				if l.TLS != nil && l.TLS.CertificateRefs != nil {
					for _, certRef := range l.TLS.CertificateRefs {
						certRefNS := ""
						if certRef.Namespace == nil {
							certRefNS = l.GetNamespace()
						} else {
							certRefNS = string(*certRef.Namespace)
						}
						if certRefNS == cert.GetNamespace() && string(certRef.Name) == cert.GetName() {
							return true
						}
					}
				}

				return false
			})

			return lo.Map(linkedListeners, func(l *machinery.Listener, index int) machinery.Object {
				return l
			})
		},
	}
}

func LinkGatewayToIssuerFunc(objs controller.Store) machinery.LinkFunc {
	gateways := lo.Map(objs.FilterByGroupKind(machinery.GatewayGroupKind), controller.ObjectAs[*gwapiv1.Gateway])
	tlsPolicies := lo.Map(objs.FilterByGroupKind(kuadrantv1alpha1.TLSPolicyGroupKind), controller.ObjectAs[*kuadrantv1alpha1.TLSPolicy])

	return machinery.LinkFunc{
		From: machinery.GatewayGroupKind,
		To:   CertManagerIssuerKind,
		Func: func(child machinery.Object) []machinery.Object {
			o := child.(*controller.RuntimeObject)
			issuer := o.Object.(*certmanagerv1.Issuer)

			// Policies linked to Issuer
			// Issuer must be in the same namespace as the policy
			linkedPolicies := lo.Filter(tlsPolicies, func(p *kuadrantv1alpha1.TLSPolicy, index int) bool {
				return p.Spec.IssuerRef.Name == issuer.GetName() && p.GetNamespace() == issuer.GetNamespace() && p.Spec.IssuerRef.Kind == certmanagerv1.IssuerKind
			})

			if len(linkedPolicies) == 0 {
				return nil
			}

			// Can infer linked gateways through the policy
			linkedGateways := lo.Filter(gateways, func(g *gwapiv1.Gateway, index int) bool {
				for _, l := range linkedPolicies {
					if string(l.Spec.TargetRef.Name) == g.GetName() && g.GetNamespace() == l.GetNamespace() {
						return true
					}
				}

				return false
			})

			return lo.Map(linkedGateways, func(item *gwapiv1.Gateway, index int) machinery.Object {
				return &machinery.Gateway{Gateway: item}
			})
		},
	}
}

func LinkGatewayToClusterIssuerFunc(objs controller.Store) machinery.LinkFunc {
	gateways := lo.Map(objs.FilterByGroupKind(machinery.GatewayGroupKind), controller.ObjectAs[*gwapiv1.Gateway])
	tlsPolicies := lo.Map(objs.FilterByGroupKind(kuadrantv1alpha1.TLSPolicyGroupKind), controller.ObjectAs[*kuadrantv1alpha1.TLSPolicy])

	return machinery.LinkFunc{
		From: machinery.GatewayGroupKind,
		To:   CertManagerClusterIssuerKind,
		Func: func(child machinery.Object) []machinery.Object {
			o := child.(*controller.RuntimeObject)
			clusterIssuer := o.Object.(*certmanagerv1.ClusterIssuer)

			// Policies linked to ClusterIssuer
			linkedPolicies := lo.Filter(tlsPolicies, func(p *kuadrantv1alpha1.TLSPolicy, index int) bool {
				return p.Spec.IssuerRef.Name == clusterIssuer.GetName() && p.Spec.IssuerRef.Kind == certmanagerv1.ClusterIssuerKind
			})

			if len(linkedPolicies) == 0 {
				return nil
			}

			// Can infer linked gateways through the policy
			linkedGateways := lo.Filter(gateways, func(g *gwapiv1.Gateway, index int) bool {
				for _, l := range linkedPolicies {
					if string(l.Spec.TargetRef.Name) == g.GetName() && g.GetNamespace() == l.GetNamespace() {
						return true
					}
				}

				return false
			})

			return lo.Map(linkedGateways, func(item *gwapiv1.Gateway, index int) machinery.Object {
				return &machinery.Gateway{Gateway: item}
			})
		},
	}
}

// Common functions used across multiple reconcilers

func IsTLSPolicyValid(ctx context.Context, s *sync.Map, policy *kuadrantv1alpha1.TLSPolicy) (bool, error) {
	logger := controller.LoggerFromContext(ctx).WithName("IsPolicyValid")

	store, ok := s.Load(TLSPolicyAcceptedKey)
	if !ok {
		logger.V(1).Info("TLSPolicyAcceptedKey not found, policies will be checked for validity by current status")
		return meta.IsStatusConditionTrue(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyReasonAccepted)), nil
	}

	isPolicyValidErrorMap := store.(map[string]error)

	return isPolicyValidErrorMap[policy.GetLocator()] == nil, isPolicyValidErrorMap[policy.GetLocator()]
}
