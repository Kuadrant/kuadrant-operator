//go:build unit

package controllers

import (
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

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
		name                string
		tokenLimit          *kuadrantv1alpha1.TokenLimit
		limitIdentifier     string
		scope               ActionScope
		topLevelPredicates  kuadrantv1.WhenPredicates
		mode                kuadrantv1beta1.TokenRateLimitingMode
		defaultTTL          string
		totalTokensPointers []string
		expectedActions     []wasm.ActionSpec
	}{
		{
			name:                "token limit without conditions nor counters",
			tokenLimit:          &kuadrantv1alpha1.TokenLimit{},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			totalTokensPointers: []string{"/usage/total_tokens"},
			mode:                kuadrantv1beta1.TokenRateLimitingModeOptimistic,
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
											Value: `responseBodyJSON(["/usage/total_tokens"], "number")`,
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
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeOptimistic,
			totalTokensPointers: []string{"/usage/total_tokens"},
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
											Value: `responseBodyJSON(["/usage/total_tokens"], "number")`,
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
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeOptimistic,
			totalTokensPointers: []string{"/usage/total_tokens"},
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
											Value: `responseBodyJSON(["/usage/total_tokens"], "number")`,
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
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			topLevelPredicates:  kuadrantv1.WhenPredicates{{Predicate: `request.method == "POST"`}},
			mode:                kuadrantv1beta1.TokenRateLimitingModeOptimistic,
			totalTokensPointers: []string{"/usage/total_tokens"},
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
											Value: `responseBodyJSON(["/usage/total_tokens"], "number")`,
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
			name:                "token limit with multi-candidate dataExtraction",
			tokenLimit:          &kuadrantv1alpha1.TokenLimit{},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeOptimistic,
			totalTokensPointers: []string{"/usage/total_tokens", "/usageMetadata/totalTokenCount"},
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
											Value: `responseBodyJSON(["/usage/total_tokens", "/usageMetadata/totalTokenCount"], "number")`,
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
			name:                "reservation mode with defaults (no reservation spec, no route ttl)",
			tokenLimit:          &kuadrantv1alpha1.TokenLimit{},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeReservation,
			totalTokensPointers: []string{"/usage/total_tokens"},
			expectedActions: []wasm.ActionSpec{
				// Reserve (request phase): default amount (0, no capacity held), ttl omitted
				{
					ServiceName: wasm.RateLimitReserveServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:     "tokenlimit.myTokenLimit__d681f6c3",
						Amount: "0",
					},
				},
				// Commit (response phase): actual usage from response body
				{
					ServiceName: wasm.RateLimitCommitServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:           "tokenlimit.myTokenLimit__d681f6c3",
						ActualAmount: `responseBodyJSON(["/usage/total_tokens"], "number")`,
					},
				},
			},
		},
		{
			name:                "reservation mode with route backendRequest ttl fallback",
			tokenLimit:          &kuadrantv1alpha1.TokenLimit{},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeReservation,
			totalTokensPointers: []string{"/usage/total_tokens"},
			defaultTTL:          "45s",
			expectedActions: []wasm.ActionSpec{
				{
					ServiceName: wasm.RateLimitReserveServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:     "tokenlimit.myTokenLimit__d681f6c3",
						Amount: "0",
						TTL:    `duration("45s")`,
					},
				},
				{
					ServiceName: wasm.RateLimitCommitServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:           "tokenlimit.myTokenLimit__d681f6c3",
						ActualAmount: `responseBodyJSON(["/usage/total_tokens"], "number")`,
					},
				},
			},
		},
		{
			name: "reservation mode with explicit integer amount and ttl overriding route fallback",
			tokenLimit: &kuadrantv1alpha1.TokenLimit{
				Reservation: &kuadrantv1alpha1.Reservation{
					Amount: ptr.To(intstr.FromInt32(8000)),
					TTL:    ptr.To(kuadrantv1.Expression(`duration("30s")`)),
				},
			},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeReservation,
			totalTokensPointers: []string{"/usage/total_tokens"},
			defaultTTL:          "45s", // overridden by explicit policy ttl
			expectedActions: []wasm.ActionSpec{
				{
					ServiceName: wasm.RateLimitReserveServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:     "tokenlimit.myTokenLimit__d681f6c3",
						Amount: "8000",
						TTL:    `duration("30s")`,
					},
				},
				{
					ServiceName: wasm.RateLimitCommitServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:           "tokenlimit.myTokenLimit__d681f6c3",
						ActualAmount: `responseBodyJSON(["/usage/total_tokens"], "number")`,
					},
				},
			},
		},
		{
			name: "reservation mode with CEL expression amount",
			tokenLimit: &kuadrantv1alpha1.TokenLimit{
				Reservation: &kuadrantv1alpha1.Reservation{
					Amount: ptr.To(intstr.FromString("1 + 1")),
				},
			},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeReservation,
			totalTokensPointers: []string{"/usage/total_tokens"},
			expectedActions: []wasm.ActionSpec{
				{
					ServiceName: wasm.RateLimitReserveServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:     "tokenlimit.myTokenLimit__d681f6c3",
						Amount: "1 + 1",
					},
				},
				{
					ServiceName: wasm.RateLimitCommitServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:           "tokenlimit.myTokenLimit__d681f6c3",
						ActualAmount: `responseBodyJSON(["/usage/total_tokens"], "number")`,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			computedActions := wasmActionSpecsFromTokenLimit(tc.tokenLimit, tc.limitIdentifier, tc.scope, "test/policy/locator", tc.topLevelPredicates, tc.mode, tc.defaultTTL, tc.totalTokensPointers)
			if diff := cmp.Diff(tc.expectedActions, computedActions); diff != "" {
				t.Errorf("unexpected wasm actions (-want +got):\n%s", diff)
			}
		})
	}
}
