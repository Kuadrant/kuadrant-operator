package controllers

import (
	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

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
func translatePolicy(crt *certmanv1.Certificate, tlsPolicy v1alpha1.TLSPolicySpec) {
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
