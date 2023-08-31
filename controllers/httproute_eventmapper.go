package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

// HTTPRouteEventMapper is an EventHandler that maps HTTPRoute object events to Policy events.
type HTTPRouteEventMapper struct {
	Logger logr.Logger
}

func (m *HTTPRouteEventMapper) MapToRateLimitPolicy(_ context.Context, obj client.Object) []reconcile.Request {
	return m.mapToPolicyRequest(obj, "ratelimitpolicy", common.RateLimitPolicyBackRefAnnotation)
}

func (m *HTTPRouteEventMapper) MapToAuthPolicy(_ context.Context, obj client.Object) []reconcile.Request {
	return m.mapToPolicyRequest(obj, "authpolicy", common.AuthPolicyBackRefAnnotation)
}

func (m *HTTPRouteEventMapper) mapToPolicyRequest(obj client.Object, policyKind, policyBackRefAnnotationName string) []reconcile.Request {
	policyRef, found := common.ReadAnnotationsFromObject(obj)[policyBackRefAnnotationName]
	if !found {
		return []reconcile.Request{}
	}

	policyKey := common.NamespacedNameToObjectKey(policyRef, obj.GetNamespace())

	m.Logger.V(1).Info("Processing object", "object", client.ObjectKeyFromObject(obj), policyKind, policyKey)

	return []reconcile.Request{{NamespacedName: policyKey}}
}
