//go:build unit

package gatewayapi

import (
	"context"
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

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func TestGetGatewayWorkloadSelector(t *testing.T) {
	hostnameAddress := gatewayapiv1.AddressType("Hostname")
	gateway := &gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-gw",
			Labels: map[string]string{
				"app":           "foo",
				"control-plane": "kuadrant",
			},
		},
		Status: gatewayapiv1.GatewayStatus{
			Addresses: []gatewayapiv1.GatewayStatusAddress{
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
	_ = gatewayapiv1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(gateway, service).Build()

	var selector map[string]string
	var err error

	selector, err = GetGatewayWorkloadSelector(context.TODO(), k8sClient, gateway)
	if err != nil || len(selector) != 1 || selector["a-selector"] != "what-we-are-looking-for" {
		t.Error("should not have failed to get the gateway workload selector")
	}
}

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

func TestIsNotPolicyAccepted(t *testing.T) {
	testCases := utils.Map(isPolicyAcceptedTestCases, func(tc isPolicyAcceptedTestCase) isPolicyAcceptedTestCase {
		tc.expected = !tc.expected
		return tc
	})
	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res := IsNotPolicyAccepted(tc.policy)
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

func TestGetRouteAcceptedGatewayParentKeys(t *testing.T) {
	testCases := []struct {
		name     string
		route    *gatewayapiv1.HTTPRoute
		expected []client.ObjectKey
	}{
		{
			"nil",
			nil,
			[]client.ObjectKey{},
		},
		{
			"empty parent refs",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{},
			},
			[]client.ObjectKey{},
		},
		{
			"single gateway parentref accepted",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{
							{
								Kind:  ptr.To(gatewayapiv1.Kind("Gateway")),
								Group: ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
								Name:  "a",
							},
						},
					},
				},
				Status: gatewayapiv1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1.RouteStatus{
						Parents: []gatewayapiv1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1.ParentReference{
									Kind:  ptr.To(gatewayapiv1.Kind("Gateway")),
									Group: ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
									Name:  "a",
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
			[]client.ObjectKey{
				{
					Name: "a",
				},
			},
		},
		{
			"single not gateway parent ref accepted",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{
							{
								Kind:  ptr.To(gatewayapiv1.Kind("Other")),
								Group: ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
								Name:  "a",
							},
						},
					},
				},
				Status: gatewayapiv1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1.RouteStatus{
						Parents: []gatewayapiv1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1.ParentReference{
									Kind:  ptr.To(gatewayapiv1.Kind("Other")),
									Group: ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
									Name:  "a",
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
			[]client.ObjectKey{},
		},
		{
			"multiple parents only gateway ones are accepted",
			&gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{
							{
								Kind:  ptr.To(gatewayapiv1.Kind("Gateway")),
								Group: ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
								Name:  "a",
							},
							{
								Kind:  ptr.To(gatewayapiv1.Kind("Other")),
								Group: ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
								Name:  "b",
							},
						},
					},
				},
				Status: gatewayapiv1.HTTPRouteStatus{
					RouteStatus: gatewayapiv1.RouteStatus{
						Parents: []gatewayapiv1.RouteParentStatus{
							{
								ParentRef: gatewayapiv1.ParentReference{
									Kind:  ptr.To(gatewayapiv1.Kind("Gateway")),
									Group: ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
									Name:  "a",
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
									Kind:  ptr.To(gatewayapiv1.Kind("Other")),
									Group: ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
									Name:  "b",
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
			[]client.ObjectKey{
				{
					Name: "a",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res := GetRouteAcceptedGatewayParentKeys(tc.route)
			assert.DeepEqual(subT, res, tc.expected)
		})
	}
}

func TestFilterValidSubdomains(t *testing.T) {
	testCases := []struct {
		name       string
		domains    []gatewayapiv1.Hostname
		subdomains []gatewayapiv1.Hostname
		expected   []gatewayapiv1.Hostname
	}{
		{
			name:       "when all subdomains are valid",
			domains:    []gatewayapiv1.Hostname{"my-app.apps.io", "*.acme.com"},
			subdomains: []gatewayapiv1.Hostname{"toystore.acme.com", "my-app.apps.io", "carstore.acme.com"},
			expected:   []gatewayapiv1.Hostname{"toystore.acme.com", "my-app.apps.io", "carstore.acme.com"},
		},
		{
			name:       "when some subdomains are valid and some are not",
			domains:    []gatewayapiv1.Hostname{"my-app.apps.io", "*.acme.com"},
			subdomains: []gatewayapiv1.Hostname{"toystore.acme.com", "my-app.apps.io", "other-app.apps.io"},
			expected:   []gatewayapiv1.Hostname{"toystore.acme.com", "my-app.apps.io"},
		},
		{
			name:       "when none of subdomains are valid",
			domains:    []gatewayapiv1.Hostname{"my-app.apps.io", "*.acme.com"},
			subdomains: []gatewayapiv1.Hostname{"other-app.apps.io"},
			expected:   []gatewayapiv1.Hostname{},
		},
		{
			name:       "when the set of super domains is empty",
			domains:    []gatewayapiv1.Hostname{},
			subdomains: []gatewayapiv1.Hostname{"toystore.acme.com"},
			expected:   []gatewayapiv1.Hostname{},
		},
		{
			name:       "when the set of subdomains is empty",
			domains:    []gatewayapiv1.Hostname{"my-app.apps.io", "*.acme.com"},
			subdomains: []gatewayapiv1.Hostname{},
			expected:   []gatewayapiv1.Hostname{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if r := FilterValidSubdomains(tc.domains, tc.subdomains); !reflect.DeepEqual(r, tc.expected) {
				t.Errorf("expected=%v; got=%v", tc.expected, r)
			}
		})
	}
}

func TestGetGatewayWorkloadSelectorWithoutHostnameAddress(t *testing.T) {
	gateway := &gatewayapiv1.Gateway{
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
	_ = gatewayapiv1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(gateway, service).Build()

	var selector map[string]string
	var err error

	selector, err = GetGatewayWorkloadSelector(context.TODO(), k8sClient, gateway)
	if err == nil || err.Error() != "cannot find service Hostname in the Gateway status" || selector != nil {
		t.Error("should have failed to get the gateway workload selector")
	}
}
