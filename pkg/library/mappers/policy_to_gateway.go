package mappers

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

// PolicyToParentGatewaysEventMapper is an EventHandler that maps policies to gateway events,
// by going through the policies targetRefs and parentRefs of the route
type PolicyToParentGatewaysEventMapper struct {
	opts MapperOptions
}

func NewPolicyToParentGatewaysEventMapper(o ...MapperOption) *PolicyToParentGatewaysEventMapper {
	return &PolicyToParentGatewaysEventMapper{opts: Apply(o...)}
}

func (k *PolicyToParentGatewaysEventMapper) Map(eventCtx context.Context, obj client.Object) []reconcile.Request {
	logger := k.opts.Logger.WithValues("object", client.ObjectKeyFromObject(obj), "kind", obj.GetObjectKind().GroupVersionKind().Kind)
	ctx := logr.NewContext(eventCtx, logger)

	policy, ok := obj.(kuadrantgatewayapi.Policy)
	if !ok {
		logger.Error(fmt.Errorf("%T is not a Policy", obj), "cannot map")
		return []reconcile.Request{}
	}

	gwKeys, err := kuadrant.GatewaysFromPolicy(ctx, k.opts.Client, policy)
	if err != nil {
		logger.Error(err, "reading gateways affected by the policy")
		return []reconcile.Request{}
	}

	return utils.Map(gwKeys, func(key client.ObjectKey) reconcile.Request {
		logger.V(1).Info("new gateway event", "key", key.String())
		return reconcile.Request{NamespacedName: key}
	})
}
