//go:build unit

package kuadranttools

import (
	"reflect"
	"strings"
	"testing"

	"k8s.io/utils/ptr"

	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestLimitadorMutator(t *testing.T) {
	type args struct {
		existingObj client.Object
		desiredObj  client.Object
	}
	tests := []struct {
		name          string
		args          args
		want          bool
		wantErr       bool
		errorContains string
	}{
		{
			name:          "existingObj is not a limitador type",
			wantErr:       true,
			errorContains: "existingObj",
		},
		{
			name: "desiredObj is not a limitador type",
			args: args{
				existingObj: &limitadorv1alpha1.Limitador{},
			},
			wantErr:       true,
			errorContains: "desireObj",
		},
		{
			name: "No update required",
			args: args{
				existingObj: &limitadorv1alpha1.Limitador{},
				desiredObj:  &limitadorv1alpha1.Limitador{},
			},
			want: false,
		},
		{
			name: "Update required",
			args: args{
				existingObj: &limitadorv1alpha1.Limitador{
					Spec: limitadorv1alpha1.LimitadorSpec{
						Replicas: ptr.To(3),
					},
				},
				desiredObj: &limitadorv1alpha1.Limitador{
					Spec: limitadorv1alpha1.LimitadorSpec{
						Replicas: ptr.To(1),
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LimitadorMutator(tt.args.existingObj, tt.args.desiredObj)
			if (err != nil) != tt.wantErr {
				t.Errorf("LimitadorMutator() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.wantErr {
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("LimitadorMutator() error = %v, should contain %v", err, tt.errorContains)
				}
			}
			if got != tt.want {
				t.Errorf("LimitadorMutator() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_limitadorSpecSubSet(t *testing.T) {
	type args struct {
		spec limitadorv1alpha1.LimitadorSpec
	}
	tests := []struct {
		name string
		args args
		want v1beta1.LimitadorSpec
	}{
		{
			name: "Empty spec passed",
			args: args{spec: limitadorv1alpha1.LimitadorSpec{}},
			want: v1beta1.LimitadorSpec{},
		},
		{
			name: "Full spec passed",
			args: args{spec: limitadorv1alpha1.LimitadorSpec{
				Affinity: &corev1.Affinity{
					PodAffinity: &corev1.PodAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								Weight: 100,
								PodAffinityTerm: corev1.PodAffinityTerm{LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app": "limitador",
									},
								}},
							},
						},
					},
				},
				Replicas: ptr.To(3),
				Storage: &limitadorv1alpha1.Storage{
					Redis: &limitadorv1alpha1.Redis{
						ConfigSecretRef: &corev1.LocalObjectReference{
							Name: "secret_config",
						},
					},
				},
				PodDisruptionBudget: &limitadorv1alpha1.PodDisruptionBudgetType{
					MinAvailable: &intstr.IntOrString{
						IntVal: 1,
					},
				},
				ResourceRequirements: &corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			}},
			want: v1beta1.LimitadorSpec{
				Affinity: &corev1.Affinity{
					PodAffinity: &corev1.PodAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								Weight: 100,
								PodAffinityTerm: corev1.PodAffinityTerm{LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app": "limitador",
									},
								}},
							},
						},
					},
				},
				Replicas: ptr.To(3),
				Storage: &limitadorv1alpha1.Storage{
					Redis: &limitadorv1alpha1.Redis{
						ConfigSecretRef: &corev1.LocalObjectReference{
							Name: "secret_config",
						},
					},
				},
				PodDisruptionBudget: &limitadorv1alpha1.PodDisruptionBudgetType{
					MinAvailable: &intstr.IntOrString{
						IntVal: 1,
					},
				},
				ResourceRequirements: &corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			},
		},
		{
			name: "Partial spec passed",
			args: args{spec: limitadorv1alpha1.LimitadorSpec{
				Replicas: ptr.To(3),
				ResourceRequirements: &corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			}},
			want: v1beta1.LimitadorSpec{
				Replicas: ptr.To(3),
				ResourceRequirements: &corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := limitadorSpecSubSet(tt.args.spec); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("limitadorSpecSubSet() = %v, want %v", got, tt.want)
			}
		})
	}
}
