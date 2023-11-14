package v1beta2

import (
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	orderedmap "github.com/elliotchance/orderedmap/v2"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

// RouteSelector defines semantics for matching an HTTP request based on conditions
// https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPRouteSpec
type RouteSelector struct {
	// Hostnames defines a set of hostname that should match against the HTTP Host header to select a HTTPRoute to process the request
	// https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPRouteSpec
	// +optional
	Hostnames []gatewayapiv1.Hostname `json:"hostnames,omitempty"`

	// Matches define conditions used for matching the rule against incoming HTTP requests.
	// https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPRouteSpec
	// +optional
	// +kubebuilder:validation:MaxItems=8
	Matches []gatewayapiv1.HTTPRouteMatch `json:"matches,omitempty"`
}

// SelectRules returns, from a HTTPRoute, all HTTPRouteRules that either specify no HTTRouteMatches or that contain at
// least one HTTRouteMatch whose statements expressly include (partially or totally) the statements of at least one of
// the matches of the selector. If the selector does not specify any matches, then all HTTPRouteRules are selected.
//
// Additionally, if the selector specifies a non-empty list of hostnames, a non-empty intersection between the literal
// hostnames of the selector and set of hostnames specified in the HTTPRoute must exist. Otherwise, the function
// returns nil.
func (s *RouteSelector) SelectRules(route *gatewayapiv1.HTTPRoute) (rules []gatewayapiv1.HTTPRouteRule) {
	rulesIndices := orderedmap.NewOrderedMap[int, gatewayapiv1.HTTPRouteRule]()
	if len(s.Hostnames) > 0 && !common.Intersect(s.Hostnames, route.Spec.Hostnames) {
		return nil
	}
	if len(s.Matches) == 0 {
		return route.Spec.Rules
	}
	for idx := range s.Matches {
		routeSelectorMatch := s.Matches[idx]
		for idx, rule := range route.Spec.Rules {
			rs := common.HTTPRouteRuleSelector{HTTPRouteMatch: &routeSelectorMatch}
			if rs.Selects(rule) {
				rulesIndices.Set(idx, rule)
			}
		}
	}
	for el := rulesIndices.Front(); el != nil; el = el.Next() {
		rules = append(rules, el.Value)
	}
	return
}

// HostnamesForConditions allows avoiding building conditions for hostnames that are excluded by the selector
// or when the hostname is irrelevant (i.e. matches all hostnames)
func (s *RouteSelector) HostnamesForConditions(route *gatewayapiv1.HTTPRoute) []gatewayapiv1.Hostname {
	hostnames := route.Spec.Hostnames

	if len(s.Hostnames) > 0 {
		hostnames = common.Intersection(s.Hostnames, hostnames)
	}

	if common.SameElements(hostnames, route.Spec.Hostnames) {
		return []gatewayapiv1.Hostname{"*"}
	}

	return hostnames
}

// +kubebuilder:object:generate=false
type RouteSelectorsGetter interface {
	GetRouteSelectors() []RouteSelector
}
