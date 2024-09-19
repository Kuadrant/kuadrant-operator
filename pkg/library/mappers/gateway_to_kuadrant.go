package mappers

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

type GatewayToKuadrantEventMapper struct {
	opts MapperOptions
}

func NewGatewayToKuadrantEventMapper(o ...MapperOption) *GatewayToKuadrantEventMapper {
	return &GatewayToKuadrantEventMapper{opts: Apply(o...)}
}

func (m *GatewayToKuadrantEventMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := m.opts.Logger.WithValues("object", client.ObjectKeyFromObject(obj))

	gw, ok := obj.(*gatewayapiv1.Gateway)
	if !ok {
		logger.Error(fmt.Errorf("%T is not a *gatweayapiv1.Gateway", obj), "cannot map")
		return []reconcile.Request{}
	}

	if !kuadrant.IsKuadrantManaged(gw) {
		logger.V(1).Info("gateway is not kuadrant managed", "gateway", gw)
		return []reconcile.Request{}
	}

	kuadrantNamespace, err := kuadrant.GetKuadrantNamespace(gw)
	if err != nil {
		logger.Error(err, "cannot get kuadrant namespace")
		return []reconcile.Request{}
	}
	kuadrantList := &kuadrantv1beta1.KuadrantList{}
	err = m.opts.Client.List(ctx, kuadrantList, &client.ListOptions{Namespace: kuadrantNamespace})
	if err != nil {
		logger.Error(err, "cannot list kuadrants")
		return []reconcile.Request{}
	}
	if len(kuadrantList.Items) == 0 {
		logger.Error(err, "kuadrant does not exist in expected namespace")
		return []reconcile.Request{}
	}

	return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(&kuadrantList.Items[0])}}
}
