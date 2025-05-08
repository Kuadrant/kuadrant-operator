//go:build unit

package controllers

import (
	"testing"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/intstr"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

func TestResilienceCounterStorageReconciler_isConfigured(t *testing.T) {
	testCases := []struct {
		name     string
		kObj     *kuadrantv1beta1.Kuadrant
		expected bool
	}{

		{
			name:     "expected, isConfigured=true",
			kObj:     &kuadrantv1beta1.Kuadrant{Spec: kuadrantv1beta1.KuadrantSpec{Resilience: &kuadrantv1beta1.Resilience{CounterStorage: &limitadorv1alpha1.Storage{}}}},
			expected: true,
		},
		{
			name:     "expected, isConfigured=false, no storage object",
			kObj:     &kuadrantv1beta1.Kuadrant{Spec: kuadrantv1beta1.KuadrantSpec{Resilience: &kuadrantv1beta1.Resilience{}}},
			expected: false,
		},
		{
			name:     "expected, isConfigured=false, no reilience object",
			kObj:     &kuadrantv1beta1.Kuadrant{Spec: kuadrantv1beta1.KuadrantSpec{}},
			expected: false,
		},
		{
			name:     "expected, isConfigured=false, kObj is nil",
			kObj:     nil,
			expected: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			rcsr := NewResilienceCounterStorageReconciler(nil)
			result := rcsr.isConfigured(tc.kObj)
			if result != tc.expected {
				subT.Fatalf("isConfigured result not as expected. Expected: %v, Actual: %v", tc.expected, result)
			}
		})
	}
}

func TestLimitadorPDBIsConfigured(t *testing.T) {
	testCases := []struct {
		name     string
		lObj     *limitadorv1alpha1.Limitador
		expected bool
	}{
		{
			name:     "limitador object is nil",
			expected: false,
			lObj:     nil,
		},
		{
			name:     "limitador PBD spec is nil",
			expected: false,
			lObj:     &limitadorv1alpha1.Limitador{},
		},
		{
			name:     "limitador PBD spec is not nil, but empty",
			expected: false,
			lObj: &limitadorv1alpha1.Limitador{
				Spec: limitadorv1alpha1.LimitadorSpec{
					PodDisruptionBudget: &limitadorv1alpha1.PodDisruptionBudgetType{},
				},
			},
		},
		{
			name:     "limitador PDB MaxUnaviaible is set",
			expected: true,
			lObj: &limitadorv1alpha1.Limitador{
				Spec: limitadorv1alpha1.LimitadorSpec{
					PodDisruptionBudget: &limitadorv1alpha1.PodDisruptionBudgetType{
						MaxUnavailable: &intstr.IntOrString{IntVal: 1},
					},
				},
			},
		},
		{
			name:     "limitador PDB MinAviaible is set",
			expected: true,
			lObj: &limitadorv1alpha1.Limitador{
				Spec: limitadorv1alpha1.LimitadorSpec{
					PodDisruptionBudget: &limitadorv1alpha1.PodDisruptionBudgetType{
						MinAvailable: &intstr.IntOrString{IntVal: 1},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			result := limitadorPDBIsConfigured(tc.lObj)
			if result != tc.expected {
				subT.Fatalf("limitadorPDBIsConfigured result not as expected. Expected: %v, Actual: %v", tc.expected, result)
			}
		})
	}

}
