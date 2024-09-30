package controllers

import (
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

func LinkGatewayToCertificateFunc(objs controller.Store) machinery.LinkFunc {
	gateways := lo.Map(objs.FilterByGroupKind(machinery.GatewayGroupKind), controller.ObjectAs[*gwapiv1.Gateway])

	return machinery.LinkFunc{
		From: machinery.GatewayGroupKind,
		To:   CertManagerCertificateKind,
		Func: func(child machinery.Object) []machinery.Object {
			o := child.(*controller.RuntimeObject)
			cert := o.Object.(*certmanagerv1.Certificate)

			gateway, ok := lo.Find(gateways, func(g *gwapiv1.Gateway) bool {
				for _, l := range g.Spec.Listeners {
					if l.TLS != nil && l.TLS.CertificateRefs != nil {
						for _, certRef := range l.TLS.CertificateRefs {
							certRefNS := ""
							if certRef.Namespace == nil {
								certRefNS = g.GetNamespace()
							} else {
								certRefNS = string(*certRef.Namespace)
							}
							if certRefNS == cert.GetNamespace() && string(certRef.Name) == cert.GetName() {
								return true
							}
						}
					}
				}

				return false
			})

			if ok {
				return []machinery.Object{&machinery.Gateway{Gateway: gateway}}
			}

			return nil
		},
	}
}

func LinkGatewayToIssuerFunc(objs controller.Store) machinery.LinkFunc {
	gateways := lo.Map(objs.FilterByGroupKind(machinery.GatewayGroupKind), controller.ObjectAs[*gwapiv1.Gateway])
	tlsPolicies := lo.Map(objs.FilterByGroupKind(kuadrantv1alpha1.TLSPolicyKind), controller.ObjectAs[*kuadrantv1alpha1.TLSPolicy])

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
	tlsPolicies := lo.Map(objs.FilterByGroupKind(kuadrantv1alpha1.TLSPolicyKind), controller.ObjectAs[*kuadrantv1alpha1.TLSPolicy])

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
