package mappers

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/go-logr/logr"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

// PolicyToKuadrantEventMapper is an EventHandler that maps policy events to kuadrant events,
// by using the kuadrant annotations in the gateway
type PolicyToKuadrantEventMapper struct {
	opts MapperOptions
}

func NewPolicyToKuadrantEventMapper(o ...MapperOption) *PolicyToKuadrantEventMapper {
	return &PolicyToKuadrantEventMapper{opts: Apply(o...)}
}

// Map triggers reconciliation event for a kuadrant CR
// approach:
// Policy -> gateways
// Gateway -> Kuadrant CR name
func (k *PolicyToKuadrantEventMapper) Map(eventCtx context.Context, obj client.Object) []reconcile.Request {
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

	for _, gwKey := range gwKeys {
		gateway := &gatewayapiv1.Gateway{}
		if err := k.opts.Client.Get(ctx, gwKey, gateway); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("no gateway found", "gateway", gwKey)
				return []reconcile.Request{}
			}
			logger.Error(err, "failed to get target", "gateway", gwKey)
			return []reconcile.Request{}
		}

		kuadrantNamespace, err := kuadrant.GetKuadrantNamespace(gateway)
		if err != nil {
			logger.Info("cannot get kuadrant namespace from gateway", "gateway", client.ObjectKeyFromObject(gateway))
			continue
		}

		kuadrantName, ok := kuadrant.GetKuadrantName(gateway)
		if !ok {
			logger.Info("cannot get kuadrant name from gateway", "gateway", client.ObjectKeyFromObject(gateway))
			continue
		}

		// Currently, only one kuadrant instance is supported.
		// Then, reading only one valid gateway is enough
		// When multiple kuadrant instances are supported,
		// each gateway could be managed by one kuadrant instances and
		// this mapper would generate multiple requests
		return []reconcile.Request{{NamespacedName: client.ObjectKey{
			Name: kuadrantName, Namespace: kuadrantNamespace,
		}}}
	}

	// nothing to return
	logger.V(1).Info("no valid gateways found")
	return []reconcile.Request{}
}
