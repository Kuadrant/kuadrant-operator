//go:build unit

package mappers

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
)

func gatewayFactory(ns, name string) *gatewayapiv1.Gateway {
	return &gatewayapiv1.Gateway{
		TypeMeta:   metav1.TypeMeta{Kind: "Gateway", APIVersion: gatewayapiv1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       gatewayapiv1.GatewaySpec{},
	}
}

func routeFactory(ns, name string, parentRef gatewayapiv1.ParentReference) *gatewayapiv1.HTTPRoute {
	return &gatewayapiv1.HTTPRoute{
		TypeMeta:   metav1.TypeMeta{Kind: "HTTPRoute", APIVersion: gatewayapiv1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: gatewayapiv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1.ParentReference{parentRef},
			},
		},
	}
}

func policyFactory(ns, name string, targetRef gatewayapiv1alpha2.PolicyTargetReference) *kuadrantv1beta2.RateLimitPolicy {
	return &kuadrantv1beta2.RateLimitPolicy{
		TypeMeta:   metav1.TypeMeta{Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       kuadrantv1beta2.RateLimitPolicySpec{TargetRef: targetRef},
	}
}
