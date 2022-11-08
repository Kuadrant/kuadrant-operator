//go:build unit

package rlptools

import (
	"reflect"
	"testing"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestHTTPRouteRulesToRLPRules(t *testing.T) {
	testCases := []struct {
		name             string
		routeRules       []common.HTTPRouteRule
		expectedRLPRules []kuadrantv1beta1.Rule
	}{
		{
			"nil rules", nil, make([]kuadrantv1beta1.Rule, 0),
		},
		{
			"rule with paths methods and hosts",
			[]common.HTTPRouteRule{
				{
					Hosts:   []string{"*", "*.example.com"},
					Paths:   []string{"/admin/*", "/cats"},
					Methods: []string{"GET", "POST"},
				},
			}, []kuadrantv1beta1.Rule{
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

	rlp := &kuadrantv1beta1.RateLimitPolicy{
		Spec: kuadrantv1beta1.RateLimitPolicySpec{
			RateLimits: []kuadrantv1beta1.RateLimit{
				{
					Rules: []kuadrantv1beta1.Rule{
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
			Rules: []kuadrantv1beta1.Rule{
				{
					Hosts: []string{"*.protected.example.com"},
				},
			},
		},
		{
			Rules: []kuadrantv1beta1.Rule{
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
