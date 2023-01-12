package common

import (
	"context"
	"encoding/json"
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

type PolicyRefsConfig interface {
	PolicyRefsAnnotation() string
}

type KuadrantRateLimitPolicyRefsConfig struct{}

func (c *KuadrantRateLimitPolicyRefsConfig) PolicyRefsAnnotation() string {
	return KuadrantRateLimitPolicyRefAnnotation
}

// TODO(guicassolato): Define KuadrantAuthPolicyRefsConfig

func GatewaysMissingPolicyRef(gwList *gatewayapiv1alpha2.GatewayList, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, config PolicyRefsConfig) []GatewayWrapper {
	// gateways referenced by the policy but do not have reference to it in the annotations
	gateways := make([]GatewayWrapper, 0)
	for _, gateway := range gwList.Items {
		gw := GatewayWrapper{&gateway, config}
		if ContainsObjectKey(policyGwKeys, client.ObjectKeyFromObject(&gateway)) && !gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

func GatewaysWithValidPolicyRef(gwList *gatewayapiv1alpha2.GatewayList, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, config PolicyRefsConfig) []GatewayWrapper {
	// gateways referenced by the policy but also have reference to it in the annotations
	gateways := make([]GatewayWrapper, 0)
	for _, gateway := range gwList.Items {
		gw := GatewayWrapper{&gateway, config}
		if ContainsObjectKey(policyGwKeys, client.ObjectKeyFromObject(&gateway)) && gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

func GatewaysWithInvalidPolicyRef(gwList *gatewayapiv1alpha2.GatewayList, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, config PolicyRefsConfig) []GatewayWrapper {
	// gateways not referenced by the policy but still have reference in the annotations
	gateways := make([]GatewayWrapper, 0)
	for _, gateway := range gwList.Items {
		gw := GatewayWrapper{&gateway, config}
		if !ContainsObjectKey(policyGwKeys, client.ObjectKeyFromObject(&gateway)) && gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

// GatewayWrapper wraps a Gateway API Gateway adding methods and configs to manage policy references in annotations
type GatewayWrapper struct {
	*gatewayapiv1alpha2.Gateway
	PolicyRefsConfig
}

func (g GatewayWrapper) Key() client.ObjectKey {
	if g.Gateway == nil {
		return client.ObjectKey{}
	}
	return client.ObjectKeyFromObject(g.Gateway)
}

func (g GatewayWrapper) PolicyRefs() []client.ObjectKey {
	if g.Gateway == nil {
		return make([]client.ObjectKey, 0)
	}

	gwAnnotations := g.GetAnnotations()
	if gwAnnotations == nil {
		gwAnnotations = map[string]string{}
	}

	val, ok := gwAnnotations[g.PolicyRefsAnnotation()]
	if !ok {
		return make([]client.ObjectKey, 0)
	}

	var refs []client.ObjectKey

	err := json.Unmarshal([]byte(val), &refs)
	if err != nil {
		return make([]client.ObjectKey, 0)
	}

	return refs
}

func (g GatewayWrapper) ContainsPolicy(policyKey client.ObjectKey) bool {
	if g.Gateway == nil {
		return false
	}

	gwAnnotations := g.GetAnnotations()
	if gwAnnotations == nil {
		gwAnnotations = map[string]string{}
	}

	val, ok := gwAnnotations[g.PolicyRefsAnnotation()]
	if !ok {
		return false
	}

	var refs []client.ObjectKey

	err := json.Unmarshal([]byte(val), &refs)
	if err != nil {
		return false
	}

	return ContainsObjectKey(refs, policyKey)
}

// AddPolicy tries to add a policy to the existing ref list.
// Returns true if policy was added, false otherwise
func (g GatewayWrapper) AddPolicy(policyKey client.ObjectKey) bool {
	if g.Gateway == nil {
		return false
	}

	gwAnnotations := g.GetAnnotations()
	if gwAnnotations == nil {
		gwAnnotations = map[string]string{}
	}

	val, ok := gwAnnotations[g.PolicyRefsAnnotation()]
	if !ok {
		refs := []client.ObjectKey{policyKey}
		serialized, err := json.Marshal(refs)
		if err != nil {
			return false
		}
		gwAnnotations[g.PolicyRefsAnnotation()] = string(serialized)
		g.SetAnnotations(gwAnnotations)
		return true
	}

	var refs []client.ObjectKey

	err := json.Unmarshal([]byte(val), &refs)
	if err != nil {
		return false
	}

	if ContainsObjectKey(refs, policyKey) {
		return false
	}

	refs = append(refs, policyKey)
	serialized, err := json.Marshal(refs)
	if err != nil {
		return false
	}
	gwAnnotations[g.PolicyRefsAnnotation()] = string(serialized)
	g.SetAnnotations(gwAnnotations)
	return true
}

// DeletePolicy tries to delete a policy from the existing ref list.
// Returns true if the policy was deleted, false otherwise
func (g GatewayWrapper) DeletePolicy(policyKey client.ObjectKey) bool {
	if g.Gateway == nil {
		return false
	}

	gwAnnotations := g.GetAnnotations()
	if gwAnnotations == nil {
		gwAnnotations = map[string]string{}
	}

	val, ok := gwAnnotations[g.PolicyRefsAnnotation()]
	if !ok {
		return false
	}

	var refs []client.ObjectKey

	err := json.Unmarshal([]byte(val), &refs)
	if err != nil {
		return false
	}

	if refID := FindObjectKey(refs, policyKey); refID != len(refs) {
		// remove index
		refs = append(refs[:refID], refs[refID+1:]...)
		serialized, err := json.Marshal(refs)
		if err != nil {
			return false
		}
		gwAnnotations[g.PolicyRefsAnnotation()] = string(serialized)
		g.SetAnnotations(gwAnnotations)
		return true
	}

	return false
}

// Hostnames builds a list of hostnames from the listeners.
func (g GatewayWrapper) Hostnames() []string {
	hostnames := make([]string, 0)
	if g.Gateway == nil {
		return hostnames
	}

	for idx := range g.Spec.Listeners {
		if g.Spec.Listeners[idx].Hostname != nil {
			hostnames = append(hostnames, string(*g.Spec.Listeners[idx].Hostname))
		}
	}

	return hostnames
}
