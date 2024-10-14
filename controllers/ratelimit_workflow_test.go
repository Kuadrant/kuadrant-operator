//go:build unit

package controllers

import (
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/wasm"
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

func TestWasmRuleFromLimit(t *testing.T) {
	testCases := []struct {
		name            string
		limit           kuadrantv1beta3.Limit
		limitIdentifier string
		scope           string
		routeMatch      gatewayapiv1.HTTPRouteMatch
		expectedRule    wasm.Rule
	}{
		{
			name:            "limit without conditions nor counters",
			limit:           kuadrantv1beta3.Limit{},
			limitIdentifier: "limit.myLimit__d681f6c3",
			scope:           "my-ns/my-route",
			routeMatch:      gatewayapiv1.HTTPRouteMatch{},
			expectedRule: wasm.Rule{
				Actions: []wasm.Action{
					{
						Scope:         "my-ns/my-route",
						ExtensionName: wasm.RateLimitExtensionName,
						Data: []wasm.DataType{
							{
								Value: &wasm.Static{
									Static: wasm.StaticSpec{
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
			name:            "limit with httproutematch",
			limit:           kuadrantv1beta3.Limit{},
			limitIdentifier: "limit.myLimit__d681f6c3",
			scope:           "my-ns/my-route",
			routeMatch: gatewayapiv1.HTTPRouteMatch{
				Path: &gatewayapiv1.HTTPPathMatch{
					Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
					Value: ptr.To("/v1"),
				},
				Method: ptr.To(gatewayapiv1.HTTPMethodGet),
				Headers: []gatewayapiv1.HTTPHeaderMatch{
					{
						Name:  gatewayapiv1.HTTPHeaderName("X-kuadrant-a"),
						Value: "1",
					},
					{
						Name:  gatewayapiv1.HTTPHeaderName("X-kuadrant-b"),
						Value: "1",
					},
				},
			},
			expectedRule: wasm.Rule{
				Conditions: []wasm.Condition{
					{
						AllOf: []wasm.PatternExpression{
							{
								Selector: "request.method",
								Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
								Value:    "GET",
							},
							{
								Selector: "request.url_path",
								Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
								Value:    "/v1",
							},
							{
								Selector: "request.headers.X-kuadrant-a",
								Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
								Value:    "1",
							},
							{
								Selector: "request.headers.X-kuadrant-b",
								Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
								Value:    "1",
							},
						},
					},
				},
				Actions: []wasm.Action{
					{
						Scope:         "my-ns/my-route",
						ExtensionName: wasm.RateLimitExtensionName,
						Data: []wasm.DataType{
							{
								Value: &wasm.Static{
									Static: wasm.StaticSpec{
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
			name: "limit with httproutematch and when conditions",
			limit: kuadrantv1beta3.Limit{
				When: []kuadrantv1beta3.WhenCondition{
					{
						Selector: kuadrantv1beta3.ContextSelector("auth.identity.group"),
						Operator: kuadrantv1beta3.NotEqualOperator,
						Value:    "admin",
					},
				},
			},
			limitIdentifier: "limit.myLimit__d681f6c3",
			scope:           "my-ns/my-route",
			routeMatch: gatewayapiv1.HTTPRouteMatch{
				Method: ptr.To(gatewayapiv1.HTTPMethodGet),
				Path: &gatewayapiv1.HTTPPathMatch{
					Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
					Value: ptr.To("/toys"),
				},
			},
			expectedRule: wasm.Rule{
				Conditions: []wasm.Condition{
					{
						AllOf: []wasm.PatternExpression{
							{
								Selector: "request.method",
								Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
								Value:    "GET",
							},
							{
								Selector: "request.url_path",
								Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
								Value:    "/toys",
							},
							{
								Selector: "auth.identity.group",
								Operator: wasm.PatternOperator(kuadrantv1beta3.NotEqualOperator),
								Value:    "admin",
							},
						},
					},
				},
				Actions: []wasm.Action{
					{
						Scope:         "my-ns/my-route",
						ExtensionName: wasm.RateLimitExtensionName,
						Data: []wasm.DataType{
							{
								Value: &wasm.Static{
									Static: wasm.StaticSpec{
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
			limit: kuadrantv1beta3.Limit{
				Counters: []kuadrantv1beta3.ContextSelector{"auth.identity.username"},
			},
			limitIdentifier: "limit.myLimit__d681f6c3",
			scope:           "my-ns/my-route",
			routeMatch:      gatewayapiv1.HTTPRouteMatch{},
			expectedRule: wasm.Rule{
				Actions: []wasm.Action{
					{
						Scope:         "my-ns/my-route",
						ExtensionName: wasm.RateLimitExtensionName,
						Data: []wasm.DataType{
							{
								Value: &wasm.Static{
									Static: wasm.StaticSpec{
										Key:   "limit.myLimit__d681f6c3",
										Value: "1",
									},
								},
							},
							{
								Value: &wasm.Selector{
									Selector: wasm.SelectorSpec{
										Selector: "auth.identity.username",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "limit with counter qualifiers and httproutematch",
			limit: kuadrantv1beta3.Limit{
				Counters: []kuadrantv1beta3.ContextSelector{"auth.identity.username"},
			},
			limitIdentifier: "limit.myLimit__d681f6c3",
			scope:           "my-ns/my-route",
			routeMatch: gatewayapiv1.HTTPRouteMatch{
				Method: ptr.To(gatewayapiv1.HTTPMethodGet),
				Path: &gatewayapiv1.HTTPPathMatch{
					Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
					Value: ptr.To("/toys"),
				},
			},
			expectedRule: wasm.Rule{
				Conditions: []wasm.Condition{
					{
						AllOf: []wasm.PatternExpression{
							{
								Selector: "request.method",
								Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
								Value:    "GET",
							},
							{
								Selector: "request.url_path",
								Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
								Value:    "/toys",
							},
						},
					},
				},
				Actions: []wasm.Action{
					{
						Scope:         "my-ns/my-route",
						ExtensionName: wasm.RateLimitExtensionName,
						Data: []wasm.DataType{
							{
								Value: &wasm.Static{
									Static: wasm.StaticSpec{
										Key:   "limit.myLimit__d681f6c3",
										Value: "1",
									},
								},
							},
							{
								Value: &wasm.Selector{
									Selector: wasm.SelectorSpec{
										Selector: "auth.identity.username",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "limit with counter qualifiers and when conditions",
			limit: kuadrantv1beta3.Limit{
				Counters: []kuadrantv1beta3.ContextSelector{"auth.identity.username"},
				When: []kuadrantv1beta3.WhenCondition{
					{
						Selector: kuadrantv1beta3.ContextSelector("auth.identity.group"),
						Operator: kuadrantv1beta3.NotEqualOperator,
						Value:    "admin",
					},
				},
			},
			limitIdentifier: "limit.myLimit__d681f6c3",
			scope:           "my-ns/my-route",
			routeMatch:      gatewayapiv1.HTTPRouteMatch{},
			expectedRule: wasm.Rule{
				Conditions: []wasm.Condition{
					{
						AllOf: []wasm.PatternExpression{
							{
								Selector: "auth.identity.group",
								Operator: wasm.PatternOperator(kuadrantv1beta3.NotEqualOperator),
								Value:    "admin",
							},
						},
					},
				},
				Actions: []wasm.Action{
					{
						Scope:         "my-ns/my-route",
						ExtensionName: wasm.RateLimitExtensionName,
						Data: []wasm.DataType{
							{
								Value: &wasm.Static{
									Static: wasm.StaticSpec{
										Key:   "limit.myLimit__d681f6c3",
										Value: "1",
									},
								},
							},
							{
								Value: &wasm.Selector{
									Selector: wasm.SelectorSpec{
										Selector: "auth.identity.username",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "limit with counter qualifiers, httproutematch and when conditions",
			limit: kuadrantv1beta3.Limit{
				Counters: []kuadrantv1beta3.ContextSelector{"auth.identity.username"},
				When: []kuadrantv1beta3.WhenCondition{
					{
						Selector: kuadrantv1beta3.ContextSelector("auth.identity.group"),
						Operator: kuadrantv1beta3.NotEqualOperator,
						Value:    "admin",
					},
				},
			},
			limitIdentifier: "limit.myLimit__d681f6c3",
			scope:           "my-ns/my-route",
			routeMatch: gatewayapiv1.HTTPRouteMatch{
				Method: ptr.To(gatewayapiv1.HTTPMethodGet),
				Path: &gatewayapiv1.HTTPPathMatch{
					Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
					Value: ptr.To("/toys"),
				},
			},
			expectedRule: wasm.Rule{
				Conditions: []wasm.Condition{
					{
						AllOf: []wasm.PatternExpression{
							{
								Selector: "request.method",
								Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
								Value:    "GET",
							},
							{
								Selector: "request.url_path",
								Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
								Value:    "/toys",
							},
							{
								Selector: "auth.identity.group",
								Operator: wasm.PatternOperator(kuadrantv1beta3.NotEqualOperator),
								Value:    "admin",
							},
						},
					},
				},
				Actions: []wasm.Action{
					{
						Scope:         "my-ns/my-route",
						ExtensionName: wasm.RateLimitExtensionName,
						Data: []wasm.DataType{
							{
								Value: &wasm.Static{
									Static: wasm.StaticSpec{
										Key:   "limit.myLimit__d681f6c3",
										Value: "1",
									},
								},
							},
							{
								Value: &wasm.Selector{
									Selector: wasm.SelectorSpec{
										Selector: "auth.identity.username",
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
			computedRule := wasmRuleFromLimit(tc.limit, tc.limitIdentifier, tc.scope, tc.routeMatch)
			if diff := cmp.Diff(tc.expectedRule, computedRule); diff != "" {
				t.Errorf("unexpected wasm rule (-want +got):\n%s", diff)
			}
		})
	}
}
