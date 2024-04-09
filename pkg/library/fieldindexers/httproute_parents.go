package fieldindexers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

const (
	HTTPRouteGatewayParentField = ".metadata.parentRefs.gateway"
)

// HTTPRouteByGatewayIndexer declares an index key that we can later use with the client as a pseudo-field name,
// allowing to query all the routes parented by a given gateway
// to prevent creating the same index field multiple times, the function is declared private to be
// called only by this controller
func HTTPRouteIndexByGateway(mgr ctrl.Manager, baseLogger logr.Logger) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayapiv1.HTTPRoute{}, HTTPRouteGatewayParentField, func(rawObj client.Object) []string {
		// grab the route object, extract the parents
		route, assertionOk := rawObj.(*gatewayapiv1.HTTPRoute)
		if !assertionOk {
			baseLogger.V(1).Error(fmt.Errorf("%T is not a *gatewayapiv1.HTTPRoute", rawObj), "cannot map")
			return nil
		}

		logger := baseLogger.WithValues("route", client.ObjectKeyFromObject(route).String())

		return utils.Map(kuadrantgatewayapi.GetRouteAcceptedGatewayParentKeys(route), func(key client.ObjectKey) string {
			logger.V(1).Info("new gateway added", "key", key.String())
			return key.String()
		})
	}); err != nil {
		return err
	}

	return nil
}
