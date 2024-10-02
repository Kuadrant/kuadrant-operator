//go:build unit

package controllers

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

func TestTLSPolicyAcceptedKey(t *testing.T) {
	type args struct {
		uid types.UID
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "test uid is appended",
			args: args{
				types.UID("unqiueid"),
			},
			want: "TLSPolicyValid:unqiueid",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TLSPolicyAcceptedKey(tt.args.uid); got != tt.want {
				t.Errorf("TLSPolicyValidKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTLSPolicyStatusTask_enforcedCondition(t *testing.T) {
	const (
		ns              = "default"
		tlsPolicyName   = "kuadrant-tls-policy"
		issuerName      = "kuadrant-issuer"
		certificateName = "kuadrant-certifcate"
		gwName          = "kuadrant-gateway"
	)

	policyFactory := func(mutateFn ...func(policy *kuadrantv1alpha1.TLSPolicy)) *kuadrantv1alpha1.TLSPolicy {
		p := &kuadrantv1alpha1.TLSPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns,
				Name:      tlsPolicyName,
				UID:       types.UID(rand.String(9)),
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "TLSPolicy",
				APIVersion: kuadrantv1alpha1.GroupVersion.String(),
			},
			Spec: kuadrantv1alpha1.TLSPolicySpec{
				CertificateSpec: kuadrantv1alpha1.CertificateSpec{
					IssuerRef: certmanmetav1.ObjectReference{
						Name: issuerName,
						Kind: certmanv1.IssuerKind,
					},
				},
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReference{
					Name:  gwName,
					Kind:  "Gateway",
					Group: gatewayapiv1alpha2.GroupName,
				},
			},
		}
		for _, mutate := range mutateFn {
			mutate(p)
		}

		return p
	}

	withClusterIssuerMutater := func(p *kuadrantv1alpha1.TLSPolicy) {
		p.Spec.CertificateSpec.IssuerRef.Kind = certmanv1.ClusterIssuerKind
	}

	issuerFactory := func(mutateFn ...func(issuer *certmanv1.Issuer)) *certmanv1.Issuer {
		issuer := &certmanv1.Issuer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      issuerName,
				Namespace: ns,
				UID:       types.UID(rand.String(9)),
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       certmanv1.IssuerKind,
				APIVersion: certmanv1.SchemeGroupVersion.String(),
			},
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

	clusterIssuerFactory := func(mutateFn ...func(issuer *certmanv1.ClusterIssuer)) *certmanv1.ClusterIssuer {
		issuer := &certmanv1.ClusterIssuer{
			ObjectMeta: metav1.ObjectMeta{Name: issuerName, Namespace: ns},
			TypeMeta: metav1.TypeMeta{
				Kind:       certmanv1.ClusterIssuerKind,
				APIVersion: certmanv1.SchemeGroupVersion.String(),
			},
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

	clusterIssuerNotReadyMutater := func(issuer *certmanv1.ClusterIssuer) {
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
			ObjectMeta: metav1.ObjectMeta{
				Name:      certificateName,
				Namespace: ns,
				UID:       types.UID(rand.String(9)),
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       certmanv1.CertificateKind,
				APIVersion: certmanv1.SchemeGroupVersion.String(),
			},
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

	gwFactory := func() *gatewayapiv1.Gateway {
		return &gatewayapiv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      gwName,
				Namespace: ns,
				UID:       types.UID(rand.String(9)),
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Gateway",
				APIVersion: gatewayapiv1.GroupVersion.String(),
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

	topologyOpts := func(policy *kuadrantv1alpha1.TLSPolicy, additionalOps ...machinery.GatewayAPITopologyOptionsFunc) []machinery.GatewayAPITopologyOptionsFunc {
		store := make(controller.Store)
		gw := gwFactory()
		store[string(gw.UID)] = gw
		store[string(policy.UID)] = policy

		opts := []machinery.GatewayAPITopologyOptionsFunc{
			machinery.WithGateways(gw),
			machinery.WithGatewayAPITopologyPolicies(policy),
			machinery.WithGatewayAPITopologyLinks(
				LinkGatewayToCertificateFunc(store),
				LinkGatewayToIssuerFunc(store),
				LinkGatewayToClusterIssuerFunc(store),
			),
		}
		opts = append(opts, additionalOps...)

		return opts
	}

	type args struct {
		tlsPolicy *kuadrantv1alpha1.TLSPolicy
		topology  func(*kuadrantv1alpha1.TLSPolicy) *machinery.Topology
	}
	tests := []struct {
		name string
		args args
		want *metav1.Condition
	}{
		{
			name: "unable to get issuer",
			args: args{
				tlsPolicy: policyFactory(),
				topology: func(p *kuadrantv1alpha1.TLSPolicy) *machinery.Topology {
					topology, _ := machinery.NewGatewayAPITopology(
						topologyOpts(p)...,
					)
					return topology
				},
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: fmt.Sprintf("TLSPolicy has encountered some issues: Issuer \"%s\" not found", issuerName),
			},
		},
		{
			name: "unable to get cluster issuer",
			args: args{
				tlsPolicy: policyFactory(withClusterIssuerMutater),
				topology: func(p *kuadrantv1alpha1.TLSPolicy) *machinery.Topology {
					topology, _ := machinery.NewGatewayAPITopology(
						topologyOpts(p)...,
					)
					return topology
				},
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: fmt.Sprintf("TLSPolicy has encountered some issues: ClusterIssuer \"%s\" not found", issuerName),
			},
		},
		{
			name: "issuer not ready",
			args: args{
				tlsPolicy: policyFactory(),
				topology: func(p *kuadrantv1alpha1.TLSPolicy) *machinery.Topology {
					opts := topologyOpts(p, machinery.WithGatewayAPITopologyObjects(
						&controller.RuntimeObject{Object: issuerFactory(issuerNotReadyMutater)},
					))
					topology, _ := machinery.NewGatewayAPITopology(opts...)
					return topology
				},
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: "TLSPolicy has encountered some issues: Issuer not ready",
			},
		},
		{
			name: "issuer has no ready condition",
			args: args{
				tlsPolicy: policyFactory(),
				topology: func(p *kuadrantv1alpha1.TLSPolicy) *machinery.Topology {
					opts := topologyOpts(p, machinery.WithGatewayAPITopologyObjects(
						&controller.RuntimeObject{Object: issuerFactory(func(issuer *certmanv1.Issuer) {
							issuer.Status.Conditions = []certmanv1.IssuerCondition{}
						})},
					))
					topology, _ := machinery.NewGatewayAPITopology(opts...)
					return topology
				},
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: "TLSPolicy has encountered some issues: Issuer not ready",
			},
		},
		{
			name: "cluster issuer not ready",
			args: args{
				tlsPolicy: policyFactory(withClusterIssuerMutater),
				topology: func(p *kuadrantv1alpha1.TLSPolicy) *machinery.Topology {
					opts := topologyOpts(p, machinery.WithGatewayAPITopologyObjects(
						&controller.RuntimeObject{Object: clusterIssuerFactory(clusterIssuerNotReadyMutater)},
					))
					topology, _ := machinery.NewGatewayAPITopology(opts...)
					return topology
				},
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: "TLSPolicy has encountered some issues: ClusterIssuer not ready",
			},
		},
		{
			name: "cluster issuer has no ready condition",
			args: args{
				tlsPolicy: policyFactory(withClusterIssuerMutater),
				topology: func(p *kuadrantv1alpha1.TLSPolicy) *machinery.Topology {
					opts := topologyOpts(p, machinery.WithGatewayAPITopologyObjects(
						&controller.RuntimeObject{Object: clusterIssuerFactory(func(issuer *certmanv1.ClusterIssuer) {
							issuer.Status.Conditions = []certmanv1.IssuerCondition{}
						})},
					))
					topology, _ := machinery.NewGatewayAPITopology(opts...)
					return topology
				},
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: "TLSPolicy has encountered some issues: ClusterIssuer not ready",
			},
		},
		{
			name: "no valid gateways found",
			args: args{
				tlsPolicy: policyFactory(),
				topology: func(_ *kuadrantv1alpha1.TLSPolicy) *machinery.Topology {
					opts := topologyOpts(policyFactory(), machinery.WithGatewayAPITopologyObjects(
						&controller.RuntimeObject{Object: issuerFactory()},
					))
					topology, _ := machinery.NewGatewayAPITopology(opts...)
					return topology
				},
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
			args: args{
				tlsPolicy: policyFactory(),
				topology: func(policy *kuadrantv1alpha1.TLSPolicy) *machinery.Topology {
					opts := topologyOpts(policy, machinery.WithGatewayAPITopologyObjects(
						&controller.RuntimeObject{Object: issuerFactory()},
					))
					topology, _ := machinery.NewGatewayAPITopology(opts...)
					return topology
				},
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: "TLSPolicy has encountered some issues: certificate not found",
			},
		},
		{
			name: "certificate is not ready",
			args: args{
				tlsPolicy: policyFactory(),
				topology: func(policy *kuadrantv1alpha1.TLSPolicy) *machinery.Topology {
					opts := topologyOpts(policy, machinery.WithGatewayAPITopologyObjects(
						&controller.RuntimeObject{Object: issuerFactory()},
						&controller.RuntimeObject{Object: certificateFactory(certificateNotReadyMutater)},
					))
					topology, _ := machinery.NewGatewayAPITopology(opts...)
					return topology
				},
			},
			want: &metav1.Condition{
				Type:    string(kuadrant.PolicyConditionEnforced),
				Status:  metav1.ConditionFalse,
				Reason:  string(kuadrant.PolicyReasonUnknown),
				Message: fmt.Sprintf("TLSPolicy has encountered some issues: certificate %s not ready", certificateName),
			},
		},
		{
			name: "certificate has no ready condition",
			args: args{
				tlsPolicy: policyFactory(),
				topology: func(policy *kuadrantv1alpha1.TLSPolicy) *machinery.Topology {
					opts := topologyOpts(policy, machinery.WithGatewayAPITopologyObjects(
						&controller.RuntimeObject{Object: issuerFactory()},
						&controller.RuntimeObject{Object: certificateFactory(func(certificate *certmanv1.Certificate) {
							certificate.Status.Conditions = []certmanv1.CertificateCondition{}
						})},
					))
					topology, _ := machinery.NewGatewayAPITopology(opts...)
					return topology
				},
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
			args: args{
				tlsPolicy: policyFactory(),
				topology: func(policy *kuadrantv1alpha1.TLSPolicy) *machinery.Topology {
					opts := topologyOpts(policy, machinery.WithGatewayAPITopologyObjects(
						&controller.RuntimeObject{Object: issuerFactory()},
						&controller.RuntimeObject{Object: certificateFactory()},
					))
					topology, _ := machinery.NewGatewayAPITopology(opts...)
					return topology
				},
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
		t.Run(tt.name, func(t1 *testing.T) {
			t := &TLSPolicyStatusTask{}
			if got := t.enforcedCondition(context.Background(), tt.args.tlsPolicy, tt.args.topology(tt.args.tlsPolicy)); !reflect.DeepEqual(got, tt.want) {
				t1.Errorf("enforcedCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}
