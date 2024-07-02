//go:build unit

package controllers

import (
	"context"
	"errors"
	"reflect"
	"testing"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	v1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

func Test_mapClusterIssuerToPolicy(t *testing.T) {
	clusterIssuer := &certmanagerv1.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-issuer",
		},
	}

	s := runtime.NewScheme()
	sb := runtime.NewSchemeBuilder(certmanagerv1.AddToScheme, v1alpha1.AddToScheme)
	if err := sb.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	type args struct {
		k8sClient client.Client
		object    client.Object
	}
	tests := []struct {
		name string
		args args
		want []reconcile.Request
	}{
		{
			name: "not a cluster issuer",
			args: args{
				object: &certmanagerv1.Certificate{},
			},
			want: nil,
		},
		{
			name: "list error",
			args: args{
				k8sClient: fake.NewClientBuilder().WithScheme(s).WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						return errors.New("list error")
					},
				}).Build(),
				object: clusterIssuer,
			},
			want: nil,
		},
		{
			name: "map cluster issuer to matching policies",
			args: args{
				k8sClient: fake.NewClientBuilder().WithScheme(s).WithObjects(clusterIssuer).WithLists(testInitTLSPolicies(clusterIssuer.Name, certmanagerv1.ClusterIssuerKind)).Build(),
				object:    clusterIssuer,
			},
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{Name: "p1", Namespace: "n1"},
				},
				{
					NamespacedName: types.NamespacedName{Name: "p4", Namespace: "n2"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapClusterIssuerToPolicy(context.Background(), tt.args.k8sClient, logr.Logger{}, tt.args.object); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mapClusterIssuerToPolicy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_mapIssuerToPolicy(t *testing.T) {
	issuer := &certmanagerv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-issuer",
			Namespace: "n1",
		},
	}

	s := runtime.NewScheme()
	sb := runtime.NewSchemeBuilder(certmanagerv1.AddToScheme, v1alpha1.AddToScheme)
	if err := sb.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	type args struct {
		ctx       context.Context
		k8sClient client.Client
		logger    logr.Logger
		object    client.Object
	}
	tests := []struct {
		name string
		args args
		want []reconcile.Request
	}{
		{
			name: "not an issuer",
			args: args{
				object: &certmanagerv1.Certificate{},
			},
			want: nil,
		},
		{
			name: "list error",
			args: args{
				k8sClient: fake.NewClientBuilder().WithScheme(s).WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						return errors.New("list error")
					},
				}).Build(),
				object: issuer,
			},
			want: nil,
		},
		{
			name: "map issuer to matching policies",
			args: args{
				k8sClient: fake.NewClientBuilder().WithScheme(s).WithObjects(issuer).WithLists(testInitTLSPolicies(issuer.Name, certmanagerv1.IssuerKind)).Build(),
				object:    issuer,
			},
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{Name: "p1", Namespace: "n1"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapIssuerToPolicy(tt.args.ctx, tt.args.k8sClient, tt.args.logger, tt.args.object); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mapIssuerToPolicy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func testInitTLSPolicies(issuerName, issuerKind string) *v1alpha1.TLSPolicyList {
	return &v1alpha1.TLSPolicyList{
		Items: []v1alpha1.TLSPolicy{

			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "p1",
					Namespace: "n1",
				},
				Spec: v1alpha1.TLSPolicySpec{
					CertificateSpec: v1alpha1.CertificateSpec{
						IssuerRef: v1.ObjectReference{
							Name:  issuerName,
							Group: certmanagerv1.SchemeGroupVersion.Group,
							Kind:  issuerKind,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "p2",
					Namespace: "n1",
				},
				Spec: v1alpha1.TLSPolicySpec{
					CertificateSpec: v1alpha1.CertificateSpec{
						IssuerRef: v1.ObjectReference{
							Name:  issuerName,
							Group: "unknown.example.com",
							Kind:  issuerKind,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "p3",
					Namespace: "n1",
				},
				Spec: v1alpha1.TLSPolicySpec{
					CertificateSpec: v1alpha1.CertificateSpec{
						IssuerRef: v1.ObjectReference{
							Name:  issuerName,
							Group: certmanagerv1.SchemeGroupVersion.Group,
							Kind:  "Unknown",
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "p4",
					Namespace: "n2",
				},
				Spec: v1alpha1.TLSPolicySpec{
					CertificateSpec: v1alpha1.CertificateSpec{
						IssuerRef: v1.ObjectReference{
							Name:  issuerName,
							Group: certmanagerv1.SchemeGroupVersion.Group,
							Kind:  issuerKind,
						},
					},
				},
			},
		},
	}
}
