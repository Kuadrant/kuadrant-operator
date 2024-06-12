//go:build unit

package wasm

import (
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
)

// TODO(eastizle): missing WASMPluginMutator tests
// TODO(eastizle): missing TestWasmRules use cases tests. Only happy path
func TestRules(t *testing.T) {
	httpRoute := &gatewayapiv1.HTTPRoute{
		Spec: gatewayapiv1.HTTPRouteSpec{
			Hostnames: []gatewayapiv1.Hostname{
				"*.example.com",
				"*.apps.example.internal",
			},
			Rules: []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchPathPrefix}[0],
								Value: &[]string{"/toy"}[0],
							},
							Method: &[]gatewayapiv1.HTTPMethod{"GET"}[0],
						},
					},
				},
			},
		},
	}

	catchAllHTTPRoute := &gatewayapiv1.HTTPRoute{
		Spec: gatewayapiv1.HTTPRouteSpec{
			Hostnames: []gatewayapiv1.Hostname{"*"},
		},
	}

	rlp := func(name string, limits map[string]kuadrantv1beta2.Limit) *kuadrantv1beta2.RateLimitPolicy {
		return &kuadrantv1beta2.RateLimitPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "my-app",
			},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				RateLimitPolicyCommonSpec: kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: limits,
				},
			},
		}
	}

	// a simple 50rps counter, for convinience, to be used in tests
	counter50rps := kuadrantv1beta2.Rate{
		Limit:    50,
		Duration: 1,
		Unit:     kuadrantv1beta2.TimeUnit("second"),
	}

	testCases := []struct {
		name          string
		rlp           *kuadrantv1beta2.RateLimitPolicy
		route         *gatewayapiv1.HTTPRoute
		expectedRules []Rule
	}{
		{
			name: "minimal RLP",
			rlp: rlp("minimal", map[string]kuadrantv1beta2.Limit{
				"50rps": {
					Rates: []kuadrantv1beta2.Rate{counter50rps},
				},
			}),
			route: httpRoute,
			expectedRules: []Rule{
				{
					Conditions: []Condition{
						{
							AllOf: []PatternExpression{
								{
									Selector: "request.url_path",
									Operator: PatternOperator(kuadrantv1beta2.StartsWithOperator),
									Value:    "/toy",
								},
								{
									Selector: "request.method",
									Operator: PatternOperator(kuadrantv1beta2.EqualOperator),
									Value:    "GET",
								},
							},
						},
					},
					Data: []DataItem{
						{
							Static: &StaticSpec{
								Key:   "limit.50rps__36e9aa4c",
								Value: "1",
							},
						},
					},
				},
			},
		},
		{
			name: "RLP with route selector based on hostname",
			rlp: rlp("my-rlp", map[string]kuadrantv1beta2.Limit{
				"50rps-for-selected-hostnames": {
					Rates: []kuadrantv1beta2.Rate{counter50rps},
					RouteSelectors: []kuadrantv1beta2.RouteSelector{
						{
							Hostnames: []gatewayapiv1.Hostname{
								"*.example.com",
								"myapp.apps.example.com", // ignored
							},
						},
					},
				},
			}),
			route: httpRoute,
			expectedRules: []Rule{
				{
					Conditions: []Condition{
						{
							AllOf: []PatternExpression{
								{
									Selector: "request.url_path",
									Operator: PatternOperator(kuadrantv1beta2.StartsWithOperator),
									Value:    "/toy",
								},
								{
									Selector: "request.method",
									Operator: PatternOperator(kuadrantv1beta2.EqualOperator),
									Value:    "GET",
								},
								{
									Selector: "request.host",
									Operator: PatternOperator(kuadrantv1beta2.EndsWithOperator),
									Value:    ".example.com",
								},
							},
						},
					},
					Data: []DataItem{
						{
							Static: &StaticSpec{
								Key:   "limit.50rps_for_selected_hostnames__ac4044ab",
								Value: "1",
							},
						},
					},
				},
			},
		},
		{
			name: "RLP with route selector based on http route matches (full match)",
			rlp: rlp("my-rlp", map[string]kuadrantv1beta2.Limit{
				"50rps-for-selected-route": {
					Rates: []kuadrantv1beta2.Rate{counter50rps},
					RouteSelectors: []kuadrantv1beta2.RouteSelector{
						{
							Matches: []gatewayapiv1.HTTPRouteMatch{
								{
									Path: &gatewayapiv1.HTTPPathMatch{
										Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchPathPrefix}[0],
										Value: &[]string{"/toy"}[0],
									},
									Method: &[]gatewayapiv1.HTTPMethod{"GET"}[0],
								},
							},
						},
					},
				},
			}),
			route: httpRoute,
			expectedRules: []Rule{
				{
					Conditions: []Condition{
						{
							AllOf: []PatternExpression{
								{
									Selector: "request.url_path",
									Operator: PatternOperator(kuadrantv1beta2.StartsWithOperator),
									Value:    "/toy",
								},
								{
									Selector: "request.method",
									Operator: PatternOperator(kuadrantv1beta2.EqualOperator),
									Value:    "GET",
								},
							},
						},
					},
					Data: []DataItem{
						{
							Static: &StaticSpec{
								Key:   "limit.50rps_for_selected_route__db289136",
								Value: "1",
							},
						},
					},
				},
			},
		},
		{
			name: "RLP with route selector based on http route matches (partial match)",
			rlp: rlp("my-rlp", map[string]kuadrantv1beta2.Limit{
				"50rps-for-selected-path": {
					Rates: []kuadrantv1beta2.Rate{counter50rps},
					RouteSelectors: []kuadrantv1beta2.RouteSelector{
						{
							Matches: []gatewayapiv1.HTTPRouteMatch{
								{
									Path: &gatewayapiv1.HTTPPathMatch{
										Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchPathPrefix}[0],
										Value: &[]string{"/toy"}[0],
									},
								},
							},
						},
					},
				},
			}),
			route: httpRoute,
			expectedRules: []Rule{
				{
					Conditions: []Condition{
						{
							AllOf: []PatternExpression{
								{
									Selector: "request.url_path",
									Operator: PatternOperator(kuadrantv1beta2.StartsWithOperator),
									Value:    "/toy",
								},
								{
									Selector: "request.method",
									Operator: PatternOperator(kuadrantv1beta2.EqualOperator),
									Value:    "GET",
								},
							},
						},
					},
					Data: []DataItem{
						{
							Static: &StaticSpec{
								Key:   "limit.50rps_for_selected_path__38eb97a4",
								Value: "1",
							},
						},
					},
				},
			},
		},
		{
			name: "RLP with mismatching route selectors",
			rlp: rlp("my-rlp", map[string]kuadrantv1beta2.Limit{
				"50rps-for-non-existent-route": {
					Rates: []kuadrantv1beta2.Rate{counter50rps},
					RouteSelectors: []kuadrantv1beta2.RouteSelector{
						{
							Matches: []gatewayapiv1.HTTPRouteMatch{
								{
									Method: &[]gatewayapiv1.HTTPMethod{"POST"}[0],
								},
							},
						},
					},
				},
			}),
			route:         httpRoute,
			expectedRules: []Rule{},
		},
		{
			name: "HTTPRouteRules without rule matches",
			rlp: rlp("my-rlp", map[string]kuadrantv1beta2.Limit{
				"50rps": {
					Rates: []kuadrantv1beta2.Rate{counter50rps},
				},
			}),
			route: catchAllHTTPRoute,
			expectedRules: []Rule{
				{
					Conditions: nil,
					Data: []DataItem{
						{
							Static: &StaticSpec{
								Key:   "limit.50rps__783b9343",
								Value: "1",
							},
						},
					},
				},
			},
		},
		{
			name: "RLP with counter qualifier",
			rlp: rlp("my-rlp", map[string]kuadrantv1beta2.Limit{
				"50rps-per-username": {
					Rates:    []kuadrantv1beta2.Rate{counter50rps},
					Counters: []kuadrantv1beta2.ContextSelector{"auth.identity.username"},
				},
			}),
			route: catchAllHTTPRoute,
			expectedRules: []Rule{
				{
					Conditions: nil,
					Data: []DataItem{
						{
							Static: &StaticSpec{
								Key:   "limit.50rps_per_username__d681f6c3",
								Value: "1",
							},
						},
						{
							Selector: &SelectorSpec{
								Selector: "auth.identity.username",
							},
						},
					},
				},
			},
		},
		{
			name: "Route with header match",
			rlp: rlp("my-rlp", map[string]kuadrantv1beta2.Limit{
				"50rps": {
					Rates: []kuadrantv1beta2.Rate{counter50rps},
				},
			}),
			route: &gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					Hostnames: []gatewayapiv1.Hostname{"*.example.com"},
					Rules: []gatewayapiv1.HTTPRouteRule{
						{
							Matches: []gatewayapiv1.HTTPRouteMatch{
								{
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
							},
						},
					},
				},
			},
			expectedRules: []Rule{
				{
					Conditions: []Condition{
						{
							AllOf: []PatternExpression{
								{
									Selector: "request.url_path",
									Operator: PatternOperator(kuadrantv1beta2.StartsWithOperator),
									Value:    "/v1",
								},
								{
									Selector: "request.method",
									Operator: PatternOperator(kuadrantv1beta2.EqualOperator),
									Value:    "GET",
								},
								{
									Selector: "request.headers.X-kuadrant-a",
									Operator: PatternOperator(kuadrantv1beta2.EqualOperator),
									Value:    "1",
								},
								{
									Selector: "request.headers.X-kuadrant-b",
									Operator: PatternOperator(kuadrantv1beta2.EqualOperator),
									Value:    "1",
								},
							},
						},
					},
					Data: []DataItem{
						{
							Static: &StaticSpec{Key: "limit.50rps__783b9343", Value: "1"},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			computedRules := Rules(tc.rlp, tc.route)
			if diff := cmp.Diff(tc.expectedRules, computedRules); diff != "" {
				t.Errorf("unexpected wasm rules (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLimitNameToLimitadorIdentifier(t *testing.T) {
	testCases := []struct {
		name            string
		rlpKey          types.NamespacedName
		uniqueLimitName string
		expected        *regexp.Regexp
	}{
		{
			name:            "prepends the limitador limit identifier prefix",
			rlpKey:          types.NamespacedName{Namespace: "testNS", Name: "rlpA"},
			uniqueLimitName: "foo",
			expected:        regexp.MustCompile(`^limit\.foo.+`),
		},
		{
			name:            "sanitizes invalid chars",
			rlpKey:          types.NamespacedName{Namespace: "testNS", Name: "rlpA"},
			uniqueLimitName: "my/limit-0",
			expected:        regexp.MustCompile(`^limit\.my_limit_0.+$`),
		},
		{
			name:            "sanitizes the dot char (.) even though it is a valid char in limitador identifiers",
			rlpKey:          types.NamespacedName{Namespace: "testNS", Name: "rlpA"},
			uniqueLimitName: "my.limit",
			expected:        regexp.MustCompile(`^limit\.my_limit.+$`),
		},
		{
			name:            "appends a hash of the original name to avoid breaking uniqueness",
			rlpKey:          types.NamespacedName{Namespace: "testNS", Name: "rlpA"},
			uniqueLimitName: "foo",
			expected:        regexp.MustCompile(`^.+__1da6e70a$`),
		},
		{
			name:            "different rlp keys result in different identifiers",
			rlpKey:          types.NamespacedName{Namespace: "testNS", Name: "rlpB"},
			uniqueLimitName: "foo",
			expected:        regexp.MustCompile(`^.+__2c1520b6$`),
		},
		{
			name:            "empty string",
			rlpKey:          types.NamespacedName{Namespace: "testNS", Name: "rlpA"},
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
