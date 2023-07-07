package v1beta2

import (
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

// RouteSelector defines semantics for matching an HTTP request based on conditions
// https://gateway-api.sigs.k8s.io/v1alpha2/references/spec/#gateway.networking.k8s.io/v1beta1.HTTPRouteSpec
type RouteSelector struct {
	// Hostnames defines a set of hostname that should match against the HTTP Host header to select a HTTPRoute to process the request
	// https://gateway-api.sigs.k8s.io/v1alpha2/references/spec/#gateway.networking.k8s.io/v1beta1.HTTPRouteSpec
	// +optional
	Hostnames []gatewayapiv1beta1.Hostname `json:"hostnames,omitempty"`

	// Matches define conditions used for matching the rule against incoming HTTP requests.
	// https://gateway-api.sigs.k8s.io/v1alpha2/references/spec/#gateway.networking.k8s.io/v1beta1.HTTPRouteSpec
	// +optional
	Matches []gatewayapiv1beta1.HTTPRouteMatch `json:"matches,omitempty"`
}

// SelectRules returns, from a HTTPRoute, all HTTPRouteRules that either specify no HTTRouteMatches or that contain at
// least one HTTRouteMatch whose statements expressly include (partially or totally) the statements of at least one of
// the matches of the selector. If the selector does not specify any matches, then all HTTPRouteRules are selected.
//
// Additionally, if the selector specifies a non-empty list of hostnames, a non-empty intersection between the literal
// hostnames of the selector and set of hostnames specified in the HTTPRoute must be exist. Otherwise, the function
// returns nil.
func (s *RouteSelector) SelectRules(route *gatewayapiv1beta1.HTTPRoute) []gatewayapiv1beta1.HTTPRouteRule {
	rulesIndices := make(map[int]gatewayapiv1beta1.HTTPRouteRule, 0) // using a map is an easy way to avoid repeated rules but it may not preserve the order
	if len(s.Hostnames) > 0 && !common.Intersect(s.Hostnames, route.Spec.Hostnames) {
		return nil
	}
	if len(s.Matches) == 0 {
		return route.Spec.Rules
	}
	for _, routeSelectorMatch := range s.Matches {
		for idx, rule := range route.Spec.Rules {
			rs := common.HTTPRouteRuleSelector{HTTPRouteMatch: &routeSelectorMatch}
			if rs.Selects(rule) {
				rulesIndices[idx] = rule
			}
		}
	}
	return common.MapValues(rulesIndices)
}
