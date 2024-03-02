package gatewayapi

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const (
	GatewayProgrammedConditionType = "Programmed"
)

func IsTargetRefHTTPRoute(targetRef gatewayapiv1alpha2.PolicyTargetReference) bool {
	return targetRef.Group == (gatewayapiv1.GroupName) && targetRef.Kind == ("HTTPRoute")
}

func IsTargetRefGateway(targetRef gatewayapiv1alpha2.PolicyTargetReference) bool {
	return targetRef.Group == (gatewayapiv1.GroupName) && targetRef.Kind == ("Gateway")
}

// TargetHostnames returns an array of hostnames coming from the network object (HTTPRoute, Gateway)
func TargetHostnames(targetNetworkObject client.Object) []string {
	hosts := make([]string, 0)
	switch obj := targetNetworkObject.(type) {
	case *gatewayapiv1.HTTPRoute:
		for _, hostname := range obj.Spec.Hostnames {
			hosts = append(hosts, string(hostname))
		}
	case *gatewayapiv1.Gateway:
		for idx := range obj.Spec.Listeners {
			if obj.Spec.Listeners[idx].Hostname != nil {
				hosts = append(hosts, string(*obj.Spec.Listeners[idx].Hostname))
			}
		}
	}

	if len(hosts) == 0 {
		hosts = append(hosts, "*")
	}

	return hosts
}

func GatewayHostnames(gw *gatewayapiv1.Gateway) []gatewayapiv1.Hostname {
	hostnames := make([]gatewayapiv1.Hostname, 0)
	if gw == nil {
		return hostnames
	}

	for idx := range gw.Spec.Listeners {
		if gw.Spec.Listeners[idx].Hostname != nil {
			hostnames = append(hostnames, *gw.Spec.Listeners[idx].Hostname)
		}
	}

	return hostnames
}

func GetGatewayWorkloadSelector(ctx context.Context, cli client.Client, gateway *gatewayapiv1.Gateway) (map[string]string, error) {
	address, found := utils.Find(
		gateway.Status.Addresses,
		func(address gatewayapiv1.GatewayStatusAddress) bool {
			return address.Type != nil && *address.Type == gatewayapiv1.HostnameAddressType
		},
	)
	if !found {
		return nil, fmt.Errorf("cannot find service Hostname in the Gateway status")
	}
	serviceNameParts := strings.Split(address.Value, ".")
	serviceKey := client.ObjectKey{
		Name:      serviceNameParts[0],
		Namespace: serviceNameParts[1],
	}
	return utils.GetServiceWorkloadSelector(ctx, cli, serviceKey)
}

func IsHTTPRouteAccepted(httpRoute *gatewayapiv1.HTTPRoute) bool {
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

		if routeParentStatus == nil {
			return false
		}

		if meta.IsStatusConditionFalse(routeParentStatus.Conditions, "Accepted") {
			return false
		}
	}

	return true
}

func IsParentGateway(ref gatewayapiv1.ParentReference) bool {
	return (ref.Kind == nil || *ref.Kind == "Gateway") && (ref.Group == nil || *ref.Group == gatewayapiv1.GroupName)
}

func GetRouteAcceptedGatewayParentKeys(route *gatewayapiv1.HTTPRoute) []client.ObjectKey {
	if route == nil {
		return nil
	}

	gatewayParentRefs := utils.Filter(route.Spec.ParentRefs, IsParentGateway)

	acceptedParentRefs := utils.Filter(gatewayParentRefs, func(p gatewayapiv1.ParentReference) bool {
		parentStatus, found := utils.Find(route.Status.RouteStatus.Parents, func(pStatus gatewayapiv1.RouteParentStatus) bool {
			return pStatus.ParentRef == p
		})

		if !found {
			return false
		}

		return meta.IsStatusConditionTrue(parentStatus.Conditions, "Accepted")
	})

	return utils.Map(acceptedParentRefs, func(p gatewayapiv1.ParentReference) client.ObjectKey {
		return client.ObjectKey{
			Name:      string(p.Name),
			Namespace: string(ptr.Deref(p.Namespace, gatewayapiv1.Namespace(route.Namespace))),
		}
	})
}

// FilterValidSubdomains returns every subdomain that is a subset of at least one of the (super) domains specified in the first argument.
func FilterValidSubdomains(domains, subdomains []gatewayapiv1.Hostname) []gatewayapiv1.Hostname {
	arr := make([]gatewayapiv1.Hostname, 0)
	for _, subsubdomain := range subdomains {
		if _, found := utils.Find(domains, func(domain gatewayapiv1.Hostname) bool {
			return utils.Name(subsubdomain).SubsetOf(utils.Name(domain))
		}); found {
			arr = append(arr, subsubdomain)
		}
	}
	return arr
}
