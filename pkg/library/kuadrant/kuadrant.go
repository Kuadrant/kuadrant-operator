package kuadrant

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const (
	KuadrantNamespaceAnnotation = "kuadrant.io/namespace"
	ControllerName              = "kuadrant.io/policy-controller"
)

type Policy interface {
	kuadrantgatewayapi.Policy
	GetWrappedNamespace() gatewayapiv1.Namespace
	GetRulesHostnames() []string
	Kind() string
}

type PolicyList interface {
	GetItems() []Policy
}

type HTTPRouteRule struct {
	Paths   []string
	Methods []string
	Hosts   []string
}

func IsKuadrantManaged(obj client.Object) bool {
	_, isSet := obj.GetAnnotations()[KuadrantNamespaceAnnotation]
	return isSet
}

func GetKuadrantNamespaceFromPolicyTargetRef(ctx context.Context, cli client.Client, policy Policy) (string, error) {
	targetRef := policy.GetTargetRef()
	gwNamespacedName := types.NamespacedName{Namespace: string(ptr.Deref(targetRef.Namespace, policy.GetWrappedNamespace())), Name: string(targetRef.Name)}
	if kuadrantgatewayapi.IsTargetRefHTTPRoute(targetRef) {
		route := &gatewayapiv1.HTTPRoute{}
		if err := cli.Get(
			ctx,
			types.NamespacedName{Namespace: string(ptr.Deref(targetRef.Namespace, policy.GetWrappedNamespace())), Name: string(targetRef.Name)},
			route,
		); err != nil {
			return "", err
		}
		// First should be OK considering there's 1 Kuadrant instance per cluster and all are tagged
		parentRef := route.Spec.ParentRefs[0]
		gwNamespacedName = types.NamespacedName{Namespace: string(ptr.Deref(parentRef.Namespace, gatewayapiv1.Namespace(route.Namespace))), Name: string(parentRef.Name)}
	}
	gw := &gatewayapiv1.Gateway{}
	if err := cli.Get(ctx, gwNamespacedName, gw); err != nil {
		return "", err
	}
	return GetKuadrantNamespace(gw)
}

func GetKuadrantNamespaceFromPolicy(p Policy) (string, bool) {
	if kuadrantNamespace, isSet := p.GetAnnotations()[KuadrantNamespaceAnnotation]; isSet {
		return kuadrantNamespace, true
	}
	return "", false
}

func GetKuadrantNamespace(obj client.Object) (string, error) {
	if !IsKuadrantManaged(obj) {
		return "", errors.NewInternalError(fmt.Errorf("object %T is not Kuadrant managed", obj))
	}
	return obj.GetAnnotations()[KuadrantNamespaceAnnotation], nil
}

func AnnotateObject(obj client.Object, namespace string) {
	annotations := obj.GetAnnotations()
	if len(annotations) == 0 {
		obj.SetAnnotations(
			map[string]string{
				KuadrantNamespaceAnnotation: namespace,
			},
		)
	} else {
		annotations[KuadrantNamespaceAnnotation] = namespace
		obj.SetAnnotations(annotations)
	}
}

// RulesFromHTTPRoute computes a list of rules from the HTTPRoute object
func RulesFromHTTPRoute(route *gatewayapiv1.HTTPRoute) []HTTPRouteRule {
	if route == nil {
		return nil
	}

	var rules []HTTPRouteRule

	for routeRuleIdx := range route.Spec.Rules {
		for matchIdx := range route.Spec.Rules[routeRuleIdx].Matches {
			match := &route.Spec.Rules[routeRuleIdx].Matches[matchIdx]

			rule := HTTPRouteRule{
				Hosts:   RouteHostnames(route),
				Methods: RouteHTTPMethodToRuleMethod(match.Method),
				Paths:   routePathMatchToRulePath(match.Path),
			}

			if len(rule.Methods) != 0 || len(rule.Paths) != 0 {
				// Only append rule when there are methods or path rules
				// a valid rule must include HTTPRoute hostnames as well
				rules = append(rules, rule)
			}
		}
	}

	// If no rules compiled from the route, at least one rule for the hosts
	if len(rules) == 0 {
		rules = []HTTPRouteRule{{Hosts: RouteHostnames(route)}}
	}

	return rules
}

// routePathMatchToRulePath converts HTTPRoute pathmatch rule to kuadrant's rule path
func routePathMatchToRulePath(pathMatch *gatewayapiv1.HTTPPathMatch) []string {
	if pathMatch == nil {
		return nil
	}

	// Only support for Exact and Prefix match
	if pathMatch.Type != nil && *pathMatch.Type != gatewayapiv1.PathMatchPathPrefix &&
		*pathMatch.Type != gatewayapiv1.PathMatchExact {
		return nil
	}

	// Exact path match
	suffix := ""
	if pathMatch.Type == nil || *pathMatch.Type == gatewayapiv1.PathMatchPathPrefix {
		// defaults to path prefix match type
		suffix = "*"
	}

	val := "/"
	if pathMatch.Value != nil {
		val = *pathMatch.Value
	}

	return []string{val + suffix}
}

// ValidateHierarchicalRules returns error if the policy rules hostnames fail to match the target network hosts
func ValidateHierarchicalRules(policy Policy, targetNetworkObject client.Object) error {
	targetHostnames := kuadrantgatewayapi.TargetHostnames(targetNetworkObject)

	if valid, invalidHost := utils.ValidSubdomains(targetHostnames, policy.GetRulesHostnames()); !valid {
		return fmt.Errorf(
			"rule host (%s) does not follow any hierarchical constraints, "+
				"for the %T to be validated, it must match with at least one of the target network hostnames %+q",
			invalidHost,
			policy,
			targetHostnames,
		)
	}

	return nil
}

type HTTPRouteRuleSelector struct {
	*gatewayapiv1.HTTPRouteMatch
}

func (s *HTTPRouteRuleSelector) Selects(rule gatewayapiv1.HTTPRouteRule) bool {
	if s.HTTPRouteMatch == nil {
		return true
	}

	_, found := utils.Find(rule.Matches, func(ruleMatch gatewayapiv1.HTTPRouteMatch) bool {
		// path
		if s.Path != nil && !reflect.DeepEqual(s.Path, ruleMatch.Path) {
			return false
		}

		// method
		if s.Method != nil && !reflect.DeepEqual(s.Method, ruleMatch.Method) {
			return false
		}

		// headers
		for _, header := range s.Headers {
			if _, found := utils.Find(ruleMatch.Headers, func(otherHeader gatewayapiv1.HTTPHeaderMatch) bool {
				return reflect.DeepEqual(header, otherHeader)
			}); !found {
				return false
			}
		}

		// query params
		for _, param := range s.QueryParams {
			if _, found := utils.Find(ruleMatch.QueryParams, func(otherParam gatewayapiv1.HTTPQueryParamMatch) bool {
				return reflect.DeepEqual(param, otherParam)
			}); !found {
				return false
			}
		}

		return true
	})

	return found
}

// HostnamesFromHTTPRoute returns an array of all hostnames specified in a HTTPRoute or inherited from its parent Gateways
func HostnamesFromHTTPRoute(ctx context.Context, route *gatewayapiv1.HTTPRoute, cli client.Client) ([]string, error) {
	if len(route.Spec.Hostnames) > 0 {
		return RouteHostnames(route), nil
	}

	hosts := []string{}

	for _, ref := range route.Spec.ParentRefs {
		if (ref.Kind != nil && *ref.Kind != "Gateway") || (ref.Group != nil && *ref.Group != gatewayapiv1.GroupName) {
			continue
		}
		gw := &gatewayapiv1.Gateway{}
		ns := route.Namespace
		if ref.Namespace != nil {
			ns = string(*ref.Namespace)
		}
		if err := cli.Get(ctx, types.NamespacedName{Namespace: ns, Name: string(ref.Name)}, gw); err != nil {
			return nil, err
		}
		gwHostanmes := utils.HostnamesToStrings(kuadrantgatewayapi.GatewayHostnames(gw))
		hosts = append(hosts, gwHostanmes...)
	}

	return hosts, nil
}

func RouteHostnames(route *gatewayapiv1.HTTPRoute) []string {
	if route == nil {
		return nil
	}

	if len(route.Spec.Hostnames) == 0 {
		return []string{"*"}
	}

	hosts := make([]string, 0, len(route.Spec.Hostnames))

	for _, hostname := range route.Spec.Hostnames {
		hosts = append(hosts, string(hostname))
	}

	return hosts
}

func RouteHTTPMethodToRuleMethod(httpMethod *gatewayapiv1.HTTPMethod) []string {
	if httpMethod == nil {
		return nil
	}

	return []string{string(*httpMethod)}
}

// HTTPRouteRuleToString prints the matches of a  HTTPRouteRule as string
func HTTPRouteRuleToString(rule gatewayapiv1.HTTPRouteRule) string {
	matches := utils.Map(rule.Matches, HTTPRouteMatchToString)
	return fmt.Sprintf("{matches:[%s]}", strings.Join(matches, ","))
}

func HTTPRouteMatchToString(match gatewayapiv1.HTTPRouteMatch) string {
	var patterns []string
	if method := match.Method; method != nil {
		patterns = append(patterns, fmt.Sprintf("method:%v", HTTPMethodToString(method)))
	}
	if path := match.Path; path != nil {
		patterns = append(patterns, fmt.Sprintf("path:%s", HTTPPathMatchToString(path)))
	}
	if len(match.QueryParams) > 0 {
		queryParams := utils.Map(match.QueryParams, HTTPQueryParamMatchToString)
		patterns = append(patterns, fmt.Sprintf("queryParams:[%s]", strings.Join(queryParams, ",")))
	}
	if len(match.Headers) > 0 {
		headers := utils.Map(match.Headers, HTTPHeaderMatchToString)
		patterns = append(patterns, fmt.Sprintf("headers:[%s]", strings.Join(headers, ",")))
	}
	return fmt.Sprintf("{%s}", strings.Join(patterns, ","))
}

func HTTPPathMatchToString(path *gatewayapiv1.HTTPPathMatch) string {
	if path == nil {
		return "*"
	}
	if path.Type != nil {
		switch *path.Type {
		case gatewayapiv1.PathMatchExact:
			return *path.Value
		case gatewayapiv1.PathMatchRegularExpression:
			return fmt.Sprintf("~/%s/", *path.Value)
		}
	}
	return fmt.Sprintf("%s*", *path.Value)
}

func HTTPHeaderMatchToString(header gatewayapiv1.HTTPHeaderMatch) string {
	if header.Type != nil {
		switch *header.Type {
		case gatewayapiv1.HeaderMatchRegularExpression:
			return fmt.Sprintf("{%s:~/%s/}", header.Name, header.Value)
		}
	}
	return fmt.Sprintf("{%s:%s}", header.Name, header.Value)
}

func HTTPQueryParamMatchToString(queryParam gatewayapiv1.HTTPQueryParamMatch) string {
	if queryParam.Type != nil {
		switch *queryParam.Type {
		case gatewayapiv1.QueryParamMatchRegularExpression:
			return fmt.Sprintf("{%s:~/%s/}", queryParam.Name, queryParam.Value)
		}
	}
	return fmt.Sprintf("{%s:%s}", queryParam.Name, queryParam.Value)
}

func HTTPMethodToString(method *gatewayapiv1.HTTPMethod) string {
	if method == nil {
		return "*"
	}
	return string(*method)
}
