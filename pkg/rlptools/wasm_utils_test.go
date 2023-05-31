//go:build unit

package rlptools

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
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
	httpRoute := &gatewayapiv1beta1.HTTPRoute{
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			Hostnames: []gatewayapiv1beta1.Hostname{"*.example.com"},
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

	t.Run("empty rate limits return empty actions", func(subT *testing.T) {
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String()},
			ObjectMeta: metav1.ObjectMeta{Name: "rlpA", Namespace: "nsA"},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				Limits: map[string]kuadrantv1beta2.Limit{
					"l1": kuadrantv1beta2.Limit{
						Rates: []kuadrantv1beta2.Rate{
							Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
						},
					},
				},
			},
		}

		expectedGatewayActions := []GatewayAction{
			{
				Configurations: []Configuration{
					{
						Actions: []ActionSpecifier{
							{
								GenericKey: &GenericKeySpec{
									DescriptorKey:   "nsA/rlpA/l1",
									DescriptorValue: "1",
								},
							},
						},
					},
				},
				Rules: []Rule{
					{
						Paths:   []string{"/toy*"},
						Methods: []string{"GET"},
						Hosts:   []string{"*.example.com"},
					},
				},
			},
		}

		gatewayActions := GatewayActionsFromRateLimitPolicy(rlp, httpRoute)
		if !reflect.DeepEqual(gatewayActions, expectedGatewayActions) {
			t.Errorf("expected gw actions (%+v), got (%+v)", expectedGatewayActions, gatewayActions)
		}
	})

	t.Run("basic test", func(subT *testing.T) {
		rlp := &kuadrantv1beta1.RateLimitPolicy{
			Spec: kuadrantv1beta1.RateLimitPolicySpec{
				RateLimits: []kuadrantv1beta1.RateLimit{
					{
						Configurations: defaultConfigurations(),
						Rules: []kuadrantv1beta1.Rule{
							{
								Hosts: []string{"*.protected.example.com"},
							},
						},
					},
					{
						Configurations: defaultConfigurations(),
						Rules:          nil,
					},
				},
			},
		}

		expectedGatewayActions := []GatewayAction{
			{
				Configurations: defaultConfigurations(),
				Rules: []kuadrantv1beta1.Rule{
					{
						Hosts: []string{"*.protected.example.com"},
					},
				},
			},
			{
				Configurations: defaultConfigurations(),
				Rules: []kuadrantv1beta1.Rule{
					{
						Hosts:   []string{"*.example.com"},
						Paths:   []string{"/toy*"},
						Methods: []string{"GET"},
					},
				},
			},
		}
		gatewayActions := GatewayActionsFromRateLimitPolicy(rlp, httpRoute)
		if !reflect.DeepEqual(gatewayActions, expectedGatewayActions) {
			t.Errorf("expected gw actions (%+v), got (%+v)", expectedGatewayActions, gatewayActions)
		}
	})

	t.Run("when the configuration obj is missing skip it", func(subT *testing.T) {
		rlp := &kuadrantv1beta1.RateLimitPolicy{
			Spec: kuadrantv1beta1.RateLimitPolicySpec{
				RateLimits: []kuadrantv1beta1.RateLimit{
					{
						// configurations object is missing
						Rules: []kuadrantv1beta1.Rule{{Hosts: []string{"a.example.com"}}},
					},
					{
						Configurations: defaultConfigurations(),
						Rules:          []kuadrantv1beta1.Rule{{Hosts: []string{"b.example.com"}}},
					},
				},
			},
		}

		expectedGatewayActions := []GatewayAction{
			{
				Configurations: defaultConfigurations(),
				Rules:          []kuadrantv1beta1.Rule{{Hosts: []string{"b.example.com"}}},
			},
		}

		gatewayActions := GatewayActionsFromRateLimitPolicy(rlp, httpRoute)
		if !reflect.DeepEqual(gatewayActions, expectedGatewayActions) {
			t.Errorf("expected gw actions (%+v), got (%+v)", expectedGatewayActions, gatewayActions)
		}
	})

	t.Run("when rlp targeting a httproute does not have any configuration obj then default is applied", func(subT *testing.T) {
		rlp := &kuadrantv1beta1.RateLimitPolicy{
			Spec: kuadrantv1beta1.RateLimitPolicySpec{
				RateLimits: []kuadrantv1beta1.RateLimit{
					{
						// configurations object is missing
						Rules: []kuadrantv1beta1.Rule{{Hosts: []string{"a.example.com"}}},
					},
					{
						// configurations object is missing
						Rules: []kuadrantv1beta1.Rule{{Hosts: []string{"b.example.com"}}},
					},
				},
			},
		}

		expectedGatewayActions := []GatewayAction{
			{
				Configurations: DefaultGatewayConfiguration(client.ObjectKeyFromObject(rlp)),
				Rules: []kuadrantv1beta1.Rule{
					{
						Hosts:   []string{"*.example.com"},
						Paths:   []string{"/toy*"},
						Methods: []string{"GET"},
					},
				},
			},
		}

		gatewayActions := GatewayActionsFromRateLimitPolicy(rlp, httpRoute)
		if !reflect.DeepEqual(gatewayActions, expectedGatewayActions) {
			t.Errorf("expected gw actions (%+v), got (%+v)", expectedGatewayActions, gatewayActions)
		}
	})

	t.Run("when rlp targeting a gateway does not have any configuration obj then default is applied", func(subT *testing.T) {
		rlp := &kuadrantv1beta1.RateLimitPolicy{
			Spec: kuadrantv1beta1.RateLimitPolicySpec{
				RateLimits: []kuadrantv1beta1.RateLimit{
					{
						// configurations object is missing
						Rules: []kuadrantv1beta1.Rule{{Hosts: []string{"a.example.com"}}},
					},
					{
						// configurations object is missing
						Rules: []kuadrantv1beta1.Rule{{Hosts: []string{"b.example.com"}}},
					},
				},
			},
		}

		expectedGatewayActions := []GatewayAction{
			{
				Configurations: DefaultGatewayConfiguration(client.ObjectKeyFromObject(rlp)),
				Rules:          []kuadrantv1beta1.Rule{},
			},
		}

		gatewayActions := GatewayActionsFromRateLimitPolicy(rlp, nil)
		if !reflect.DeepEqual(gatewayActions, expectedGatewayActions) {
			t.Errorf("expected gw actions (%+v), got (%+v)", expectedGatewayActions, gatewayActions)
		}
	})
}

func defaultConfigurations() []kuadrantv1beta1.Configuration {
	return []kuadrantv1beta1.Configuration{
		{
			Actions: []kuadrantv1beta1.ActionSpecifier{
				{
					GenericKey: &kuadrantv1beta1.GenericKeySpec{
						DescriptorValue: "some value",
						DescriptorKey:   &[]string{"some key"}[0],
					},
				},
			},
		},
	}
}
