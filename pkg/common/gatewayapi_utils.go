package common

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

const GatewayProgrammedConditionType = "Programmed"

type HTTPRouteRule struct {
	Paths   []string
	Methods []string
	Hosts   []string
}

func IsTargetRefHTTPRoute(targetRef gatewayapiv1alpha2.PolicyTargetReference) bool {
	return targetRef.Group == ("gateway.networking.k8s.io") && targetRef.Kind == ("HTTPRoute")
}

func IsTargetRefGateway(targetRef gatewayapiv1alpha2.PolicyTargetReference) bool {
	return targetRef.Group == ("gateway.networking.k8s.io") && targetRef.Kind == ("Gateway")
}

func RouteHTTPMethodToRuleMethod(httpMethod *gatewayapiv1.HTTPMethod) []string {
	if httpMethod == nil {
		return nil
	}

	return []string{string(*httpMethod)}
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

type HTTPRouteRuleSelector struct {
	*gatewayapiv1.HTTPRouteMatch
}

func (s *HTTPRouteRuleSelector) Selects(rule gatewayapiv1.HTTPRouteRule) bool {
	if s.HTTPRouteMatch == nil {
		return true
	}

	_, found := Find(rule.Matches, func(ruleMatch gatewayapiv1.HTTPRouteMatch) bool {
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
			if _, found := Find(ruleMatch.Headers, func(otherHeader gatewayapiv1.HTTPHeaderMatch) bool {
				return reflect.DeepEqual(header, otherHeader)
			}); !found {
				return false
			}
		}

		// query params
		for _, param := range s.QueryParams {
			if _, found := Find(ruleMatch.QueryParams, func(otherParam gatewayapiv1.HTTPQueryParamMatch) bool {
				return reflect.DeepEqual(param, otherParam)
			}); !found {
				return false
			}
		}

		return true
	})

	return found
}

// HTTPRouteRuleToString prints the matches of a  HTTPRouteRule as string
func HTTPRouteRuleToString(rule gatewayapiv1.HTTPRouteRule) string {
	matches := Map(rule.Matches, HTTPRouteMatchToString)
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
		queryParams := Map(match.QueryParams, HTTPQueryParamMatchToString)
		patterns = append(patterns, fmt.Sprintf("queryParams:[%s]", strings.Join(queryParams, ",")))
	}
	if len(match.Headers) > 0 {
		headers := Map(match.Headers, HTTPHeaderMatchToString)
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

func GetKuadrantNamespaceFromPolicyTargetRef(ctx context.Context, cli client.Client, policy KuadrantPolicy) (string, error) {
	targetRef := policy.GetTargetRef()
	gwNamespacedName := types.NamespacedName{Namespace: string(ptr.Deref(targetRef.Namespace, policy.GetWrappedNamespace())), Name: string(targetRef.Name)}
	if IsTargetRefHTTPRoute(targetRef) {
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

func GetKuadrantNamespaceFromPolicy(policy KuadrantPolicy) (string, bool) {
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

func DeleteKuadrantAnnotationFromGateway(gw *gatewayapiv1.Gateway, namespace string) {
	annotations := gw.GetAnnotations()
	if IsKuadrantManaged(gw) && annotations[KuadrantNamespaceLabel] == namespace {
		delete(gw.Annotations, KuadrantNamespaceLabel)
	}
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

type PolicyRefsConfig interface {
	PolicyRefsAnnotation() string
}

type KuadrantRateLimitPolicyRefsConfig struct{}

func (c *KuadrantRateLimitPolicyRefsConfig) PolicyRefsAnnotation() string {
	return RateLimitPoliciesBackRefAnnotation
}

type KuadrantAuthPolicyRefsConfig struct{}

func (c *KuadrantAuthPolicyRefsConfig) PolicyRefsAnnotation() string {
	return AuthPoliciesBackRefAnnotation
}

type KuadrantTLSPolicyRefsConfig struct{}

func (c *KuadrantTLSPolicyRefsConfig) PolicyRefsAnnotation() string {
	return TLSPoliciesBackRefAnnotation
}

func GatewaysMissingPolicyRef(gwList *gatewayapiv1.GatewayList, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, config PolicyRefsConfig) []GatewayWrapper {
	// gateways referenced by the policy but do not have reference to it in the annotations
	gateways := make([]GatewayWrapper, 0)
	for i := range gwList.Items {
		gateway := gwList.Items[i]
		gw := GatewayWrapper{&gateway, config}
		if slices.Contains(policyGwKeys, client.ObjectKeyFromObject(&gateway)) && !gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

func GatewaysWithValidPolicyRef(gwList *gatewayapiv1.GatewayList, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, config PolicyRefsConfig) []GatewayWrapper {
	// gateways referenced by the policy but also have reference to it in the annotations
	gateways := make([]GatewayWrapper, 0)
	for i := range gwList.Items {
		gateway := gwList.Items[i]
		gw := GatewayWrapper{&gateway, config}
		if slices.Contains(policyGwKeys, client.ObjectKeyFromObject(&gateway)) && gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

func GatewaysWithInvalidPolicyRef(gwList *gatewayapiv1.GatewayList, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, config PolicyRefsConfig) []GatewayWrapper {
	// gateways not referenced by the policy but still have reference in the annotations
	gateways := make([]GatewayWrapper, 0)
	for i := range gwList.Items {
		gateway := gwList.Items[i]
		gw := GatewayWrapper{&gateway, config}
		if !slices.Contains(policyGwKeys, client.ObjectKeyFromObject(&gateway)) && gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

// GatewayWrapper wraps a Gateway API Gateway adding methods and configs to manage policy references in annotations
type GatewayWrapper struct {
	*gatewayapiv1.Gateway
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

	gwAnnotations := ReadAnnotationsFromObject(g)

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

	gwAnnotations := ReadAnnotationsFromObject(g)

	val, ok := gwAnnotations[g.PolicyRefsAnnotation()]
	if !ok {
		return false
	}

	var refs []client.ObjectKey

	err := json.Unmarshal([]byte(val), &refs)
	if err != nil {
		return false
	}

	return slices.Contains(refs, policyKey)
}

// AddPolicy tries to add a policy to the existing ref list.
// Returns true if policy was added, false otherwise
func (g GatewayWrapper) AddPolicy(policyKey client.ObjectKey) bool {
	if g.Gateway == nil {
		return false
	}

	gwAnnotations := ReadAnnotationsFromObject(g)

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

	if slices.Contains(refs, policyKey) {
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

	gwAnnotations := ReadAnnotationsFromObject(g)

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
func (g GatewayWrapper) Hostnames() []gatewayapiv1.Hostname {
	hostnames := make([]gatewayapiv1.Hostname, 0)
	if g.Gateway == nil {
		return hostnames
	}

	for idx := range g.Spec.Listeners {
		if g.Spec.Listeners[idx].Hostname != nil {
			hostnames = append(hostnames, *g.Spec.Listeners[idx].Hostname)
		}
	}

	return hostnames
}

// GatewayWrapperList is a list of GatewayWrappers that implements sort.Interface
type GatewayWrapperList []GatewayWrapper

func (g GatewayWrapperList) Len() int {
	return len(g)
}

func (g GatewayWrapperList) Less(i, j int) bool {
	return g[i].CreationTimestamp.Before(&g[j].CreationTimestamp)
}

func (g GatewayWrapperList) Swap(i, j int) {
	g[i], g[j] = g[j], g[i]
}

// TargetHostnames returns an array of hostnames coming from the network object (HTTPRoute, Gateway)
func TargetHostnames(targetNetworkObject client.Object) ([]string, error) {
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

	return hosts, nil
}

// HostnamesFromHTTPRoute returns an array of all hostnames specified in a HTTPRoute or inherited from its parent Gateways
func HostnamesFromHTTPRoute(ctx context.Context, route *gatewayapiv1.HTTPRoute, cli client.Client) ([]string, error) {
	if len(route.Spec.Hostnames) > 0 {
		return RouteHostnames(route), nil
	}

	hosts := []string{}

	for _, ref := range route.Spec.ParentRefs {
		if (ref.Kind != nil && *ref.Kind != "Gateway") || (ref.Group != nil && *ref.Group != "gateway.networking.k8s.io") {
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
		gwHostanmes := HostnamesToStrings(GatewayWrapper{Gateway: gw}.Hostnames())
		hosts = append(hosts, gwHostanmes...)
	}

	return hosts, nil
}

// ValidateHierarchicalRules returns error if the policy rules hostnames fail to match the target network hosts
func ValidateHierarchicalRules(policy KuadrantPolicy, targetNetworkObject client.Object) error {
	targetHostnames, err := TargetHostnames(targetNetworkObject)
	if err != nil {
		return err
	}

	if valid, invalidHost := ValidSubdomains(targetHostnames, policy.GetRulesHostnames()); !valid {
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

func GetGatewayWorkloadSelector(ctx context.Context, cli client.Client, gateway *gatewayapiv1.Gateway) (map[string]string, error) {
	address, found := Find(
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
	return GetServiceWorkloadSelector(ctx, cli, serviceKey)
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
