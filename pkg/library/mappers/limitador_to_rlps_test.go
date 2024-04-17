package mappers

import (
	"context"
	"reflect"
	"testing"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestLimitadorToRateLimitPoliciesEventMapper_Map(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kuadrantv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := kuadrantv1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	type fields struct {
		opts MapperOptions
	}
	type args struct {
		ctx context.Context
		obj client.Object
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   []reconcile.Request
	}{
		{
			name: "no kuadrants in object ns",
			fields: fields{
				opts: MapperOptions{Logger: log.NewLogger(),
					Client: fake.NewClientBuilder().WithObjects(
						&kuadrantv1beta1.Kuadrant{ObjectMeta: metav1.ObjectMeta{Namespace: "kuadrant"}},
					).WithScheme(scheme).Build()},
			},
			args: args{
				ctx: context.Background(),
				obj: &limitadorv1alpha1.Limitador{ObjectMeta: metav1.ObjectMeta{Namespace: "limitador"}},
			},
			want: []reconcile.Request{},
		},
		{
			name: "kuadrant in object ns - map RLP to requests",
			fields: fields{
				opts: MapperOptions{
					Logger: log.NewLogger(),
					Client: fake.NewClientBuilder().WithObjects(
						&kuadrantv1beta1.Kuadrant{ObjectMeta: metav1.ObjectMeta{Namespace: "kuadrant"}},
						&kuadrantv1beta2.RateLimitPolicy{ObjectMeta: metav1.ObjectMeta{Name: "rlp1", Namespace: "ns1"}},
						&kuadrantv1beta2.RateLimitPolicy{ObjectMeta: metav1.ObjectMeta{Name: "rlp2", Namespace: "ns2"}},
					).WithScheme(scheme).Build()},
			},
			args: args{
				ctx: context.Background(),
				obj: &limitadorv1alpha1.Limitador{ObjectMeta: metav1.ObjectMeta{Namespace: "kuadrant"}},
			},
			want: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "rlp1", Namespace: "ns1"}},
				{NamespacedName: types.NamespacedName{Name: "rlp2", Namespace: "ns2"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &LimitadorToRateLimitPoliciesEventMapper{
				opts: tt.fields.opts,
			}
			if got := m.Map(tt.args.ctx, tt.args.obj); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Map() = %v, want %v", got, tt.want)
			}
		})
	}
}
