//go:build unit

package kuadrant

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestRulesFromHTTPRoute(t *testing.T) {
	var (
		getMethod                                    = "GET"
		catsPath                                     = "/cats"
		dogsPath                                     = "/dogs"
		rabbitsPath                                  = "/rabbits"
		getHTTPMethod        gatewayapiv1.HTTPMethod = "GET"
		postHTTPMethod       gatewayapiv1.HTTPMethod = "POST"
		pathPrefix                                   = gatewayapiv1.PathMatchPathPrefix
		pathExact                                    = gatewayapiv1.PathMatchExact
		catsPrefixPatchMatch                         = gatewayapiv1.HTTPPathMatch{
			Type:  &pathPrefix,
			Value: &catsPath,
		}
		dogsExactPatchMatch = gatewayapiv1.HTTPPathMatch{
			Type:  &pathExact,
			Value: &dogsPath,
		}
		rabbitsPrefixPatchMatch = gatewayapiv1.HTTPPathMatch{
			Value: &rabbitsPath,
		}
	)

	testCases := []struct {
		name     string
		route    *gatewayapiv1.HTTPRoute
		expected []HTTPRouteRule
	}{
		{
			"nil",
			nil,
			nil,
		},
		{
			"nil rules",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					Rules:     nil,
					Hostnames: []gatewayapiv1.Hostname{"*.com"},
				},
			},
			[]HTTPRouteRule{{Hosts: []string{"*.com"}}},
		},
		{
			"empty rules",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					Rules:     make([]gatewayapiv1.HTTPRouteRule, 0),
					Hostnames: []gatewayapiv1.Hostname{"*.com"},
				},
			},
			[]HTTPRouteRule{{Hosts: []string{"*.com"}}},
		},
		{
			"with method",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					Rules: []gatewayapiv1.HTTPRouteRule{
						{
							Matches: []gatewayapiv1.HTTPRouteMatch{
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
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					Rules: []gatewayapiv1.HTTPRouteRule{
						{
							Matches: []gatewayapiv1.HTTPRouteMatch{
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
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					Rules: []gatewayapiv1.HTTPRouteRule{
						{
							Matches: []gatewayapiv1.HTTPRouteMatch{
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
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					Rules: []gatewayapiv1.HTTPRouteRule{
						{
							Matches: []gatewayapiv1.HTTPRouteMatch{
								{
									Headers: []gatewayapiv1.HTTPHeaderMatch{
										{
											Name:  "someheader",
											Value: "somevalue",
										},
									},
								},
							},
						},
					},
					Hostnames: []gatewayapiv1.Hostname{"*.com"},
				},
			},
			[]HTTPRouteRule{{Hosts: []string{"*.com"}}},
		},
		{
			"basic",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					Hostnames: []gatewayapiv1.Hostname{"*.com"},
					Rules: []gatewayapiv1.HTTPRouteRule{
						{
							// GET /cats*
							// POST /dogs
							Matches: []gatewayapiv1.HTTPRouteMatch{
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

func TestRouteHostnames(t *testing.T) {
	testCases := []struct {
		name     string
		route    *gatewayapiv1.HTTPRoute
		expected []string
	}{
		{
			"nil",
			nil,
			nil,
		},
		{
			"nil hostname",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					Hostnames: nil,
				},
			},
			[]string{"*"},
		},
		{
			"basic",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					Hostnames: []gatewayapiv1.Hostname{"*.com", "example.net", "test.example.net"},
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

func TestHTTPRouteRuleSelectorSelects(t *testing.T) {
	testCases := []struct {
		name     string
		selector HTTPRouteRuleSelector
		rule     gatewayapiv1.HTTPRouteRule
		expected bool
	}{
		{
			name: "when the httproutrule contains the exact match then return true",
			selector: HTTPRouteRuleSelector{
				HTTPRouteMatch: &gatewayapiv1.HTTPRouteMatch{
					Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodGet}[0],
					Headers: []gatewayapiv1.HTTPHeaderMatch{
						{
							Type:  &[]gatewayapiv1.HeaderMatchType{gatewayapiv1.HeaderMatchExact}[0],
							Name:  "someheader",
							Value: "somevalue",
						},
					},
				},
			},
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodGet}[0],
						Headers: []gatewayapiv1.HTTPHeaderMatch{
							{
								Type:  &[]gatewayapiv1.HeaderMatchType{gatewayapiv1.HeaderMatchExact}[0],
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
				HTTPRouteMatch: &gatewayapiv1.HTTPRouteMatch{
					Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodGet}[0],
				},
			},
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodGet}[0],
						Headers: []gatewayapiv1.HTTPHeaderMatch{
							{
								Type:  &[]gatewayapiv1.HeaderMatchType{gatewayapiv1.HeaderMatchExact}[0],
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
				HTTPRouteMatch: &gatewayapiv1.HTTPRouteMatch{
					Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodGet}[0],
					Headers: []gatewayapiv1.HTTPHeaderMatch{
						{
							Type:  &[]gatewayapiv1.HeaderMatchType{gatewayapiv1.HeaderMatchExact}[0],
							Name:  "someheader",
							Value: "somevalue",
						},
					},
				},
			},
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodGet}[0],
						Headers: []gatewayapiv1.HTTPHeaderMatch{
							{
								Type:  &[]gatewayapiv1.HeaderMatchType{gatewayapiv1.HeaderMatchExact}[0],
								Name:  "someheader",
								Value: "somevalue",
							},
							{
								Type:  &[]gatewayapiv1.HeaderMatchType{gatewayapiv1.HeaderMatchRegularExpression}[0],
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
				HTTPRouteMatch: &gatewayapiv1.HTTPRouteMatch{
					Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodGet}[0],
					Headers: []gatewayapiv1.HTTPHeaderMatch{
						{
							Type:  &[]gatewayapiv1.HeaderMatchType{gatewayapiv1.HeaderMatchExact}[0],
							Name:  "someheader",
							Value: "somevalue",
						},
					},
				},
			},
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodPost}[0],
						Headers: []gatewayapiv1.HTTPHeaderMatch{
							{
								Type:  &[]gatewayapiv1.HeaderMatchType{gatewayapiv1.HeaderMatchExact}[0],
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
			rule: gatewayapiv1.HTTPRouteRule{},
			selector: HTTPRouteRuleSelector{
				HTTPRouteMatch: &gatewayapiv1.HTTPRouteMatch{
					Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodGet}[0],
				},
			},
			expected: false,
		},
		{
			name:     "when the selector is empty then return true",
			selector: HTTPRouteRuleSelector{},
			rule: gatewayapiv1.HTTPRouteRule{
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodGet}[0],
						Headers: []gatewayapiv1.HTTPHeaderMatch{
							{
								Type:  &[]gatewayapiv1.HeaderMatchType{gatewayapiv1.HeaderMatchExact}[0],
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
		input    *gatewayapiv1.HTTPPathMatch
		expected string
	}{
		{
			name: "exact path match",
			input: &[]gatewayapiv1.HTTPPathMatch{
				{
					Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchExact}[0],
					Value: &[]string{"/foo"}[0],
				},
			}[0],
			expected: "/foo",
		},
		{
			name: "regex path match",
			input: &[]gatewayapiv1.HTTPPathMatch{
				{
					Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchRegularExpression}[0],
					Value: &[]string{"^\\/foo.*"}[0],
				},
			}[0],
			expected: "~/^\\/foo.*/",
		},
		{
			name: "path prefix match",
			input: &[]gatewayapiv1.HTTPPathMatch{
				{
					Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchPathPrefix}[0],
					Value: &[]string{"/foo"}[0],
				},
			}[0],
			expected: "/foo*",
		},
		{
			name: "path match with default type",
			input: &[]gatewayapiv1.HTTPPathMatch{
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
		input    gatewayapiv1.HTTPHeaderMatch
		expected string
	}{
		{
			name: "exact header match",
			input: gatewayapiv1.HTTPHeaderMatch{
				Type:  &[]gatewayapiv1.HeaderMatchType{gatewayapiv1.HeaderMatchExact}[0],
				Name:  "some-header",
				Value: "foo",
			},
			expected: "{some-header:foo}",
		},
		{
			name: "regex header match",
			input: gatewayapiv1.HTTPHeaderMatch{
				Type:  &[]gatewayapiv1.HeaderMatchType{gatewayapiv1.HeaderMatchRegularExpression}[0],
				Name:  "some-header",
				Value: "^foo.*",
			},
			expected: "{some-header:~/^foo.*/}",
		},
		{
			name: "header match with default type",
			input: gatewayapiv1.HTTPHeaderMatch{
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

func TestHTTPRouteMatchToString(t *testing.T) {
	match := gatewayapiv1.HTTPRouteMatch{
		Path: &gatewayapiv1.HTTPPathMatch{
			Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchExact}[0],
			Value: &[]string{"/foo"}[0],
		},
		Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodGet}[0],
		QueryParams: []gatewayapiv1.HTTPQueryParamMatch{
			{
				Type:  &[]gatewayapiv1.QueryParamMatchType{gatewayapiv1.QueryParamMatchRegularExpression}[0],
				Name:  "page",
				Value: "\\d+",
			},
		},
	}

	expected := "{method:GET,path:/foo,queryParams:[{page:~/\\d+/}]}"

	if r := HTTPRouteMatchToString(match); r != expected {
		t.Errorf("expected: %s, got: %s", expected, r)
	}

	match.Headers = []gatewayapiv1.HTTPHeaderMatch{
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
	rule := gatewayapiv1.HTTPRouteRule{}

	expected := "{matches:[]}"

	if r := HTTPRouteRuleToString(rule); r != expected {
		t.Errorf("expected: %s, got: %s", expected, r)
	}

	rule.Matches = []gatewayapiv1.HTTPRouteMatch{
		{
			Path: &gatewayapiv1.HTTPPathMatch{
				Type:  &[]gatewayapiv1.PathMatchType{gatewayapiv1.PathMatchExact}[0],
				Value: &[]string{"/foo"}[0],
			},
			Method: &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodGet}[0],
			QueryParams: []gatewayapiv1.HTTPQueryParamMatch{
				{
					Type:  &[]gatewayapiv1.QueryParamMatchType{gatewayapiv1.QueryParamMatchRegularExpression}[0],
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

func TestHTTPMethodToString(t *testing.T) {
	testCases := []struct {
		input    *gatewayapiv1.HTTPMethod
		expected string
	}{
		{
			input:    &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodGet}[0],
			expected: "GET",
		},
		{
			input:    &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodHead}[0],
			expected: "HEAD",
		},
		{
			input:    &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodPost}[0],
			expected: "POST",
		},
		{
			input:    &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodPut}[0],
			expected: "PUT",
		},
		{
			input:    &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodPatch}[0],
			expected: "PATCH",
		},
		{
			input:    &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodDelete}[0],
			expected: "DELETE",
		},
		{
			input:    &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodConnect}[0],
			expected: "CONNECT",
		},
		{
			input:    &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodOptions}[0],
			expected: "OPTIONS",
		},
		{
			input:    &[]gatewayapiv1.HTTPMethod{gatewayapiv1.HTTPMethodTrace}[0],
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

func TestHTTPQueryParamMatchToString(t *testing.T) {
	testCases := []struct {
		name     string
		input    gatewayapiv1.HTTPQueryParamMatch
		expected string
	}{
		{
			name: "exact query param match",
			input: gatewayapiv1.HTTPQueryParamMatch{
				Type:  &[]gatewayapiv1.QueryParamMatchType{gatewayapiv1.QueryParamMatchExact}[0],
				Name:  "some-param",
				Value: "foo",
			},
			expected: "{some-param:foo}",
		},
		{
			name: "regex query param match",
			input: gatewayapiv1.HTTPQueryParamMatch{
				Type:  &[]gatewayapiv1.QueryParamMatchType{gatewayapiv1.QueryParamMatchRegularExpression}[0],
				Name:  "some-param",
				Value: "^foo.*",
			},
			expected: "{some-param:~/^foo.*/}",
		},
		{
			name: "query param match with default type",
			input: gatewayapiv1.HTTPQueryParamMatch{
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

func TestGetKuadrantNamespaceFromPolicyTargetRef(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gatewayapiv1.AddToScheme(scheme)

	testCases := []struct {
		name        string
		k8sClient   client.Client
		policy      *FakePolicy
		expected    string
		expectedErr bool
	}{
		{
			"retrieve gateway namespace from httproute",
			fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
				&gatewayapiv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:   "my-ns",
						Name:        "my-gw",
						Annotations: map[string]string{"kuadrant.io/namespace": "my-ns"},
					},
				},
				&gatewayapiv1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "my-ns",
						Name:      "my-httproute",
					},
					Spec: gatewayapiv1.HTTPRouteSpec{
						CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
							ParentRefs: []gatewayapiv1.ParentReference{
								{
									Name:      "my-gw",
									Namespace: ptr.To[gatewayapiv1.Namespace](gatewayapiv1.Namespace("my-ns")),
								},
							},
						},
					},
				},
			).Build(),
			&FakePolicy{
				Object: &metav1.PartialObjectMetadata{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-policy",
						Namespace: "my-ns",
					},
				},
				targetRef: gatewayapiv1alpha2.NamespacedPolicyTargetReference{
					Group:     gatewayapiv1.GroupName,
					Kind:      "HTTPRoute",
					Name:      "my-httproute",
					Namespace: ptr.To[gatewayapiv1.Namespace](gatewayapiv1.Namespace("my-ns")),
				},
			},
			"my-ns",
			false,
		},
		{
			"retrieve gateway namespace from httproute implicitly",
			fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
				&gatewayapiv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:   "my-ns",
						Name:        "my-gw",
						Annotations: map[string]string{"kuadrant.io/namespace": "my-ns"},
					},
				},
				&gatewayapiv1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "my-ns",
						Name:      "my-httproute",
					},
					Spec: gatewayapiv1.HTTPRouteSpec{
						CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
							ParentRefs: []gatewayapiv1.ParentReference{
								{
									Name: "my-gw",
								},
							},
						},
					},
				},
			).Build(),
			&FakePolicy{
				Object: &metav1.PartialObjectMetadata{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-policy",
						Namespace: "my-ns",
					},
				},
				targetRef: gatewayapiv1alpha2.NamespacedPolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "HTTPRoute",
					Name:  "my-httproute",
				},
			},
			"my-ns",
			false,
		},
		{
			"error retrieving gateway namespace not annotated",
			fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
				&gatewayapiv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "my-ns",
						Name:      "my-gw",
					},
				},
				&gatewayapiv1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "my-ns",
						Name:      "my-httproute",
					},
					Spec: gatewayapiv1.HTTPRouteSpec{
						CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
							ParentRefs: []gatewayapiv1.ParentReference{
								{
									Name: "my-gw",
								},
							},
						},
					},
				},
			).Build(),
			&FakePolicy{
				Object: &metav1.PartialObjectMetadata{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-policy",
						Namespace: "my-ns",
					},
				},
				targetRef: gatewayapiv1alpha2.NamespacedPolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "HTTPRoute",
					Name:  "my-httproute",
				},
			},
			"",
			true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res, err := GetKuadrantNamespaceFromPolicyTargetRef(context.TODO(), tc.k8sClient, tc.policy)
			if err != nil && !tc.expectedErr {
				subT.Errorf("received err (%s) when expected error (%T)", err, tc.expectedErr)
			}
			if res != tc.expected {
				subT.Errorf("result (%s) does not match expected (%s)", res, tc.expected)
			}
		})
	}
}

func TestValidateHierarchicalRules(t *testing.T) {
	hostname := gatewayapiv1.Hostname("*.example.com")
	gateway := &gatewayapiv1.Gateway{
		Spec: gatewayapiv1.GatewaySpec{Listeners: []gatewayapiv1.Listener{
			{
				Hostname: &hostname,
			},
		}},
	}
	httpRoute := &gatewayapiv1.HTTPRoute{
		Spec: gatewayapiv1.HTTPRouteSpec{
			Hostnames: []gatewayapiv1.Hostname{hostname},
		},
	}

	policy1 := FakePolicy{Hosts: []string{"this.example.com", "*.example.com"}}
	policy2 := FakePolicy{Hosts: []string{"*.z.com"}}

	if err := ValidateHierarchicalRules(&policy1, gateway); err != nil {
		t.Fatal(err)
	}

	t.Run("gateway - contains host", func(subT *testing.T) {
		assert.NilError(subT, ValidateHierarchicalRules(&policy1, gateway))
	})

	t.Run("gateway error - host has no match", func(subT *testing.T) {
		expectedError := fmt.Sprintf("rule host (%s) does not follow any hierarchical constraints, "+
			"for the %T to be validated, it must match with at least one of the target network hostnames %+q",
			"*.z.com",
			&policy2,
			[]string{"*.example.com"},
		)
		assert.Error(subT, ValidateHierarchicalRules(&policy2, gateway), expectedError)
	})

	t.Run("gateway - no hosts", func(subT *testing.T) {
		assert.NilError(subT, ValidateHierarchicalRules(&policy1, &gatewayapiv1.Gateway{}))
	})

	t.Run("httpRoute - contains host ", func(subT *testing.T) {
		assert.NilError(subT, ValidateHierarchicalRules(&policy1, httpRoute))
	})
}
