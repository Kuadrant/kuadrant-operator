//go:build unit

package gatewayapi

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const (
	NS = "nsA"
)

func testBasicGateway(name, namespace string) *gatewayapiv1.Gateway {
	// Valid gateway
	return &gatewayapiv1.Gateway{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gatewayapiv1.GroupVersion.String(),
			Kind:       "Gateway",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: gatewayapiv1.GatewayStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(gatewayapiv1.GatewayConditionProgrammed),
					Status: metav1.ConditionTrue,
				},
			},
		},
	}
}

func testInvalidGateway(name, namespace string) *gatewayapiv1.Gateway {
	gw := testBasicGateway(name, namespace)
	// remove conditions to make it invalid
	gw.Status = gatewayapiv1.GatewayStatus{}

	return gw
}

func testBasicRoute(name, namespace string, parents ...*gatewayapiv1.Gateway) *gatewayapiv1.HTTPRoute {
	parentRefs := make([]gatewayapiv1.ParentReference, 0)
	for _, val := range parents {
		parentRefs = append(parentRefs, gatewayapiv1.ParentReference{
			Group:     ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
			Kind:      ptr.To(gatewayapiv1.Kind("Gateway")),
			Namespace: ptr.To(gatewayapiv1.Namespace(val.Namespace)),
			Name:      gatewayapiv1.ObjectName(val.Name),
		})
	}

	parentStatusRefs := utils.Map(parentRefs, func(p gatewayapiv1.ParentReference) gatewayapiv1.RouteParentStatus {
		return gatewayapiv1.RouteParentStatus{
			ParentRef:  p,
			Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue}},
		}
	})

	return &gatewayapiv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gatewayapiv1.GroupVersion.String(),
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: gatewayapiv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
		},
		Status: gatewayapiv1.HTTPRouteStatus{
			RouteStatus: gatewayapiv1.RouteStatus{
				Parents: parentStatusRefs,
			},
		},
	}
}

func testBasicGatewayPolicy(name, namespace string, gateway *gatewayapiv1.Gateway) Policy {
	return &TestPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "example.com/v1",
			Kind:       "TestPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
			Group:     gatewayapiv1.Group(gatewayapiv1.GroupName),
			Kind:      gatewayapiv1.Kind("Gateway"),
			Namespace: ptr.To(gatewayapiv1.Namespace(gateway.Namespace)),
			Name:      gatewayapiv1.ObjectName(gateway.Name),
		},
	}
}

func testBasicRoutePolicy(name, namespace string, route *gatewayapiv1.HTTPRoute) Policy {
	return &TestPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "example.com/v1",
			Kind:       "TestPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
			Group:     gatewayapiv1.Group(gatewayapiv1.GroupName),
			Kind:      gatewayapiv1.Kind("HTTPRoute"),
			Namespace: ptr.To(gatewayapiv1.Namespace(route.Namespace)),
			Name:      gatewayapiv1.ObjectName(route.Name),
		},
	}
}
