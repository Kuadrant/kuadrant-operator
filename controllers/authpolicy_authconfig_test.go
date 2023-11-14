//go:build unit

package controllers

import (
	"reflect"
	"testing"

	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestAuthorinoConditionsFromHTTPRouteRule(t *testing.T) {
	testCases := []struct {
		name      string
		hostnames []gatewayapiv1.Hostname
		rule      gatewayapiv1.HTTPRouteRule
		expected  []authorinoapi.PatternExpressionOrRef
	}{
		{
			name:      "No HTTPRouteMatch",
			hostnames: []gatewayapiv1.Hostname{"toystore.kuadrant.io"},
			rule:      gatewayapiv1.HTTPRouteRule{},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					PatternExpression: authorinoapi.PatternExpression{
						Selector: "request.host",
						Operator: "matches",
						Value:    `toystore\.kuadrant\.io`,
					},
				},
			},
		},
		{
			name:      "Single HTTPRouteMatch",
			hostnames: []gatewayapiv1.Hostname{"toystore.kuadrant.io"},
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  ptr.To(gatewayapiv1.PathMatchType("PathPrefix")),
							Value: ptr.To("/toy"),
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: "request.host",
												Operator: "matches",
												Value:    `toystore\.kuadrant\.io`,
											},
										},
									},
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.url_path`,
												Operator: "matches",
												Value:    `/toy.*`,
											},
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
			name:      "Multiple HTTPRouteMatches",
			hostnames: []gatewayapiv1.Hostname{"toystore.kuadrant.io"},
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  ptr.To(gatewayapiv1.PathMatchType("PathPrefix")),
							Value: ptr.To("/toy"),
						},
					},
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  ptr.To(gatewayapiv1.PathMatchType("Exact")),
							Value: ptr.To("/foo"),
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: "request.host",
												Operator: "matches",
												Value:    `toystore\.kuadrant\.io`,
											},
										},
									},
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.url_path`,
												Operator: "matches",
												Value:    `/toy.*`,
											},
										},
									},
								},
							},
						},
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: "request.host",
												Operator: "matches",
												Value:    `toystore\.kuadrant\.io`,
											},
										},
									},
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.url_path`,
												Operator: "eq",
												Value:    `/foo`,
											},
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
			name:      "Multiple hosts",
			hostnames: []gatewayapiv1.Hostname{"toystore.kuadrant.io", "gamestore.kuadrant.io"},
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  ptr.To(gatewayapiv1.PathMatchType("PathPrefix")),
							Value: ptr.To("/toy"),
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: "request.host",
												Operator: "matches",
												Value:    `toystore\.kuadrant\.io|gamestore\.kuadrant\.io`,
											},
										},
									},
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.url_path`,
												Operator: "matches",
												Value:    `/toy.*`,
											},
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
			name:      "Host wildcard",
			hostnames: []gatewayapiv1.Hostname{"*.kuadrant.io"},
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  ptr.To(gatewayapiv1.PathMatchType("PathPrefix")),
							Value: ptr.To("/toy"),
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: "request.host",
												Operator: "matches",
												Value:    `.*\.kuadrant\.io`,
											},
										},
									},
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.url_path`,
												Operator: "matches",
												Value:    `/toy.*`,
											},
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
			name:      "Catch-all host is ignored",
			hostnames: []gatewayapiv1.Hostname{"toystore.kuadrant.io", "*"},
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  ptr.To(gatewayapiv1.PathMatchType("PathPrefix")),
							Value: ptr.To("/toy"),
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: "request.host",
												Operator: "matches",
												Value:    `toystore\.kuadrant\.io`,
											},
										},
									},
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.url_path`,
												Operator: "matches",
												Value:    `/toy.*`,
											},
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
			name: "Method",
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.method`,
												Operator: "eq",
												Value:    `GET`,
											},
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
			name: "PathMatchExact",
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  ptr.To(gatewayapiv1.PathMatchType("Exact")),
							Value: ptr.To("/toy"),
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.url_path`,
												Operator: "eq",
												Value:    `/toy`,
											},
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
			name: "PathMatchPrefix",
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  ptr.To(gatewayapiv1.PathMatchType("PathPrefix")),
							Value: ptr.To("/toy"),
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.url_path`,
												Operator: "matches",
												Value:    `/toy.*`,
											},
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
			name: "PathMatchRegularExpression",
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  ptr.To(gatewayapiv1.PathMatchType("RegularExpression")),
							Value: ptr.To("^/(dolls|cars)"),
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.url_path`,
												Operator: "matches",
												Value:    "^/(dolls|cars)",
											},
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
			name: "Single header match",
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Headers: []gatewayapiv1.HTTPHeaderMatch{
							{
								Type:  ptr.To(gatewayapiv1.HeaderMatchType("Exact")),
								Name:  "X-Foo",
								Value: "a-value",
							},
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.headers.x-foo`,
												Operator: "eq",
												Value:    "a-value",
											},
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
			name: "Multiple header matches",
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Headers: []gatewayapiv1.HTTPHeaderMatch{
							{
								Type:  ptr.To(gatewayapiv1.HeaderMatchType("Exact")),
								Name:  "x-foo",
								Value: "a-value",
							},
							{
								Type:  ptr.To(gatewayapiv1.HeaderMatchType("Exact")),
								Name:  "x-bar",
								Value: "other-value",
							},
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.headers.x-foo`,
												Operator: "eq",
												Value:    "a-value",
											},
										},
									},
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.headers.x-bar`,
												Operator: "eq",
												Value:    "other-value",
											},
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
			name: "HeaderMatchRegularExpression",
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Headers: []gatewayapiv1.HTTPHeaderMatch{
							{
								Type:  ptr.To(gatewayapiv1.HeaderMatchType("RegularExpression")),
								Name:  "x-foo",
								Value: "^a+.*$",
							},
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `request.headers.x-foo`,
												Operator: "matches",
												Value:    "^a+.*$",
											},
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
			name: "Single query param match",
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						QueryParams: []gatewayapiv1.HTTPQueryParamMatch{
							{
								Type:  ptr.To(gatewayapiv1.QueryParamMatchType("Exact")),
								Name:  "x-foo",
								Value: "a-value",
							},
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
												{
													PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
														PatternExpression: authorinoapi.PatternExpression{
															Selector: `request.path.@extract:{"sep":"?x-foo=","pos":1}.@extract:{"sep":"&"}`,
															Operator: "eq",
															Value:    "a-value",
														},
													},
												},
												{
													PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
														PatternExpression: authorinoapi.PatternExpression{
															Selector: `request.path.@extract:{"sep":"&x-foo=","pos":1}.@extract:{"sep":"&"}`,
															Operator: "eq",
															Value:    "a-value",
														},
													},
												},
											},
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
			name: "Multiple query param matches",
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						QueryParams: []gatewayapiv1.HTTPQueryParamMatch{
							{
								Type:  ptr.To(gatewayapiv1.QueryParamMatchType("Exact")),
								Name:  "x-foo",
								Value: "a-value",
							},
							{
								Type:  ptr.To(gatewayapiv1.QueryParamMatchType("Exact")),
								Name:  "x-bar",
								Value: "other-value",
							},
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
												{
													PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
														PatternExpression: authorinoapi.PatternExpression{
															Selector: `request.path.@extract:{"sep":"?x-foo=","pos":1}.@extract:{"sep":"&"}`,
															Operator: "eq",
															Value:    "a-value",
														},
													},
												},
												{
													PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
														PatternExpression: authorinoapi.PatternExpression{
															Selector: `request.path.@extract:{"sep":"&x-foo=","pos":1}.@extract:{"sep":"&"}`,
															Operator: "eq",
															Value:    "a-value",
														},
													},
												},
											},
										},
									},
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
												{
													PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
														PatternExpression: authorinoapi.PatternExpression{
															Selector: `request.path.@extract:{"sep":"?x-bar=","pos":1}.@extract:{"sep":"&"}`,
															Operator: "eq",
															Value:    "other-value",
														},
													},
												},
												{
													PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
														PatternExpression: authorinoapi.PatternExpression{
															Selector: `request.path.@extract:{"sep":"&x-bar=","pos":1}.@extract:{"sep":"&"}`,
															Operator: "eq",
															Value:    "other-value",
														},
													},
												},
											},
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
			name: "QueryParamMatchRegularExpression",
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						QueryParams: []gatewayapiv1.HTTPQueryParamMatch{
							{
								Type:  ptr.To(gatewayapiv1.QueryParamMatchType("RegularExpression")),
								Name:  "x-foo",
								Value: "^a+.*$",
							},
						},
					},
				},
			},
			expected: []authorinoapi.PatternExpressionOrRef{
				{
					Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
						{
							PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
								All: []authorinoapi.UnstructuredPatternExpressionOrRef{
									{
										PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
											Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
												{
													PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
														PatternExpression: authorinoapi.PatternExpression{
															Selector: `request.path.@extract:{"sep":"?x-foo=","pos":1}.@extract:{"sep":"&"}`,
															Operator: "matches",
															Value:    "^a+.*$",
														},
													},
												},
												{
													PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
														PatternExpression: authorinoapi.PatternExpression{
															Selector: `request.path.@extract:{"sep":"&x-foo=","pos":1}.@extract:{"sep":"&"}`,
															Operator: "matches",
															Value:    "^a+.*$",
														},
													},
												},
											},
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
			result := authorinoConditionsFromHTTPRouteRule(tc.rule, tc.hostnames)
			if len(result) != len(tc.expected) {
				t.Errorf("Expected %d rule, got %d", len(tc.expected), len(result))
			}
			for i := range result {
				if !reflect.DeepEqual(result[i], tc.expected[i]) {
					t.Errorf("Expected rule %d to be %v, got %v", i, tc.expected[i], result[i])
				}
			}
		})
	}
}
