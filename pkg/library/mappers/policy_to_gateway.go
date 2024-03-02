package mappers

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

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

// KuadrantPolicyToParentGatewaysEventMapper is an EventHandler that maps Kuadrant policies to gateway events,
// by going through the policies targetRefs and parentRefs of the route
type KuadrantPolicyToParentGatewaysEventMapper struct {
	Logger logr.Logger
	Client client.Client
}

func (k *KuadrantPolicyToParentGatewaysEventMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := k.Logger.WithValues("object", client.ObjectKeyFromObject(obj))

	policy, ok := obj.(kuadrantgatewayapi.GatewayAPIPolicy)
	if !ok {
		logger.Error(fmt.Errorf("%T is not a GatewayAPIPolicy", obj), "cannot map")
		return []reconcile.Request{}
	}

	if kuadrantgatewayapi.IsTargetRefGateway(policy.GetTargetRef()) {
		namespace := string(ptr.Deref(policy.GetTargetRef().Namespace, gatewayapiv1.Namespace(policy.GetNamespace())))

		nn := types.NamespacedName{Name: string(policy.GetTargetRef().Name), Namespace: namespace}
		logger.V(1).Info("map", " gateway", nn)

		return []reconcile.Request{{NamespacedName: nn}}
	}

	if kuadrantgatewayapi.IsTargetRefHTTPRoute(policy.GetTargetRef()) {
		namespace := string(ptr.Deref(policy.GetTargetRef().Namespace, gatewayapiv1.Namespace(policy.GetNamespace())))
		routeKey := client.ObjectKey{Name: string(policy.GetTargetRef().Name), Namespace: namespace}
		route := &gatewayapiv1.HTTPRoute{}
		if err := k.Client.Get(ctx, routeKey, route); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("no route found", "route", routeKey)
				return []reconcile.Request{}
			}
			logger.Error(err, "failed to get target", "route", routeKey)
			return []reconcile.Request{}
		}

		return utils.Map(kuadrantgatewayapi.GetRouteAcceptedGatewayParentKeys(route), func(key client.ObjectKey) reconcile.Request {
			logger.V(1).Info("new gateway event", "key", key.String())
			return reconcile.Request{NamespacedName: key}
		})
	}

	logger.V(1).Info("policy targeting unexpected resource, skipping it", "key", client.ObjectKeyFromObject(policy))
	return []reconcile.Request{}
}
