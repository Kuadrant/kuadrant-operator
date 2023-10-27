//go:build unit

package v1beta2

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

func TestCommonAuthRuleSpecGetRouteSelectors(t *testing.T) {
	spec := &CommonAuthRuleSpec{}
	if spec.GetRouteSelectors() != nil {
		t.Errorf("Expected nil route selectors")
	}
	routeSelector := testBuildRouteSelector()
	spec.RouteSelectors = []RouteSelector{routeSelector}
	result := spec.GetRouteSelectors()
	if len(result) != 1 {
		t.Errorf("Expected 1 route selector, got %d", len(result))
	}
	if !reflect.DeepEqual(result[0], routeSelector) {
		t.Errorf("Expected route selector %v, got %v", routeSelector, result[0])
	}
}

func TestAuthPolicySpecGetRouteSelectors(t *testing.T) {
	spec := &AuthPolicySpec{}
	if spec.GetRouteSelectors() != nil {
		t.Errorf("Expected nil route selectors")
	}
	routeSelector := testBuildRouteSelector()
	spec.RouteSelectors = []RouteSelector{routeSelector}
	result := spec.GetRouteSelectors()
	if len(result) != 1 {
		t.Errorf("Expected 1 route selector, got %d", len(result))
	}
	if !reflect.DeepEqual(result[0], routeSelector) {
		t.Errorf("Expected route selector %v, got %v", routeSelector, result[0])
	}
}

func TestAuthPolicyTargetKey(t *testing.T) {
	policy := &AuthPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-policy",
			Namespace: "my-namespace",
		},
		Spec: AuthPolicySpec{
			TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
				Group: "gateway.networking.k8s.io",
				Kind:  "HTTPRoute",
				Name:  "my-route",
			},
		},
	}
	// targetRef missing namespace
	expected := "my-namespace/my-route"
	if result := policy.TargetKey().String(); result != expected {
		t.Errorf("Expected target key %s, got %s", expected, result)
	}

	// targetRef with namespace
	policy.Spec.TargetRef.Namespace = ptr.To(gatewayapiv1.Namespace("route-namespace"))
	expected = "route-namespace/my-route"
	if result := policy.TargetKey().String(); result != expected {
		t.Errorf("Expected target key %s, got %s", expected, result)
	}
}

func TestAuthPolicyListGetItems(t *testing.T) {
	list := &AuthPolicyList{}
	if len(list.GetItems()) != 0 {
		t.Errorf("Expected empty list of items")
	}
	policy := AuthPolicy{}
	list.Items = []AuthPolicy{policy}
	result := list.GetItems()
	if len(result) != 1 {
		t.Errorf("Expected 1 item, got %d", len(result))
	}
	_, ok := result[0].(common.KuadrantPolicy)
	if !ok {
		t.Errorf("Expected item to be a KuadrantPolicy")
	}
}

func TestAuthPolicyGetRulesHostnames(t *testing.T) {
	policy := &AuthPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-policy",
			Namespace: "my-namespace",
		},
		Spec: AuthPolicySpec{
			TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
				Group: "gateway.networking.k8s.io",
				Kind:  "HTTPRoute",
				Name:  "my-route",
			},
		},
	}
	// no route selectors
	result := policy.GetRulesHostnames()
	if expected := 0; len(result) != expected {
		t.Errorf("Expected %d hostnames, got %d", expected, len(result))
	}
	policy.Spec.RouteSelectors = []RouteSelector{
		{
			Hostnames: []gatewayapiv1.Hostname{"*.kuadrant.io", "toystore.kuadrant.io"},
		},
	}
	// 1 top-level route selectors with 2 hostnames
	result = policy.GetRulesHostnames()
	if expected := 2; len(result) != expected {
		t.Errorf("Expected %d hostnames, got %d", expected, len(result))
	}
	if expected := "*.kuadrant.io"; result[0] != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[0])
	}
	if expected := "toystore.kuadrant.io"; result[1] != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[1])
	}
	// + 1 authentication route selector with 1 hostname
	policy.Spec.AuthScheme.Authentication = map[string]AuthenticationSpec{
		"my-authn": {
			CommonAuthRuleSpec: CommonAuthRuleSpec{
				RouteSelectors: []RouteSelector{testBuildRouteSelector()},
			},
		},
	}
	result = policy.GetRulesHostnames()
	if expected := 3; len(result) != expected {
		t.Errorf("Expected %d hostnames, got %d", expected, len(result))
	}
	if expected := "*.kuadrant.io"; result[0] != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[0])
	}
	if expected := "toystore.kuadrant.io"; result[1] != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[1])
	}
	if expected := "toystore.kuadrant.io"; result[2] != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[2])
	}
	// + 1 metadata route selector with 1 hostname
	policy.Spec.AuthScheme.Metadata = map[string]MetadataSpec{
		"my-metadata": {
			CommonAuthRuleSpec: CommonAuthRuleSpec{
				RouteSelectors: []RouteSelector{testBuildRouteSelector()},
			},
		},
	}
	result = policy.GetRulesHostnames()
	if expected := 4; len(result) != expected {
		t.Errorf("Expected %d hostnames, got %d", expected, len(result))
	}
	if expected := "toystore.kuadrant.io"; result[3] != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[3])
	}
	// + 2 authorization route selector with 1 hostname each
	policy.Spec.AuthScheme.Authorization = map[string]AuthorizationSpec{
		"my-authz": {
			CommonAuthRuleSpec: CommonAuthRuleSpec{
				RouteSelectors: []RouteSelector{testBuildRouteSelector(), testBuildRouteSelector()},
			},
		},
	}
	result = policy.GetRulesHostnames()
	if expected := 6; len(result) != expected {
		t.Errorf("Expected %d hostnames, got %d", expected, len(result))
	}
	if expected := "toystore.kuadrant.io"; result[4] != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[4])
	}
	if expected := "toystore.kuadrant.io"; result[5] != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[5])
	}
	// + 2 response route selectors with 2+1 hostnames
	policy.Spec.AuthScheme.Response = &ResponseSpec{
		Success: WrappedSuccessResponseSpec{
			Headers: map[string]HeaderSuccessResponseSpec{
				"my-header": {
					SuccessResponseSpec: SuccessResponseSpec{
						CommonAuthRuleSpec: CommonAuthRuleSpec{
							RouteSelectors: []RouteSelector{
								{
									Hostnames: []gatewayapiv1.Hostname{"*.kuadrant.io", "toystore.kuadrant.io"},
								},
							},
						},
					},
				},
			},
			DynamicMetadata: map[string]SuccessResponseSpec{
				"my-dynmetadata": {
					CommonAuthRuleSpec: CommonAuthRuleSpec{
						RouteSelectors: []RouteSelector{
							{
								Hostnames: []gatewayapiv1.Hostname{"*.kuadrant.io"},
							},
						},
					},
				},
			},
		},
	}
	result = policy.GetRulesHostnames()
	if expected := 9; len(result) != expected {
		t.Errorf("Expected %d hostnames, got %d", expected, len(result))
	}
	if expected := "*.kuadrant.io"; result[6] != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[6])
	}
	if expected := "toystore.kuadrant.io"; result[7] != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[7])
	}
	if expected := "*.kuadrant.io"; result[8] != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[8])
	}
	// + 1 callbacks route selector with 1 hostname
	policy.Spec.AuthScheme.Callbacks = map[string]CallbackSpec{
		"my-callback": {
			CommonAuthRuleSpec: CommonAuthRuleSpec{
				RouteSelectors: []RouteSelector{testBuildRouteSelector()},
			},
		},
	}
	result = policy.GetRulesHostnames()
	if expected := 10; len(result) != expected {
		t.Errorf("Expected %d hostnames, got %d", expected, len(result))
	}
	if expected := "toystore.kuadrant.io"; result[9] != expected {
		t.Errorf("Expected hostname to be %s, got %s", expected, result[9])
	}
}

func TestAuthPolicyValidate(t *testing.T) {
	testCases := []struct {
		name    string
		policy  *AuthPolicy
		valid   bool
		message string
	}{
		{
			name: "valid policy targeting a httproute",
			policy: &AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-policy",
					Namespace: "my-namespace",
				},
				Spec: AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: "gateway.networking.k8s.io",
						Kind:  "HTTPRoute",
						Name:  "my-route",
					},
				},
			},
			valid: true,
		},
		{
			name: "valid policy targeting a gateway",
			policy: &AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-policy",
					Namespace: "my-namespace",
				},
				Spec: AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: "gateway.networking.k8s.io",
						Kind:  "Gateway",
						Name:  "my-gw",
					},
				},
			},
			valid: true,
		},
		{
			name: "invalid targetRef group",
			policy: &AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-policy",
					Namespace: "my-namespace",
				},
				Spec: AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: "not-gateway.networking.k8s.io.group",
						Kind:  "HTTPRoute",
						Name:  "my-non-gwapi-route",
					},
				},
			},
			message: "invalid targetRef.Group not-gateway.networking.k8s.io.group. The only supported group is gateway.networking.k8s.io",
		},
		{
			name: "invalid targetRef kind",
			policy: &AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-policy",
					Namespace: "my-namespace",
				},
				Spec: AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: "gateway.networking.k8s.io",
						Kind:  "TCPRoute",
						Name:  "my-tcp-route",
					},
				},
			},
			message: "invalid targetRef.Kind TCPRoute. The only supported kinds are HTTPRoute and Gateway",
		},
		{
			name: "invalid usage of top-level route selectors with a gateway targetRef",
			policy: &AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-policy",
					Namespace: "my-namespace",
				},
				Spec: AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: "gateway.networking.k8s.io",
						Kind:  "Gateway",
						Name:  "my-gw",
					},
					RouteSelectors: []RouteSelector{
						{
							Hostnames: []gatewayapiv1.Hostname{"*.foo.io"},
							Matches: []gatewayapiv1.HTTPRouteMatch{
								{
									Path: &gatewayapiv1.HTTPPathMatch{
										Value: ptr.To("/foo"),
									},
								},
							},
						},
					},
				},
			},
			message: "route selectors not supported when targeting a Gateway",
		},
		{
			name: "invalid usage of config-level route selectors with a gateway targetRef",
			policy: &AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-policy",
					Namespace: "my-namespace",
				},
				Spec: AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: "gateway.networking.k8s.io",
						Kind:  "Gateway",
						Name:  "my-gw",
					},
					AuthScheme: AuthSchemeSpec{
						Authentication: map[string]AuthenticationSpec{
							"my-rule": {
								AuthenticationSpec: authorinoapi.AuthenticationSpec{
									AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
										AnonymousAccess: &authorinoapi.AnonymousAccessSpec{},
									},
								},
								CommonAuthRuleSpec: CommonAuthRuleSpec{
									RouteSelectors: []RouteSelector{
										{
											Hostnames: []gatewayapiv1.Hostname{"*.foo.io"},
											Matches: []gatewayapiv1.HTTPRouteMatch{
												{
													Path: &gatewayapiv1.HTTPPathMatch{
														Value: ptr.To("/foo"),
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
			message: "route selectors not supported when targeting a Gateway",
		},
		{
			name: "invalid targetRef namespace",
			policy: &AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-policy",
					Namespace: "my-namespace",
				},
				Spec: AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "HTTPRoute",
						Name:      "my-route",
						Namespace: ptr.To(gatewayapiv1.Namespace("other-namespace")),
					},
					AuthScheme: AuthSchemeSpec{
						Authentication: map[string]AuthenticationSpec{
							"my-rule": {
								AuthenticationSpec: authorinoapi.AuthenticationSpec{
									AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
										AnonymousAccess: &authorinoapi.AnonymousAccessSpec{},
									},
								},
								CommonAuthRuleSpec: CommonAuthRuleSpec{
									RouteSelectors: []RouteSelector{
										{
											Hostnames: []gatewayapiv1.Hostname{"*.foo.io"},
											Matches: []gatewayapiv1.HTTPRouteMatch{
												{
													Path: &gatewayapiv1.HTTPPathMatch{
														Value: ptr.To("/foo"),
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
			message: "invalid targetRef.Namespace other-namespace. Currently only supporting references to the same namespace",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.policy.Validate()
			if tc.valid && result != nil {
				t.Errorf("Expected policy to be valid, got %t", result)
			}
			if !tc.valid && result == nil {
				t.Error("Expected policy to be invalid, got no validation error")
			}
		})
	}
}

func testBuildRouteSelector() RouteSelector {
	return RouteSelector{
		Hostnames: []gatewayapiv1.Hostname{"toystore.kuadrant.io"},
		Matches: []gatewayapiv1.HTTPRouteMatch{
			{
				Path: &gatewayapiv1.HTTPPathMatch{
					Value: ptr.To("/toy"),
				},
			},
		},
	}
}
