//go:build unit

package gatewayapi

import (
	"testing"

	"gotest.tools/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestIsHTTPRouteAccepted(t *testing.T) {
	testCases := []struct {
		name     string
		route    *gatewayapiv1.HTTPRoute
		expected bool
	}{
		{
			"nil",
			nil,
			false,
		},
		{
			"empty parent refs",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{},
			},
			false,
		},
		{
			"single parent accepted",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{
							{
								Name: "a",
							},
						},
					},
				},
				Status: gatewayapiv1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1.RouteStatus{
						Parents: []gatewayapiv1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1.ParentReference{
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
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{
							{
								Name: "a",
							},
						},
					},
				},
				Status: gatewayapiv1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1.RouteStatus{
						Parents: []gatewayapiv1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1.ParentReference{
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
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{
							{
								Name: "a",
							},
						},
					},
				},
				Status: gatewayapiv1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1.RouteStatus{
						Parents: []gatewayapiv1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1.ParentReference{
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
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{
							{
								Name: "a",
							},
							{
								Name: "b",
							},
						},
					},
				},
				Status: gatewayapiv1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1.RouteStatus{
						Parents: []gatewayapiv1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1.ParentReference{
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
								ParentRef: gatewayapiv1.ParentReference{
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

type isPolicyAcceptedTestCase struct {
	name     string
	policy   Policy
	expected bool
}

var isPolicyAcceptedTestCases = []isPolicyAcceptedTestCase{
	{
		name:     "no status",
		policy:   &TestPolicy{},
		expected: false,
	},
	{
		name: "no policy accepted condition",
		policy: &TestPolicy{
			Status: FakePolicyStatus{
				Conditions: []metav1.Condition{
					{
						Type:   "Other",
						Status: metav1.ConditionTrue,
					},
				},
			},
		},
		expected: false,
	},
	{
		name: "truthy accepted condition",
		policy: &TestPolicy{
			Status: FakePolicyStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(gatewayapiv1alpha2.PolicyConditionAccepted),
						Status: metav1.ConditionTrue,
					},
				},
			},
		},
		expected: true,
	},
	{
		name: "falsey accepted condition",
		policy: &TestPolicy{
			Status: FakePolicyStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(gatewayapiv1alpha2.PolicyConditionAccepted),
						Status: metav1.ConditionFalse,
					},
				},
			},
		},
		expected: false,
	},
}

func TestIsPolicyAccepted(t *testing.T) {
	testCases := isPolicyAcceptedTestCases
	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res := IsPolicyAccepted(tc.policy)
			if res != tc.expected {
				subT.Errorf("result (%t) does not match expected (%t)", res, tc.expected)
			}
		})
	}
}

func TestGetRouteAcceptedParentRefs(t *testing.T) {
	testCases := []struct {
		name     string
		route    *gatewayapiv1.HTTPRoute
		expected []gatewayapiv1.ParentReference
	}{
		{
			"nil",
			nil,
			nil,
		},
		{
			"empty parent refs",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{},
			},
			[]gatewayapiv1.ParentReference{},
		},
		{
			"single parentref accepted",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{
							{
								Name: "a",
							},
						},
					},
				},
				Status: gatewayapiv1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1.RouteStatus{
						Parents: []gatewayapiv1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1.ParentReference{
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
			[]gatewayapiv1.ParentReference{
				{
					Name: "a",
				},
			},
		},
		{
			"single parent ref not accepted",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{
							{
								Name: "a",
							},
						},
					},
				},
				Status: gatewayapiv1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1.RouteStatus{
						Parents: []gatewayapiv1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1.ParentReference{
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
			[]gatewayapiv1.ParentReference{},
		},
		{
			"wrong parent is accepted",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{
							{
								Name: "a",
							},
						},
					},
				},
				Status: gatewayapiv1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1.RouteStatus{
						Parents: []gatewayapiv1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1.ParentReference{
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
			[]gatewayapiv1.ParentReference{},
		},
		{
			"multiple parents only one is accepted",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{
							{
								Name: "a",
							},
							{
								Name: "b",
							},
						},
					},
				},
				Status: gatewayapiv1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1.RouteStatus{
						Parents: []gatewayapiv1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1.ParentReference{
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
								ParentRef: gatewayapiv1.ParentReference{
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
			[]gatewayapiv1.ParentReference{
				{
					Name: "a",
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res := GetRouteAcceptedParentRefs(tc.route)
			assert.DeepEqual(subT, res, tc.expected)
		})
	}
}
