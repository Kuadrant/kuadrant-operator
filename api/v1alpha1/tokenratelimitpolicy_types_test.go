/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"testing"

	"github.com/kuadrant/policy-machinery/machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
)

func TestTokenRateLimitPolicy_GetTargetRef(t *testing.T) {
	policy := &TokenRateLimitPolicy{
		Spec: TokenRateLimitPolicySpec{
			TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
					Group: "gateway.networking.k8s.io",
					Kind:  "Gateway",
					Name:  "test-gateway",
				},
			},
		},
	}

	targetRef := policy.GetTargetRef()
	if targetRef.Group != "gateway.networking.k8s.io" {
		t.Errorf("Expected group 'gateway.networking.k8s.io', got '%s'", targetRef.Group)
	}
	if targetRef.Kind != "Gateway" {
		t.Errorf("Expected kind 'Gateway', got '%s'", targetRef.Kind)
	}
	if targetRef.Name != "test-gateway" {
		t.Errorf("Expected name 'test-gateway', got '%s'", targetRef.Name)
	}
}

func TestTokenRateLimitPolicy_GetTargetRefs(t *testing.T) {
	policy := &TokenRateLimitPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: "test-namespace",
		},
		Spec: TokenRateLimitPolicySpec{
			TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
					Group: "gateway.networking.k8s.io",
					Kind:  "HTTPRoute",
					Name:  "test-route",
				},
			},
		},
	}

	targetRefs := policy.GetTargetRefs()
	if len(targetRefs) != 1 {
		t.Errorf("Expected 1 target ref, got %d", len(targetRefs))
	}

	if ref, ok := targetRefs[0].(machinery.LocalPolicyTargetReferenceWithSectionName); ok {
		if ref.PolicyNamespace != "test-namespace" {
			t.Errorf("Expected policy namespace 'test-namespace', got '%s'", ref.PolicyNamespace)
		}
		if ref.Name != "test-route" {
			t.Errorf("Expected name 'test-route', got '%s'", ref.Name)
		}
	} else {
		t.Error("Expected LocalPolicyTargetReferenceWithSectionName type")
	}
}

func TestTokenRateLimitPolicy_Kind(t *testing.T) {
	policy := &TokenRateLimitPolicy{}
	if policy.Kind() != "TokenRateLimitPolicy" {
		t.Errorf("Expected kind 'TokenRateLimitPolicy', got '%s'", policy.Kind())
	}
}

func TestTokenRateLimitPolicy_Empty(t *testing.T) {
	tests := []struct {
		name     string
		policy   *TokenRateLimitPolicy
		expected bool
	}{
		{
			name: "empty policy",
			policy: &TokenRateLimitPolicy{
				Spec: TokenRateLimitPolicySpec{},
			},
			expected: true,
		},
		{
			name: "policy with limits",
			policy: &TokenRateLimitPolicy{
				Spec: TokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: TokenRateLimitPolicySpecProper{
						Limits: map[string]TokenLimit{
							"test": {},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "policy with defaults containing limits",
			policy: &TokenRateLimitPolicy{
				Spec: TokenRateLimitPolicySpec{
					Defaults: &MergeableTokenRateLimitPolicySpec{
						TokenRateLimitPolicySpecProper: TokenRateLimitPolicySpecProper{
							Limits: map[string]TokenLimit{
								"test": {},
							},
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.Empty(); got != tt.expected {
				t.Errorf("Empty() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTokenRateLimitPolicy_GetMergeStrategy(t *testing.T) {
	tests := []struct {
		name   string
		policy *TokenRateLimitPolicy
	}{
		{
			name: "atomic defaults",
			policy: &TokenRateLimitPolicy{
				Spec: TokenRateLimitPolicySpec{
					Defaults: &MergeableTokenRateLimitPolicySpec{
						Strategy: "atomic",
					},
				},
			},
		},
		{
			name: "merge defaults",
			policy: &TokenRateLimitPolicy{
				Spec: TokenRateLimitPolicySpec{
					Defaults: &MergeableTokenRateLimitPolicySpec{
						Strategy: "merge",
					},
				},
			},
		},
		{
			name: "atomic overrides",
			policy: &TokenRateLimitPolicy{
				Spec: TokenRateLimitPolicySpec{
					Overrides: &MergeableTokenRateLimitPolicySpec{
						Strategy: "atomic",
					},
				},
			},
		},
		{
			name: "implicit defaults",
			policy: &TokenRateLimitPolicy{
				Spec: TokenRateLimitPolicySpec{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy := tt.policy.GetMergeStrategy()
			if strategy == nil {
				t.Error("Expected merge strategy, got nil")
			}

			// Test that the merge strategy works
			source := &TokenRateLimitPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "source"},
			}
			target := &TokenRateLimitPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "target"},
			}

			result := strategy(source, target)
			if result == nil {
				t.Error("Expected merge result, got nil")
			}
		})
	}
}

func TestTokenRateLimitPolicy_Proper(t *testing.T) {
	tests := []struct {
		name     string
		spec     TokenRateLimitPolicySpec
		expected *TokenRateLimitPolicySpecProper
	}{
		{
			name: "defaults",
			spec: TokenRateLimitPolicySpec{
				Defaults: &MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: TokenRateLimitPolicySpecProper{
						Limits: map[string]TokenLimit{"test": {}},
					},
				},
			},
			expected: &TokenRateLimitPolicySpecProper{
				Limits: map[string]TokenLimit{"test": {}},
			},
		},
		{
			name: "overrides",
			spec: TokenRateLimitPolicySpec{
				Overrides: &MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: TokenRateLimitPolicySpecProper{
						Limits: map[string]TokenLimit{"test": {}},
					},
				},
			},
			expected: &TokenRateLimitPolicySpecProper{
				Limits: map[string]TokenLimit{"test": {}},
			},
		},
		{
			name: "implicit defaults",
			spec: TokenRateLimitPolicySpec{
				TokenRateLimitPolicySpecProper: TokenRateLimitPolicySpecProper{
					Limits: map[string]TokenLimit{"test": {}},
				},
			},
			expected: &TokenRateLimitPolicySpecProper{
				Limits: map[string]TokenLimit{"test": {}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proper := tt.spec.Proper()
			if proper == nil {
				t.Error("Expected proper spec, got nil")
				return
			}

			// Compare the limits map
			if len(proper.Limits) != len(tt.expected.Limits) {
				t.Errorf("Expected limits count mismatch: got %d, expected %d", len(proper.Limits), len(tt.expected.Limits))
			}
		})
	}
}

func TestTokenLimit_CountersAsStringList(t *testing.T) {
	tests := []struct {
		name     string
		limit    TokenLimit
		expected []string
	}{
		{
			name:     "no counters",
			limit:    TokenLimit{},
			expected: nil,
		},
		{
			name: "single counter",
			limit: TokenLimit{
				Counters: []kuadrantv1.Counter{
					{Expression: kuadrantv1.Expression("auth.identity.userid")},
				},
			},
			expected: []string{"descriptors[0][\"auth.identity.userid\"]"},
		},
		{
			name: "multiple counters",
			limit: TokenLimit{
				Counters: []kuadrantv1.Counter{
					{Expression: kuadrantv1.Expression("auth.identity.userid")},
					{Expression: kuadrantv1.Expression("request.headers.client-id")},
				},
			},
			expected: []string{"descriptors[0][\"auth.identity.userid\"]", "descriptors[0][\"request.headers.client-id\"]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.limit.CountersAsStringList()
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d counters, got %d", len(tt.expected), len(result))
				return
			}
			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("Expected counter %d to be '%s', got '%s'", i, expected, result[i])
				}
			}
		})
	}
}
