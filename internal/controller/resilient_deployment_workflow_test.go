//go:build unit

package controllers

import (
	"testing"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

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
