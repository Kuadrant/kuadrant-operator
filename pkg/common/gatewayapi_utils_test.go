//go:build unit

package common

import (
	"reflect"
	"testing"

	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestRouteHostnames(t *testing.T) {
	testCases := []struct {
		name     string
		route    *gatewayapiv1alpha2.HTTPRoute
		expected []string
	}{
		{
			"nil",
			nil,
			nil,
		},
		{
			"nil hostname",
			&gatewayapiv1alpha2.HTTPRoute{
				Spec: gatewayapiv1alpha2.HTTPRouteSpec{
					Hostnames: nil,
				},
			},
			[]string{"*"},
		},
		{
			"basic",
			&gatewayapiv1alpha2.HTTPRoute{
				Spec: gatewayapiv1alpha2.HTTPRouteSpec{
					Hostnames: []gatewayapiv1alpha2.Hostname{"*.com", "example.net", "test.example.net"},
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
		getMethod                                          = "GET"
		catsPath                                           = "/cats"
		dogsPath                                           = "/dogs"
		rabbitsPath                                        = "/rabbits"
		getHTTPMethod        gatewayapiv1alpha2.HTTPMethod = "GET"
		postHTTPMethod       gatewayapiv1alpha2.HTTPMethod = "POST"
		pathPrefix                                         = gatewayapiv1alpha2.PathMatchPathPrefix
		pathExact                                          = gatewayapiv1alpha2.PathMatchExact
		catsPrefixPatchMatch                               = gatewayapiv1alpha2.HTTPPathMatch{
			Type:  &pathPrefix,
			Value: &catsPath,
		}
		dogsExactPatchMatch = gatewayapiv1alpha2.HTTPPathMatch{
			Type:  &pathExact,
			Value: &dogsPath,
		}
		rabbitsPrefixPatchMatch = gatewayapiv1alpha2.HTTPPathMatch{
			Value: &rabbitsPath,
		}
	)

	testCases := []struct {
		name     string
		route    *gatewayapiv1alpha2.HTTPRoute
		expected []HTTPRouteRule
	}{
		{
			"nil",
			nil,
			nil,
		},
		{
			"nil rules",
			&gatewayapiv1alpha2.HTTPRoute{
				Spec: gatewayapiv1alpha2.HTTPRouteSpec{
					Rules:     nil,
					Hostnames: []gatewayapiv1alpha2.Hostname{"*.com"},
				},
			},
			[]HTTPRouteRule{{Hosts: []string{"*.com"}}},
		},
		{
			"empty rules",
			&gatewayapiv1alpha2.HTTPRoute{
				Spec: gatewayapiv1alpha2.HTTPRouteSpec{
					Rules:     make([]gatewayapiv1alpha2.HTTPRouteRule, 0),
					Hostnames: []gatewayapiv1alpha2.Hostname{"*.com"},
				},
			},
			[]HTTPRouteRule{{Hosts: []string{"*.com"}}},
		},
		{
			"with method",
			&gatewayapiv1alpha2.HTTPRoute{
				Spec: gatewayapiv1alpha2.HTTPRouteSpec{
					Rules: []gatewayapiv1alpha2.HTTPRouteRule{
						{
							Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
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
			&gatewayapiv1alpha2.HTTPRoute{
				Spec: gatewayapiv1alpha2.HTTPRouteSpec{
					Rules: []gatewayapiv1alpha2.HTTPRouteRule{
						{
							Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
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
			&gatewayapiv1alpha2.HTTPRoute{
				Spec: gatewayapiv1alpha2.HTTPRouteSpec{
					Rules: []gatewayapiv1alpha2.HTTPRouteRule{
						{
							Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
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
			&gatewayapiv1alpha2.HTTPRoute{
				Spec: gatewayapiv1alpha2.HTTPRouteSpec{
					Rules: []gatewayapiv1alpha2.HTTPRouteRule{
						{
							Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
								{
									Headers: []gatewayapiv1alpha2.HTTPHeaderMatch{
										{
											Name:  "someheader",
											Value: "somevalue",
										},
									},
								},
							},
						},
					},
					Hostnames: []gatewayapiv1alpha2.Hostname{"*.com"},
				},
			},
			[]HTTPRouteRule{{Hosts: []string{"*.com"}}},
		},
		{
			"basic",
			&gatewayapiv1alpha2.HTTPRoute{
				Spec: gatewayapiv1alpha2.HTTPRouteSpec{
					Hostnames: []gatewayapiv1alpha2.Hostname{"*.com"},
					Rules: []gatewayapiv1alpha2.HTTPRouteRule{
						{
							// GET /cats*
							// POST /dogs
							Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
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
