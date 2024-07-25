package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

// HTTPRouteParentRefsEventMapper is an EventHandler that maps HTTPRoute events to policy events,
// by going through the parentRefs of the route and finding all policies that target one of its
// parent resources, thus yielding events for those policies.
type HTTPRouteParentRefsEventMapper struct {
	Logger logr.Logger
	Client client.Client
}

func (m *HTTPRouteParentRefsEventMapper) MapToRateLimitPolicy(obj client.Object) []reconcile.Request {
	return m.mapToPolicyRequest(obj, "ratelimitpolicy", &api.RateLimitPolicyList{})
}

func (m *HTTPRouteParentRefsEventMapper) MapToAuthPolicy(obj client.Object) []reconcile.Request {
	return m.mapToPolicyRequest(obj, "authpolicy", &api.AuthPolicyList{})
}

func (m *HTTPRouteParentRefsEventMapper) mapToPolicyRequest(obj client.Object, policyKind string, policyList client.ObjectList) []reconcile.Request {
	logger := m.Logger.V(1).WithValues(
		"object", client.ObjectKeyFromObject(obj),
		"policyKind", policyKind,
	)

	route, ok := obj.(*gatewayapiv1.HTTPRoute)
	if !ok {
		logger.Info("mapToPolicyRequest:", "error", fmt.Sprintf("%T is not a *gatewayapiv1.HTTPRoute", obj))
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0)

	for _, parentRef := range route.Spec.ParentRefs {
		// skips if parentRef is not a Gateway
		if (parentRef.Group != nil && *parentRef.Group != gatewayapiv1.GroupName) || (parentRef.Kind != nil && *parentRef.Kind != "Gateway") {
			continue
		}
		// list policies in the same namespace as the parent gateway of the route
		parentRefNamespace := parentRef.Namespace
		if parentRefNamespace == nil {
			ns := gatewayapiv1.Namespace(route.GetNamespace())
			parentRefNamespace = &ns
		}
		if err := m.Client.List(context.Background(), policyList, &client.ListOptions{Namespace: string(*parentRefNamespace)}); err != nil {
			logger.Error(err, "failed to list policies")
		}
		// triggers the reconciliation of any policy that targets the parent gateway of the route
		policies, ok := policyList.(kuadrant.PolicyList)
		if !ok {
			logger.Info("mapToPolicyRequest:", "error", fmt.Sprintf("%T is not a PolicyList", policyList))
			continue
		}
		for _, policy := range policies.GetItems() {
			targetRef := policy.GetTargetRef()
			if !kuadrantgatewayapi.IsTargetRefGateway(targetRef) {
				continue
			}
			targetRefNamespace := ptr.To(policy.GetWrappedNamespace())

			if *parentRefNamespace == *targetRefNamespace && parentRef.Name == targetRef.Name {
				obj, _ := policy.(client.Object)
				requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(obj)})
			}
		}
	}

	return requests
}
