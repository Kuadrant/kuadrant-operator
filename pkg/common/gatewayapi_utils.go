package common

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type HTTPRouteRule struct {
	Paths   []string
	Methods []string
	Hosts   []string
}

func IsTargetRefHTTPRoute(targetRef gatewayapiv1alpha2.PolicyTargetReference) bool {
	return targetRef.Kind == gatewayapiv1alpha2.Kind("HTTPRoute")
}

func IsTargetRefGateway(targetRef gatewayapiv1alpha2.PolicyTargetReference) bool {
	return targetRef.Kind == gatewayapiv1alpha2.Kind("Gateway")
}

func RouteHTTPMethodToRuleMethod(httpMethod *gatewayapiv1alpha2.HTTPMethod) []string {
	if httpMethod == nil {
		return nil
	}

	return []string{string(*httpMethod)}
}

func RouteHostnames(route *gatewayapiv1alpha2.HTTPRoute) []string {
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

// RulesFromHTTPRoute computes a list of rules from the HTTPRoute object
func RulesFromHTTPRoute(route *gatewayapiv1alpha2.HTTPRoute) []HTTPRouteRule {
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

func GetNamespaceFromPolicyTargetRef(ctx context.Context, cli client.Client, policy KuadrantPolicy) (string, error) {
	targetRef := policy.GetTargetRef()
	gwNamespacedName := types.NamespacedName{Namespace: string(GetDefaultIfNil(targetRef.Namespace, policy.GetWrappedNamespace())), Name: string(targetRef.Name)}
	if IsTargetRefHTTPRoute(targetRef) {
		route := &gatewayapiv1alpha2.HTTPRoute{}
		if err := cli.Get(
			ctx,
			types.NamespacedName{Namespace: string(GetDefaultIfNil(targetRef.Namespace, policy.GetWrappedNamespace())), Name: string(targetRef.Name)},
			route,
		); err != nil {
			return "", err
		}
		// First should be OK considering there's 1 Kuadrant instance per cluster and all are tagged
		parentRef := route.Spec.ParentRefs[0]
		gwNamespacedName = types.NamespacedName{Namespace: string(*parentRef.Namespace), Name: string(parentRef.Name)}
	}
	gw := &gatewayapiv1alpha2.Gateway{}
	if err := cli.Get(ctx, gwNamespacedName, gw); err != nil {
		return "", err
	}
	return GetKuadrantNamespace(gw)
}

func GetNamespaceFromPolicy(policy KuadrantPolicy) (string, bool) {
	if kuadrantNamespace, isSet := policy.GetAnnotations()[KuadrantNamespaceLabel]; isSet {
		return kuadrantNamespace, true
	}
	return "", false
}

func GetKuadrantNamespace(obj client.Object) (string, error) {
	if !IsKuadrantManaged(obj) {
		return "", errors.NewInternalError(fmt.Errorf("object %T is not Kuadrant managed", obj))
	}
	return obj.GetAnnotations()[KuadrantNamespaceLabel], nil
}

func IsKuadrantManaged(obj client.Object) bool {
	_, isSet := obj.GetAnnotations()[KuadrantNamespaceLabel]
	return isSet
}

func AnnotateObject(obj client.Object, namespace string) {
	annotations := obj.GetAnnotations()
	if len(annotations) == 0 {
		obj.SetAnnotations(
			map[string]string{
				KuadrantNamespaceLabel: namespace,
			},
		)
	} else {
		if !IsKuadrantManaged(obj) {
			annotations[KuadrantNamespaceLabel] = namespace
			obj.SetAnnotations(annotations)
		}
	}
}

func DeleteKuadrantAnnotationFromGateway(gw *gatewayapiv1alpha2.Gateway, namespace string) {
	annotations := gw.GetAnnotations()
	if IsKuadrantManaged(gw) && annotations[KuadrantNamespaceLabel] == namespace {
		delete(gw.Annotations, KuadrantNamespaceLabel)
	}
}

// routePathMatchToRulePath converts HTTPRoute pathmatch rule to kuadrant's rule path
func routePathMatchToRulePath(pathMatch *gatewayapiv1alpha2.HTTPPathMatch) []string {
	if pathMatch == nil {
		return nil
	}

	// Only support for Exact and Prefix match
	if pathMatch.Type != nil && *pathMatch.Type != gatewayapiv1alpha2.PathMatchPathPrefix &&
		*pathMatch.Type != gatewayapiv1alpha2.PathMatchExact {
		return nil
	}

	// Exact path match
	suffix := ""
	if pathMatch.Type == nil || *pathMatch.Type == gatewayapiv1alpha2.PathMatchPathPrefix {
		// defaults to path prefix match type
		suffix = "*"
	}

	val := "/"
	if pathMatch.Value != nil {
		val = *pathMatch.Value
	}

	return []string{val + suffix}
}
