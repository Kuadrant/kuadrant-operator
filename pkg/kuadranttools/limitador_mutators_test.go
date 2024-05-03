//go:build unit

package kuadranttools

import (
	"strings"
	"testing"

	"k8s.io/utils/ptr"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestLimitadorMutator(t *testing.T) {
	limitadorMutator := LimitadorMutator(
		LimitadorOwnerRefsMutator,
		LimitadorAffinityMutator,
		LimitadorReplicasMutator,
		LimitadorStorageMutator,
		LimitadorRateLimitHeadersMutator,
		LimitadorTelemetryMutator,
		LimitadorPodDisruptionBudgetMutator,
		LimitadorResourceRequirementsMutator,
		LimitadorVerbosityMutator,
	)

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
			errorContains: "desiredObj",
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
			got, err := limitadorMutator(tt.args.existingObj, tt.args.desiredObj)
			if (err != nil) != tt.wantErr {
				t.Errorf("limitadorMutator() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.wantErr {
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("limitadorMutator() error = %v, should contain %v", err, tt.errorContains)
				}
			}
			if got != tt.want {
				t.Errorf("limitadorMutator() got = %v, want %v", got, tt.want)
			}
		})
	}
}
