package apim

import (
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

// HTTPRouteEventMapper is an EventHandler that maps HTTPRoute object events to Policy events.
type HTTPRouteEventMapper struct {
	Logger logr.Logger
}

func (h *HTTPRouteEventMapper) MapToRateLimitPolicy(obj client.Object) []reconcile.Request {
	httpRouteAnnotations := obj.GetAnnotations()
	if httpRouteAnnotations == nil {
		httpRouteAnnotations = map[string]string{}
	}

	rateLimitRef, ok := httpRouteAnnotations[common.RateLimitPolicyBackRefAnnotation]
	if !ok {
		return []reconcile.Request{}
	}

	rlpKey := common.NamespacedNameToObjectKey(rateLimitRef, obj.GetNamespace())

	h.Logger.V(1).Info("Processing object", "key", client.ObjectKeyFromObject(obj), "ratelimitpolicy", rlpKey)

	requests := []reconcile.Request{
		{
			NamespacedName: rlpKey,
		},
	}

	return requests
}

func (h *HTTPRouteEventMapper) MapToAuthPolicy(obj client.Object) []reconcile.Request {
	httpRouteAnnotations := obj.GetAnnotations()
	if httpRouteAnnotations == nil {
		httpRouteAnnotations = map[string]string{}
	}

	apRef, present := httpRouteAnnotations[common.AuthPolicyBackRefAnnotation]
	if !present {
		return []reconcile.Request{}
	}

	apKey := common.NamespacedNameToObjectKey(apRef, obj.GetNamespace())

	h.Logger.V(1).Info("Processing object", "key", client.ObjectKeyFromObject(obj), "AuthPolicy", apKey)

	requests := []reconcile.Request{
		{
			NamespacedName: apKey,
		},
	}

	return requests
}
