//go:build unit

package common

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestRouteHostnames(t *testing.T) {
	testCases := []struct {
		name     string
		route    *gatewayapiv1beta1.HTTPRoute
		expected []string
	}{
		{
			"nil",
			nil,
			nil,
		},
		{
			"nil hostname",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{
					Hostnames: nil,
				},
			},
			[]string{"*"},
		},
		{
			"basic",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{
					Hostnames: []gatewayapiv1beta1.Hostname{"*.com", "example.net", "test.example.net"},
				},
			},
			[]string{"*.com", "example.net", "test.example.net"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res := RouteHostnames(tc.route)
			if !reflect.DeepEqual(res, tc.expected) {
				subT.Errorf("result (%v) does not match expected (%v)", res, tc.expected)
			}
		})
	}
}

func TestRulesFromHTTPRoute(t *testing.T) {
	var (
		getMethod                                         = "GET"
		catsPath                                          = "/cats"
		dogsPath                                          = "/dogs"
		rabbitsPath                                       = "/rabbits"
		getHTTPMethod        gatewayapiv1beta1.HTTPMethod = "GET"
		postHTTPMethod       gatewayapiv1beta1.HTTPMethod = "POST"
		pathPrefix                                        = gatewayapiv1beta1.PathMatchPathPrefix
		pathExact                                         = gatewayapiv1beta1.PathMatchExact
		catsPrefixPatchMatch                              = gatewayapiv1beta1.HTTPPathMatch{
			Type:  &pathPrefix,
			Value: &catsPath,
		}
		dogsExactPatchMatch = gatewayapiv1beta1.HTTPPathMatch{
			Type:  &pathExact,
			Value: &dogsPath,
		}
		rabbitsPrefixPatchMatch = gatewayapiv1beta1.HTTPPathMatch{
			Value: &rabbitsPath,
		}
	)

	testCases := []struct {
		name     string
		route    *gatewayapiv1beta1.HTTPRoute
		expected []HTTPRouteRule
	}{
		{
			"nil",
			nil,
			nil,
		},
		{
			"nil rules",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{
					Rules:     nil,
					Hostnames: []gatewayapiv1beta1.Hostname{"*.com"},
				},
			},
			[]HTTPRouteRule{{Hosts: []string{"*.com"}}},
		},
		{
			"empty rules",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{
					Rules:     make([]gatewayapiv1beta1.HTTPRouteRule, 0),
					Hostnames: []gatewayapiv1beta1.Hostname{"*.com"},
				},
			},
			[]HTTPRouteRule{{Hosts: []string{"*.com"}}},
		},
		{
			"with method",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{
					Rules: []gatewayapiv1beta1.HTTPRouteRule{
						{
							Matches: []gatewayapiv1beta1.HTTPRouteMatch{
								{
									Method: &getHTTPMethod,
								},
							},
						},
					},
				},
			},
			[]HTTPRouteRule{{
				Hosts:   []string{"*"},
				Methods: []string{getMethod},
			}},
		},
		{
			"with path",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{
					Rules: []gatewayapiv1beta1.HTTPRouteRule{
						{
							Matches: []gatewayapiv1beta1.HTTPRouteMatch{
								{
									Path: &catsPrefixPatchMatch,
								},
							},
						},
					},
				},
			},
			[]HTTPRouteRule{{
				Hosts: []string{"*"},
				Paths: []string{"/cats*"},
			}},
		},
		{
			"with path and default path match type",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{
					Rules: []gatewayapiv1beta1.HTTPRouteRule{
						{
							Matches: []gatewayapiv1beta1.HTTPRouteMatch{
								{
									Path: &rabbitsPrefixPatchMatch,
								},
							},
						},
					},
				},
			},
			[]HTTPRouteRule{{
				Hosts: []string{"*"},
				Paths: []string{"/rabbits*"},
			}},
		},
		{
			"no paths or methods",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{
					Rules: []gatewayapiv1beta1.HTTPRouteRule{
						{
							Matches: []gatewayapiv1beta1.HTTPRouteMatch{
								{
									Headers: []gatewayapiv1beta1.HTTPHeaderMatch{
										{
											Name:  "someheader",
											Value: "somevalue",
										},
									},
								},
							},
						},
					},
					Hostnames: []gatewayapiv1beta1.Hostname{"*.com"},
				},
			},
			[]HTTPRouteRule{{Hosts: []string{"*.com"}}},
		},
		{
			"basic",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{
					Hostnames: []gatewayapiv1beta1.Hostname{"*.com"},
					Rules: []gatewayapiv1beta1.HTTPRouteRule{
						{
							// GET /cats*
							// POST /dogs
							Matches: []gatewayapiv1beta1.HTTPRouteMatch{
								{
									Path:   &catsPrefixPatchMatch,
									Method: &getHTTPMethod,
								},
								{
									Path:   &dogsExactPatchMatch,
									Method: &postHTTPMethod,
								},
							},
						},
					},
				},
			},
			[]HTTPRouteRule{
				{
					Hosts:   []string{"*.com"},
					Methods: []string{"GET"},
					Paths:   []string{"/cats*"},
				}, {
					Hosts:   []string{"*.com"},
					Methods: []string{"POST"},
					Paths:   []string{"/dogs"},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res := RulesFromHTTPRoute(tc.route)
			if !reflect.DeepEqual(res, tc.expected) {
				subT.Errorf("result (%+v) does not match expected (%+v)", res, tc.expected)
			}
		})
	}
}

func TestHTTPRouteRuleSelectorSelects(t *testing.T) {
	testCases := []struct {
		name     string
		selector HTTPRouteRuleSelector
		rule     gatewayapiv1beta1.HTTPRouteRule
		expected bool
	}{
		{
			name: "when the httproutrule contains the exact match then return true",
			selector: HTTPRouteRuleSelector{
				HTTPRouteMatch: &gatewayapiv1beta1.HTTPRouteMatch{
					Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodGet}[0],
					Headers: []gatewayapiv1beta1.HTTPHeaderMatch{
						{
							Type:  &[]gatewayapiv1beta1.HeaderMatchType{gatewayapiv1beta1.HeaderMatchExact}[0],
							Name:  "someheader",
							Value: "somevalue",
						},
					},
				},
			},
			rule: gatewayapiv1beta1.HTTPRouteRule{
				Matches: []gatewayapiv1beta1.HTTPRouteMatch{
					{
						Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodGet}[0],
						Headers: []gatewayapiv1beta1.HTTPHeaderMatch{
							{
								Type:  &[]gatewayapiv1beta1.HeaderMatchType{gatewayapiv1beta1.HeaderMatchExact}[0],
								Name:  "someheader",
								Value: "somevalue",
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "when the httproutrule contains the exact match and more then return true",
			selector: HTTPRouteRuleSelector{
				HTTPRouteMatch: &gatewayapiv1beta1.HTTPRouteMatch{
					Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodGet}[0],
				},
			},
			rule: gatewayapiv1beta1.HTTPRouteRule{
				Matches: []gatewayapiv1beta1.HTTPRouteMatch{
					{
						Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodGet}[0],
						Headers: []gatewayapiv1beta1.HTTPHeaderMatch{
							{
								Type:  &[]gatewayapiv1beta1.HeaderMatchType{gatewayapiv1beta1.HeaderMatchExact}[0],
								Name:  "someheader",
								Value: "somevalue",
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "when the httproutrule contains all the matching headers and more then return true",
			selector: HTTPRouteRuleSelector{
				HTTPRouteMatch: &gatewayapiv1beta1.HTTPRouteMatch{
					Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodGet}[0],
					Headers: []gatewayapiv1beta1.HTTPHeaderMatch{
						{
							Type:  &[]gatewayapiv1beta1.HeaderMatchType{gatewayapiv1beta1.HeaderMatchExact}[0],
							Name:  "someheader",
							Value: "somevalue",
						},
					},
				},
			},
			rule: gatewayapiv1beta1.HTTPRouteRule{
				Matches: []gatewayapiv1beta1.HTTPRouteMatch{
					{
						Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodGet}[0],
						Headers: []gatewayapiv1beta1.HTTPHeaderMatch{
							{
								Type:  &[]gatewayapiv1beta1.HeaderMatchType{gatewayapiv1beta1.HeaderMatchExact}[0],
								Name:  "someheader",
								Value: "somevalue",
							},
							{
								Type:  &[]gatewayapiv1beta1.HeaderMatchType{gatewayapiv1beta1.HeaderMatchRegularExpression}[0],
								Name:  "someotherheader",
								Value: "someregex.*",
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "when the httproutrule contains an inexact match then return false",
			selector: HTTPRouteRuleSelector{
				HTTPRouteMatch: &gatewayapiv1beta1.HTTPRouteMatch{
					Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodGet}[0],
					Headers: []gatewayapiv1beta1.HTTPHeaderMatch{
						{
							Type:  &[]gatewayapiv1beta1.HeaderMatchType{gatewayapiv1beta1.HeaderMatchExact}[0],
							Name:  "someheader",
							Value: "somevalue",
						},
					},
				},
			},
			rule: gatewayapiv1beta1.HTTPRouteRule{
				Matches: []gatewayapiv1beta1.HTTPRouteMatch{
					{
						Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodPost}[0],
						Headers: []gatewayapiv1beta1.HTTPHeaderMatch{
							{
								Type:  &[]gatewayapiv1beta1.HeaderMatchType{gatewayapiv1beta1.HeaderMatchExact}[0],
								Name:  "someheader",
								Value: "somevalue",
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "when the httproutrule is empty then return false",
			rule: gatewayapiv1beta1.HTTPRouteRule{},
			selector: HTTPRouteRuleSelector{
				HTTPRouteMatch: &gatewayapiv1beta1.HTTPRouteMatch{
					Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodGet}[0],
				},
			},
			expected: false,
		},
		{
			name:     "when the selector is empty then return true",
			selector: HTTPRouteRuleSelector{},
			rule: gatewayapiv1beta1.HTTPRouteRule{
				Matches: []gatewayapiv1beta1.HTTPRouteMatch{
					{
						Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodGet}[0],
						Headers: []gatewayapiv1beta1.HTTPHeaderMatch{
							{
								Type:  &[]gatewayapiv1beta1.HeaderMatchType{gatewayapiv1beta1.HeaderMatchExact}[0],
								Name:  "someheader",
								Value: "somevalue",
							},
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if r := tc.selector.Selects(tc.rule); r != tc.expected {
				expectedStr := ""
				resultStr := "not"
				if !tc.expected {
					expectedStr = "not"
					resultStr = ""
				}
				t.Error("expected selector", HTTPRouteMatchToString(*tc.selector.HTTPRouteMatch), expectedStr, "to select rule", HTTPRouteRuleToString(tc.rule), "but it does", resultStr)
			}
		})
	}
}

func TestHTTPPathMatchToString(t *testing.T) {
	testCases := []struct {
		name     string
		input    *gatewayapiv1beta1.HTTPPathMatch
		expected string
	}{
		{
			name: "exact path match",
			input: &[]gatewayapiv1beta1.HTTPPathMatch{
				{
					Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchExact}[0],
					Value: &[]string{"/foo"}[0],
				},
			}[0],
			expected: "/foo",
		},
		{
			name: "regex path match",
			input: &[]gatewayapiv1beta1.HTTPPathMatch{
				{
					Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchRegularExpression}[0],
					Value: &[]string{"^\\/foo.*"}[0],
				},
			}[0],
			expected: "~/^\\/foo.*/",
		},
		{
			name: "path prefix match",
			input: &[]gatewayapiv1beta1.HTTPPathMatch{
				{
					Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchPathPrefix}[0],
					Value: &[]string{"/foo"}[0],
				},
			}[0],
			expected: "/foo*",
		},
		{
			name: "path match with default type",
			input: &[]gatewayapiv1beta1.HTTPPathMatch{
				{
					Value: &[]string{"/foo"}[0],
				},
			}[0],
			expected: "/foo*",
		},
		{
			name:     "nil path match",
			input:    nil,
			expected: "*",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if r := HTTPPathMatchToString(tc.input); r != tc.expected {
				t.Errorf("expected: %s, got: %s", tc.expected, r)
			}
		})
	}
}

func TestHTTPHeaderMatchToString(t *testing.T) {
	testCases := []struct {
		name     string
		input    gatewayapiv1beta1.HTTPHeaderMatch
		expected string
	}{
		{
			name: "exact header match",
			input: gatewayapiv1beta1.HTTPHeaderMatch{
				Type:  &[]gatewayapiv1beta1.HeaderMatchType{gatewayapiv1beta1.HeaderMatchExact}[0],
				Name:  "some-header",
				Value: "foo",
			},
			expected: "{some-header:foo}",
		},
		{
			name: "regex header match",
			input: gatewayapiv1beta1.HTTPHeaderMatch{
				Type:  &[]gatewayapiv1beta1.HeaderMatchType{gatewayapiv1beta1.HeaderMatchRegularExpression}[0],
				Name:  "some-header",
				Value: "^foo.*",
			},
			expected: "{some-header:~/^foo.*/}",
		},
		{
			name: "header match with default type",
			input: gatewayapiv1beta1.HTTPHeaderMatch{
				Name:  "some-header",
				Value: "foo",
			},
			expected: "{some-header:foo}",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if r := HTTPHeaderMatchToString(tc.input); r != tc.expected {
				t.Errorf("expected: %s, got: %s", tc.expected, r)
			}
		})
	}
}

func TestHTTPQueryParamMatchToString(t *testing.T) {
	testCases := []struct {
		name     string
		input    gatewayapiv1beta1.HTTPQueryParamMatch
		expected string
	}{
		{
			name: "exact query param match",
			input: gatewayapiv1beta1.HTTPQueryParamMatch{
				Type:  &[]gatewayapiv1beta1.QueryParamMatchType{gatewayapiv1beta1.QueryParamMatchExact}[0],
				Name:  "some-param",
				Value: "foo",
			},
			expected: "{some-param:foo}",
		},
		{
			name: "regex query param match",
			input: gatewayapiv1beta1.HTTPQueryParamMatch{
				Type:  &[]gatewayapiv1beta1.QueryParamMatchType{gatewayapiv1beta1.QueryParamMatchRegularExpression}[0],
				Name:  "some-param",
				Value: "^foo.*",
			},
			expected: "{some-param:~/^foo.*/}",
		},
		{
			name: "query param match with default type",
			input: gatewayapiv1beta1.HTTPQueryParamMatch{
				Name:  "some-param",
				Value: "foo",
			},
			expected: "{some-param:foo}",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if r := HTTPQueryParamMatchToString(tc.input); r != tc.expected {
				t.Errorf("expected: %s, got: %s", tc.expected, r)
			}
		})
	}
}

func TestHTTPMethodToString(t *testing.T) {
	testCases := []struct {
		input    *gatewayapiv1beta1.HTTPMethod
		expected string
	}{
		{
			input:    &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodGet}[0],
			expected: "GET",
		},
		{
			input:    &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodHead}[0],
			expected: "HEAD",
		},
		{
			input:    &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodPost}[0],
			expected: "POST",
		},
		{
			input:    &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodPut}[0],
			expected: "PUT",
		},
		{
			input:    &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodPatch}[0],
			expected: "PATCH",
		},
		{
			input:    &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodDelete}[0],
			expected: "DELETE",
		},
		{
			input:    &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodConnect}[0],
			expected: "CONNECT",
		},
		{
			input:    &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodOptions}[0],
			expected: "OPTIONS",
		},
		{
			input:    &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodTrace}[0],
			expected: "TRACE",
		},
		{
			input:    nil,
			expected: "*",
		},
	}
	for _, tc := range testCases {
		if r := HTTPMethodToString(tc.input); r != tc.expected {
			t.Errorf("expected: %s, got: %s", tc.expected, r)
		}
	}
}

func TestHTTPRouteMatchToString(t *testing.T) {
	match := gatewayapiv1beta1.HTTPRouteMatch{
		Path: &gatewayapiv1beta1.HTTPPathMatch{
			Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchExact}[0],
			Value: &[]string{"/foo"}[0],
		},
		Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodGet}[0],
		QueryParams: []gatewayapiv1beta1.HTTPQueryParamMatch{
			{
				Type:  &[]gatewayapiv1beta1.QueryParamMatchType{gatewayapiv1beta1.QueryParamMatchRegularExpression}[0],
				Name:  "page",
				Value: "\\d+",
			},
		},
	}

	expected := "{method:GET,path:/foo,queryParams:[{page:~/\\d+/}]}"

	if r := HTTPRouteMatchToString(match); r != expected {
		t.Errorf("expected: %s, got: %s", expected, r)
	}

	match.Headers = []gatewayapiv1beta1.HTTPHeaderMatch{
		{
			Name:  "x-foo",
			Value: "bar",
		},
	}

	expected = "{method:GET,path:/foo,queryParams:[{page:~/\\d+/}],headers:[{x-foo:bar}]}"

	if r := HTTPRouteMatchToString(match); r != expected {
		t.Errorf("expected: %s, got: %s", expected, r)
	}
}

func TestHTTPRouteRuleToString(t *testing.T) {
	rule := gatewayapiv1beta1.HTTPRouteRule{}

	expected := "{matches:[]}"

	if r := HTTPRouteRuleToString(rule); r != expected {
		t.Errorf("expected: %s, got: %s", expected, r)
	}

	rule.Matches = []gatewayapiv1beta1.HTTPRouteMatch{
		{
			Path: &gatewayapiv1beta1.HTTPPathMatch{
				Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchExact}[0],
				Value: &[]string{"/foo"}[0],
			},
			Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethodGet}[0],
			QueryParams: []gatewayapiv1beta1.HTTPQueryParamMatch{
				{
					Type:  &[]gatewayapiv1beta1.QueryParamMatchType{gatewayapiv1beta1.QueryParamMatchRegularExpression}[0],
					Name:  "page",
					Value: "\\d+",
				},
			},
		},
	}

	expected = "{matches:[{method:GET,path:/foo,queryParams:[{page:~/\\d+/}]}]}"

	if r := HTTPRouteRuleToString(rule); r != expected {
		t.Errorf("expected: %s, got: %s", expected, r)
	}
}

func TestGatewaysMissingPolicyRef(t *testing.T) {
	gwList := &gatewayapiv1beta1.GatewayList{
		Items: []gatewayapiv1beta1.Gateway{
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "gw-ns",
					Name:        "gw-1",
					Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "gw-ns",
					Name:        "gw-2",
					Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"}]`},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gw-ns",
					Name:      "gw-3",
				},
			},
		},
	}

	var gws []string
	policyRefConfig := &KuadrantRateLimitPolicyRefsConfig{}
	gwName := func(gw GatewayWrapper) string { return gw.Gateway.Name }

	gws = Map(GatewaysMissingPolicyRef(gwList, k8stypes.NamespacedName{Namespace: "app-ns", Name: "policy-1"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-2"},
		{Namespace: "gw-ns", Name: "gw-3"},
	}, policyRefConfig), gwName)

	if Contains(gws, "gw-1") {
		t.Error("gateway expected not to be listed as missing policy ref")
	}
	if Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as missing policy ref")
	}
	if !Contains(gws, "gw-3") {
		t.Error("gateway expected to be listed as missing policy ref")
	}

	gws = Map(GatewaysMissingPolicyRef(gwList, k8stypes.NamespacedName{Namespace: "app-ns", Name: "policy-2"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-1"},
	}, policyRefConfig), gwName)

	if Contains(gws, "gw-1") {
		t.Error("gateway expected not to be listed as missing policy ref")
	}
	if Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as missing policy ref")
	}
	if Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as missing policy ref")
	}

	gws = Map(GatewaysMissingPolicyRef(gwList, k8stypes.NamespacedName{Namespace: "app-ns", Name: "policy-3"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-1"},
		{Namespace: "gw-ns", Name: "gw-3"},
	}, policyRefConfig), gwName)

	if !Contains(gws, "gw-1") {
		t.Error("gateway expected to be listed as missing policy ref")
	}
	if Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as missing policy ref")
	}
	if !Contains(gws, "gw-3") {
		t.Error("gateway expected to be listed as missing policy ref")
	}
}

func TestGatewaysWithValidPolicyRef(t *testing.T) {
	gwList := &gatewayapiv1beta1.GatewayList{
		Items: []gatewayapiv1beta1.Gateway{
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "gw-ns",
					Name:        "gw-1",
					Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "gw-ns",
					Name:        "gw-2",
					Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"}]`},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gw-ns",
					Name:      "gw-3",
				},
			},
		},
	}

	var gws []string
	policyRefConfig := &KuadrantRateLimitPolicyRefsConfig{}
	gwName := func(gw GatewayWrapper) string { return gw.Gateway.Name }

	gws = Map(GatewaysWithValidPolicyRef(gwList, k8stypes.NamespacedName{Namespace: "app-ns", Name: "policy-1"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-2"},
		{Namespace: "gw-ns", Name: "gw-3"},
	}, policyRefConfig), gwName)

	if Contains(gws, "gw-1") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}
	if !Contains(gws, "gw-2") {
		t.Error("gateway expected to be listed as with valid policy ref")
	}
	if Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}

	gws = Map(GatewaysWithValidPolicyRef(gwList, k8stypes.NamespacedName{Namespace: "app-ns", Name: "policy-2"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-1"},
	}, policyRefConfig), gwName)

	if !Contains(gws, "gw-1") {
		t.Error("gateway expected to be listed as with valid policy ref")
	}
	if Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}
	if Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}

	gws = Map(GatewaysWithValidPolicyRef(gwList, k8stypes.NamespacedName{Namespace: "app-ns", Name: "policy-3"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-1"},
		{Namespace: "gw-ns", Name: "gw-3"},
	}, policyRefConfig), gwName)

	if Contains(gws, "gw-1") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}
	if Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}
	if Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}
}

func TestGatewaysWithInvalidPolicyRef(t *testing.T) {
	gwList := &gatewayapiv1beta1.GatewayList{
		Items: []gatewayapiv1beta1.Gateway{
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "gw-ns",
					Name:        "gw-1",
					Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "gw-ns",
					Name:        "gw-2",
					Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"}]`},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gw-ns",
					Name:      "gw-3",
				},
			},
		},
	}

	var gws []string
	policyRefConfig := &KuadrantRateLimitPolicyRefsConfig{}
	gwName := func(gw GatewayWrapper) string { return gw.Gateway.Name }

	gws = Map(GatewaysWithInvalidPolicyRef(gwList, k8stypes.NamespacedName{Namespace: "app-ns", Name: "policy-1"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-2"},
		{Namespace: "gw-ns", Name: "gw-3"},
	}, policyRefConfig), gwName)

	if !Contains(gws, "gw-1") {
		t.Error("gateway expected to be listed as with invalid policy ref")
	}
	if Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}
	if Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}

	gws = Map(GatewaysWithInvalidPolicyRef(gwList, k8stypes.NamespacedName{Namespace: "app-ns", Name: "policy-2"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-1"},
	}, policyRefConfig), gwName)

	if Contains(gws, "gw-1") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}
	if Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}
	if Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}

	gws = Map(GatewaysWithInvalidPolicyRef(gwList, k8stypes.NamespacedName{Namespace: "app-ns", Name: "policy-3"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-1"},
		{Namespace: "gw-ns", Name: "gw-3"},
	}, policyRefConfig), gwName)

	if Contains(gws, "gw-1") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}
	if Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}
	if Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}
}

func TestGatewayWrapperKey(t *testing.T) {
	gw := GatewayWrapper{
		Gateway: &gatewayapiv1beta1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "gw-ns",
				Name:        "gw-1",
				Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
			},
		},
		PolicyRefsConfig: &KuadrantRateLimitPolicyRefsConfig{},
	}
	if gw.Key().Namespace != "gw-ns" || gw.Key().Name != "gw-1" {
		t.Fail()
	}
}

func TestGatewayWrapperPolicyRefs(t *testing.T) {
	gw := GatewayWrapper{
		Gateway: &gatewayapiv1beta1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "gw-ns",
				Name:        "gw-1",
				Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
			},
		},
		PolicyRefsConfig: &KuadrantRateLimitPolicyRefsConfig{},
	}
	refs := Map(gw.PolicyRefs(), func(ref k8stypes.NamespacedName) string { return ref.String() })
	if !Contains(refs, "app-ns/policy-1") {
		t.Error("GatewayWrapper.PolicyRefs() should contain app-ns/policy-1")
	}
	if !Contains(refs, "app-ns/policy-2") {
		t.Error("GatewayWrapper.PolicyRefs() should contain app-ns/policy-2")
	}
	if len(refs) != 2 {
		t.Fail()
	}
}

func TestGatewayWrapperContainsPolicy(t *testing.T) {
	gw := GatewayWrapper{
		Gateway: &gatewayapiv1beta1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "gw-ns",
				Name:        "gw-1",
				Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
			},
		},
		PolicyRefsConfig: &KuadrantRateLimitPolicyRefsConfig{},
	}
	if !gw.ContainsPolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-1"}) {
		t.Error("GatewayWrapper.ContainsPolicy() should contain app-ns/policy-1")
	}
	if !gw.ContainsPolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-2"}) {
		t.Error("GatewayWrapper.ContainsPolicy() should contain app-ns/policy-1")
	}
	if gw.ContainsPolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-3"}) {
		t.Error("GatewayWrapper.ContainsPolicy() should not contain app-ns/policy-1")
	}
}

func TestGatewayWrapperAddPolicy(t *testing.T) {
	gateway := gatewayapiv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "gw-ns",
			Name:        "gw-1",
			Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
		},
	}
	gw := GatewayWrapper{
		Gateway:          &gateway,
		PolicyRefsConfig: &KuadrantRateLimitPolicyRefsConfig{},
	}
	if gw.AddPolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-1"}) {
		t.Error("GatewayWrapper.AddPolicy() expected to return false")
	}
	if !gw.AddPolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-3"}) {
		t.Error("GatewayWrapper.AddPolicy() expected to return true")
	}
	if gw.Annotations["kuadrant.io/ratelimitpolicies"] != `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"},{"Namespace":"app-ns","Name":"policy-3"}]` {
		t.Error("GatewayWrapper.AddPolicy() expected to have added policy ref to the annotations")
	}
}

func TestGatewayDeletePolicy(t *testing.T) {
	gateway := gatewayapiv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "gw-ns",
			Name:        "gw-1",
			Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
		},
	}
	gw := GatewayWrapper{
		Gateway:          &gateway,
		PolicyRefsConfig: &KuadrantRateLimitPolicyRefsConfig{},
	}
	if !gw.DeletePolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-1"}) {
		t.Error("GatewayWrapper.DeletePolicy() expected to return true")
	}
	if gw.DeletePolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-3"}) {
		t.Error("GatewayWrapper.DeletePolicy() expected to return false")
	}
	if gw.Annotations["kuadrant.io/ratelimitpolicies"] != `[{"Namespace":"app-ns","Name":"policy-2"}]` {
		t.Error("GatewayWrapper.DeletePolicy() expected to have deleted policy ref from the annotations")
	}
}

func TestGatewayHostnames(t *testing.T) {
	hostname := gatewayapiv1beta1.Hostname("toystore.com")
	gateway := gatewayapiv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "gw-ns",
			Name:        "gw-1",
			Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
		},
		Spec: gatewayapiv1beta1.GatewaySpec{
			Listeners: []gatewayapiv1beta1.Listener{
				{
					Name:     "my-listener",
					Hostname: &hostname,
				},
			},
		},
	}
	gw := GatewayWrapper{
		Gateway:          &gateway,
		PolicyRefsConfig: &KuadrantRateLimitPolicyRefsConfig{},
	}
	hostnames := gw.Hostnames()
	if !Contains(hostnames, "toystore.com") {
		t.Error("GatewayWrapper.Hostnames() expected to contain 'toystore.com'")
	}
	if len(hostnames) != 1 {
		t.Fail()
	}
}

func TestGatewayWrapperPolicyRefsAnnotation(t *testing.T) {
	gw := GatewayWrapper{
		Gateway: &gatewayapiv1beta1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "gw-ns",
				Name:        "gw-1",
				Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
			},
		},
		PolicyRefsConfig: &KuadrantRateLimitPolicyRefsConfig{},
	}
	if gw.PolicyRefsAnnotation() != RateLimitPoliciesBackRefAnnotation {
		t.Fail()
	}
}

func TestGetGatewayWorkloadSelector(t *testing.T) {
	hostnameAddress := gatewayapiv1beta1.AddressType("Hostname")
	gateway := &gatewayapiv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-gw",
			Labels: map[string]string{
				"app":           "foo",
				"control-plane": "kuadrant",
			},
		},
		Status: gatewayapiv1beta1.GatewayStatus{
			Addresses: []gatewayapiv1beta1.GatewayAddress{
				{
					Type:  &hostnameAddress,
					Value: "my-gw-svc.my-ns.svc.cluster.local:80",
				},
			},
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-gw-svc",
			Labels: map[string]string{
				"a-label": "irrelevant",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"a-selector": "what-we-are-looking-for",
			},
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gatewayapiv1beta1.AddToScheme(scheme)
	k8sClient := fake.NewFakeClientWithScheme(scheme, gateway, service)

	var selector map[string]string
	var err error

	selector, err = GetGatewayWorkloadSelector(context.TODO(), k8sClient, gateway)
	if err != nil || len(selector) != 1 || selector["a-selector"] != "what-we-are-looking-for" {
		t.Error("should not have failed to get the gateway workload selector")
	}
}

func TestGetGatewayWorkloadSelectorWithoutHostnameAddress(t *testing.T) {
	gateway := &gatewayapiv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-gw",
			Labels: map[string]string{
				"app":           "foo",
				"control-plane": "kuadrant",
			},
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-gw-svc",
			Labels: map[string]string{
				"a-label": "irrelevant",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"a-selector": "what-we-are-looking-for",
			},
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gatewayapiv1beta1.AddToScheme(scheme)
	k8sClient := fake.NewFakeClientWithScheme(scheme, gateway, service)

	var selector map[string]string
	var err error

	selector, err = GetGatewayWorkloadSelector(context.TODO(), k8sClient, gateway)
	if err == nil || err.Error() != "cannot find service Hostname in the Gateway status" || selector != nil {
		t.Error("should have failed to get the gateway workload selector")
	}
}

type FakePolicy struct {
	client.Object
	Hosts []string
}

func (p *FakePolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return gatewayapiv1alpha2.PolicyTargetReference{}
}

func (p *FakePolicy) GetWrappedNamespace() gatewayapiv1beta1.Namespace {
	return gatewayapiv1beta1.Namespace("")
}

func (p *FakePolicy) GetRulesHostnames() []string {
	return p.Hosts
}

func TestValidateHierarchicalRules(t *testing.T) {
	hostname := gatewayapiv1beta1.Hostname("*.example.com")
	gateway := &gatewayapiv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "cool-namespace",
			Name:      "cool-gateway",
		},
		Spec: gatewayapiv1beta1.GatewaySpec{Listeners: []gatewayapiv1beta1.Listener{
			{
				Hostname: &hostname,
			},
		}},
	}
	policy1 := FakePolicy{Hosts: []string{"this.example.com", "*.example.com"}}
	policy2 := FakePolicy{Hosts: []string{"*.z.com"}}

	if err := ValidateHierarchicalRules(&policy1, gateway); err != nil {
		t.Fatal(err)
	}

	expectedError := fmt.Errorf(
		"rule host (%s) does not follow any hierarchical constraints, "+
			"for the %T to be validated, it must match with at least one of the target network hostnames %+q",
		"*.z.com",
		&policy2,
		[]string{"*.example.com"},
	)

	if err := ValidateHierarchicalRules(&policy2, gateway); err.Error() != expectedError.Error() {
		t.Fatal("the error message does not match the expected error one")
		fmt.Println(expectedError.Error())
		fmt.Println(err.Error())
	}

}

func TestIsHTTPRouteAccepted(t *testing.T) {
	testCases := []struct {
		name     string
		route    *gatewayapiv1beta1.HTTPRoute
		expected bool
	}{
		{
			"nil",
			nil,
			false,
		},
		{
			"empty parent refs",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{},
			},
			false,
		},
		{
			"single parent accepted",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1beta1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1beta1.ParentReference{
							{
								Name: "a",
							},
						},
					},
				},
				Status: gatewayapiv1beta1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1beta1.RouteStatus{
						Parents: []gatewayapiv1beta1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1beta1.ParentReference{
									Name: "a",
								},
								Conditions: []metav1.Condition{
									{
										Type:   "Accepted",
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			true,
		},
		{
			"single parent not accepted",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1beta1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1beta1.ParentReference{
							{
								Name: "a",
							},
						},
					},
				},
				Status: gatewayapiv1beta1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1beta1.RouteStatus{
						Parents: []gatewayapiv1beta1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1beta1.ParentReference{
									Name: "a",
								},
								Conditions: []metav1.Condition{
									{
										Type:   "Accepted",
										Status: metav1.ConditionFalse,
									},
								},
							},
						},
					},
				},
			},
			false,
		},
		{
			"wrong parent is accepted",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1beta1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1beta1.ParentReference{
							{
								Name: "a",
							},
						},
					},
				},
				Status: gatewayapiv1beta1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1beta1.RouteStatus{
						Parents: []gatewayapiv1beta1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1beta1.ParentReference{
									Name: "b",
								},
								Conditions: []metav1.Condition{
									{
										Type:   "Accepted",
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			false,
		},
		{
			"multiple parents only one is accepted",
			&gatewayapiv1beta1.HTTPRoute{
				Spec: gatewayapiv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1beta1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1beta1.ParentReference{
							{
								Name: "a",
							},
							{
								Name: "b",
							},
						},
					},
				},
				Status: gatewayapiv1beta1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1beta1.RouteStatus{
						Parents: []gatewayapiv1beta1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1beta1.ParentReference{
									Name: "a",
								},
								Conditions: []metav1.Condition{
									{
										Type:   "Accepted",
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								ParentRef: gatewayapiv1beta1.ParentReference{
									Name: "b",
								},
								Conditions: []metav1.Condition{
									{
										Type:   "Accepted",
										Status: metav1.ConditionFalse,
									},
								},
							},
						},
					},
				},
			},
			false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res := IsHTTPRouteAccepted(tc.route)
			if res != tc.expected {
				subT.Errorf("result (%t) does not match expected (%t)", res, tc.expected)
			}
		})
	}
}
