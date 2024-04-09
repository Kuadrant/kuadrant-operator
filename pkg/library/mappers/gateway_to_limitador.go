package mappers

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

// GatewayToLimitadorEventMapper is an EventHandler that maps gateway events to limitador events,
// by using the kuadrant namespace annotated in the gateway
type GatewayToLimitadorEventMapper struct {
	opts MapperOptions
}

func NewGatewayToLimitadorEventMapper(o ...MapperOption) *GatewayToLimitadorEventMapper {
	return &GatewayToLimitadorEventMapper{opts: Apply(o...)}
}

// Map triggers reconciliation event for a limitador CR
// approach:
// Gateway -> get Kuadrant NS
// Kuadrant NS -> Limitador CR NS/name
func (k *GatewayToLimitadorEventMapper) Map(_ context.Context, obj client.Object) []reconcile.Request {
	logger := k.opts.Logger.WithValues("object", client.ObjectKeyFromObject(obj))

	gateway, ok := obj.(*gatewayapiv1.Gateway)
	if !ok {
		logger.Info("cannot map gateway related event", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.Gateway", obj))
		return []reconcile.Request{}
	}

	kuadrantNS, err := kuadrant.GetKuadrantNamespace(gateway)
	if err != nil {
		logger.Info("cannot get kuadrant namespace", "gateway", client.ObjectKeyFromObject(gateway))
		return []reconcile.Request{}
	}

	limitadorKey := common.LimitadorObjectKey(kuadrantNS)
	logger.V(1).Info("map", "limitador", limitadorKey)
	return []reconcile.Request{{NamespacedName: limitadorKey}}
}
