//go:build unit

package controllers

import (
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	k8stypes "k8s.io/apimachinery/pkg/types"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
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

func TestWasmActionsFromTokenLimit(t *testing.T) {
	testCases := []struct {
		name                   string
		tokenLimit             *kuadrantv1alpha1.TokenLimit
		limitIdentifier        string
		scope                  string
		topLevelPredicates     kuadrantv1.WhenPredicates
		expectedRequestAction  wasm.Action
		expectedResponseAction wasm.Action
	}{
		{
			name:            "token limit without conditions nor counters",
			tokenLimit:      &kuadrantv1alpha1.TokenLimit{},
			limitIdentifier: "tokenlimit.myTokenLimit__d681f6c3",
			scope:           "my-ns/my-route",
			expectedRequestAction: wasm.Action{
				ServiceName: wasm.RateLimitCheckServiceName,
				Scope:       "my-ns/my-route",
				Predicates:  []string{},
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
			expectedResponseAction: wasm.Action{
				ServiceName: wasm.RateLimitServiceName,
				Scope:       "my-ns/my-route",
				Predicates:  []string{},
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
								Value: `responseBodyJSON("usage.total_tokens")`,
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
			scope:           "my-ns/my-route",
			expectedRequestAction: wasm.Action{
				ServiceName: wasm.RateLimitCheckServiceName,
				Scope:       "my-ns/my-route",
				Predicates:  []string{},
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
			expectedResponseAction: wasm.Action{
				ServiceName: wasm.RateLimitServiceName,
				Scope:       "my-ns/my-route",
				Predicates:  []string{},
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
								Value: `responseBodyJSON("usage.total_tokens")`,
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
			scope:           "my-ns/my-route",
			expectedRequestAction: wasm.Action{
				ServiceName: wasm.RateLimitCheckServiceName,
				Scope:       "my-ns/my-route",
				Predicates:  []string{`request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")`},
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
			expectedResponseAction: wasm.Action{
				ServiceName: wasm.RateLimitServiceName,
				Scope:       "my-ns/my-route",
				Predicates:  []string{`request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")`},
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
								Value: `responseBodyJSON("usage.total_tokens")`,
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
			scope:              "my-ns/my-route",
			topLevelPredicates: kuadrantv1.WhenPredicates{{Predicate: `request.method == "POST"`}},
			expectedRequestAction: wasm.Action{
				ServiceName: wasm.RateLimitCheckServiceName,
				Scope:       "my-ns/my-route",
				Predicates:  []string{`request.method == "POST"`, `request.auth.claims["tier"] == "free"`},
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
			expectedResponseAction: wasm.Action{
				ServiceName: wasm.RateLimitServiceName,
				Scope:       "my-ns/my-route",
				Predicates:  []string{`request.method == "POST"`, `request.auth.claims["tier"] == "free"`},
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
								Value: `responseBodyJSON("usage.total_tokens")`,
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requestAction, responseAction := wasmActionsFromTokenLimit(tc.tokenLimit, tc.limitIdentifier, tc.scope, tc.topLevelPredicates)
			if diff := cmp.Diff(tc.expectedRequestAction, requestAction); diff != "" {
				t.Errorf("unexpected wasm request action (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.expectedResponseAction, responseAction); diff != "" {
				t.Errorf("unexpected wasm response action (-want +got):\n%s", diff)
			}
		})
	}
}
