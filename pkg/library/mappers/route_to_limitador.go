package mappers

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

// HTTPRouteToLimitadorEventMapper is an EventHandler that maps HTTPRoutes to limitador events,
// by using the kuadrant namespace annotated in the gateway
type HTTPRouteToLimitadorEventMapper struct {
	opts MapperOptions
}

func NewHTTPRouteToLimitadorEventMapper(o ...MapperOption) *HTTPRouteToLimitadorEventMapper {
	return &HTTPRouteToLimitadorEventMapper{opts: Apply(o...)}
}

// Map triggers reconciliation event for a limitador CR
// approach:
// HTTPRoute -> get parent gateway
// Gateway -> get Kuadrant NS
// Kuadrant NS -> Limitador CR NS/name
func (k *HTTPRouteToLimitadorEventMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := k.opts.Logger.WithValues("object", client.ObjectKeyFromObject(obj))

	httpRoute, ok := obj.(*gatewayapiv1.HTTPRoute)
	if !ok {
		logger.Info("cannot map httproute event", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.HTTPRoute", obj))
		return []reconcile.Request{}
	}

	var gatewayKey *client.ObjectKey

	gwKeys := kuadrantgatewayapi.GetRouteAcceptedGatewayParentKeys(httpRoute)

	// Currently, only one kuadrant instance is supported.
	// Then, reading only one gateway is enough
	// When multiple kuadrant instances are supported,
	// each gateway could be managed by one kuadrant instances and
	// this mapper would generate multiple request for limitador's limits  reconciliation
	if len(gwKeys) > 0 {
		gatewayKey = &client.ObjectKey{Name: gwKeys[0].Name, Namespace: gwKeys[0].Namespace}
	}

	if gatewayKey == nil {
		// nothing to return
		logger.V(1).Info("no valid gateways found")
		return []reconcile.Request{}
	}

	logger.V(1).Info("map", "gateway", *gatewayKey)

	gw := &gatewayapiv1.Gateway{}
	if err := k.opts.Client.Get(ctx, *gatewayKey, gw); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("no gateway found", "gateway", gatewayKey)
			return []reconcile.Request{}
		}
		logger.Error(err, "failed to get target", "gateway", gatewayKey)
		return []reconcile.Request{}
	}

	kuadrantNS, err := kuadrant.GetKuadrantNamespace(gw)
	if err != nil {
		logger.Info("cannot get kuadrant namespace", "gateway", client.ObjectKeyFromObject(gw))
		return []reconcile.Request{}
	}
	limitadorKey := common.LimitadorObjectKey(kuadrantNS)
	logger.V(1).Info("map", "limitador", limitadorKey)
	return []reconcile.Request{{NamespacedName: limitadorKey}}
}
