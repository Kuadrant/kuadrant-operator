//go:build unit

package controllers

import (
	"reflect"
	"testing"

	istiosecurity "istio.io/api/security/v1beta1"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestIstioAuthorizationPolicyRulesFromHTTPRouteRule(t *testing.T) {
	testCases := []struct {
		name      string
		hostnames []gatewayapiv1.Hostname
		rule      gatewayapiv1.HTTPRouteRule
		expected  []*istiosecurity.Rule
	}{
		{
			name:      "No HTTPRouteMatch",
			hostnames: []gatewayapiv1.Hostname{"toystore.kuadrant.io"},
			rule:      gatewayapiv1.HTTPRouteRule{},
			expected: []*istiosecurity.Rule{
				{
					To: []*istiosecurity.Rule_To{
						{
							Operation: &istiosecurity.Operation{
								Hosts: []string{"toystore.kuadrant.io"},
							},
						},
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
			expected: []*istiosecurity.Rule{
				{
					To: []*istiosecurity.Rule_To{
						{
							Operation: &istiosecurity.Operation{
								Hosts: []string{"toystore.kuadrant.io"},
								Paths: []string{"/toy*"},
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
			expected: []*istiosecurity.Rule{
				{
					To: []*istiosecurity.Rule_To{
						{
							Operation: &istiosecurity.Operation{
								Hosts: []string{"toystore.kuadrant.io"},
								Paths: []string{"/toy*"},
							},
						},
					},
				},
				{
					To: []*istiosecurity.Rule_To{
						{
							Operation: &istiosecurity.Operation{
								Hosts: []string{"toystore.kuadrant.io"},
								Paths: []string{"/foo"},
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
			expected: []*istiosecurity.Rule{
				{
					To: []*istiosecurity.Rule_To{
						{
							Operation: &istiosecurity.Operation{
								Hosts: []string{"toystore.kuadrant.io", "gamestore.kuadrant.io"},
								Paths: []string{"/toy*"},
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
			expected: []*istiosecurity.Rule{
				{
					To: []*istiosecurity.Rule_To{
						{
							Operation: &istiosecurity.Operation{
								Hosts: []string{"toystore.kuadrant.io"},
								Paths: []string{"/toy*"},
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
			expected: []*istiosecurity.Rule{
				{
					To: []*istiosecurity.Rule_To{
						{
							Operation: &istiosecurity.Operation{
								Methods: []string{"GET"},
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
			expected: []*istiosecurity.Rule{
				{
					To: []*istiosecurity.Rule_To{
						{
							Operation: &istiosecurity.Operation{
								Paths: []string{"/toy"},
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
			expected: []*istiosecurity.Rule{
				{
					To: []*istiosecurity.Rule_To{
						{
							Operation: &istiosecurity.Operation{
								Paths: []string{"/toy*"},
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
							Value: ptr.To("/toy"),
						},
					},
				},
			},
			expected: []*istiosecurity.Rule{
				{
					To: []*istiosecurity.Rule_To{
						{
							Operation: &istiosecurity.Operation{},
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
								Name:  "x-foo",
								Value: "a-value",
							},
						},
					},
				},
			},
			expected: []*istiosecurity.Rule{
				{
					When: []*istiosecurity.Condition{
						{
							Key:    "request.headers[x-foo]",
							Values: []string{"a-value"},
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
			expected: []*istiosecurity.Rule{
				{
					When: []*istiosecurity.Condition{
						{
							Key:    "request.headers[x-foo]",
							Values: []string{"a-value"},
						},
						{
							Key:    "request.headers[x-bar]",
							Values: []string{"other-value"},
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
			expected: []*istiosecurity.Rule{
				{
					When: []*istiosecurity.Condition{},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := istioAuthorizationPolicyRulesFromHTTPRouteRule(tc.rule, tc.hostnames)
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
