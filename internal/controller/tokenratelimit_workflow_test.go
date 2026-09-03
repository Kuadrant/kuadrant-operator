//go:build unit

package controllers

import (
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

func TestTokenLimitNameToLimitadorIdentifier(t *testing.T) {
	testCases := []struct {
		name            string
		trlpKey         k8stypes.NamespacedName
		uniqueLimitName string
		expected        *regexp.Regexp
	}{
		{
			name:            "prepends the token limitador limit identifier prefix",
			trlpKey:         k8stypes.NamespacedName{Namespace: "testNS", Name: "trlpA"},
			uniqueLimitName: "foo",
			expected:        regexp.MustCompile(`^tokenlimit\.foo.+`),
		},
		{
			name:            "creates deterministic identifier",
			trlpKey:         k8stypes.NamespacedName{Namespace: "testNS", Name: "trlpA"},
			uniqueLimitName: "foo",
			expected:        regexp.MustCompile(`^tokenlimit\.foo__13adad8e`),
		},
		{
			name:            "identifier includes unique limit name",
			trlpKey:         k8stypes.NamespacedName{Namespace: "testNS", Name: "trlpA"},
			uniqueLimitName: "myUniqueLimit",
			expected:        regexp.MustCompile(`tokenlimit\.myUniqueLimit.+`),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			identifier := TokenLimitNameToLimitadorIdentifier(tc.trlpKey, tc.uniqueLimitName)
			if !tc.expected.MatchString(identifier) {
				subT.Errorf("identifier does not match, expected(%s), got (%s)", tc.expected, identifier)
			}
		})
	}
}

func TestWasmActionSpecsFromTokenLimit(t *testing.T) {
	testCases := []struct {
		name               string
		tokenLimit         *kuadrantv1alpha1.TokenLimit
		limitIdentifier    string
		scope              ActionScope
		topLevelPredicates kuadrantv1.WhenPredicates
		mode               kuadrantv1beta1.TokenRateLimitingMode
		backendTimeout     *gatewayapiv1.Duration
		expectedActions    []wasm.ActionSpec
	}{
		{
			name:            "token limit without conditions nor counters",
			tokenLimit:      &kuadrantv1alpha1.TokenLimit{},
			limitIdentifier: "tokenlimit.myTokenLimit__d681f6c3",
			scope:           ActionScope("my-ns/my-route"),
			expectedActions: []wasm.ActionSpec{
				// Request phase action
				{
					ServiceName: wasm.RateLimitCheckServiceName,
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: "0",
										},
									},
								},
							},
						},
					},
					Sources: []string{"test/policy/locator"},
				},
				// Response phase action
				{
					ServiceName: wasm.RateLimitReportServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: `responseBodyJSON("/usage/total_tokens")`,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "token limit with counter expression",
			tokenLimit: &kuadrantv1alpha1.TokenLimit{
				Counters: []kuadrantv1.Counter{
					{Expression: kuadrantv1.Expression("auth.identity.userid")},
				},
			},
			limitIdentifier: "tokenlimit.myTokenLimit__d681f6c3",
			scope:           ActionScope("my-ns/my-route"),
			expectedActions: []wasm.ActionSpec{
				// Request phase action
				{
					ServiceName: wasm.RateLimitCheckServiceName,
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "auth.identity.userid",
											Value: "auth.identity.userid",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: "0",
										},
									},
								},
							},
						},
					},
					Sources: []string{"test/policy/locator"},
				},
				// Response phase action
				{
					ServiceName: wasm.RateLimitReportServiceName,
					Sources:     []string{"test/policy/locator"},
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "auth.identity.userid",
											Value: "auth.identity.userid",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: `responseBodyJSON("/usage/total_tokens")`,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "token limit with counter and when predicates",
			tokenLimit: &kuadrantv1alpha1.TokenLimit{
				Counters: []kuadrantv1.Counter{
					{Expression: kuadrantv1.Expression("auth.identity.userid")},
				},
				When: kuadrantv1.WhenPredicates{
					{Predicate: `request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")`},
				},
			},
			limitIdentifier: "tokenlimit.myTokenLimit__d681f6c3",
			scope:           ActionScope("my-ns/my-route"),
			expectedActions: []wasm.ActionSpec{
				// Request phase action
				{
					ServiceName: wasm.RateLimitCheckServiceName,
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{`request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")`},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "auth.identity.userid",
											Value: "auth.identity.userid",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: "0",
										},
									},
								},
							},
						},
					},
					Sources: []string{"test/policy/locator"},
				},
				// Response phase action
				{
					ServiceName: wasm.RateLimitReportServiceName,
					Sources:     []string{"test/policy/locator"},
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{`request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")`},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "auth.identity.userid",
											Value: "auth.identity.userid",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: `responseBodyJSON("/usage/total_tokens")`,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "token limit with top-level and limit-level predicates",
			tokenLimit: &kuadrantv1alpha1.TokenLimit{
				When: kuadrantv1.WhenPredicates{
					{Predicate: `request.auth.claims["tier"] == "free"`},
				},
			},
			limitIdentifier:    "tokenlimit.myTokenLimit__d681f6c3",
			scope:              ActionScope("my-ns/my-route"),
			topLevelPredicates: kuadrantv1.WhenPredicates{{Predicate: `request.method == "POST"`}},
			expectedActions: []wasm.ActionSpec{
				// Request phase action
				{
					ServiceName: wasm.RateLimitCheckServiceName,
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{`request.method == "POST"`, `request.auth.claims["tier"] == "free"`},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: "0",
										},
									},
								},
							},
						},
					},
					Sources: []string{"test/policy/locator"},
				},
				// Response phase action
				{
					ServiceName: wasm.RateLimitReportServiceName,
					Sources:     []string{"test/policy/locator"},
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{`request.method == "POST"`, `request.auth.claims["tier"] == "free"`},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: `responseBodyJSON("/usage/total_tokens")`,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "token limit in reservation mode with explicit reservation",
			tokenLimit: &kuadrantv1alpha1.TokenLimit{
				Reservation: &kuadrantv1alpha1.Reservation{
					Amount: ptr.To("uint(2000)"),
					TTL:    ptr.To("duration('30s')"),
				},
			},
			limitIdentifier: "tokenlimit.myTokenLimit__d681f6c3",
			scope:           ActionScope("my-ns/my-route"),
			mode:            kuadrantv1beta1.TokenRateLimitingModeReservation,
			expectedActions: []wasm.ActionSpec{
				// Request phase action (Reserve)
				{
					ServiceName: wasm.RateLimitReserveServiceName,
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.amount",
											Value: "uint(2000)",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.ttl",
											Value: "duration('30s')",
										},
									},
								},
							},
						},
					},
					Sources: []string{"test/policy/locator"},
				},
				// Response phase action (Commit)
				{
					ServiceName: wasm.RateLimitCommitServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: `responseBodyJSON("/usage/total_tokens")`,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:            "token limit in reservation mode with omitted reservation and route timeouts",
			tokenLimit:      &kuadrantv1alpha1.TokenLimit{},
			limitIdentifier: "tokenlimit.myTokenLimit__d681f6c3",
			scope:           ActionScope("my-ns/my-route"),
			mode:            kuadrantv1beta1.TokenRateLimitingModeReservation,
			backendTimeout:  ptr.To(gatewayapiv1.Duration("45s")),
			expectedActions: []wasm.ActionSpec{
				// Request phase action (Reserve with default amount and route timeout TTL)
				{
					ServiceName: wasm.RateLimitReserveServiceName,
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.amount",
											Value: "uint(5000)",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.ttl",
											Value: "duration('45s')",
										},
									},
								},
							},
						},
					},
					Sources: []string{"test/policy/locator"},
				},
				// Response phase action (Commit)
				{
					ServiceName: wasm.RateLimitCommitServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: `responseBodyJSON("/usage/total_tokens")`,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:            "token limit in reservation mode with omitted reservation and no route timeouts (fallback 60s)",
			tokenLimit:      &kuadrantv1alpha1.TokenLimit{},
			limitIdentifier: "tokenlimit.myTokenLimit__d681f6c3",
			scope:           ActionScope("my-ns/my-route"),
			mode:            kuadrantv1beta1.TokenRateLimitingModeReservation,
			expectedActions: []wasm.ActionSpec{
				// Request phase action (Reserve with default amount and fallback 60s TTL)
				{
					ServiceName: wasm.RateLimitReserveServiceName,
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.amount",
											Value: "uint(5000)",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.ttl",
											Value: "duration('60s')",
										},
									},
								},
							},
						},
					},
					Sources: []string{"test/policy/locator"},
				},
				// Response phase action (Commit)
				{
					ServiceName: wasm.RateLimitCommitServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: `responseBodyJSON("/usage/total_tokens")`,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "token limit in reservation mode with amount 0 (per-limit bypass)",
			tokenLimit: &kuadrantv1alpha1.TokenLimit{
				Reservation: &kuadrantv1alpha1.Reservation{
					Amount: ptr.To("0"),
				},
			},
			limitIdentifier: "tokenlimit.myTokenLimit__d681f6c3",
			scope:           ActionScope("my-ns/my-route"),
			mode:            kuadrantv1beta1.TokenRateLimitingModeReservation,
			expectedActions: []wasm.ActionSpec{
				// Request phase action (Reserve with 0 amount)
				{
					ServiceName: wasm.RateLimitReserveServiceName,
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.amount",
											Value: "0",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.ttl",
											Value: "duration('60s')",
										},
									},
								},
							},
						},
					},
					Sources: []string{"test/policy/locator"},
				},
				// Response phase action (Commit)
				{
					ServiceName: wasm.RateLimitCommitServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: `responseBodyJSON("/usage/total_tokens")`,
										},
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
			mode := tc.mode
			if mode == "" {
				mode = kuadrantv1beta1.TokenRateLimitingModeCheckReport
			}
			computedActions := wasmActionSpecsFromTokenLimit(tc.tokenLimit, tc.limitIdentifier, tc.scope, "test/policy/locator", tc.topLevelPredicates, mode, tc.backendTimeout)
			if diff := cmp.Diff(tc.expectedActions, computedActions); diff != "" {
				t.Errorf("unexpected wasm actions (-want +got):\n%s", diff)
			}
		})
	}
}

func TestKuadrantGetTokenRateLimitingMode(t *testing.T) {
	testCases := []struct {
		name     string
		kuadrant *kuadrantv1beta1.Kuadrant
		expected kuadrantv1beta1.TokenRateLimitingMode
	}{
		{
			name:     "nil Kuadrant returns Reservation by default",
			kuadrant: nil,
			expected: kuadrantv1beta1.TokenRateLimitingModeReservation,
		},
		{
			name:     "nil TokenRateLimiting returns Reservation by default",
			kuadrant: &kuadrantv1beta1.Kuadrant{},
			expected: kuadrantv1beta1.TokenRateLimitingModeReservation,
		},
		{
			name: "nil Mode returns Reservation by default",
			kuadrant: &kuadrantv1beta1.Kuadrant{
				Spec: kuadrantv1beta1.KuadrantSpec{
					TokenRateLimiting: &kuadrantv1beta1.TokenRateLimiting{},
				},
			},
			expected: kuadrantv1beta1.TokenRateLimitingModeReservation,
		},
		{
			name: "explicit Reservation mode returns Reservation",
			kuadrant: &kuadrantv1beta1.Kuadrant{
				Spec: kuadrantv1beta1.KuadrantSpec{
					TokenRateLimiting: &kuadrantv1beta1.TokenRateLimiting{
						Mode: ptr.To(kuadrantv1beta1.TokenRateLimitingModeReservation),
					},
				},
			},
			expected: kuadrantv1beta1.TokenRateLimitingModeReservation,
		},
		{
			name: "explicit CheckReport mode returns CheckReport",
			kuadrant: &kuadrantv1beta1.Kuadrant{
				Spec: kuadrantv1beta1.KuadrantSpec{
					TokenRateLimiting: &kuadrantv1beta1.TokenRateLimiting{
						Mode: ptr.To(kuadrantv1beta1.TokenRateLimitingModeCheckReport),
					},
				},
			},
			expected: kuadrantv1beta1.TokenRateLimitingModeCheckReport,
		},
		{
			name: "empty string Mode returns Reservation by default",
			kuadrant: &kuadrantv1beta1.Kuadrant{
				Spec: kuadrantv1beta1.KuadrantSpec{
					TokenRateLimiting: &kuadrantv1beta1.TokenRateLimiting{
						Mode: ptr.To(kuadrantv1beta1.TokenRateLimitingMode("")),
					},
				},
			},
			expected: kuadrantv1beta1.TokenRateLimitingModeReservation,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.kuadrant.GetTokenRateLimitingMode(); got != tc.expected {
				t.Errorf("GetTokenRateLimitingMode() = %v, want %v", got, tc.expected)
			}
		})
	}
}
