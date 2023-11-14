//go:build unit

package rlptools

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

// TODO(eastizle): missing WASMPluginMutator tests
// TODO(eastizle): missing TestWasmRules use cases tests. Only happy path
func TestWasmRules(t *testing.T) {
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
				Limits: limits,
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
		expectedRules []wasm.Rule
	}{
		{
			name: "minimal RLP",
			rlp: rlp("minimal", map[string]kuadrantv1beta2.Limit{
				"50rps": {
					Rates: []kuadrantv1beta2.Rate{counter50rps},
				},
			}),
			route: httpRoute,
			expectedRules: []wasm.Rule{
				{
					Conditions: []wasm.Condition{
						{
							AllOf: []wasm.PatternExpression{
								{
									Selector: "request.url_path",
									Operator: wasm.PatternOperator(kuadrantv1beta2.StartsWithOperator),
									Value:    "/toy",
								},
								{
									Selector: "request.method",
									Operator: wasm.PatternOperator(kuadrantv1beta2.EqualOperator),
									Value:    "GET",
								},
							},
						},
					},
					Data: []wasm.DataItem{
						{
							Static: &wasm.StaticSpec{
								Key:   "limit.50rps__770adfd9",
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
			expectedRules: []wasm.Rule{
				{
					Conditions: []wasm.Condition{
						{
							AllOf: []wasm.PatternExpression{
								{
									Selector: "request.url_path",
									Operator: wasm.PatternOperator(kuadrantv1beta2.StartsWithOperator),
									Value:    "/toy",
								},
								{
									Selector: "request.method",
									Operator: wasm.PatternOperator(kuadrantv1beta2.EqualOperator),
									Value:    "GET",
								},
								{
									Selector: "request.host",
									Operator: wasm.PatternOperator(kuadrantv1beta2.EndsWithOperator),
									Value:    ".example.com",
								},
							},
						},
					},
					Data: []wasm.DataItem{
						{
							Static: &wasm.StaticSpec{
								Key:   "limit.50rps_for_selected_hostnames__5af2c820",
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
			expectedRules: []wasm.Rule{
				{
					Conditions: []wasm.Condition{
						{
							AllOf: []wasm.PatternExpression{
								{
									Selector: "request.url_path",
									Operator: wasm.PatternOperator(kuadrantv1beta2.StartsWithOperator),
									Value:    "/toy",
								},
								{
									Selector: "request.method",
									Operator: wasm.PatternOperator(kuadrantv1beta2.EqualOperator),
									Value:    "GET",
								},
							},
						},
					},
					Data: []wasm.DataItem{
						{
							Static: &wasm.StaticSpec{
								Key:   "limit.50rps_for_selected_route__b6640119",
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
			expectedRules: []wasm.Rule{
				{
					Conditions: []wasm.Condition{
						{
							AllOf: []wasm.PatternExpression{
								{
									Selector: "request.url_path",
									Operator: wasm.PatternOperator(kuadrantv1beta2.StartsWithOperator),
									Value:    "/toy",
								},
								{
									Selector: "request.method",
									Operator: wasm.PatternOperator(kuadrantv1beta2.EqualOperator),
									Value:    "GET",
								},
							},
						},
					},
					Data: []wasm.DataItem{
						{
							Static: &wasm.StaticSpec{
								Key:   "limit.50rps_for_selected_path__4088dcf9",
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
			expectedRules: []wasm.Rule{},
		},
		{
			name: "HTTPRouteRules without rule matches",
			rlp: rlp("my-rlp", map[string]kuadrantv1beta2.Limit{
				"50rps": {
					Rates: []kuadrantv1beta2.Rate{counter50rps},
				},
			}),
			route: catchAllHTTPRoute,
			expectedRules: []wasm.Rule{
				{
					Conditions: nil,
					Data: []wasm.DataItem{
						{
							Static: &wasm.StaticSpec{
								Key:   "limit.50rps__770adfd9",
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
			expectedRules: []wasm.Rule{
				{
					Conditions: nil,
					Data: []wasm.DataItem{
						{
							Static: &wasm.StaticSpec{
								Key:   "limit.50rps_per_username__f5bebfb8",
								Value: "1",
							},
						},
						{
							Selector: &wasm.SelectorSpec{
								Selector: "auth.identity.username",
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			computedRules := WasmRules(tc.rlp, tc.route)
			if diff := cmp.Diff(tc.expectedRules, computedRules); diff != "" {
				t.Errorf("unexpected wasm rules (-want +got):\n%s", diff)
			}
		})
	}
}
