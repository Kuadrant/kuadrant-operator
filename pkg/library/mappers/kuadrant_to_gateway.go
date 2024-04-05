package mappers

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func NewKuadrantToGatewayEventMapper(o ...MapperOption) *KuadrantToGatewayEventMapper {
	return &KuadrantToGatewayEventMapper{opts: Apply(o...)}
}

type KuadrantToGatewayEventMapper struct {
	opts MapperOptions
}

func (k *KuadrantToGatewayEventMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := k.opts.Logger.WithValues("object", client.ObjectKeyFromObject(obj))

	_, ok := obj.(*kuadrantv1beta1.Kuadrant)
	if !ok {
		logger.Error(fmt.Errorf("%T is not a kuadrant instance", obj), "cannot map")
		return []reconcile.Request{}
	}

	gwList := &gatewayapiv1.GatewayList{}
	if err := k.opts.Client.List(ctx, gwList); err != nil {
		logger.Error(err, "failed to list gateways")
		return []reconcile.Request{}
	}

	return utils.Map(gwList.Items, func(gw gatewayapiv1.Gateway) reconcile.Request {
		return reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&gw)}
	})
}
