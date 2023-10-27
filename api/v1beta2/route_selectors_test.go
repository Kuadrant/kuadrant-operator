//go:build unit

package v1beta2

import (
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

func TestRouteSelectors(t *testing.T) {
	route := testBuildHttpRoute(testBuildGateway())

	testCases := []struct {
		name          string
		routeSelector RouteSelector
		route         *gatewayapiv1.HTTPRoute
		expected      []gatewayapiv1.HTTPRouteRule
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
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchPathPrefix}[0],
							Value: &[]string{"/assets"}[0],
						},
					},
				},
			},
			route:    route,
			expected: []gatewayapiv1.HTTPRouteRule{route.Spec.Rules[1]},
		},
		{
			name: "route selector selects the HTTPRouteRules whose set of HTTPRouteMatch contains at least one match",
			routeSelector: RouteSelector{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchPathPrefix}[0],
							Value: &[]string{"/toy"}[0],
						},
						Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethod("POST")}[0],
					},
				},
			},
			route:    route,
			expected: []gatewayapiv1.HTTPRouteRule{route.Spec.Rules[0]},
		},
		{
			name: "route selector with missing part of a HTTPRouteMatch still selects the HTTPRouteRules that match",
			routeSelector: RouteSelector{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchPathPrefix}[0],
							Value: &[]string{"/toy"}[0],
						},
					},
				},
			},
			route:    route,
			expected: []gatewayapiv1.HTTPRouteRule{route.Spec.Rules[0]},
		},
		{
			name: "route selector selects no HTTPRouteRule when no criterion matches",
			routeSelector: RouteSelector{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchExact}[0],
							Value: &[]string{"/toy"}[0],
						},
					},
				},
			},
			route:    route,
			expected: nil,
		},
		{
			name: "route selector selects the HTTPRouteRules whose HTTPRoute's hostnames match the selector",
			routeSelector: RouteSelector{
				Hostnames: []gatewayapiv1.Hostname{"api.toystore.com"},
			},
			route:    route,
			expected: route.Spec.Rules,
		},
		{
			name: "route selector selects the HTTPRouteRules whose HTTPRoute's hostnames match the selector additionally to other criteria",
			routeSelector: RouteSelector{
				Hostnames: []gatewayapiv1.Hostname{"api.toystore.com"},
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchPathPrefix}[0],
							Value: &[]string{"/toy"}[0],
						},
					},
				},
			},
			route:    route,
			expected: []gatewayapiv1.HTTPRouteRule{route.Spec.Rules[0]},
		},
		{
			name: "route selector does not select HTTPRouteRules whose HTTPRoute's hostnames do not match the selector",
			routeSelector: RouteSelector{
				Hostnames: []gatewayapiv1.Hostname{"www.toystore.com"},
			},
			route:    route,
			expected: nil,
		},
		{
			name: "route selector does not select HTTPRouteRules whose HTTPRoute's hostnames do not match the selector even when other criteria match",
			routeSelector: RouteSelector{
				Hostnames: []gatewayapiv1.Hostname{"www.toystore.com"},
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchPathPrefix}[0],
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
			rulesToStringSlice := func(rules []gatewayapiv1.HTTPRouteRule) []string {
				return common.Map(common.Map(rules, common.HTTPRouteRuleToString), func(r string) string { return fmt.Sprintf("{%s}", r) })
			}
			if !reflect.DeepEqual(rules, tc.expected) {
				t.Errorf("expected %v, got %v", rulesToStringSlice(tc.expected), rulesToStringSlice(rules))
			}
		})
	}
}

func TestRouteSelectorsHostnamesForConditions(t *testing.T) {
	route := testBuildHttpRoute(testBuildGateway())
	route.Spec.Hostnames = append(route.Spec.Hostnames, gatewayapiv1.Hostname("www.toystore.com"))

	// route and selector with exact same hostnames
	selector := RouteSelector{
		Hostnames: []gatewayapiv1.Hostname{"api.toystore.com", "www.toystore.com"},
	}
	result := selector.HostnamesForConditions(route)
	if expected := 1; len(result) != expected {
		t.Errorf("Expected %d hostnames, got %d", expected, len(result))
	}
	if expected := "*"; string(result[0]) != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[0])
	}

	// route and selector with some overlapping hostnames
	selector = RouteSelector{
		Hostnames: []gatewayapiv1.Hostname{"api.toystore.com", "other.io"},
	}
	result = selector.HostnamesForConditions(route)
	if expected := 1; len(result) != expected {
		t.Errorf("Expected %d hostnames, got %d", expected, len(result))
	}
	if expected := "api.toystore.com"; string(result[0]) != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[0])
	}

	// route and selector with no overlapping hostnames
	selector = RouteSelector{
		Hostnames: []gatewayapiv1.Hostname{"other.io"},
	}
	result = selector.HostnamesForConditions(route)
	if expected := 0; len(result) != expected {
		t.Errorf("Expected %d hostnames, got %d", expected, len(result))
	}

	// route with hostnames and selector without hostnames
	selector = RouteSelector{}
	result = selector.HostnamesForConditions(route)
	if expected := 1; len(result) != expected {
		t.Errorf("Expected %d hostnames, got %d", expected, len(result))
	}
	if expected := "*"; string(result[0]) != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[0])
	}

	// route without hostnames and selector with hostnames
	route.Spec.Hostnames = []gatewayapiv1.Hostname{}
	selector = RouteSelector{
		Hostnames: []gatewayapiv1.Hostname{"api.toystore.com"},
	}
	result = selector.HostnamesForConditions(route)
	if expected := 1; len(result) != expected {
		t.Errorf("Expected %d hostnames, got %d", expected, len(result))
	}

	// route and selector without hostnames
	selector = RouteSelector{}
	result = selector.HostnamesForConditions(route)
	if expected := 1; len(result) != expected {
		t.Errorf("Expected %d hostnames, got %d", expected, len(result))
	}
	if expected := "*"; string(result[0]) != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[0])
	}
}

func testBuildGateway() *gatewayapiv1.Gateway {
	return &gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-gateway",
		},
		Spec: gatewayapiv1.GatewaySpec{
			Listeners: []gatewayapiv1.Listener{
				{
					Hostname: ptr.To(gatewayapiv1.Hostname("*.toystore.com")),
				},
			},
		},
	}
}

func testBuildHttpRoute(parentGateway *gatewayapiv1.Gateway) *gatewayapiv1.HTTPRoute {
	return &gatewayapiv1.HTTPRoute{
		Spec: gatewayapiv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1.ParentReference{
					{
						Name: gatewayapiv1.ObjectName(parentGateway.Name),
					},
				},
			},
			Hostnames: []gatewayapiv1.Hostname{"api.toystore.com"},
			Rules: []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						// get /toys*
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchPathPrefix}[0],
								Value: &[]string{"/toy"}[0],
							},
							Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethod("GET")}[0],
						},
						// post /toys*
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchPathPrefix}[0],
								Value: &[]string{"/toy"}[0],
							},
							Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethod("POST")}[0],
						},
					},
				},
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						// /assets*
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchPathPrefix}[0],
								Value: &[]string{"/assets"}[0],
							},
						},
					},
				},
			},
		},
	}
}
