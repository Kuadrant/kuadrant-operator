package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/dynamic"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
)

type CertificateReconciler struct {
	client *dynamic.DynamicClient
}

func NewCertificateReconciler(client *dynamic.DynamicClient) *CertificateReconciler {
	return &CertificateReconciler{client: client}
}

func (t *CertificateReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1.TLSPolicyGroupKind},
			{Kind: &CertManagerCertificateKind},
		},
		ReconcileFunc: t.Reconcile,
	}
}

type CertTarget struct {
	cert   *certmanagerv1.Certificate
	target machinery.Targetable
}

func (t *CertificateReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, s *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("CertificateReconciler").WithName("Reconcile")

	effectivePolicies, ok := s.Load(StateEffectiveTLSPolicies)
	if !ok {
		logger.Error(errors.New("missing effective tls policies"), "failed to reconcile certificate objects")
		return nil
	}
	effectivePoliciesMap := effectivePolicies.(EffectiveTLSPolicies)

	certs := getCertificatesFromTopology(topology)

	var certTargets []CertTarget
	for _, effectivePolicy := range effectivePoliciesMap {
		if len(effectivePolicy.Path) != 2 {
			logger.Error(errors.New("invalid effective policy"), "failed to reconcile certificate objects")
			continue
		}

		target := effectivePolicy.Path[1]

		l, ok := target.(*machinery.Listener)
		if !ok {
			return fmt.Errorf("unexpected type %T", target)
		}

		hostname := getListenerHostname(l)

		for _, certRef := range l.TLS.CertificateRefs {
			secretRef := getSecretReference(certRef, l)

			cert := buildCertManagerCertificate(l, &effectivePolicy.Spec, secretRef, []string{hostname})
			certTargets = append(certTargets, CertTarget{target: l, cert: cert})
		}
	}

	expectedCerts := t.reconcileCertificates(ctx, certTargets, topology, logger)

	// Clean up orphaned certs
	uniqueExpectedCerts := lo.UniqBy(expectedCerts, func(item *certmanagerv1.Certificate) types.UID {
		return item.GetUID()
	})
	orphanedCerts, _ := lo.Difference(certs, uniqueExpectedCerts)
	for _, orphanedCert := range orphanedCerts {
		resource := t.client.Resource(CertManagerCertificatesResource).Namespace(orphanedCert.GetNamespace())
		if err := resource.Delete(ctx, orphanedCert.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "unable to delete orphaned certificate", "name", orphanedCert.GetName(), "namespace", orphanedCert.GetNamespace(), "uid", orphanedCert.GetUID())
			continue
		}
	}

	return nil
}

func (t *CertificateReconciler) reconcileCertificates(ctx context.Context, certTargets []CertTarget, topology *machinery.Topology, logger logr.Logger) []*certmanagerv1.Certificate {
	expectedCerts := make([]*certmanagerv1.Certificate, 0, len(certTargets))
	for _, certTarget := range certTargets {
		resource := t.client.Resource(CertManagerCertificatesResource).Namespace(certTarget.cert.GetNamespace())

		// Check is cert already in topology
		objs := topology.Objects().Children(certTarget.target)
		obj, ok := lo.Find(objs, func(o machinery.Object) bool {
			return o.GroupVersionKind().GroupKind() == CertManagerCertificateKind && o.GetNamespace() == certTarget.cert.GetNamespace() && o.GetName() == certTarget.cert.GetName()
		})

		// Create
		if !ok {
			expectedCerts = append(expectedCerts, certTarget.cert)
			un, err := controller.Destruct(certTarget.cert)
			if err != nil {
				logger.Error(err, "unable to destruct cert")
				continue
			}
			_, err = resource.Create(ctx, un, metav1.CreateOptions{})
			if err != nil {
				logger.Error(err, "unable to create certificate", "name", certTarget.cert.GetName(), "namespace", certTarget.cert.GetNamespace(), "uid", certTarget.target.GetLocator())
			}

			continue
		}

		// Update
		tCert := obj.(*controller.RuntimeObject).Object.(*certmanagerv1.Certificate)
		expectedCerts = append(expectedCerts, tCert)
		if reflect.DeepEqual(tCert.Spec, certTarget.cert.Spec) {
			logger.V(1).Info("skipping update, cert specs are the same, nothing to do")
			continue
		}

		tCert.Spec = certTarget.cert.Spec
		un, err := controller.Destruct(tCert)
		if err != nil {
			logger.Error(err, "unable to destruct cert")
			continue
		}
		_, err = resource.Update(ctx, un, metav1.UpdateOptions{})
		if err != nil {
			logger.Error(err, "unable to update certificate", "name", certTarget.cert.GetName(), "namespace", certTarget.cert.GetNamespace(), "uid", certTarget.target.GetLocator())
		}
	}
	return expectedCerts
}

func getCertificatesFromTopology(topology *machinery.Topology) []*certmanagerv1.Certificate {
	return lo.FilterMap(topology.Objects().Items(), func(item machinery.Object, _ int) (*certmanagerv1.Certificate, bool) {
		r, ok := item.(*controller.RuntimeObject)
		if !ok {
			return nil, false
		}
		c, ok := r.Object.(*certmanagerv1.Certificate)
		return c, ok
	})
}

func getListenerHostname(l *machinery.Listener) string {
	hostname := "*"
	if l.Hostname != nil {
		hostname = string(*l.Hostname)
	}
	return hostname
}

func getSecretReference(certRef gatewayapiv1.SecretObjectReference, l *machinery.Listener) corev1.ObjectReference {
	secretRef := corev1.ObjectReference{
		Name: string(certRef.Name),
	}
	if certRef.Namespace != nil {
		secretRef.Namespace = string(*certRef.Namespace)
	} else {
		secretRef.Namespace = l.GetNamespace()
	}
	return secretRef
}

func buildCertManagerCertificate(l *machinery.Listener, tlsPolicy *kuadrantv1.TLSPolicy, secretRef corev1.ObjectReference, hosts []string) *certmanagerv1.Certificate {
	crt := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName(l.Gateway.Name, l.Name),
			Namespace: secretRef.Namespace,
			Labels:    CommonLabels(),
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       certmanagerv1.CertificateKind,
			APIVersion: certmanagerv1.SchemeGroupVersion.String(),
		},
		Spec: certmanagerv1.CertificateSpec{
			DNSNames:   hosts,
			SecretName: secretRef.Name,
			IssuerRef:  tlsPolicy.Spec.IssuerRef,
			Usages:     certmanagerv1.DefaultKeyUsages(),
		},
	}
	translatePolicy(crt, tlsPolicy.Spec)
	return crt
}

// https://cert-manager.io/docs/usage/gateway/#supported-annotations
// Helper functions largely based on cert manager https://github.com/cert-manager/cert-manager/blob/master/pkg/controller/certificate-shim/sync.go

func validateGatewayListenerBlock(path *field.Path, l gatewayapiv1.Listener, ingLike metav1.Object) field.ErrorList {
	var errs field.ErrorList

	if l.Hostname == nil || *l.Hostname == "" {
		errs = append(errs, field.Required(path.Child("hostname"), "the hostname cannot be empty"))
	}

	if l.TLS == nil {
		errs = append(errs, field.Required(path.Child("tls"), "the TLS block cannot be empty"))
		return errs
	}

	if len(l.TLS.CertificateRefs) == 0 {
		errs = append(errs, field.Required(path.Child("tls").Child("certificateRef"),
			"listener has no certificateRefs"))
	} else {
		// check that each CertificateRef is valid
		for i, secretRef := range l.TLS.CertificateRefs {
			if *secretRef.Group != "core" && *secretRef.Group != "" {
				errs = append(errs, field.NotSupported(path.Child("tls").Child("certificateRef").Index(i).Child("group"),
					*secretRef.Group, []string{"core", ""}))
			}

			if *secretRef.Kind != "Secret" && *secretRef.Kind != "" {
				errs = append(errs, field.NotSupported(path.Child("tls").Child("certificateRef").Index(i).Child("kind"),
					*secretRef.Kind, []string{"Secret", ""}))
			}

			if secretRef.Namespace != nil && string(*secretRef.Namespace) != ingLike.GetNamespace() {
				errs = append(errs, field.Invalid(path.Child("tls").Child("certificateRef").Index(i).Child("namespace"),
					*secretRef.Namespace, "cross-namespace secret references are not allowed in listeners"))
			}
		}
	}

	if l.TLS.Mode == nil {
		errs = append(errs, field.Required(path.Child("tls").Child("mode"),
			"the mode field is required"))
	} else {
		if *l.TLS.Mode != gatewayapiv1.TLSModeTerminate {
			errs = append(errs, field.NotSupported(path.Child("tls").Child("mode"),
				*l.TLS.Mode, []string{string(gatewayapiv1.TLSModeTerminate)}))
		}
	}

	return errs
}

// translatePolicy updates the Certificate spec using the TLSPolicy spec
// converted from https://github.com/cert-manager/cert-manager/blob/master/pkg/controller/certificate-shim/helper.go#L63
func translatePolicy(crt *certmanagerv1.Certificate, tlsPolicy kuadrantv1.TLSPolicySpec) {
	if tlsPolicy.CommonName != "" {
		crt.Spec.CommonName = tlsPolicy.CommonName
	}

	if tlsPolicy.Duration != nil {
		crt.Spec.Duration = tlsPolicy.Duration
	}

	if tlsPolicy.RenewBefore != nil {
		crt.Spec.RenewBefore = tlsPolicy.RenewBefore
	}

	if tlsPolicy.RenewBefore != nil {
		crt.Spec.RenewBefore = tlsPolicy.RenewBefore
	}

	if tlsPolicy.Usages != nil {
		crt.Spec.Usages = tlsPolicy.Usages
	}

	if tlsPolicy.RevisionHistoryLimit != nil {
		crt.Spec.RevisionHistoryLimit = tlsPolicy.RevisionHistoryLimit
	}

	if tlsPolicy.PrivateKey != nil {
		if crt.Spec.PrivateKey == nil {
			crt.Spec.PrivateKey = &certmanagerv1.CertificatePrivateKey{}
		}

		if tlsPolicy.PrivateKey.Algorithm != "" {
			crt.Spec.PrivateKey.Algorithm = tlsPolicy.PrivateKey.Algorithm
		}

		if tlsPolicy.PrivateKey.Encoding != "" {
			crt.Spec.PrivateKey.Encoding = tlsPolicy.PrivateKey.Encoding
		}

		if tlsPolicy.PrivateKey.Size != 0 {
			crt.Spec.PrivateKey.Size = tlsPolicy.PrivateKey.Size
		}

		if tlsPolicy.PrivateKey.RotationPolicy != "" {
			crt.Spec.PrivateKey.RotationPolicy = tlsPolicy.PrivateKey.RotationPolicy
		}
	}
}

func certName(gatewayName string, listenerName gatewayapiv1.SectionName) string {
	return fmt.Sprintf("%s-%s", gatewayName, listenerName)
}
