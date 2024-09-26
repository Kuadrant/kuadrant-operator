package controllers

import (
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func LinkGatewayToCertificateFunc(objs controller.Store) machinery.LinkFunc {
	gateways := lo.Map(objs.FilterByGroupKind(machinery.GatewayGroupKind), controller.ObjectAs[*gwapiv1.Gateway])

	return machinery.LinkFunc{
		From: machinery.GatewayGroupKind,
		To:   CertManagerCertificateKind,
		Func: func(child machinery.Object) []machinery.Object {
			o := child.(*controller.RuntimeObject)
			cert := o.Object.(*certmanagerv1.Certificate)

			gateway, ok := lo.Find(gateways, func(item *gwapiv1.Gateway) bool {
				for _, l := range item.Spec.Listeners {
					if l.TLS != nil && l.TLS.CertificateRefs != nil {
						for _, certRef := range l.TLS.CertificateRefs {
							certRefNS := ""
							if certRef.Namespace == nil {
								certRefNS = item.GetNamespace()
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

	return machinery.LinkFunc{
		From: machinery.GatewayGroupKind,
		To:   CertManagerIssuerKind,
		Func: func(child machinery.Object) []machinery.Object {
			o := child.(*controller.RuntimeObject)
			issuer := o.Object.(*certmanagerv1.Issuer)

			// TODO: Refine
			gateway, ok := lo.Find(gateways, func(item *gwapiv1.Gateway) bool {
				return item.GetNamespace() == issuer.GetNamespace()
			})

			if ok {
				return []machinery.Object{&machinery.Gateway{Gateway: gateway}}
			}

			return nil
		},
	}
}

func LinkGatewayToClusterIssuerFunc(objs controller.Store) machinery.LinkFunc {
	gateways := lo.Map(objs.FilterByGroupKind(machinery.GatewayGroupKind), controller.ObjectAs[machinery.Object])

	return machinery.LinkFunc{
		From: machinery.GatewayGroupKind,
		To:   CertManagerClusterIssuerKind,
		Func: func(child machinery.Object) []machinery.Object {
			o := child.(*controller.RuntimeObject)
			_ = o.Object.(*certmanagerv1.ClusterIssuer)
			return gateways
		},
	}
}
