package mappers

import (
	"context"

	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	KuadrantManagedLabel              = "kuadrant.io/managed"
	KuadrantRateLimitPolicyAnnotation = "kuadrant.io/ratelimitpolicy"
	KuadrantAuthProviderAnnotation    = "kuadrant.io/auth-provider"
)

// RateLimitPolicyToRouteEventMapper is an EventHandler that maps RateLimitPolicy to Route objects
type RateLimitPolicyToRouteEventMapper struct {
	K8sClient client.Client
	Logger    logr.Logger
}

func (r *RateLimitPolicyToRouteEventMapper) Map(obj client.Object) []reconcile.Request {
	routeList := &routev1.RouteList{}
	// all namespaces
	// filter by API UID
	err := r.K8sClient.List(context.Background(), routeList, client.HasLabels{KuadrantManagedLabel})
	if err != nil {
		r.Logger.Error(err, "reading route list")
		return nil
	}

	r.Logger.V(1).Info("Processing object", "key", client.ObjectKeyFromObject(obj))

	requests := []reconcile.Request{}
	for idx := range routeList.Items {
		routeAnnotations := routeList.Items[idx].GetAnnotations()
		if routeAnnotations == nil {
			routeAnnotations = make(map[string]string)
		}

		if rlpName, ok := routeAnnotations[KuadrantRateLimitPolicyAnnotation]; ok {
			r.Logger.V(1).Info("Accepted object", "key", client.ObjectKeyFromObject(obj))
			requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      rlpName,
				Namespace: routeList.Items[idx].GetNamespace(),
			}})
		}
	}

	return requests
}
