package mappers

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

type HTTPRouteToKuadrantEventMapper struct {
	opts MapperOptions
}

func NewHTTPRouteToKuadrantEventMapper(o ...MapperOption) *HTTPRouteToKuadrantEventMapper {
	return &HTTPRouteToKuadrantEventMapper{opts: Apply(o...)}
}

func (m *HTTPRouteToKuadrantEventMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := m.opts.Logger.WithValues("object", client.ObjectKeyFromObject(obj))

	httpRoute, ok := obj.(*gatewayapiv1.HTTPRoute)
	if !ok {
		logger.Error(fmt.Errorf("%T is not a *gatweayapiv1.HTTPRoute", obj), "cannot map")
		return []reconcile.Request{}
	}

	gatewayKeys := kuadrantgatewayapi.GetRouteAcceptedGatewayParentKeys(httpRoute)

	for _, gatewayKey := range gatewayKeys {
		gateway := &gatewayapiv1.Gateway{}
		err := m.opts.Client.Get(ctx, gatewayKey, gateway)
		if err != nil {
			logger.Info("cannot get gateway", "error", err)
			continue
		}

		if !kuadrant.IsKuadrantManaged(gateway) {
			logger.V(1).Info("gateway is not kuadrant managed", "gateway", gateway)
			continue
		}
		kuadrantNamespace, err := kuadrant.GetKuadrantNamespace(gateway)
		if err != nil {
			logger.Error(err, "cannot get kuadrant namespace")
			continue
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
	logger.V(1).Info("no matching kuadrant instance found")
	return []reconcile.Request{}
}
