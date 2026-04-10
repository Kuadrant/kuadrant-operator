// TODO: Move to github.com/kuadrant/policy-machinery

package policymachinery

import (
	"fmt"
	"reflect"
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

// isNil checks if an object is nil, including typed nil pointers.
// Type assertions can succeed but return typed nils (e.g., (*MyType)(nil)),
// where any(x) == nil returns false but the underlying pointer is nil.
// This function uses reflection to detect both regular nils and typed nils.
//
// Performance: Benchmarked at 3-6ns per call (see BenchmarkIsNil), which is
// negligible in controller reconciliation context where this is called during
// path validation (not request path).
func isNil(obj any) bool {
	if obj == nil {
		return true
	}
	v := reflect.ValueOf(obj)
	// Only certain kinds can be nil - check them
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		return v.IsNil()
	default:
		return false
	}
}

type ErrInvalidPath struct {
	message string
}

func (e ErrInvalidPath) Error() string {
	return fmt.Sprintf("invalid path: %s", e.message)
}

// RouteType represents the type of route in a path.
type RouteType int

const (
	RouteTypeUnknown RouteType = iota
	RouteTypeHTTP
	RouteTypeGRPC
	// Future: RouteTypeTCP, RouteTypeUDP, RouteTypeTLS
)

func (rt RouteType) String() string {
	switch rt {
	case RouteTypeHTTP:
		return "HTTP"
	case RouteTypeGRPC:
		return "gRPC"
	default:
		return "Unknown"
	}
}

// DetectRouteType determines the route type from a path without full validation.
// Returns RouteTypeUnknown if the path is invalid or route type is unsupported.
func DetectRouteType(path []machinery.Targetable) RouteType {
	if len(path) < 4 {
		return RouteTypeUnknown
	}
	switch path[3].(type) {
	case *machinery.HTTPRoute:
		return RouteTypeHTTP
	case *machinery.GRPCRoute:
		return RouteTypeGRPC
	default:
		return RouteTypeUnknown
	}
}

// ParsedTopologyPath contains validated objects from a topology path.
type ParsedTopologyPath struct {
	GatewayClass *machinery.GatewayClass
	Gateway      *machinery.Gateway
	Listener     *machinery.Listener
	RouteType    RouteType

	// Only one pair will be set based on RouteType
	HTTPRoute     *machinery.HTTPRoute
	HTTPRouteRule *machinery.HTTPRouteRule
	GRPCRoute     *machinery.GRPCRoute
	GRPCRouteRule *machinery.GRPCRouteRule
}

// GetRouteName returns the route name regardless of type.
func (p *ParsedTopologyPath) GetRouteName() string {
	if p.HTTPRoute != nil {
		return p.HTTPRoute.GetName()
	}
	if p.GRPCRoute != nil {
		return p.GRPCRoute.GetName()
	}
	return ""
}

// GetRouteNamespace returns the route namespace regardless of type.
func (p *ParsedTopologyPath) GetRouteNamespace() string {
	if p.HTTPRoute != nil {
		return p.HTTPRoute.GetNamespace()
	}
	if p.GRPCRoute != nil {
		return p.GRPCRoute.GetNamespace()
	}
	return ""
}

// GetRouteNamespacedName returns a k8s namespaced name for the route.
func (p *ParsedTopologyPath) GetRouteNamespacedName() k8stypes.NamespacedName {
	return k8stypes.NamespacedName{
		Name:      p.GetRouteName(),
		Namespace: p.GetRouteNamespace(),
	}
}

// GetRouteRuleName returns the route rule name regardless of type.
func (p *ParsedTopologyPath) GetRouteRuleName() string {
	if p.HTTPRouteRule != nil {
		return string(p.HTTPRouteRule.Name)
	}
	if p.GRPCRouteRule != nil {
		return string(p.GRPCRouteRule.Name)
	}
	return ""
}

// GetRouteRule returns the route rule as machinery.Object.
func (p *ParsedTopologyPath) GetRouteRule() machinery.Object {
	if p.HTTPRouteRule != nil {
		return p.HTTPRouteRule
	}
	if p.GRPCRouteRule != nil {
		return p.GRPCRouteRule
	}
	return nil
}

// GetRouteRuleLocator returns the locator string for the route rule.
func (p *ParsedTopologyPath) GetRouteRuleLocator() string {
	if p.HTTPRouteRule != nil {
		return p.HTTPRouteRule.GetLocator()
	}
	if p.GRPCRouteRule != nil {
		return p.GRPCRouteRule.GetLocator()
	}
	return ""
}

// ParseTopologyPath parses a topology path and returns validated objects.
// It detects the route type and calls the appropriate parser.
func ParseTopologyPath(path []machinery.Targetable) (*ParsedTopologyPath, error) {
	routeType := DetectRouteType(path)

	switch routeType {
	case RouteTypeHTTP:
		gc, gw, listener, route, rule, err := ObjectsInHTTPRequestPath(path)
		if err != nil {
			return nil, err
		}
		return &ParsedTopologyPath{
			GatewayClass:  gc,
			Gateway:       gw,
			Listener:      listener,
			RouteType:     RouteTypeHTTP,
			HTTPRoute:     route,
			HTTPRouteRule: rule,
		}, nil

	case RouteTypeGRPC:
		gc, gw, listener, route, rule, err := ObjectsInGRPCRequestPath(path)
		if err != nil {
			return nil, err
		}
		return &ParsedTopologyPath{
			GatewayClass:  gc,
			Gateway:       gw,
			Listener:      listener,
			RouteType:     RouteTypeGRPC,
			GRPCRoute:     route,
			GRPCRouteRule: rule,
		}, nil

	default:
		return nil, NewErrInvalidPath("unsupported route type")
	}
}

// ObjectsInRequestPath returns the objects in an HTTP data plane path converted to their respective types.
// The last returned value is an error that indicates if the path is valid if present.
//
// Note: For new code, consider using ParseTopologyPath which supports both HTTP and gRPC routes.
// This function will be migrated to ParseTopologyPath once all policies have GRPCRoute support
// (tracked in https://github.com/Kuadrant/architecture/issues/156).
func ObjectsInRequestPath(path []machinery.Targetable) (*machinery.GatewayClass, *machinery.Gateway, *machinery.Listener, *machinery.HTTPRoute, *machinery.HTTPRouteRule, error) {
	return ObjectsInHTTPRequestPath(path)
}

// routeAccessor provides functions to access route-specific fields in a type-agnostic way.
type routeAccessor[R any] struct {
	getParentRefs func(R) []gatewayapiv1.ParentReference
	getHostnames  func(R) []gatewayapiv1.Hostname
}

// routeRuleAccessor provides functions to access route rule-specific fields in a type-agnostic way.
type routeRuleAccessor[R, RR any] struct {
	getRoute func(RR) R
}

// objectsInRequestPath is a generic helper for extracting and validating route paths.
func objectsInRequestPath[R machinery.Object, RR machinery.Object](
	path []machinery.Targetable,
	routeTypeName string, // e.g., "HTTPRoute" for type checks
	routeDisplayName string, // e.g., "http route" for error messages
	routeRuleTypeName string, // e.g., "HTTPRouteRule"
	routeAcc routeAccessor[R],
	ruleAcc routeRuleAccessor[R, RR],
) (*machinery.GatewayClass, *machinery.Gateway, *machinery.Listener, R, RR, error) {
	var zeroRoute R
	var zeroRouteRule RR

	if len(path) != 5 {
		return nil, nil, nil, zeroRoute, zeroRouteRule, NewErrInvalidPath(fmt.Sprintf("path must have exactly 5 elements, got %d", len(path)))
	}

	gatewayClass, ok := path[0].(*machinery.GatewayClass)
	if !ok {
		return nil, nil, nil, zeroRoute, zeroRouteRule, NewErrInvalidPath("index 0 is not a GatewayClass")
	}
	if isNil(gatewayClass) {
		return nil, nil, nil, zeroRoute, zeroRouteRule, NewErrInvalidPath("gateway class is a typed nil")
	}

	gateway, ok := path[1].(*machinery.Gateway)
	if !ok {
		return gatewayClass, nil, nil, zeroRoute, zeroRouteRule, NewErrInvalidPath("index 1 is not a Gateway")
	}
	if isNil(gateway) {
		return gatewayClass, nil, nil, zeroRoute, zeroRouteRule, NewErrInvalidPath("gateway is a typed nil")
	}
	if gateway.Spec.GatewayClassName != gatewayapiv1.ObjectName(gatewayClass.GetName()) {
		return gatewayClass, gateway, nil, zeroRoute, zeroRouteRule, NewErrInvalidPath("gateway does not belong to the gateway class")
	}

	listener, ok := path[2].(*machinery.Listener)
	if !ok {
		return gatewayClass, gateway, nil, zeroRoute, zeroRouteRule, NewErrInvalidPath("index 2 is not a Listener")
	}
	if isNil(listener) {
		return gatewayClass, gateway, nil, zeroRoute, zeroRouteRule, NewErrInvalidPath("listener is a typed nil")
	}
	if listener.Gateway == nil || listener.Gateway.GetNamespace() != gateway.GetNamespace() || listener.Gateway.GetName() != gateway.GetName() {
		return gatewayClass, gateway, listener, zeroRoute, zeroRouteRule, NewErrInvalidPath("listener does not belong to the gateway")
	}

	route, ok := path[3].(R)
	if !ok {
		return gatewayClass, gateway, listener, zeroRoute, zeroRouteRule, NewErrInvalidPath(fmt.Sprintf("index 3 is not a %s", routeTypeName))
	}
	if isNil(route) {
		return gatewayClass, gateway, listener, zeroRoute, zeroRouteRule, NewErrInvalidPath("route is a typed nil")
	}
	if !lo.ContainsBy(routeAcc.getParentRefs(route), func(ref gatewayapiv1.ParentReference) bool {
		gateway := listener.Gateway
		defaultGroup := gatewayapiv1.Group(gatewayapiv1.GroupName)
		defaultKind := gatewayapiv1.Kind(machinery.GatewayGroupKind.Kind)
		defaultNamespace := gatewayapiv1.Namespace(route.GetNamespace())
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
		if hostnames := routeAcc.getHostnames(route); len(hostnames) > 0 {
			return lo.SomeBy(hostnames, func(routeHostname gatewayapiv1.Hostname) bool {
				return lo.SomeBy(hostnameSupersets, func(hostnameSuperset gatewayapiv1.Hostname) bool {
					return utils.Name(routeHostname).SubsetOf(utils.Name(hostnameSuperset))
				})
			})
		}
		return true
	}) {
		return gatewayClass, gateway, listener, route, zeroRouteRule, NewErrInvalidPath(fmt.Sprintf("%s does not belong to the listener", routeDisplayName))
	}

	routeRule, ok := path[4].(RR)
	if !ok {
		return gatewayClass, gateway, listener, route, zeroRouteRule, NewErrInvalidPath(fmt.Sprintf("index 4 is not a %s", routeRuleTypeName))
	}
	if isNil(routeRule) {
		return gatewayClass, gateway, listener, route, zeroRouteRule, NewErrInvalidPath("route rule is a typed nil")
	}
	parentRoute := ruleAcc.getRoute(routeRule)
	if isNil(parentRoute) || parentRoute.GetNamespace() != route.GetNamespace() || parentRoute.GetName() != route.GetName() {
		return gatewayClass, gateway, listener, route, routeRule, NewErrInvalidPath(fmt.Sprintf("%s rule does not belong to the %s", routeDisplayName, routeDisplayName))
	}

	return gatewayClass, gateway, listener, route, routeRule, nil
}

// ObjectsInHTTPRequestPath returns the objects in an HTTP data plane path converted to their respective types.
// The last returned value is an error that indicates if the path is valid if present.
func ObjectsInHTTPRequestPath(path []machinery.Targetable) (*machinery.GatewayClass, *machinery.Gateway, *machinery.Listener, *machinery.HTTPRoute, *machinery.HTTPRouteRule, error) {
	return objectsInRequestPath(
		path,
		"HTTPRoute",
		"http route",
		"HTTPRouteRule",
		routeAccessor[*machinery.HTTPRoute]{
			getParentRefs: func(r *machinery.HTTPRoute) []gatewayapiv1.ParentReference { return r.Spec.ParentRefs },
			getHostnames:  func(r *machinery.HTTPRoute) []gatewayapiv1.Hostname { return r.Spec.Hostnames },
		},
		routeRuleAccessor[*machinery.HTTPRoute, *machinery.HTTPRouteRule]{
			getRoute: func(rr *machinery.HTTPRouteRule) *machinery.HTTPRoute { return rr.HTTPRoute },
		},
	)
}

// ObjectsInGRPCRequestPath returns the objects in a gRPC data plane path converted to their respective types.
// The last returned value is an error that indicates if the path is valid if present.
func ObjectsInGRPCRequestPath(path []machinery.Targetable) (*machinery.GatewayClass, *machinery.Gateway, *machinery.Listener, *machinery.GRPCRoute, *machinery.GRPCRouteRule, error) {
	return objectsInRequestPath(
		path,
		"GRPCRoute",
		"grpc route",
		"GRPCRouteRule",
		routeAccessor[*machinery.GRPCRoute]{
			getParentRefs: func(r *machinery.GRPCRoute) []gatewayapiv1.ParentReference { return r.Spec.ParentRefs },
			getHostnames:  func(r *machinery.GRPCRoute) []gatewayapiv1.Hostname { return r.Spec.Hostnames },
		},
		routeRuleAccessor[*machinery.GRPCRoute, *machinery.GRPCRouteRule]{
			getRoute: func(rr *machinery.GRPCRouteRule) *machinery.GRPCRoute { return rr.GRPCRoute },
		},
	)
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
