// TODO: Move to github.com/kuadrant/policy-machinery

package policymachinery

import (
	"fmt"
	"strings"

	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

func NewErrInvalidPath(message string) error {
	return ErrInvalidPath{message: message}
}

type ErrInvalidPath struct {
	message string
}

func (e ErrInvalidPath) Error() string {
	return fmt.Sprintf("invalid path: %s", e.message)
}

// ObjectsInRequestPath returns the objects in a data plane path converted to their respective types
// The last returned value is an error that indicates if the path is valid if present.
func ObjectsInRequestPath(path []machinery.Targetable) (*machinery.GatewayClass, *machinery.Gateway, *machinery.Listener, *machinery.HTTPRoute, *machinery.HTTPRouteRule, error) {
	if len(path) == 0 {
		return nil, nil, nil, nil, nil, NewErrInvalidPath("empty path")
	}

	gatewayClass, ok := path[0].(*machinery.GatewayClass)
	if !ok {
		return nil, nil, nil, nil, nil, NewErrInvalidPath("index 0 is not a GatewayClass")
	}

	gateway, ok := path[1].(*machinery.Gateway)
	if !ok {
		return gatewayClass, nil, nil, nil, nil, NewErrInvalidPath("index 1 is not a Gateway")
	}
	if gateway.Spec.GatewayClassName != gatewayapiv1.ObjectName(gatewayClass.GetName()) {
		return gatewayClass, gateway, nil, nil, nil, NewErrInvalidPath("gateway does not belong to the gateway class")
	}

	listener, ok := path[2].(*machinery.Listener)
	if !ok {
		return gatewayClass, gateway, nil, nil, nil, NewErrInvalidPath("index 2 is not a Listener")
	}
	if listener.Gateway == nil || listener.Gateway.GetNamespace() != gateway.GetNamespace() || listener.Gateway.GetName() != gateway.GetName() {
		return gatewayClass, gateway, listener, nil, nil, NewErrInvalidPath("listener does not belong to the gateway")
	}

	httpRoute, ok := path[3].(*machinery.HTTPRoute)
	if !ok {
		return gatewayClass, gateway, listener, nil, nil, NewErrInvalidPath("index 3 is not a HTTPRoute")
	}
	if !lo.ContainsBy(httpRoute.Spec.ParentRefs, func(ref gatewayapiv1.ParentReference) bool {
		gateway := listener.Gateway
		defaultGroup := gatewayapiv1.Group(gatewayapiv1.GroupName)
		defaultKind := gatewayapiv1.Kind(machinery.GatewayGroupKind.Kind)
		defaultNamespace := gatewayapiv1.Namespace(httpRoute.GetNamespace())
		if ptr.Deref(ref.Group, defaultGroup) != gatewayapiv1.Group(gateway.GroupVersionKind().Group) || ptr.Deref(ref.Kind, defaultKind) != gatewayapiv1.Kind(gateway.GroupVersionKind().Kind) || ptr.Deref(ref.Namespace, defaultNamespace) != gatewayapiv1.Namespace(gateway.GetNamespace()) || ref.Name != gatewayapiv1.ObjectName(gateway.GetName()) {
			return false
		}
		if sectionName := ptr.Deref(ref.SectionName, gatewayapiv1.SectionName("")); sectionName != "" && sectionName != listener.Name {
			return false
		}
		hostnameSupersets := []gatewayapiv1.Hostname{"*"}
		if listener.Hostname != nil {
			hostnameSupersets = []gatewayapiv1.Hostname{*(listener.Hostname)}
		}
		if len(httpRoute.Spec.Hostnames) > 0 {
			return lo.SomeBy(httpRoute.Spec.Hostnames, func(routeHostname gatewayapiv1.Hostname) bool {
				return lo.SomeBy(hostnameSupersets, func(hostnameSuperset gatewayapiv1.Hostname) bool {
					return utils.Name(routeHostname).SubsetOf(utils.Name(hostnameSuperset))
				})
			})
		}
		return true
	}) {
		return gatewayClass, gateway, listener, httpRoute, nil, NewErrInvalidPath("http route does not belong to the listener")
	}

	httpRouteRule, ok := path[4].(*machinery.HTTPRouteRule)
	if !ok {
		return gatewayClass, gateway, listener, httpRoute, nil, NewErrInvalidPath("index 4 is not a HTTPRouteRule")
	}
	if httpRouteRule.HTTPRoute == nil || httpRouteRule.HTTPRoute.GetNamespace() != httpRoute.GetNamespace() || httpRouteRule.HTTPRoute.GetName() != httpRoute.GetName() {
		return gatewayClass, gateway, listener, httpRoute, httpRouteRule, NewErrInvalidPath("http route rule does not belong to the http route")
	}

	return gatewayClass, gateway, listener, httpRoute, httpRouteRule, nil
}

// NamespacedNameFromLocator returns a k8s namespaced name from a Policy Machinery object locator
func NamespacedNameFromLocator(locator string) (k8stypes.NamespacedName, error) {
	parts := strings.SplitN(locator, ":", 2) // <groupKind>:<namespacedName>
	if len(parts) != 2 {
		return k8stypes.NamespacedName{}, fmt.Errorf("invalid locator: %s", locator)
	}
	namespacedName := strings.SplitN(parts[1], string(k8stypes.Separator), 2)
	if len(namespacedName) == 1 {
		return k8stypes.NamespacedName{Name: namespacedName[0]}, nil
	}
	return k8stypes.NamespacedName{Namespace: namespacedName[0], Name: namespacedName[1]}, nil
}
