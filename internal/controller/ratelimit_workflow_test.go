//go:build unit

package controllers

import (
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	k8stypes "k8s.io/apimachinery/pkg/types"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

func TestLimitNameToLimitadorIdentifier(t *testing.T) {
	testCases := []struct {
		name            string
		rlpKey          k8stypes.NamespacedName
		uniqueLimitName string
		expected        *regexp.Regexp
	}{
		{
			name:            "prepends the limitador limit identifier prefix",
			rlpKey:          k8stypes.NamespacedName{Namespace: "testNS", Name: "rlpA"},
			uniqueLimitName: "foo",
			expected:        regexp.MustCompile(`^limit\.foo.+`),
		},
		{
			name:            "sanitizes invalid chars",
			rlpKey:          k8stypes.NamespacedName{Namespace: "testNS", Name: "rlpA"},
			uniqueLimitName: "my/limit-0",
			expected:        regexp.MustCompile(`^limit\.my_limit_0.+$`),
		},
		{
			name:            "sanitizes the dot char (.) even though it is a valid char in limitador identifiers",
			rlpKey:          k8stypes.NamespacedName{Namespace: "testNS", Name: "rlpA"},
			uniqueLimitName: "my.limit",
			expected:        regexp.MustCompile(`^limit\.my_limit.+$`),
		},
		{
			name:            "appends a hash of the original name to avoid breaking uniqueness",
			rlpKey:          k8stypes.NamespacedName{Namespace: "testNS", Name: "rlpA"},
			uniqueLimitName: "foo",
			expected:        regexp.MustCompile(`^.+__1da6e70a$`),
		},
		{
			name:            "different rlp keys result in different identifiers",
			rlpKey:          k8stypes.NamespacedName{Namespace: "testNS", Name: "rlpB"},
			uniqueLimitName: "foo",
			expected:        regexp.MustCompile(`^.+__2c1520b6$`),
		},
		{
			name:            "empty string",
			rlpKey:          k8stypes.NamespacedName{Namespace: "testNS", Name: "rlpA"},
			uniqueLimitName: "",
			expected:        regexp.MustCompile(`^limit.__6d5e49dc$`),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			identifier := LimitNameToLimitadorIdentifier(tc.rlpKey, tc.uniqueLimitName)
			if !tc.expected.MatchString(identifier) {
				subT.Errorf("identifier does not match, expected(%s), got (%s)", tc.expected, identifier)
			}
		})
	}
}

func TestWasmActionFromLimit(t *testing.T) {
	testCases := []struct {
		name               string
		limit              *kuadrantv1.Limit
		limitIdentifier    string
		scope              string
		topLevelPredicates kuadrantv1.WhenPredicates
		expectedAction     wasm.Action
	}{
		{
			name:            "limit without conditions nor counters",
			limit:           &kuadrantv1.Limit{},
			limitIdentifier: "limit.myLimit__d681f6c3",
			scope:           "my-ns/my-route",
			expectedAction: wasm.Action{
				SourcePolicyLocators: []string{"test/policy/locator"},
				ServiceName:          wasm.RateLimitServiceName,
				Scope:                "my-ns/my-route",
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{
							{
								Value: &wasm.Expression{
									ExpressionItem: wasm.ExpressionItem{
										Key:   "limit.myLimit__d681f6c3",
										Value: "1",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "limit with counter qualifiers",
			limit: &kuadrantv1.Limit{
				Counters: []kuadrantv1.Counter{{Expression: "auth.identity.username"}},
			},
			limitIdentifier: "limit.myLimit__d681f6c3",
			scope:           "my-ns/my-route",
			expectedAction: wasm.Action{
				SourcePolicyLocators: []string{"test/policy/locator"},
				ServiceName:          wasm.RateLimitServiceName,
				Scope:                "my-ns/my-route",
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{
							{
								Value: &wasm.Expression{
									ExpressionItem: wasm.ExpressionItem{
										Key:   "limit.myLimit__d681f6c3",
										Value: "1",
									},
								},
							},
							{
								Value: &wasm.Expression{
									ExpressionItem: wasm.ExpressionItem{
										Key:   "auth.identity.username",
										Value: "auth.identity.username",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "limit with counter qualifiers and when predicates",
			limit: &kuadrantv1.Limit{
				Counters: []kuadrantv1.Counter{{Expression: "auth.identity.username"}},
				When:     kuadrantv1.NewWhenPredicates("auth.identity.group != admin"),
			},
			limitIdentifier: "limit.myLimit__d681f6c3",
			scope:           "my-ns/my-route",
			expectedAction: wasm.Action{
				SourcePolicyLocators: []string{"test/policy/locator"},
				ServiceName:          wasm.RateLimitServiceName,
				Scope:                "my-ns/my-route",
				ConditionalData: []wasm.ConditionalData{
					{
						Predicates: []string{"auth.identity.group != admin"},
						Data: []wasm.DataType{
							{
								Value: &wasm.Expression{
									ExpressionItem: wasm.ExpressionItem{
										Key:   "limit.myLimit__d681f6c3",
										Value: "1",
									},
								},
							},
							{
								Value: &wasm.Expression{
									ExpressionItem: wasm.ExpressionItem{
										Key:   "auth.identity.username",
										Value: "auth.identity.username",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:               "limit with top level predicates and no when predicates",
			limit:              &kuadrantv1.Limit{},
			topLevelPredicates: kuadrantv1.NewWhenPredicates("auth.identity.group != admin"),
			limitIdentifier:    "limit.myLimit__d681f6c3",
			scope:              "my-ns/my-route",
			expectedAction: wasm.Action{
				SourcePolicyLocators: []string{"test/policy/locator"},
				ServiceName:          wasm.RateLimitServiceName,
				Scope:                "my-ns/my-route",
				ConditionalData: []wasm.ConditionalData{
					{
						Predicates: []string{"auth.identity.group != admin"},
						Data: []wasm.DataType{
							{
								Value: &wasm.Expression{
									ExpressionItem: wasm.ExpressionItem{
										Key:   "limit.myLimit__d681f6c3",
										Value: "1",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "limit with top level predicates and when predicates",
			limit: &kuadrantv1.Limit{
				When: kuadrantv1.NewWhenPredicates("auth.identity.from-limit"),
			},
			topLevelPredicates: kuadrantv1.NewWhenPredicates("auth.identity.from-top-level"),
			limitIdentifier:    "limit.myLimit__d681f6c3",
			scope:              "my-ns/my-route",
			expectedAction: wasm.Action{
				SourcePolicyLocators: []string{"test/policy/locator"},
				ServiceName:          wasm.RateLimitServiceName,
				Scope:                "my-ns/my-route",
				ConditionalData: []wasm.ConditionalData{
					{
						Predicates: []string{
							"auth.identity.from-top-level",
							"auth.identity.from-limit",
						},
						Data: []wasm.DataType{
							{
								Value: &wasm.Expression{
									ExpressionItem: wasm.ExpressionItem{
										Key:   "limit.myLimit__d681f6c3",
										Value: "1",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			computedRule := wasmActionFromLimit(tc.limit, tc.limitIdentifier, tc.scope, "test/policy/locator", tc.topLevelPredicates)
			if diff := cmp.Diff(tc.expectedAction, computedRule); diff != "" {
				t.Errorf("unexpected wasm rule (-want +got):\n%s", diff)
			}
		})
	}
}
