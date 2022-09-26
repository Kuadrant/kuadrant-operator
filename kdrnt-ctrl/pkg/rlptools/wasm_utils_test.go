//go:build unit

package rlptools

import (
	"reflect"
	"testing"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestHTTPRouteRulesToRLPRules(t *testing.T) {
	testCases := []struct {
		name             string
		routeRules       []common.HTTPRouteRule
		expectedRLPRules []apimv1alpha1.Rule
	}{
		{
			"nil rules", nil, make([]apimv1alpha1.Rule, 0),
		},
		{
			"rule with paths methods and hosts",
			[]common.HTTPRouteRule{
				{
					Hosts:   []string{"*", "*.example.com"},
					Paths:   []string{"/admin/*", "/cats"},
					Methods: []string{"GET", "POST"},
				},
			}, []apimv1alpha1.Rule{
				{
					Hosts:   []string{"*", "*.example.com"},
					Paths:   []string{"/admin/*", "/cats"},
					Methods: []string{"GET", "POST"},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			rules := HTTPRouteRulesToRLPRules(tc.routeRules)
			if !reflect.DeepEqual(rules, tc.expectedRLPRules) {
				subT.Errorf("expected rules (%+v), got (%+v)", tc.expectedRLPRules, rules)
			}
		})
	}
}

func TestGatewayActionsFromRateLimitPolicy(t *testing.T) {
	tmpMatchPathPrefix := gatewayapiv1alpha2.PathMatchPathPrefix
	tmpMatchValue := "/toy"
	tmpMatchMethod := gatewayapiv1alpha2.HTTPMethod("GET")

	route := &gatewayapiv1alpha2.HTTPRoute{
		Spec: gatewayapiv1alpha2.HTTPRouteSpec{
			Hostnames: []gatewayapiv1alpha2.Hostname{"*.example.com"},
			Rules: []gatewayapiv1alpha2.HTTPRouteRule{
				{
					Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
						{
							Path: &gatewayapiv1alpha2.HTTPPathMatch{
								Type:  &tmpMatchPathPrefix,
								Value: &tmpMatchValue,
							},
							Method: &tmpMatchMethod,
						},
					},
				},
			},
		},
	}

	rlp := &apimv1alpha1.RateLimitPolicy{
		Spec: apimv1alpha1.RateLimitPolicySpec{
			RateLimits: []apimv1alpha1.RateLimit{
				{
					Rules: []apimv1alpha1.Rule{
						{
							Hosts: []string{"*.protected.example.com"},
						},
					},
				},
				{
					Rules: nil,
				},
			},
		},
	}

	expectedGatewayActions := []GatewayAction{
		{
			Rules: []apimv1alpha1.Rule{
				{
					Hosts: []string{"*.protected.example.com"},
				},
			},
		},
		{
			Rules: []apimv1alpha1.Rule{
				{
					Hosts:   []string{"*.example.com"},
					Paths:   []string{"/toy*"},
					Methods: []string{"GET"},
				},
			},
		},
	}

	gatewayActions := GatewayActionsFromRateLimitPolicy(rlp, route)
	if !reflect.DeepEqual(gatewayActions, expectedGatewayActions) {
		t.Errorf("expected gw actions (%+v), got (%+v)", expectedGatewayActions, gatewayActions)
	}
}
