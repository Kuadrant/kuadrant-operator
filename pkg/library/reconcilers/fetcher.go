package reconcilers

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// FetchTargetRefObject fetches the target reference object and checks the status is valid
func FetchTargetRefObject(ctx context.Context, k8sClient client.Reader, targetRef gatewayapiv1alpha2.LocalPolicyTargetReference, defaultNs string, fetchOnlyProgrammedGateways bool) (client.Object, error) {
	ns := defaultNs
	objKey := client.ObjectKey{Name: string(targetRef.Name), Namespace: ns}

	switch targetRef.Kind {
	case "Gateway":
		if fetchOnlyProgrammedGateways {
			return fetchProgrammedGateway(ctx, k8sClient, objKey)
		}
		return fetchGateway(ctx, k8sClient, objKey)
	case "HTTPRoute":
		return fetchHTTPRoute(ctx, k8sClient, objKey)
	default:
		return nil, fmt.Errorf("FetchValidTargetRef: targetRef (%v) to unknown network resource", targetRef)
	}
}

func fetchGateway(ctx context.Context, k8sClient client.Reader, key client.ObjectKey) (*gatewayapiv1.Gateway, error) {
	logger, _ := logr.FromContext(ctx)

	gw := &gatewayapiv1.Gateway{}
	err := k8sClient.Get(ctx, key, gw)
	logger.V(1).Info("fetch Gateway policy targetRef", "gateway", key, "err", err)
	if err != nil {
		return nil, err
	}

	return gw, nil
}

func fetchProgrammedGateway(ctx context.Context, k8sClient client.Reader, key client.ObjectKey) (*gatewayapiv1.Gateway, error) {
	gw, err := fetchGateway(ctx, k8sClient, key)
	if err != nil {
		return nil, err
	}
	if meta.IsStatusConditionFalse(gw.Status.Conditions, string(gatewayapiv1.GatewayConditionProgrammed)) {
		return nil, fmt.Errorf("gateway (%v) not ready", key)
	}

	return gw, nil
}

func fetchHTTPRoute(ctx context.Context, k8sClient client.Reader, key client.ObjectKey) (*gatewayapiv1.HTTPRoute, error) {
	logger, _ := logr.FromContext(ctx)

	httpRoute := &gatewayapiv1.HTTPRoute{}
	err := k8sClient.Get(ctx, key, httpRoute)
	logger.V(1).Info("fetch HTTPRoute policy targetRef", "httpRoute", key, "err", err)
	if err != nil {
		return nil, err
	}

	if !httpRouteAccepted(httpRoute) {
		return nil, fmt.Errorf("httproute (%v) not accepted", key)
	}

	return httpRoute, nil
}

func httpRouteAccepted(httpRoute *gatewayapiv1.HTTPRoute) bool {
	if httpRoute == nil {
		return false
	}

	if len(httpRoute.Spec.CommonRouteSpec.ParentRefs) == 0 {
		return false
	}

	// Check HTTProute parents (gateways) in the status object
	// if any of the current parent gateways reports not "Admitted", return false
	for _, parentRef := range httpRoute.Spec.CommonRouteSpec.ParentRefs {
		routeParentStatus := func(pRef gatewayapiv1.ParentReference) *gatewayapiv1.RouteParentStatus {
			for idx := range httpRoute.Status.RouteStatus.Parents {
				if reflect.DeepEqual(pRef, httpRoute.Status.RouteStatus.Parents[idx].ParentRef) {
					return &httpRoute.Status.RouteStatus.Parents[idx]
				}
			}
			return nil
		}(parentRef)

		if routeParentStatus == nil || meta.IsStatusConditionFalse(routeParentStatus.Conditions, string(gatewayapiv1.RouteReasonAccepted)) {
			return false
		}
	}

	return true
}
