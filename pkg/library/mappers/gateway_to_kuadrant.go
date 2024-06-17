package mappers

import (
	"context"
	"fmt"

	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// GatewayToKuadrantEventMapper is an EventHandler that maps gateway events to kuadrant events,
// by using the kuadrant annotations in the gateway
type GatewayToKuadrantEventMapper struct {
	opts MapperOptions
}

func NewGatewayToKuadrantEventMapper(o ...MapperOption) *GatewayToKuadrantEventMapper {
	return &GatewayToKuadrantEventMapper{opts: Apply(o...)}
}

// Map triggers reconciliation event for a kuadrant CR
// approach:
// Gateway -> kuadrant CR name
func (k *GatewayToKuadrantEventMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := k.opts.Logger.WithValues("object", client.ObjectKeyFromObject(obj))

	gateway, ok := obj.(*gatewayapiv1.Gateway)
	if !ok {
		logger.Info("cannot map gateway related event", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.Gateway", obj))
		return []reconcile.Request{}
	}

	kuadrantNamespace, err := kuadrant.GetKuadrantNamespace(gateway)
	if err != nil {
		logger.Info("cannot get kuadrant namespace from gateway", "gateway", client.ObjectKeyFromObject(gateway))
		return []reconcile.Request{}
	}

	kuadrantName, ok := kuadrant.GetKuadrantName(gateway)
	if !ok {
		logger.Info("cannot get kuadrant name from gateway", "gateway", client.ObjectKeyFromObject(gateway))
		return []reconcile.Request{}
	}

	kuadrantKey := client.ObjectKey{Name: kuadrantName, Namespace: kuadrantNamespace}
	logger.V(1).Info("map", "kuadrant instance", kuadrantKey)
	return []reconcile.Request{{NamespacedName: kuadrantKey}}
}
