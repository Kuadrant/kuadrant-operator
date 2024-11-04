package controllers

import (
	"context"
	"errors"
	"reflect"
	"sync"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

type EffectiveTLSPoliciesReconciler struct {
	client *dynamic.DynamicClient
	scheme *runtime.Scheme
}

func NewEffectiveTLSPoliciesReconciler(client *dynamic.DynamicClient, scheme *runtime.Scheme) *EffectiveTLSPoliciesReconciler {
	return &EffectiveTLSPoliciesReconciler{client: client, scheme: scheme}
}

func (t *EffectiveTLSPoliciesReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1alpha1.TLSPolicyGroupKind},
			{Kind: &CertManagerCertificateKind},
		},
		ReconcileFunc: t.Reconcile,
	}
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=tlspolicies,verbs=get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=tlspolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=tlspolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups="cert-manager.io",resources=issuers,verbs=get;list;watch;
//+kubebuilder:rbac:groups="cert-manager.io",resources=clusterissuers,verbs=get;list;watch;
//+kubebuilder:rbac:groups="cert-manager.io",resources=certificates,verbs=get;list;watch;create;update;patch;delete

func (t *EffectiveTLSPoliciesReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, s *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("EffectiveTLSPoliciesReconciler").WithName("Reconcile")

	listeners := topology.Targetables().Items(func(object machinery.Object) bool {
		_, ok := object.(*machinery.Listener)
		return ok
	})

	// Get all certs in the topology for comparison with expected certs to determine orphaned certs later
	// Only certs owned by TLSPolicies should be in the topology - no need to check again
	certs := lo.FilterMap(topology.Objects().Items(), func(item machinery.Object, index int) (*certmanv1.Certificate, bool) {
		r, ok := item.(*controller.RuntimeObject)
		if !ok {
			return nil, false
		}
		c, ok := r.Object.(*certmanv1.Certificate)

		return c, ok
	})

	var expectedCerts []*certmanv1.Certificate
	filterForTLSPolicies := func(p machinery.Policy, _ int) bool {
		_, ok := p.(*kuadrantv1alpha1.TLSPolicy)
		return ok
	}

	for _, listener := range listeners {
		l := listener.(*machinery.Listener)

		policies := lo.Filter(l.Policies(), filterForTLSPolicies)

		if len(policies) == 0 {
			policies = lo.Filter(l.Gateway.Policies(), filterForTLSPolicies)
		}

		for _, p := range policies {
			policy := p.(*kuadrantv1alpha1.TLSPolicy)

			// Policy is deleted
			if policy.DeletionTimestamp != nil {
				logger.V(1).Info("policy is marked for deletion, nothing to do", "name", policy.Name, "namespace", policy.Namespace, "uid", policy.GetUID())
				continue
			}

			// Policy is not valid
			isValid, _ := IsTLSPolicyValid(ctx, s, policy)
			if !isValid {
				logger.V(1).Info("deleting certs for invalid policy", "name", policy.Name, "namespace", policy.Namespace, "uid", policy.GetUID())
				if err := t.deleteCertificatesForTargetable(ctx, topology, l); err != nil {
					logger.Error(err, "unable to delete certs for invalid policy", "name", policy.Name, "namespace", policy.Namespace, "uid", policy.GetUID())
				}
				continue
			}

			// Policy is valid
			// Need to use Gateway as listener hosts can be merged into a singular cert if using the same cert reference
			expectedCertificates := expectedCertificatesForGateway(ctx, l.Gateway.Gateway, policy)

			for _, cert := range expectedCertificates {
				resource := t.client.Resource(CertManagerCertificatesResource).Namespace(cert.GetNamespace())

				// Check is cert already in topology
				objs := topology.Objects().Children(l)
				obj, ok := lo.Find(objs, func(o machinery.Object) bool {
					return o.GroupVersionKind().GroupKind() == CertManagerCertificateKind && o.GetNamespace() == cert.GetNamespace() && o.GetName() == cert.GetName()
				})

				// Create
				if !ok {
					expectedCerts = append(expectedCerts, cert)
					if err := controllerutil.SetControllerReference(policy, cert, t.scheme); err != nil {
						logger.Error(err, "failed to set owner reference on certificate", "name", policy.Name, "namespace", policy.Namespace, "uid", policy.GetUID())
						continue
					}

					un, err := controller.Destruct(cert)
					if err != nil {
						logger.Error(err, "unable to destruct cert")
						continue
					}
					_, err = resource.Create(ctx, un, metav1.CreateOptions{})
					if err != nil && !apierrors.IsAlreadyExists(err) {
						logger.Error(err, "unable to create certificate", "name", policy.Name, "namespace", policy.Namespace, "uid", policy.GetUID())
					}

					continue
				}

				// Update
				tCert := obj.(*controller.RuntimeObject).Object.(*certmanv1.Certificate)
				expectedCerts = append(expectedCerts, tCert)
				if reflect.DeepEqual(tCert.Spec, cert.Spec) {
					logger.V(1).Info("skipping update, cert specs are the same, nothing to do")
					continue
				}

				tCert.Spec = cert.Spec
				un, err := controller.Destruct(tCert)
				if err != nil {
					logger.Error(err, "unable to destruct cert")
					continue
				}
				_, err = resource.Update(ctx, un, metav1.UpdateOptions{})
				if err != nil {
					logger.Error(err, "unable to update certificate", "policy", policy.Name)
				}
			}
		}
	}

	// Clean up orphaned certs
	uniqueExpectedCerts := lo.UniqBy(expectedCerts, func(item *certmanv1.Certificate) types.UID {
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

func (t *EffectiveTLSPoliciesReconciler) deleteCertificatesForTargetable(ctx context.Context, topology *machinery.Topology, target machinery.Targetable) error {
	children := topology.Objects().Children(target)

	certs := lo.FilterMap(children, func(item machinery.Object, index int) (*certmanv1.Certificate, bool) {
		r, ok := item.(*controller.RuntimeObject)
		if !ok {
			return nil, false
		}
		c, ok := r.Object.(*certmanv1.Certificate)

		return c, ok
	})

	var deletionErr error
	for _, cert := range certs {
		resource := t.client.Resource(CertManagerCertificatesResource).Namespace(cert.GetNamespace())

		if err := resource.Delete(ctx, cert.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			deletionErr = errors.Join(err)
		}
	}

	return deletionErr
}

func expectedCertificatesForGateway(ctx context.Context, gateway *gatewayapiv1.Gateway, tlsPolicy *kuadrantv1alpha1.TLSPolicy) []*certmanv1.Certificate {
	log := crlog.FromContext(ctx)

	tlsHosts := make(map[corev1.ObjectReference][]string)
	for i, l := range gateway.Spec.Listeners {
		hostname := "*"
		if l.Hostname != nil {
			hostname = string(*l.Hostname)
		}

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
			tlsHosts[secretRef] = append(tlsHosts[secretRef], hostname)
		}
	}

	certs := make([]*certmanv1.Certificate, 0, len(tlsHosts))
	for secretRef, hosts := range tlsHosts {
		certs = append(certs, buildCertManagerCertificate(tlsPolicy, secretRef, hosts))
	}
	return certs
}

func expectedCertificatesForListener(l *machinery.Listener, tlsPolicy *kuadrantv1alpha1.TLSPolicy) []*certmanv1.Certificate {
	tlsHosts := make(map[corev1.ObjectReference][]string)

	hostname := "*"
	if l.Hostname != nil {
		hostname = string(*l.Hostname)
	}

	for _, certRef := range l.TLS.CertificateRefs {
		secretRef := corev1.ObjectReference{
			Name: string(certRef.Name),
		}
		if certRef.Namespace != nil {
			secretRef.Namespace = string(*certRef.Namespace)
		} else {
			secretRef.Namespace = l.GetNamespace()
		}
		// Gateway API hostname explicitly disallows IP addresses, so this
		// should be OK.
		tlsHosts[secretRef] = append(tlsHosts[secretRef], hostname)
	}

	certs := make([]*certmanv1.Certificate, 0, len(tlsHosts))
	for secretRef, hosts := range tlsHosts {
		certs = append(certs, buildCertManagerCertificate(tlsPolicy, secretRef, hosts))
	}
	return certs
}

func buildCertManagerCertificate(tlsPolicy *kuadrantv1alpha1.TLSPolicy, secretRef corev1.ObjectReference, hosts []string) *certmanv1.Certificate {
	crt := &certmanv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretRef.Name,
			Namespace: secretRef.Namespace,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       certmanv1.CertificateKind,
			APIVersion: certmanv1.SchemeGroupVersion.String(),
		},
		Spec: certmanv1.CertificateSpec{
			DNSNames:   hosts,
			SecretName: secretRef.Name,
			IssuerRef:  tlsPolicy.Spec.IssuerRef,
			Usages:     certmanv1.DefaultKeyUsages(),
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
func translatePolicy(crt *certmanv1.Certificate, tlsPolicy kuadrantv1alpha1.TLSPolicySpec) {
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
			crt.Spec.PrivateKey = &certmanv1.CertificatePrivateKey{}
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
