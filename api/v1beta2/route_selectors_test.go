//go:build unit

package v1beta2

import (
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

func TestRouteSelectors(t *testing.T) {
	gatewayHostnames := []gatewayapiv1beta1.Hostname{
		"*.toystore.com",
	}

	gateway := &gatewayapiv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-gateway",
		},
	}

	for _, hostname := range gatewayHostnames {
		gateway.Spec.Listeners = append(gateway.Spec.Listeners, gatewayapiv1beta1.Listener{Hostname: &hostname})
	}

	route := &gatewayapiv1beta1.HTTPRoute{
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1beta1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1beta1.ParentReference{
					{
						Name: gatewayapiv1beta1.ObjectName(gateway.Name),
					},
				},
			},
			Hostnames: []gatewayapiv1beta1.Hostname{"api.toystore.com"},
			Rules: []gatewayapiv1beta1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1beta1.HTTPRouteMatch{
						// get /toys*
						{
							Path: &gatewayapiv1beta1.HTTPPathMatch{
								Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchPathPrefix}[0],
								Value: &[]string{"/toy"}[0],
							},
							Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethod("GET")}[0],
						},
						// post /toys*
						{
							Path: &gatewayapiv1beta1.HTTPPathMatch{
								Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchPathPrefix}[0],
								Value: &[]string{"/toy"}[0],
							},
							Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethod("POST")}[0],
						},
					},
				},
				{
					Matches: []gatewayapiv1beta1.HTTPRouteMatch{
						// /assets*
						{
							Path: &gatewayapiv1beta1.HTTPPathMatch{
								Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchPathPrefix}[0],
								Value: &[]string{"/assets"}[0],
							},
						},
					},
				},
			},
		},
	}

	testCases := []struct {
		name          string
		routeSelector RouteSelector
		route         *gatewayapiv1beta1.HTTPRoute
		expected      []gatewayapiv1beta1.HTTPRouteRule
	}{
		{
			name:          "empty route selector selects all HTTPRouteRules",
			routeSelector: RouteSelector{},
			route:         route,
			expected:      route.Spec.Rules,
		},
		{
			name: "route selector selects the HTTPRouteRules whose set of HTTPRouteMatch is a perfect match",
			routeSelector: RouteSelector{
				Matches: []gatewayapiv1beta1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1beta1.HTTPPathMatch{
							Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchPathPrefix}[0],
							Value: &[]string{"/assets"}[0],
						},
					},
				},
			},
			route:    route,
			expected: []gatewayapiv1beta1.HTTPRouteRule{route.Spec.Rules[1]},
		},
		{
			name: "route selector selects the HTTPRouteRules whose set of HTTPRouteMatch contains at least one match",
			routeSelector: RouteSelector{
				Matches: []gatewayapiv1beta1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1beta1.HTTPPathMatch{
							Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchPathPrefix}[0],
							Value: &[]string{"/toy"}[0],
						},
						Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethod("POST")}[0],
					},
				},
			},
			route:    route,
			expected: []gatewayapiv1beta1.HTTPRouteRule{route.Spec.Rules[0]},
		},
		{
			name: "route selector with missing part of a HTTPRouteMatch still selects the HTTPRouteRules that match",
			routeSelector: RouteSelector{
				Matches: []gatewayapiv1beta1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1beta1.HTTPPathMatch{
							Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchPathPrefix}[0],
							Value: &[]string{"/toy"}[0],
						},
					},
				},
			},
			route:    route,
			expected: []gatewayapiv1beta1.HTTPRouteRule{route.Spec.Rules[0]},
		},
		{
			name: "route selector selects no HTTPRouteRule when no criterion matches",
			routeSelector: RouteSelector{
				Matches: []gatewayapiv1beta1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1beta1.HTTPPathMatch{
							Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchExact}[0],
							Value: &[]string{"/toy"}[0],
						},
					},
				},
			},
			route:    route,
			expected: []gatewayapiv1beta1.HTTPRouteRule{},
		},
		{
			name: "route selector selects the HTTPRouteRules whose HTTPRoute's hostnames match the selector",
			routeSelector: RouteSelector{
				Hostnames: []gatewayapiv1beta1.Hostname{"api.toystore.com"},
			},
			route:    route,
			expected: route.Spec.Rules,
		},
		{
			name: "route selector selects the HTTPRouteRules whose HTTPRoute's hostnames match the selector additionally to other criteria",
			routeSelector: RouteSelector{
				Hostnames: []gatewayapiv1beta1.Hostname{"api.toystore.com"},
				Matches: []gatewayapiv1beta1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1beta1.HTTPPathMatch{
							Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchPathPrefix}[0],
							Value: &[]string{"/toy"}[0],
						},
					},
				},
			},
			route:    route,
			expected: []gatewayapiv1beta1.HTTPRouteRule{route.Spec.Rules[0]},
		},
		{
			name: "route selector does not select HTTPRouteRules whose HTTPRoute's hostnames do not match the selector",
			routeSelector: RouteSelector{
				Hostnames: []gatewayapiv1beta1.Hostname{"www.toystore.com"},
			},
			route:    route,
			expected: nil,
		},
		{
			name: "route selector does not select HTTPRouteRules whose HTTPRoute's hostnames do not match the selector even when other criteria match",
			routeSelector: RouteSelector{
				Hostnames: []gatewayapiv1beta1.Hostname{"www.toystore.com"},
				Matches: []gatewayapiv1beta1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1beta1.HTTPPathMatch{
							Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchPathPrefix}[0],
							Value: &[]string{"/toy"}[0],
						},
					},
				},
			},
			route:    route,
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rules := tc.routeSelector.SelectRules(tc.route)
			rulesToStringSlice := func(rules []gatewayapiv1beta1.HTTPRouteRule) []string {
				return common.Map(common.Map(rules, common.HTTPRouteRuleToString), func(r string) string { return fmt.Sprintf("{%s}", r) })
			}
			if !reflect.DeepEqual(rules, tc.expected) {
				t.Errorf("expected %v, got %v", rulesToStringSlice(tc.expected), rulesToStringSlice(rules))
			}
		})
	}
}
