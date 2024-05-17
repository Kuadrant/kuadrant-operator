package gatewayapi

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
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

// IsHTTPRouteAccepted returns true if a given HTTPRoute has the Accepted status condition added by any of its
// parentRefs; otherwise, it returns false
func IsHTTPRouteAccepted(httpRoute *gatewayapiv1.HTTPRoute) bool {
	acceptedParentRefs := GetRouteAcceptedParentRefs(httpRoute)

	if len(acceptedParentRefs) == 0 {
		return false
	}

	return len(acceptedParentRefs) == len(httpRoute.Spec.ParentRefs)
}

func IsPolicyAccepted(policy Policy) bool {
	condition := meta.FindStatusCondition(policy.GetStatus().GetConditions(), string(gatewayapiv1alpha2.PolicyConditionAccepted))
	return condition != nil && condition.Status == metav1.ConditionTrue
}

func IsNotPolicyAccepted(policy Policy) bool {
	condition := meta.FindStatusCondition(policy.GetStatus().GetConditions(), string(gatewayapiv1alpha2.PolicyConditionAccepted))
	return condition == nil || condition.Status != metav1.ConditionTrue
}

// GetRouteAcceptedGatewayParentKeys returns the object keys of all gateways that have accepted a given route
func GetRouteAcceptedGatewayParentKeys(route *gatewayapiv1.HTTPRoute) []client.ObjectKey {
	acceptedParentRefs := GetRouteAcceptedParentRefs(route)

	gatewayParentRefs := utils.Filter(acceptedParentRefs, IsParentGateway)

	return utils.Map(gatewayParentRefs, func(p gatewayapiv1.ParentReference) client.ObjectKey {
		return client.ObjectKey{
			Name:      string(p.Name),
			Namespace: string(ptr.Deref(p.Namespace, gatewayapiv1.Namespace(route.Namespace))),
		}
	})
}

// GetRouteAcceptedParentRefs returns the list of parentRefs for which a given route has the Accepted status condition
func GetRouteAcceptedParentRefs(route *gatewayapiv1.HTTPRoute) []gatewayapiv1.ParentReference {
	if route == nil {
		return nil
	}

	return utils.Filter(route.Spec.ParentRefs, func(p gatewayapiv1.ParentReference) bool {
		for _, parentStatus := range route.Status.RouteStatus.Parents {
			if reflect.DeepEqual(parentStatus.ParentRef, p) && meta.IsStatusConditionTrue(parentStatus.Conditions, string(gatewayapiv1.RouteConditionAccepted)) {
				return true
			}
		}
		return false
	})
}

func IsParentGateway(ref gatewayapiv1.ParentReference) bool {
	return (ref.Kind == nil || *ref.Kind == "Gateway") && (ref.Group == nil || *ref.Group == gatewayapiv1.GroupName)
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

func IsGatewayAPIInstalled(restMapper meta.RESTMapper) (bool, error) {
	_, err := restMapper.RESTMapping(
		schema.GroupKind{Group: gatewayapiv1.GroupName, Kind: "HTTPRoute"},
		gatewayapiv1.SchemeGroupVersion.Version,
	)
	if err == nil {
		return true, nil
	}

	if meta.IsNoMatchError(err) {
		return false, nil
	}

	return false, err
}
