package controllers

import (
	"encoding/json"
	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type LimitadorEventMapper struct {
	Logger logr.Logger
}

func (m LimitadorEventMapper) MapToRateLimitPolicy(obj client.Object) []reconcile.Request {
	limitador, ok := obj.(*limitadorv1alpha1.Limitador)
	if !ok {
		return []reconcile.Request{}
	}

	objAnnotations := limitador.GetAnnotations()
	val, ok := objAnnotations[common.RateLimitPoliciesBackRefAnnotation]
	if !ok {
		return []reconcile.Request{}
	}

	var refs []client.ObjectKey
	err := json.Unmarshal([]byte(val), &refs)
	if err != nil {
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0)
	for _, ref := range refs {
		m.Logger.V(1).Info("MapRateLimitPolicy", "ratelimitpolicy", ref)
		requests = append(requests, reconcile.Request{
			NamespacedName: ref,
		})
	}
	return requests
}
