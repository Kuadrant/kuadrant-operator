package controllers

import (
	"context"
	"fmt"

	certmanv1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1"

	corev1 "k8s.io/api/core/v1"
	k8serror "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
)

func (r *TLSPolicyReconciler) reconcileCertificates(ctx context.Context, tlsPolicy *v1alpha1.TLSPolicy, gwDiffObj *reconcilers.GatewayDiff) error {
	log := crlog.FromContext(ctx)

	log.V(3).Info("reconciling certificates")
	for _, gw := range gwDiffObj.GatewaysWithInvalidPolicyRef {
		log.V(1).Info("reconcileCertificates: gateway with invalid policy ref", "key", gw.Key())
		if err := r.deleteGatewayCertificates(ctx, gw.Gateway, tlsPolicy); err != nil {
			return fmt.Errorf("error deleting certificates for gw %v: %w", gw.Gateway.Name, err)
		}
	}

	// Reconcile Certificates for each gateway directly referred by the policy (existing and new)
	for _, gw := range append(gwDiffObj.GatewaysWithValidPolicyRef, gwDiffObj.GatewaysMissingPolicyRef...) {
		log.V(1).Info("reconcileCertificates: gateway with valid or missing policy ref", "key", gw.Key())
		expectedCertificates := r.expectedCertificatesForGateway(ctx, gw.Gateway, tlsPolicy)
		if err := r.createOrUpdateGatewayCertificates(ctx, expectedCertificates); err != nil {
			return fmt.Errorf("error creating and updating expected certificates for gateway %v: %w", gw.Gateway.Name, err)
		}
		if err := r.deleteUnexpectedCertificates(ctx, expectedCertificates, gw.Gateway, tlsPolicy); err != nil {
			return fmt.Errorf("error removing unexpected certificate for gateway %v: %w", gw.Gateway.Name, err)
		}
	}
	return nil
}

func (r *TLSPolicyReconciler) createOrUpdateGatewayCertificates(ctx context.Context, expectedCertificates []*certmanv1.Certificate) error {
	//create or update all expected Certificates
	for _, cert := range expectedCertificates {
		p := &certmanv1.Certificate{}
		if err := r.Client().Get(ctx, client.ObjectKeyFromObject(cert), p); k8serror.IsNotFound(err) {
			if err := r.Client().Create(ctx, cert); err != nil {
				return err
			}
		} else if client.IgnoreNotFound(err) == nil {
			p.Spec = cert.Spec
			if err := r.Client().Update(ctx, p); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func (r *TLSPolicyReconciler) deleteGatewayCertificates(ctx context.Context, gateway *gatewayapiv1.Gateway, tlsPolicy *v1alpha1.TLSPolicy) error {
	return r.deleteCertificatesWithLabels(ctx, commonTLSCertificateLabels(client.ObjectKeyFromObject(gateway), client.ObjectKeyFromObject(tlsPolicy)), tlsPolicy.Namespace)
}

func (r *TLSPolicyReconciler) deleteCertificates(ctx context.Context, tlsPolicy *v1alpha1.TLSPolicy) error {
	return r.deleteCertificatesWithLabels(ctx, policyTLSCertificateLabels(client.ObjectKeyFromObject(tlsPolicy)), tlsPolicy.Namespace)
}

func (r *TLSPolicyReconciler) deleteCertificatesWithLabels(ctx context.Context, lbls map[string]string, namespace string) error {
	listOptions := &client.ListOptions{LabelSelector: labels.SelectorFromSet(lbls), Namespace: namespace}
	certList := &certmanv1.CertificateList{}
	if err := r.Client().List(ctx, certList, listOptions); err != nil {
		return err
	}

	for _, c := range certList.Items {
		if err := r.Client().Delete(ctx, &c); err != nil {
			return err
		}
	}
	return nil
}

func (r *TLSPolicyReconciler) deleteUnexpectedCertificates(ctx context.Context, expectedCertificates []*certmanv1.Certificate, gateway *gatewayapiv1.Gateway, tlsPolicy *v1alpha1.TLSPolicy) error {
	// remove any certificates for this gateway and TLSPolicy that are no longer expected
	existingCertificates := &certmanv1.CertificateList{}
	dnsLabels := commonTLSCertificateLabels(client.ObjectKeyFromObject(gateway), client.ObjectKeyFromObject(tlsPolicy))
	listOptions := &client.ListOptions{LabelSelector: labels.SelectorFromSet(dnsLabels)}
	if err := r.Client().List(ctx, existingCertificates, listOptions); client.IgnoreNotFound(err) != nil {
		return err
	}
	for _, p := range existingCertificates.Items {
		if !common.Contains(expectedCertificates, func(expectedCertificate *certmanv1.Certificate) bool {
			return expectedCertificate.Name == p.Name && expectedCertificate.Namespace == p.Namespace
		}) {
			if err := r.Client().Delete(ctx, &p); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *TLSPolicyReconciler) expectedCertificatesForGateway(ctx context.Context, gateway *gatewayapiv1.Gateway, tlsPolicy *v1alpha1.TLSPolicy) []*certmanv1.Certificate {
	log := crlog.FromContext(ctx)

	tlsHosts := make(map[corev1.ObjectReference][]string)
	for i, l := range gateway.Spec.Listeners {
		err := validateGatewayListenerBlock(field.NewPath("spec", "listeners").Index(i), l, gateway).ToAggregate()
		if err != nil {
			log.Info("Skipped a listener block: " + err.Error())
			continue
		}

		for _, certRef := range l.TLS.CertificateRefs {
			secretRef := corev1.ObjectReference{
				Name: string(certRef.Name),
			}
			if certRef.Namespace != nil {
				secretRef.Namespace = string(*certRef.Namespace)
			} else {
				secretRef.Namespace = gateway.GetNamespace()
			}
			// Gateway API hostname explicitly disallows IP addresses, so this
			// should be OK.
			tlsHosts[secretRef] = append(tlsHosts[secretRef], string(*l.Hostname))
		}
	}

	var certs []*certmanv1.Certificate
	for secretRef, hosts := range tlsHosts {
		certs = append(certs, r.buildCertManagerCertificate(gateway, tlsPolicy, secretRef, hosts))
	}
	return certs
}

func (r *TLSPolicyReconciler) buildCertManagerCertificate(gateway *gatewayapiv1.Gateway, tlsPolicy *v1alpha1.TLSPolicy, secretRef corev1.ObjectReference, hosts []string) *certmanv1.Certificate {
	tlsCertLabels := commonTLSCertificateLabels(client.ObjectKeyFromObject(gateway), client.ObjectKeyFromObject(tlsPolicy))

	crt := &certmanv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretRef.Name,
			Namespace: secretRef.Namespace,
			Labels:    tlsCertLabels,
		},
		Spec: certmanv1.CertificateSpec{
			DNSNames:   hosts,
			SecretName: secretRef.Name,
			SecretTemplate: &certmanv1.CertificateSecretTemplate{
				Labels: tlsCertLabels,
			},
			IssuerRef: tlsPolicy.Spec.IssuerRef,
			Usages:    certmanv1.DefaultKeyUsages(),
		},
	}
	translatePolicy(crt, tlsPolicy.Spec)
	return crt
}

func commonTLSCertificateLabels(gwKey, apKey client.ObjectKey) map[string]string {
	common := map[string]string{}
	for k, v := range policyTLSCertificateLabels(apKey) {
		common[k] = v
	}
	for k, v := range gatewayTLSCertificateLabels(gwKey) {
		common[k] = v
	}
	return common
}

func policyTLSCertificateLabels(apKey client.ObjectKey) map[string]string {
	return map[string]string{
		common.TLSPolicyBackRefAnnotation:                              apKey.Name,
		fmt.Sprintf("%s-namespace", common.TLSPolicyBackRefAnnotation): apKey.Namespace,
	}
}

func gatewayTLSCertificateLabels(gwKey client.ObjectKey) map[string]string {
	return map[string]string{
		"gateway-namespace": gwKey.Namespace,
		"gateway":           gwKey.Name,
	}
}
