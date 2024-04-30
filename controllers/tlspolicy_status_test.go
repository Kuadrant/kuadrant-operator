//go:build unit

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestTLSPolicyReconciler_enforcedCondition(t *testing.T) {
	const (
		ns              = "default"
		tlsPolicyName   = "kuadrant-tls-policy"
		issuerName      = "kuadrant-issuer"
		certificateName = "kuadrant-certifcate"
		gwName          = "kuadrant-gateway"
	)

	scheme := runtime.NewScheme()
	sb := runtime.NewSchemeBuilder(certmanv1.AddToScheme, gatewayapiv1.AddToScheme)
	if err := sb.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	policyFactory := func(mutateFn ...func(policy *v1alpha1.TLSPolicy)) *v1alpha1.TLSPolicy {
		p := &v1alpha1.TLSPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns,
				Name:      tlsPolicyName,
			},
			TypeMeta: metav1.TypeMeta{
				Kind: "TLSPolicy",
			},
			Spec: v1alpha1.TLSPolicySpec{
				CertificateSpec: v1alpha1.CertificateSpec{
					IssuerRef: certmanmetav1.ObjectReference{
						Name: issuerName,
					},
				},
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Name: gwName,
				},
			},
		}
		for _, mutate := range mutateFn {
			mutate(p)
		}

		return p
	}

	withClusterIssuerMutater := func(p *v1alpha1.TLSPolicy) {
		p.Spec.CertificateSpec.IssuerRef.Kind = certmanv1.ClusterIssuerKind
	}

	issuerFactory := func(mutateFn ...func(issuer *certmanv1.Issuer)) *certmanv1.Issuer {
		issuer := &certmanv1.Issuer{
			ObjectMeta: metav1.ObjectMeta{Name: issuerName, Namespace: ns},
			Status: certmanv1.IssuerStatus{
				Conditions: []certmanv1.IssuerCondition{
					{
						Type:   certmanv1.IssuerConditionReady,
						Status: certmanmetav1.ConditionTrue,
					},
				},
			},
		}

		for _, mutate := range mutateFn {
			mutate(issuer)
		}

		return issuer
	}

	issuerNotReadyMutater := func(issuer *certmanv1.Issuer) {
		issuer.Status = certmanv1.IssuerStatus{
			Conditions: []certmanv1.IssuerCondition{
				{
					Type:   certmanv1.IssuerConditionReady,
					Status: certmanmetav1.ConditionFalse,
				},
			},
		}
	}

	certificateFactory := func(mutateFn ...func(certificate *certmanv1.Certificate)) *certmanv1.Certificate {
		c := &certmanv1.Certificate{
			ObjectMeta: metav1.ObjectMeta{Name: certificateName, Namespace: ns},
			Status: certmanv1.CertificateStatus{
				Conditions: []certmanv1.CertificateCondition{
					{
						Type:   certmanv1.CertificateConditionReady,
						Status: certmanmetav1.ConditionTrue,
					},
				},
			},
		}

		for _, mutate := range mutateFn {
			mutate(c)
		}

		return c
	}

	certificateNotReadyMutater := func(certificate *certmanv1.Certificate) {
		certificate.Status = certmanv1.CertificateStatus{
			Conditions: []certmanv1.CertificateCondition{
				{
					Type:   certmanv1.CertificateConditionReady,
					Status: certmanmetav1.ConditionFalse,
				},
			},
		}
	}

	refs, err := json.Marshal([]client.ObjectKey{{Name: tlsPolicyName, Namespace: ns}})
	if err != nil {
		t.Fatal(err)
	}

	gwFactory := func() *gatewayapiv1.Gateway {
		return &gatewayapiv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      gwName,
				Namespace: ns,
				Annotations: map[string]string{
					v1alpha1.TLSPolicyBackReferenceAnnotationName: string(refs),
				},
			},
			Spec: gatewayapiv1.GatewaySpec{
				Listeners: []gatewayapiv1.Listener{
					{
						Name:     "http",
						Hostname: ptr.To[gatewayapiv1.Hostname]("localhost"),
						TLS: &gatewayapiv1.GatewayTLSConfig{
							CertificateRefs: []gatewayapiv1.SecretObjectReference{{
								Group:     ptr.To[gatewayapiv1.Group]("core"),
								Kind:      ptr.To[gatewayapiv1.Kind]("Secret"),
								Name:      certificateName,
								Namespace: ptr.To[gatewayapiv1.Namespace]("default"),
							}},
							Mode: ptr.To(gatewayapiv1.TLSModeTerminate),
						},
					},
				},
			},
		}
	}

	type fields struct {
		BaseReconciler *reconcilers.BaseReconciler
	}
	type args struct {
		tlsPolicy           *v1alpha1.TLSPolicy
		targetNetworkObject client.Object
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *metav1.Condition
	}{
		{
			name: "unable to get issuer",
			fields: fields{
				BaseReconciler: reconcilers.NewBaseReconciler(
					fake.NewClientBuilder().WithScheme(scheme).Build(), nil, nil, log.NewLogger(), nil,
				),
			},
			args: args{
				tlsPolicy: policyFactory(),
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: fmt.Sprintf("TLSPolicy has encountered some issues: issuers.cert-manager.io \"%s\" not found", issuerName),
			},
		},
		{
			name: "unable to get cluster issuer",
			fields: fields{
				BaseReconciler: reconcilers.NewBaseReconciler(
					fake.NewClientBuilder().WithScheme(scheme).Build(), nil, nil, log.NewLogger(), nil,
				),
			},
			args: args{
				tlsPolicy: policyFactory(withClusterIssuerMutater),
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: fmt.Sprintf("TLSPolicy has encountered some issues: clusterissuers.cert-manager.io \"%s\" not found", issuerName),
			},
		},
		{
			name: "issuer not ready",
			fields: fields{
				BaseReconciler: reconcilers.NewBaseReconciler(
					fake.NewClientBuilder().
						WithObjects(issuerFactory(issuerNotReadyMutater)).
						WithScheme(scheme).Build(), nil, nil, log.NewLogger(), nil,
				),
			},
			args: args{
				tlsPolicy: policyFactory(),
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: "TLSPolicy has encountered some issues: issuer not ready",
			},
		},
		{
			name: "no valid gateways found",
			fields: fields{
				BaseReconciler: reconcilers.NewBaseReconciler(
					fake.NewClientBuilder().WithObjects(issuerFactory()).
						WithScheme(scheme).Build(), nil, nil, log.NewLogger(), nil,
				),
			},
			args: args{
				tlsPolicy:           policyFactory(),
				targetNetworkObject: gwFactory(),
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: "TLSPolicy has encountered some issues: no valid gateways found",
			},
		},
		{
			name: "unable to get certificate",
			fields: fields{
				BaseReconciler: reconcilers.NewBaseReconciler(
					fake.NewClientBuilder().WithObjects(issuerFactory(), gwFactory()).
						WithScheme(scheme).Build(), nil, nil, log.NewLogger(), nil,
				),
			},
			args: args{
				tlsPolicy:           policyFactory(),
				targetNetworkObject: gwFactory(),
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: fmt.Sprintf("TLSPolicy has encountered some issues: certificates.cert-manager.io \"%s\" not found", certificateName),
			},
		},
		{
			name: "certificate is not ready",
			fields: fields{
				BaseReconciler: reconcilers.NewBaseReconciler(
					fake.NewClientBuilder().WithObjects(issuerFactory(), gwFactory(), certificateFactory(certificateNotReadyMutater)).
						WithScheme(scheme).Build(), nil, nil, log.NewLogger(), nil,
				),
			},
			args: args{
				tlsPolicy:           policyFactory(),
				targetNetworkObject: gwFactory(),
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: fmt.Sprintf("TLSPolicy has encountered some issues: certificate %s not ready", certificateName),
			},
		},
		{
			name: "is enforced",
			fields: fields{
				BaseReconciler: reconcilers.NewBaseReconciler(
					fake.NewClientBuilder().WithObjects(issuerFactory(), gwFactory(), certificateFactory()).
						WithScheme(scheme).Build(), nil, nil, log.NewLogger(), nil,
				),
			},
			args: args{
				tlsPolicy:           policyFactory(),
				targetNetworkObject: gwFactory(),
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionTrue,
				Reason:  string(kuadrant.PolicyConditionEnforced),
				Message: "TLSPolicy has been successfully enforced",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &TLSPolicyReconciler{
				BaseReconciler: tt.fields.BaseReconciler,
			}
			if got := r.enforcedCondition(context.Background(), tt.args.tlsPolicy, tt.args.targetNetworkObject); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("enforcedCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}
