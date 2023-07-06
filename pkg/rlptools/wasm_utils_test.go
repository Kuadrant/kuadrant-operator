//go:build unit

package rlptools

import (
	"encoding/json"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

// TODO(eastizle): missing WASMPluginMutator tests
// TODO(eastizle): missing TestWasmRules use cases tests. Only happy path
func TestWasmRules(t *testing.T) {
	httpRoute := &gatewayapiv1beta1.HTTPRoute{
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			Hostnames: []gatewayapiv1beta1.Hostname{
				"*.example.com",
				"*.apps.example.internal",
			},
			Rules: []gatewayapiv1beta1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1beta1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1beta1.HTTPPathMatch{
								Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchPathPrefix}[0],
								Value: &[]string{"/toy"}[0],
							},
							Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethod("GET")}[0],
						},
					},
				},
			},
		},
	}

	catchAllHTTPRoute := &gatewayapiv1beta1.HTTPRoute{
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			Hostnames: []gatewayapiv1beta1.Hostname{"*"},
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
		name            string
		rlp             *kuadrantv1beta2.RateLimitPolicy
		route           *gatewayapiv1beta1.HTTPRoute
		targetHostnames []gatewayapiv1beta1.Hostname
		expectedRules   []wasm.Rule
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
								Key:   "my-app/minimal/50rps",
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
							Hostnames: []gatewayapiv1beta1.Hostname{
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
								Key:   "my-app/my-rlp/50rps-for-selected-hostnames",
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
							Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
								{
									Path: &gatewayapiv1beta1.HTTPPathMatch{
										Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchPathPrefix}[0],
										Value: &[]string{"/toy"}[0],
									},
									Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethod("GET")}[0],
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
								Key:   "my-app/my-rlp/50rps-for-selected-route",
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
							Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
								{
									Path: &gatewayapiv1beta1.HTTPPathMatch{
										Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchPathPrefix}[0],
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
								Key:   "my-app/my-rlp/50rps-for-selected-path",
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
							Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
								{
									Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethod("POST")}[0],
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
								Key:   "my-app/my-rlp/50rps",
								Value: "1",
							},
						},
					},
				},
			},
		},
		{
			name: "RLP for when one of the hostnames in the httproute is from a different gateway",
			rlp: rlp("my-rlp", map[string]kuadrantv1beta2.Limit{
				"50rps": {
					Rates: []kuadrantv1beta2.Rate{counter50rps},
				},
			}),
			route:           httpRoute,
			targetHostnames: []gatewayapiv1beta1.Hostname{"*.example.com"}, // intentionally excluding "*.apps.example.internal"
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
								Key:   "my-app/my-rlp/50rps",
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
								Key:   "my-app/my-rlp/50rps-per-username",
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
			computedRules := WasmRules(tc.rlp, tc.route, tc.targetHostnames)

			if len(tc.expectedRules) != len(computedRules) {
				t.Errorf("expected %d wasm rules, got (%d)", len(tc.expectedRules), len(computedRules))
			}

			for _, expectedRule := range tc.expectedRules {
				if _, ruleFound := common.Find(computedRules, func(computedRule wasm.Rule) bool {
					if len(expectedRule.Conditions) != len(computedRule.Conditions) {
						return false
					}

					// we cannot guarantee the order of the conditions, so we need to find each one
					for _, expectedCondition := range expectedRule.Conditions {
						if _, conditionFound := common.Find(computedRule.Conditions, func(computedCondition wasm.Condition) bool {
							return reflect.DeepEqual(expectedCondition, computedCondition)
						}); !conditionFound {
							return false
						}
					}

					if len(expectedRule.Data) != len(computedRule.Data) {
						return false
					}

					// unlike the conditions, we can guarantee the order of the data items
					for i := range expectedRule.Data {
						if !reflect.DeepEqual(expectedRule.Data[i], computedRule.Data[i]) {
							return false
						}
					}

					return true
				}); !ruleFound {
					expectedRuleJSON, _ := json.Marshal(expectedRule)
					computedRulesJSON, _ := json.Marshal(computedRules)
					t.Errorf("cannot find expected wasm rule: %s in %s", string(expectedRuleJSON), string(computedRulesJSON))
				}
			}
		})
	}
}
