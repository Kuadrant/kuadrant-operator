package reconcilers

import (
	"context"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestFetchTargetRefObject(t *testing.T) {
	var (
		namespace = "operator-unittest"
		routeName = "my-route"
	)
	baseCtx := context.Background()
	ctx := logr.NewContext(baseCtx, log.Log)

	s := scheme.Scheme
	err := appsv1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}
	err = gatewayapiv1beta1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	targetRef := gatewayapiv1alpha2.PolicyTargetReference{
		Group: "gateway.networking.k8s.io",
		Kind:  "HTTPRoute",
		Name:  gatewayapiv1beta1.ObjectName(routeName),
	}

	existingRoute := &gatewayapiv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1beta1",
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: namespace,
		},
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1beta1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1beta1.ParentReference{
					{
						Name: "gwName",
					},
				},
			},
		},
		Status: gatewayapiv1beta1.HTTPRouteStatus{
			RouteStatus: gatewayapiv1beta1.RouteStatus{
				Parents: []gatewayapiv1beta1.RouteParentStatus{
					{
						ParentRef: gatewayapiv1beta1.ParentReference{
							Name: "gwName",
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
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{existingRoute}

	// Create a fake client to mock API calls.
	clientAPIReader := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	res, err := FetchTargetRefObject(ctx, clientAPIReader, targetRef, namespace)
	if err != nil {
		t.Fatal(err)
	}

	if res == nil {
		t.Fatal("res is nil")
	}

	switch obj := res.(type) {
	case *gatewayapiv1beta1.HTTPRoute:
		if !reflect.DeepEqual(obj.Spec, existingRoute.Spec) {
			t.Fatal("res spec not as expected")
		}
	default:
		t.Fatal("res type not known")
	}
}

func TestFetchGateway(t *testing.T) {
	var (
		namespace = "operator-unittest"
		gwName    = "my-gateway"
	)
	baseCtx := context.Background()
	ctx := logr.NewContext(baseCtx, log.Log)

	s := scheme.Scheme
	err := appsv1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}
	err = gatewayapiv1beta1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	existingGateway := &gatewayapiv1beta1.Gateway{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1beta1",
			Kind:       "Gateway",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      gwName,
			Namespace: namespace,
		},
		Spec: gatewayapiv1beta1.GatewaySpec{
			GatewayClassName: "istio",
		},
		Status: gatewayapiv1beta1.GatewayStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{existingGateway}

	// Create a fake client to mock API calls.
	clientAPIReader := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	key := client.ObjectKey{Name: gwName, Namespace: namespace}

	res, err := fetchGateway(ctx, clientAPIReader, key)
	if err != nil {
		t.Fatal(err)
	}

	if res == nil {
		t.Fatal("res is nil")
	}

	if !reflect.DeepEqual(res.Spec, existingGateway.Spec) {
		t.Fatal("res spec not as expected")
	}
}

func TestFetchHTTPRoute(t *testing.T) {
	var (
		namespace = "operator-unittest"
		routeName = "my-route"
	)
	baseCtx := context.Background()
	ctx := logr.NewContext(baseCtx, log.Log)

	s := scheme.Scheme
	err := appsv1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}
	err = gatewayapiv1beta1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	existingRoute := &gatewayapiv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1beta1",
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: namespace,
		},
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1beta1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1beta1.ParentReference{
					{
						Name: "gwName",
					},
				},
			},
		},
		Status: gatewayapiv1beta1.HTTPRouteStatus{
			RouteStatus: gatewayapiv1beta1.RouteStatus{
				Parents: []gatewayapiv1beta1.RouteParentStatus{
					{
						ParentRef: gatewayapiv1beta1.ParentReference{
							Name: "gwName",
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
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{existingRoute}

	// Create a fake client to mock API calls.
	clientAPIReader := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	key := client.ObjectKey{Name: routeName, Namespace: namespace}

	res, err := fetchHTTPRoute(ctx, clientAPIReader, key)
	if err != nil {
		t.Fatal(err)
	}

	if res == nil {
		t.Fatal("res is nil")
	}

	if !reflect.DeepEqual(res.Spec, existingRoute.Spec) {
		t.Fatal("res spec not as expected")
	}
}

func TestHTTPRouteAccepted(t *testing.T) {
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
			res := httpRouteAccepted(tc.route)
			if res != tc.expected {
				subT.Errorf("result (%t) does not match expected (%t)", res, tc.expected)
			}
		})
	}
}
