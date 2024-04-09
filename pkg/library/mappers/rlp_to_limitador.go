package mappers

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

// RLPToLimitadorEventMapper is an EventHandler that maps policies to limitador events,
// by using the kuadrant namespace annotated in the gateway
type RLPToLimitadorEventMapper struct {
	opts MapperOptions
}

func NewRLPToLimitadorEventMapper(o ...MapperOption) *RLPToLimitadorEventMapper {
	return &RLPToLimitadorEventMapper{opts: Apply(o...)}
}

// Map triggers reconciliation event for a limitador CR
// approach:
// RLP -> get parent gateway OR RLP -> get parent HTTPRoute -> get parent gateway
// Gateway -> get Kuadrant NS
// Kuadrant NS -> Limitador CR NS/name
func (k *RLPToLimitadorEventMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := k.opts.Logger.WithValues("object", client.ObjectKeyFromObject(obj))

	policy, ok := obj.(kuadrantgatewayapi.Policy)
	if !ok {
		logger.Error(fmt.Errorf("%T is not a Policy", obj), "cannot map")
		return []reconcile.Request{}
	}

	var gatewayKey *client.ObjectKey

	if kuadrantgatewayapi.IsTargetRefGateway(policy.GetTargetRef()) {
		gwNS := string(ptr.Deref(policy.GetTargetRef().Namespace, gatewayapiv1.Namespace(policy.GetNamespace())))
		gatewayKey = &client.ObjectKey{Name: string(policy.GetTargetRef().Name), Namespace: gwNS}
	} else if kuadrantgatewayapi.IsTargetRefHTTPRoute(policy.GetTargetRef()) {
		routeNS := string(ptr.Deref(policy.GetTargetRef().Namespace, gatewayapiv1.Namespace(policy.GetNamespace())))
		routeKey := client.ObjectKey{Name: string(policy.GetTargetRef().Name), Namespace: routeNS}
		route := &gatewayapiv1.HTTPRoute{}
		if err := k.opts.Client.Get(ctx, routeKey, route); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("no route found", "route", routeKey)
				return []reconcile.Request{}
			}
			logger.Error(err, "failed to get target", "route", routeKey)
			return []reconcile.Request{}
		}

		gwKeys := kuadrantgatewayapi.GetRouteAcceptedGatewayParentKeys(route)

		// Currently, only one kuadrant instance is supported.
		// Then, reading only one gateway is enough
		// When multiple kuadrant instances are supported,
		// each gateway could be managed by one kuadrant instances and
		// this mapper would generate multiple request for limitador's limits  reconciliation
		if len(gwKeys) > 0 {
			gatewayKey = &client.ObjectKey{Name: gwKeys[0].Name, Namespace: gwKeys[0].Namespace}
		}
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
