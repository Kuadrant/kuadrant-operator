package kuadrant

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/go-logr/logr"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

func GatewaysFromPolicy(ctx context.Context, cl client.Client, policy kuadrantgatewayapi.Policy) ([]client.ObjectKey, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	if kuadrantgatewayapi.IsTargetRefGateway(policy.GetTargetRef()) {
		namespace := string(ptr.Deref(policy.GetTargetRef().Namespace, gatewayapiv1.Namespace(policy.GetNamespace())))

		gwKey := client.ObjectKey{Name: string(policy.GetTargetRef().Name), Namespace: namespace}
		logger.V(1).Info("map", " gateway", gwKey)

		return []client.ObjectKey{gwKey}, nil
	}

	if kuadrantgatewayapi.IsTargetRefHTTPRoute(policy.GetTargetRef()) {
		namespace := string(ptr.Deref(policy.GetTargetRef().Namespace, gatewayapiv1.Namespace(policy.GetNamespace())))
		routeKey := client.ObjectKey{Name: string(policy.GetTargetRef().Name), Namespace: namespace}
		route := &gatewayapiv1.HTTPRoute{}
		if err := cl.Get(ctx, routeKey, route); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("no route found", "route", routeKey)
				return []client.ObjectKey{}, nil
			}
			logger.V(1).Info("failed to get route", "route", routeKey, "error", err)
			return []client.ObjectKey{}, err
		}

		return kuadrantgatewayapi.GetRouteAcceptedGatewayParentKeys(route), nil
	}

	logger.V(1).Info("policy targeting unexpected resource, skipping it", "key", client.ObjectKeyFromObject(policy))
	return []client.ObjectKey{}, nil
}
