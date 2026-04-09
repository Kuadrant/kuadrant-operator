//go:build unit

package controllers

import (
	"reflect"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/kuadrant/policy-machinery/machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

func TestGetKuadrant(t *testing.T) {
	unexpected := &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Kuadrant",
			APIVersion: "kuadrant.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "UnExpected",
			CreationTimestamp: metav1.Time{
				Time: time.Unix(3, 0),
			},
		},
	}
	expected := &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Kuadrant",
			APIVersion: "kuadrant.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Expected",
			CreationTimestamp: metav1.Time{
				Time: time.Unix(2, 0),
			},
		},
	}
	deleted := &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Kuadrant",
			APIVersion: "kuadrant.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Expected",
			CreationTimestamp: metav1.Time{
				Time: time.Unix(2, 0),
			},
			DeletionTimestamp: &metav1.Time{
				Time: time.Unix(1, 0),
			},
		},
	}
	type args struct {
		topology *machinery.Topology
	}

	newTopology := func(kuadrantList []*kuadrantv1beta1.Kuadrant) *machinery.Topology {
		topology, err := machinery.NewTopology(
			machinery.WithObjects(kuadrantList...))
		if err != nil {
			t.Fatalf("failed to create topology: %v", err)
		}
		return topology
	}
	tests := []struct {
		name string
		args args
		want *kuadrantv1beta1.Kuadrant
	}{
		{
			name: "oldest is first",
			args: args{topology: newTopology([]*kuadrantv1beta1.Kuadrant{expected, unexpected})},
			want: expected,
		}, {
			name: "oldest is second",
			args: args{topology: newTopology([]*kuadrantv1beta1.Kuadrant{unexpected, expected})},
			want: expected,
		},
		{
			name: "Empty list is passed",
			args: args{topology: newTopology([]*kuadrantv1beta1.Kuadrant{})},
			want: nil,
		},
		{
			name: "only item is marked for deletion",
			args: args{topology: newTopology([]*kuadrantv1beta1.Kuadrant{deleted})},
			want: nil,
		},
		{
			name: "first item is marked for deletion",
			args: args{topology: newTopology([]*kuadrantv1beta1.Kuadrant{deleted, expected})},
			want: expected,
		},
		{
			name: "all items is marked for deletion",
			args: args{topology: newTopology([]*kuadrantv1beta1.Kuadrant{deleted, deleted})},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetKuadrantFromTopology(tt.args.topology)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetKuadrantFromTopology() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIssuerStatusChangedPredicate(t *testing.T) {
	ready := certmanagerv1.IssuerStatus{
		Conditions: []certmanagerv1.IssuerCondition{{
			Type:   certmanagerv1.IssuerConditionReady,
			Status: certmanmetav1.ConditionTrue,
		}},
	}
	notReady := certmanagerv1.IssuerStatus{
		Conditions: []certmanagerv1.IssuerCondition{{
			Type:   certmanagerv1.IssuerConditionReady,
			Status: certmanmetav1.ConditionFalse,
		}},
	}

	predicate := issuerStatusChangedPredicate()

	tests := []struct {
		name string
		old  certmanagerv1.IssuerStatus
		new  certmanagerv1.IssuerStatus
		want bool
	}{
		{
			name: "status change triggers reconciliation",
			old:  notReady,
			new:  ready,
			want: true,
		},
		{
			name: "unchanged status is ignored",
			old:  ready,
			new:  ready,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := predicate.Update(event.TypedUpdateEvent[*certmanagerv1.Issuer]{
				ObjectOld: &certmanagerv1.Issuer{Status: tt.old},
				ObjectNew: &certmanagerv1.Issuer{Status: tt.new},
			})
			if got != tt.want {
				t.Fatalf("issuerStatusChangedPredicate().Update() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClusterIssuerStatusChangedPredicate(t *testing.T) {
	ready := certmanagerv1.IssuerStatus{
		Conditions: []certmanagerv1.IssuerCondition{{
			Type:   certmanagerv1.IssuerConditionReady,
			Status: certmanmetav1.ConditionTrue,
		}},
	}
	notReady := certmanagerv1.IssuerStatus{
		Conditions: []certmanagerv1.IssuerCondition{{
			Type:   certmanagerv1.IssuerConditionReady,
			Status: certmanmetav1.ConditionFalse,
		}},
	}

	predicate := clusterIssuerStatusChangedPredicate()

	tests := []struct {
		name string
		old  certmanagerv1.IssuerStatus
		new  certmanagerv1.IssuerStatus
		want bool
	}{
		{
			name: "status change triggers reconciliation",
			old:  notReady,
			new:  ready,
			want: true,
		},
		{
			name: "unchanged status is ignored",
			old:  ready,
			new:  ready,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := predicate.Update(event.TypedUpdateEvent[*certmanagerv1.ClusterIssuer]{
				ObjectOld: &certmanagerv1.ClusterIssuer{Status: tt.old},
				ObjectNew: &certmanagerv1.ClusterIssuer{Status: tt.new},
			})
			if got != tt.want {
				t.Fatalf("clusterIssuerStatusChangedPredicate().Update() = %v, want %v", got, tt.want)
			}
		})
	}
}
