//go:build unit

package controllers

import (
	"reflect"
	"testing"
	"time"

	"github.com/kuadrant/policy-machinery/machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
		name    string
		args    args
		want    *kuadrantv1beta1.Kuadrant
		wantErr bool
	}{
		{
			name:    "oldest is first",
			args:    args{topology: newTopology([]*kuadrantv1beta1.Kuadrant{expected, unexpected})},
			want:    expected,
			wantErr: false,
		}, {
			name:    "oldest is second",
			args:    args{topology: newTopology([]*kuadrantv1beta1.Kuadrant{unexpected, expected})},
			want:    expected,
			wantErr: false,
		},
		{
			name:    "Empty list is passed",
			args:    args{topology: newTopology([]*kuadrantv1beta1.Kuadrant{})},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "only item is marked for deletion",
			args:    args{topology: newTopology([]*kuadrantv1beta1.Kuadrant{deleted})},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "first item is marked for deletion",
			args:    args{topology: newTopology([]*kuadrantv1beta1.Kuadrant{deleted, expected})},
			want:    expected,
			wantErr: false,
		},
		{
			name:    "all items is marked for deletion",
			args:    args{topology: newTopology([]*kuadrantv1beta1.Kuadrant{deleted, deleted})},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetKuadrant(tt.args.topology)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetKuadrant() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetKuadrant() got = %v, want %v", got, tt.want)
			}
		})
	}
}
