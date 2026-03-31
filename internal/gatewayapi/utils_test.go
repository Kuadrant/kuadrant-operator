//go:build unit

package gatewayapi

import (
	"testing"

	"gotest.tools/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
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

func TestGetHTTPRouteAcceptedParentRefs(t *testing.T) {
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
			res := GetHTTPRouteAcceptedParentRefs(tc.route)
			assert.DeepEqual(subT, res, tc.expected)
		})
	}
}

func TestNormalizeParentReference(t *testing.T) {
	tests := []struct {
		name                string
		ref                 gatewayapiv1.ParentReference
		routeNamespace      string
		wantGroup           string
		wantKind            string
		wantNamespace       string
		wantName            string
		wantSectionName     string // expected SectionName value (empty = should be nil)
		wantPort            int32  // expected Port value (0 = should be nil)
		checkInputImmutable bool   // verify input ref was not modified
	}{
		{
			name: "all fields explicitly set - no changes",
			ref: gatewayapiv1.ParentReference{
				Group:     ptr.To(gatewayapiv1.Group("gateway.networking.k8s.io")),
				Kind:      ptr.To(gatewayapiv1.Kind("Gateway")),
				Namespace: ptr.To(gatewayapiv1.Namespace("custom-ns")),
				Name:      "my-gateway",
			},
			routeNamespace:      "default",
			wantGroup:           "gateway.networking.k8s.io",
			wantKind:            "Gateway",
			wantNamespace:       "custom-ns",
			wantName:            "my-gateway",
			checkInputImmutable: true,
		},
		{
			name: "only name set - all defaults applied",
			ref: gatewayapiv1.ParentReference{
				Name: "my-gateway",
			},
			routeNamespace:      "default",
			wantGroup:           "gateway.networking.k8s.io",
			wantKind:            "Gateway",
			wantNamespace:       "default",
			wantName:            "my-gateway",
			checkInputImmutable: true,
		},
		{
			name: "namespace omitted - defaults to route namespace",
			ref: gatewayapiv1.ParentReference{
				Group: ptr.To(gatewayapiv1.Group("gateway.networking.k8s.io")),
				Kind:  ptr.To(gatewayapiv1.Kind("Gateway")),
				Name:  "my-gateway",
			},
			routeNamespace:      "custom-namespace",
			wantGroup:           "gateway.networking.k8s.io",
			wantKind:            "Gateway",
			wantNamespace:       "custom-namespace",
			wantName:            "my-gateway",
			checkInputImmutable: true,
		},
		{
			name: "group omitted - defaults to gateway.networking.k8s.io",
			ref: gatewayapiv1.ParentReference{
				Kind:      ptr.To(gatewayapiv1.Kind("Gateway")),
				Namespace: ptr.To(gatewayapiv1.Namespace("ns1")),
				Name:      "my-gateway",
			},
			routeNamespace:      "default",
			wantGroup:           "gateway.networking.k8s.io",
			wantKind:            "Gateway",
			wantNamespace:       "ns1",
			wantName:            "my-gateway",
			checkInputImmutable: true,
		},
		{
			name: "kind omitted - defaults to Gateway",
			ref: gatewayapiv1.ParentReference{
				Group:     ptr.To(gatewayapiv1.Group("gateway.networking.k8s.io")),
				Namespace: ptr.To(gatewayapiv1.Namespace("ns1")),
				Name:      "my-gateway",
			},
			routeNamespace:      "default",
			wantGroup:           "gateway.networking.k8s.io",
			wantKind:            "Gateway",
			wantNamespace:       "ns1",
			wantName:            "my-gateway",
			checkInputImmutable: true,
		},
		{
			name: "section name and port preserved",
			ref: gatewayapiv1.ParentReference{
				Name:        "my-gateway",
				SectionName: ptr.To(gatewayapiv1.SectionName("https")),
				Port:        ptr.To(gatewayapiv1.PortNumber(443)),
			},
			routeNamespace:      "default",
			wantGroup:           "gateway.networking.k8s.io",
			wantKind:            "Gateway",
			wantNamespace:       "default",
			wantName:            "my-gateway",
			wantSectionName:     "https",
			wantPort:            443,
			checkInputImmutable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a deep copy of input for immutability checking
			originalRef := tt.ref.DeepCopy()

			got := normalizeParentReference(tt.ref, tt.routeNamespace)

			// Verify all fields match expectations
			if string(ptr.Deref(got.Group, "")) != tt.wantGroup {
				t.Errorf("Group = %v, want %v", ptr.Deref(got.Group, ""), tt.wantGroup)
			}
			if string(ptr.Deref(got.Kind, "")) != tt.wantKind {
				t.Errorf("Kind = %v, want %v", ptr.Deref(got.Kind, ""), tt.wantKind)
			}
			if string(ptr.Deref(got.Namespace, "")) != tt.wantNamespace {
				t.Errorf("Namespace = %v, want %v", ptr.Deref(got.Namespace, ""), tt.wantNamespace)
			}
			if string(got.Name) != tt.wantName {
				t.Errorf("Name = %v, want %v", got.Name, tt.wantName)
			}

			// Verify SectionName preservation based on expected value
			gotSectionName := string(ptr.Deref(got.SectionName, ""))
			if gotSectionName != tt.wantSectionName {
				t.Errorf("SectionName = %v, want %v", gotSectionName, tt.wantSectionName)
			}

			// Verify Port preservation based on expected value
			gotPort := int32(ptr.Deref(got.Port, 0))
			if gotPort != tt.wantPort {
				t.Errorf("Port = %v, want %v", gotPort, tt.wantPort)
			}

			// Verify input immutability if requested
			if tt.checkInputImmutable {
				// Check deep equality - input should not be modified
				if tt.ref.Group != nil && originalRef.Group != nil {
					if *tt.ref.Group != *originalRef.Group {
						t.Errorf("Input Group was modified: original=%v, after=%v", *originalRef.Group, *tt.ref.Group)
					}
				}
				if tt.ref.Kind != nil && originalRef.Kind != nil {
					if *tt.ref.Kind != *originalRef.Kind {
						t.Errorf("Input Kind was modified: original=%v, after=%v", *originalRef.Kind, *tt.ref.Kind)
					}
				}
				if tt.ref.Namespace != nil && originalRef.Namespace != nil {
					if *tt.ref.Namespace != *originalRef.Namespace {
						t.Errorf("Input Namespace was modified: original=%v, after=%v", *originalRef.Namespace, *tt.ref.Namespace)
					}
				}
				if tt.ref.Name != originalRef.Name {
					t.Errorf("Input Name was modified: original=%v, after=%v", originalRef.Name, tt.ref.Name)
				}

				// Behavior-focused immutability check: mutate the input and verify output is unaffected
				// Capture current output values before mutation
				expectedGroup := string(ptr.Deref(got.Group, ""))
				expectedKind := string(ptr.Deref(got.Kind, ""))
				expectedNamespace := string(ptr.Deref(got.Namespace, ""))
				expectedSectionName := string(ptr.Deref(got.SectionName, ""))
				expectedPort := int32(ptr.Deref(got.Port, 0))

				// Mutate the actual input reference fields
				if tt.ref.Group != nil {
					*tt.ref.Group = "mutated.example.com"
				}
				if tt.ref.Kind != nil {
					*tt.ref.Kind = "MutatedKind"
				}
				if tt.ref.Namespace != nil {
					*tt.ref.Namespace = "mutated-namespace"
				}
				if tt.ref.SectionName != nil {
					*tt.ref.SectionName = "mutated-section"
				}
				if tt.ref.Port != nil {
					*tt.ref.Port = 9999
				}

				// Verify output values remain unchanged after mutation
				if string(ptr.Deref(got.Group, "")) != expectedGroup {
					t.Errorf("Output Group changed after input mutation: expected=%v, got=%v", expectedGroup, string(ptr.Deref(got.Group, "")))
				}
				if string(ptr.Deref(got.Kind, "")) != expectedKind {
					t.Errorf("Output Kind changed after input mutation: expected=%v, got=%v", expectedKind, string(ptr.Deref(got.Kind, "")))
				}
				if string(ptr.Deref(got.Namespace, "")) != expectedNamespace {
					t.Errorf("Output Namespace changed after input mutation: expected=%v, got=%v", expectedNamespace, string(ptr.Deref(got.Namespace, "")))
				}
				if string(ptr.Deref(got.SectionName, "")) != expectedSectionName {
					t.Errorf("Output SectionName changed after input mutation: expected=%v, got=%v", expectedSectionName, string(ptr.Deref(got.SectionName, "")))
				}
				if int32(ptr.Deref(got.Port, 0)) != expectedPort {
					t.Errorf("Output Port changed after input mutation: expected=%v, got=%v", expectedPort, int32(ptr.Deref(got.Port, 0)))
				}
			}
		})
	}
}

func TestParentReferencesMatch(t *testing.T) {
	tests := []struct {
		name           string
		a              gatewayapiv1.ParentReference
		b              gatewayapiv1.ParentReference
		routeNamespace string
		want           bool
	}{
		{
			name: "identical references",
			a: gatewayapiv1.ParentReference{
				Group:     ptr.To(gatewayapiv1.Group("gateway.networking.k8s.io")),
				Kind:      ptr.To(gatewayapiv1.Kind("Gateway")),
				Namespace: ptr.To(gatewayapiv1.Namespace("default")),
				Name:      "my-gateway",
			},
			b: gatewayapiv1.ParentReference{
				Group:     ptr.To(gatewayapiv1.Group("gateway.networking.k8s.io")),
				Kind:      ptr.To(gatewayapiv1.Kind("Gateway")),
				Namespace: ptr.To(gatewayapiv1.Namespace("default")),
				Name:      "my-gateway",
			},
			routeNamespace: "default",
			want:           true,
		},
		{
			name: "one with defaults, one explicit - should match",
			a: gatewayapiv1.ParentReference{
				Name: "my-gateway",
			},
			b: gatewayapiv1.ParentReference{
				Group:     ptr.To(gatewayapiv1.Group("gateway.networking.k8s.io")),
				Kind:      ptr.To(gatewayapiv1.Kind("Gateway")),
				Namespace: ptr.To(gatewayapiv1.Namespace("default")),
				Name:      "my-gateway",
			},
			routeNamespace: "default",
			want:           true,
		},
		{
			name: "different names",
			a: gatewayapiv1.ParentReference{
				Name: "gateway-a",
			},
			b: gatewayapiv1.ParentReference{
				Name: "gateway-b",
			},
			routeNamespace: "default",
			want:           false,
		},
		{
			name: "different section names - should not match",
			a: gatewayapiv1.ParentReference{
				Name:        "my-gateway",
				SectionName: ptr.To(gatewayapiv1.SectionName("http")),
			},
			b: gatewayapiv1.ParentReference{
				Name:        "my-gateway",
				SectionName: ptr.To(gatewayapiv1.SectionName("https")),
			},
			routeNamespace: "default",
			want:           false,
		},
		{
			name: "one with section name, one without - should not match",
			a: gatewayapiv1.ParentReference{
				Name:        "my-gateway",
				SectionName: ptr.To(gatewayapiv1.SectionName("http")),
			},
			b: gatewayapiv1.ParentReference{
				Name: "my-gateway",
			},
			routeNamespace: "default",
			want:           false,
		},
		{
			name: "different ports - should not match",
			a: gatewayapiv1.ParentReference{
				Name: "my-gateway",
				Port: ptr.To(gatewayapiv1.PortNumber(80)),
			},
			b: gatewayapiv1.ParentReference{
				Name: "my-gateway",
				Port: ptr.To(gatewayapiv1.PortNumber(443)),
			},
			routeNamespace: "default",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parentReferencesMatch(tt.a, tt.b, tt.routeNamespace)
			if got != tt.want {
				t.Errorf("parentReferencesMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetGRPCRouteAcceptedParentRefs_WithSectionName(t *testing.T) {
	// Test that demonstrates the fix for distinguishing ParentRefs with different SectionNames
	route := &gatewayapiv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Spec: gatewayapiv1.GRPCRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1.ParentReference{
					{
						Name:        "gateway-1",
						SectionName: ptr.To(gatewayapiv1.SectionName("http")),
					},
					{
						Name:        "gateway-1",
						SectionName: ptr.To(gatewayapiv1.SectionName("https")),
					},
				},
			},
		},
		Status: gatewayapiv1.GRPCRouteStatus{
			RouteStatus: gatewayapiv1.RouteStatus{
				Parents: []gatewayapiv1.RouteParentStatus{
					{
						ParentRef: gatewayapiv1.ParentReference{
							Group:       ptr.To(gatewayapiv1.Group("gateway.networking.k8s.io")),
							Kind:        ptr.To(gatewayapiv1.Kind("Gateway")),
							Namespace:   ptr.To(gatewayapiv1.Namespace("default")),
							Name:        "gateway-1",
							SectionName: ptr.To(gatewayapiv1.SectionName("http")),
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
							Group:       ptr.To(gatewayapiv1.Group("gateway.networking.k8s.io")),
							Kind:        ptr.To(gatewayapiv1.Kind("Gateway")),
							Namespace:   ptr.To(gatewayapiv1.Namespace("default")),
							Name:        "gateway-1",
							SectionName: ptr.To(gatewayapiv1.SectionName("https")),
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
	}

	got := GetGRPCRouteAcceptedParentRefs(route)

	// Should return only the accepted parent ref (http section)
	if len(got) != 1 {
		t.Errorf("GetGRPCRouteAcceptedParentRefs() returned %d parent refs, want 1", len(got))
	}

	if len(got) > 0 {
		if ptr.Deref(got[0].SectionName, "") != "http" {
			t.Errorf("Expected accepted parent ref to have SectionName 'http', got %v", ptr.Deref(got[0].SectionName, ""))
		}
	}
}

func TestGetHTTPRouteAcceptedParentRefs_WithSectionName(t *testing.T) {
	// Test that the HTTP version also properly handles SectionName distinction
	// (matching fix applied to GRPCRoute version)
	route := &gatewayapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Spec: gatewayapiv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1.ParentReference{
					{
						Name:        "gateway-1",
						SectionName: ptr.To(gatewayapiv1.SectionName("http")),
					},
					{
						Name:        "gateway-1",
						SectionName: ptr.To(gatewayapiv1.SectionName("https")),
					},
				},
			},
		},
		Status: gatewayapiv1.HTTPRouteStatus{
			RouteStatus: gatewayapiv1.RouteStatus{
				Parents: []gatewayapiv1.RouteParentStatus{
					{
						ParentRef: gatewayapiv1.ParentReference{
							Group:       ptr.To(gatewayapiv1.Group("gateway.networking.k8s.io")),
							Kind:        ptr.To(gatewayapiv1.Kind("Gateway")),
							Namespace:   ptr.To(gatewayapiv1.Namespace("default")),
							Name:        "gateway-1",
							SectionName: ptr.To(gatewayapiv1.SectionName("http")),
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
							Group:       ptr.To(gatewayapiv1.Group("gateway.networking.k8s.io")),
							Kind:        ptr.To(gatewayapiv1.Kind("Gateway")),
							Namespace:   ptr.To(gatewayapiv1.Namespace("default")),
							Name:        "gateway-1",
							SectionName: ptr.To(gatewayapiv1.SectionName("https")),
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
	}

	got := GetHTTPRouteAcceptedParentRefs(route)

	// Should return only the accepted parent ref (http section)
	if len(got) != 1 {
		t.Errorf("GetHTTPRouteAcceptedParentRefs() returned %d parent refs, want 1", len(got))
	}

	if len(got) > 0 {
		if ptr.Deref(got[0].SectionName, "") != "http" {
			t.Errorf("Expected accepted parent ref to have SectionName 'http', got %v", ptr.Deref(got[0].SectionName, ""))
		}
	}
}
