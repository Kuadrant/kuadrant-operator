package rlptools

import (
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

func HTTPRouteRulesFromRouteSelector(routeSelector kuadrantv1beta2.RouteSelector, route *gatewayapiv1beta1.HTTPRoute, hostnames []gatewayapiv1beta1.Hostname) []gatewayapiv1beta1.HTTPRouteRule {
	rulesIndices := make(map[int]gatewayapiv1beta1.HTTPRouteRule, 0)
	if len(hostnames) == 0 {
		hostnames = route.Spec.Hostnames
	}
	if len(routeSelector.Hostnames) > 0 && !common.Intersect(routeSelector.Hostnames, hostnames) {
		return nil
	}
	if len(routeSelector.Matches) == 0 {
		return route.Spec.Rules
	}
	for _, routeSelectorMatch := range routeSelector.Matches {
		for idx, rule := range route.Spec.Rules {
			rs := common.HTTPRouteRuleSelector{HTTPRouteMatch: &routeSelectorMatch}
			if rs.Selects(rule) {
				rulesIndices[idx] = rule
			}
		}
	}
	return common.MapValues(rulesIndices)
}
