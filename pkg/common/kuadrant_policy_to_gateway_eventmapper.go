package common

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// KuadrantPolicyToParentGatewaysEventMapper is an EventHandler that maps Kuadrant policies to gateway events,
// by going through the policies targetRefs and parentRefs of the route
type KuadrantPolicyToParentGatewaysEventMapper struct {
	Logger logr.Logger
	Client client.Client
}

func (k *KuadrantPolicyToParentGatewaysEventMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := k.Logger.WithValues("object", client.ObjectKeyFromObject(obj))

	kuadrantPolicy, ok := obj.(KuadrantPolicy)
	if !ok {
		logger.Error(fmt.Errorf("%T is not a KuadrantPolicy", obj), "cannot map")
		return []reconcile.Request{}
	}

	if IsTargetRefGateway(kuadrantPolicy.GetTargetRef()) {
		namespace := string(ptr.Deref(kuadrantPolicy.GetTargetRef().Namespace, kuadrantPolicy.GetWrappedNamespace()))

		nn := types.NamespacedName{Name: string(kuadrantPolicy.GetTargetRef().Name), Namespace: namespace}
		logger.V(1).Info("map", " gateway", nn)

		return []reconcile.Request{{NamespacedName: nn}}
	}

	if IsTargetRefHTTPRoute(kuadrantPolicy.GetTargetRef()) {
		namespace := string(ptr.Deref(kuadrantPolicy.GetTargetRef().Namespace, kuadrantPolicy.GetWrappedNamespace()))
		routeKey := client.ObjectKey{Name: string(kuadrantPolicy.GetTargetRef().Name), Namespace: namespace}
		route := &gatewayapiv1.HTTPRoute{}
		if err := k.Client.Get(ctx, routeKey, route); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("no route found", "route", routeKey)
				return []reconcile.Request{}
			}
			logger.Error(err, "failed to get target", "route", routeKey)
			return []reconcile.Request{}
		}

		requests := make([]reconcile.Request, 0)

		for _, parentRef := range route.Spec.ParentRefs {
			// skips if parentRef is not a Gateway
			if !IsParentGateway(parentRef) {
				continue
			}

			ns := route.Namespace
			if parentRef.Namespace != nil {
				ns = string(*parentRef.Namespace)
			}

			nn := types.NamespacedName{Name: string(parentRef.Name), Namespace: ns}
			logger.V(1).Info("map", " gateway", nn)

			requests = append(requests, reconcile.Request{NamespacedName: nn})
		}

		return requests
	}

	return []reconcile.Request{}
}
