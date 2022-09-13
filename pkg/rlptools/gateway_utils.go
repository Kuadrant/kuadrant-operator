package rlptools

import (
	"encoding/json"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

func NewGateways(gwList *gatewayapiv1alpha2.GatewayList,
	rlpKey client.ObjectKey,
	rlpGwKeys []client.ObjectKey) []GatewayWrapper {
	// gateways referenced by the rlp but do not have reference to it in the annotations
	newGateways := make([]GatewayWrapper, 0)
	for idx := range gwList.Items {
		if common.ContainsObjectKey(rlpGwKeys, client.ObjectKeyFromObject(&gwList.Items[idx])) &&
			!(GatewayWrapper{&gwList.Items[idx]}).ContainsRLP(rlpKey) {
			newGateways = append(newGateways, GatewayWrapper{&gwList.Items[idx]})
		}
	}
	return newGateways
}

func SameGateways(gwList *gatewayapiv1alpha2.GatewayList,
	rlpKey client.ObjectKey,
	rlpGwKeys []client.ObjectKey) []GatewayWrapper {
	// gateways referenced by the rlp but also have reference to it in the annotations
	sameGateways := make([]GatewayWrapper, 0)
	for idx := range gwList.Items {
		if common.ContainsObjectKey(rlpGwKeys, client.ObjectKeyFromObject(&gwList.Items[idx])) &&
			(GatewayWrapper{&gwList.Items[idx]}).ContainsRLP(rlpKey) {
			sameGateways = append(sameGateways, GatewayWrapper{&gwList.Items[idx]})
		}
	}

	return sameGateways
}

func LeftGateways(gwList *gatewayapiv1alpha2.GatewayList,
	rlpKey client.ObjectKey,
	rlpGwKeys []client.ObjectKey) []GatewayWrapper {
	// gateways not referenced by the rlp but still have reference in the annotations
	leftGateways := make([]GatewayWrapper, 0)
	for idx := range gwList.Items {
		if !common.ContainsObjectKey(rlpGwKeys, client.ObjectKeyFromObject(&gwList.Items[idx])) &&
			(GatewayWrapper{&gwList.Items[idx]}).ContainsRLP(rlpKey) {
			leftGateways = append(leftGateways, GatewayWrapper{&gwList.Items[idx]})
		}
	}
	return leftGateways
}

// GatewayWrapper add methods to manage RLP references in annotations
type GatewayWrapper struct {
	*gatewayapiv1alpha2.Gateway
}

func (g GatewayWrapper) Key() client.ObjectKey {
	if g.Gateway == nil {
		return client.ObjectKey{}
	}
	return client.ObjectKeyFromObject(g.Gateway)
}

func (g GatewayWrapper) RLPRefs() []client.ObjectKey {
	if g.Gateway == nil {
		return make([]client.ObjectKey, 0)
	}

	gwAnnotations := g.GetAnnotations()
	if gwAnnotations == nil {
		gwAnnotations = map[string]string{}
	}

	val, ok := gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation]
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

func (g GatewayWrapper) ContainsRLP(rlpKey client.ObjectKey) bool {
	if g.Gateway == nil {
		return false
	}

	gwAnnotations := g.GetAnnotations()
	if gwAnnotations == nil {
		gwAnnotations = map[string]string{}
	}

	val, ok := gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation]
	if !ok {
		return false
	}

	var refs []client.ObjectKey

	err := json.Unmarshal([]byte(val), &refs)
	if err != nil {
		return false
	}

	return common.ContainsObjectKey(refs, rlpKey)
}

// AddRLP tries to add RLP to the existing ref list.
// Returns true if RLP was added, false otherwise
func (g GatewayWrapper) AddRLP(rlpKey client.ObjectKey) bool {
	if g.Gateway == nil {
		return false
	}

	gwAnnotations := g.GetAnnotations()
	if gwAnnotations == nil {
		gwAnnotations = map[string]string{}
	}

	val, ok := gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation]
	if !ok {
		refs := []client.ObjectKey{rlpKey}
		serialized, err := json.Marshal(refs)
		if err != nil {
			return false
		}
		gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation] = string(serialized)
		g.SetAnnotations(gwAnnotations)
		return true
	}

	var refs []client.ObjectKey

	err := json.Unmarshal([]byte(val), &refs)
	if err != nil {
		return false
	}

	if common.ContainsObjectKey(refs, rlpKey) {
		return false
	}

	refs = append(refs, rlpKey)
	serialized, err := json.Marshal(refs)
	if err != nil {
		return false
	}
	gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation] = string(serialized)
	g.SetAnnotations(gwAnnotations)
	return true
}

// DeleteRLP tries to delete RLP from the existing ref list.
// Returns true if RLP was deleted, false otherwise
func (g GatewayWrapper) DeleteRLP(rlpKey client.ObjectKey) bool {
	if g.Gateway == nil {
		return false
	}

	gwAnnotations := g.GetAnnotations()
	if gwAnnotations == nil {
		gwAnnotations = map[string]string{}
	}

	val, ok := gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation]
	if !ok {
		return false
	}

	var refs []client.ObjectKey

	err := json.Unmarshal([]byte(val), &refs)
	if err != nil {
		return false
	}

	if refID := common.FindObjectKey(refs, rlpKey); refID != len(refs) {
		// remove index
		refs = append(refs[:refID], refs[refID+1:]...)
		serialized, err := json.Marshal(refs)
		if err != nil {
			return false
		}
		gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation] = string(serialized)
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
func RulesFromHTTPRoute(route *gatewayapiv1alpha2.HTTPRoute) []apimv1alpha1.Rule {
	if route == nil {
		return nil
	}

	var rules []apimv1alpha1.Rule

	for routeRuleIdx := range route.Spec.Rules {
		for matchIdx := range route.Spec.Rules[routeRuleIdx].Matches {
			match := &route.Spec.Rules[routeRuleIdx].Matches[matchIdx]

			rule := apimv1alpha1.Rule{
				Hosts: RouteHostnames(route),
			}

			rule.Methods = RouteHTTPMethodToRuleMethod(match.Method)
			rule.Paths = routePathMatchToRulePath(match.Path)

			if len(rule.Methods) != 0 || len(rule.Paths) != 0 {
				// Only append rule when there are methods or path rules
				// a valid rule must include HTTPRoute hostnames as well
				rule.Hosts = RouteHostnames(route)
				rules = append(rules, rule)
			}
		}
	}

	// If no rules compiled from the route, at least one rule for the hosts
	if len(rules) == 0 {
		rules = []apimv1alpha1.Rule{{Hosts: RouteHostnames(route)}}
	}

	return rules
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

func RouteHTTPMethodToRuleMethod(httpMethod *gatewayapiv1alpha2.HTTPMethod) []string {
	if httpMethod == nil {
		return nil
	}

	return []string{string(*httpMethod)}
}
