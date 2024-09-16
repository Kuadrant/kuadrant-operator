package wasm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/env"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const (
	LimitadorRateLimitIdentifierPrefix = "limit."
)

var (
	WASMFilterImageURL = env.GetString("RELATED_IMAGE_WASMSHIM", "oci://quay.io/kuadrant/wasm-shim:latest")
)

func LimitsNamespaceFromRLP(rlp *kuadrantv1beta2.RateLimitPolicy) string {
	return fmt.Sprintf("%s/%s", rlp.GetNamespace(), rlp.GetName())
}

// Rules computes WASM rules from the policy and the targeted route.
// It returns an empty list of wasm rules if the policy specifies no limits or if all limits specified in the policy
// fail to match any route rule according to the limits route selectors.
func Rules(rlp *kuadrantv1beta2.RateLimitPolicy, route *gatewayapiv1.HTTPRoute) []Rule {
	rules := make([]Rule, 0)
	if rlp == nil {
		return rules
	}

	rlpKey := client.ObjectKeyFromObject(rlp)
	limits := rlp.Spec.CommonSpec().Limits

	// Sort RLP limits for consistent comparison with existing wasmplugin objects
	limitNames := lo.Keys(limits)
	slices.Sort(limitNames)

	for _, limitName := range limitNames {
		// 1 RLP limit <---> 1 WASM rule
		limit := limits[limitName]
		limitIdentifier := LimitNameToLimitadorIdentifier(rlpKey, limitName)
		rule, err := ruleFromLimit(limitIdentifier, &limit, route)
		if err == nil {
			rules = append(rules, rule)
		}
	}

	return rules
}

func LimitNameToLimitadorIdentifier(rlpKey types.NamespacedName, uniqueLimitName string) string {
	identifier := LimitadorRateLimitIdentifierPrefix

	// sanitize chars that are not allowed in limitador identifiers
	for _, c := range uniqueLimitName {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' {
			identifier += string(c)
		} else {
			identifier += "_"
		}
	}

	// to avoid breaking the uniqueness of the limit name after sanitization, we add a hash of the original name
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s", rlpKey.String(), uniqueLimitName)))
	identifier += "__" + hex.EncodeToString(hash[:4])

	return identifier
}

func ruleFromLimit(limitIdentifier string, limit *kuadrantv1beta2.Limit, route *gatewayapiv1.HTTPRoute) (Rule, error) {
	rule := Rule{}

	conditions, err := conditionsFromLimit(limit, route)
	if err != nil {
		return rule, err
	}

	rule.Conditions = conditions

	if data := dataFromLimit(limitIdentifier, limit); data != nil {
		rule.Data = data
	}

	return rule, nil
}

func conditionsFromLimit(limit *kuadrantv1beta2.Limit, route *gatewayapiv1.HTTPRoute) ([]Condition, error) {
	if limit == nil {
		return nil, errors.New("limit should not be nil")
	}

	routeConditions := make([]Condition, 0)

	if len(limit.RouteSelectors) > 0 {
		// build conditions from the rules selected by the route selectors
		for idx := range limit.RouteSelectors {
			routeSelector := limit.RouteSelectors[idx]
			hostnamesForConditions := routeSelector.HostnamesForConditions(route)
			for _, rule := range routeSelector.SelectRules(route) {
				routeConditions = append(routeConditions, conditionsFromRule(rule, hostnamesForConditions)...)
			}
		}
		if len(routeConditions) == 0 {
			return nil, errors.New("cannot match any route rules, check for invalid route selectors in the policy")
		}
	} else {
		// build conditions from all rules if no route selectors are defined
		hostnamesForConditions := (&kuadrantv1beta2.RouteSelector{}).HostnamesForConditions(route)
		for _, rule := range route.Spec.Rules {
			routeConditions = append(routeConditions, conditionsFromRule(rule, hostnamesForConditions)...)
		}
	}

	if len(limit.When) == 0 {
		if len(routeConditions) == 0 {
			return nil, nil
		}
		return routeConditions, nil
	}

	if len(routeConditions) > 0 {
		// merge the 'when' conditions into each route level one
		mergedConditions := make([]Condition, len(routeConditions))
		for _, when := range limit.When {
			for idx := range routeConditions {
				mergedCondition := routeConditions[idx]
				mergedCondition.AllOf = append(mergedCondition.AllOf, patternExpresionFromWhen(when))
				mergedConditions[idx] = mergedCondition
			}
		}
		return mergedConditions, nil
	}

	// build conditions only from the 'when' field
	whenConditions := make([]Condition, len(limit.When))
	for idx, when := range limit.When {
		whenConditions[idx] = Condition{AllOf: []PatternExpression{patternExpresionFromWhen(when)}}
	}
	return whenConditions, nil
}

// conditionsFromRule builds a list of conditions from a rule and a list of hostnames
// each combination of a rule match and hostname yields one condition
// rules that specify no explicit match are assumed to match all request (i.e. implicit catch-all rule)
// empty list of hostnames yields a condition without a hostname pattern expression
func conditionsFromRule(rule gatewayapiv1.HTTPRouteRule, hostnames []gatewayapiv1.Hostname) (conditions []Condition) {
	if len(rule.Matches) == 0 {
		for _, hostname := range hostnames {
			if hostname == "*" {
				continue
			}
			condition := Condition{AllOf: []PatternExpression{patternExpresionFromHostname(hostname)}}
			conditions = append(conditions, condition)
		}
		return
	}

	for _, match := range rule.Matches {
		condition := Condition{AllOf: patternExpresionsFromMatch(match)}

		if len(hostnames) > 0 {
			for _, hostname := range hostnames {
				if hostname == "*" {
					conditions = append(conditions, condition)
					continue
				}
				mergedCondition := condition
				mergedCondition.AllOf = append(mergedCondition.AllOf, patternExpresionFromHostname(hostname))
				conditions = append(conditions, mergedCondition)
			}
			continue
		}

		conditions = append(conditions, condition)
	}
	return
}

func patternExpresionsFromMatch(match gatewayapiv1.HTTPRouteMatch) []PatternExpression {
	expressions := make([]PatternExpression, 0)

	// path
	if match.Path != nil {
		expressions = append(expressions, patternExpresionFromPathMatch(*match.Path))
	}

	// method
	if match.Method != nil {
		expressions = append(expressions, patternExpresionFromMethod(*match.Method))
	}

	// headers
	for _, headerMatch := range match.Headers {
		// Multiple match values are ANDed together
		expressions = append(expressions, patternExpresionFromHeader(headerMatch))
	}

	// TODO(eguzki): query params. Investigate integration with wasm regarding Envoy params
	// from https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/attributes
	// request.query -> string : The query portion of the URL in the format of “name1=value1&name2=value2”.

	return expressions
}

func patternExpresionFromPathMatch(pathMatch gatewayapiv1.HTTPPathMatch) PatternExpression {
	var (
		operator = PatternOperator(kuadrantv1beta2.StartsWithOperator) // default value
		value    = "/"                                                 // default value
	)

	if pathMatch.Value != nil {
		value = *pathMatch.Value
	}

	if pathMatch.Type != nil {
		if val, ok := PathMatchTypeMap[*pathMatch.Type]; ok {
			operator = val
		}
	}

	return PatternExpression{
		Selector: "request.url_path",
		Operator: operator,
		Value:    value,
	}
}

func patternExpresionFromMethod(method gatewayapiv1.HTTPMethod) PatternExpression {
	return PatternExpression{
		Selector: "request.method",
		Operator: PatternOperator(kuadrantv1beta2.EqualOperator),
		Value:    string(method),
	}
}

func patternExpresionFromHeader(headerMatch gatewayapiv1.HTTPHeaderMatch) PatternExpression {
	// As for gateway api v1, the only operation type with core support is Exact match.
	// https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPHeaderMatch

	return PatternExpression{
		Selector: kuadrantv1beta2.ContextSelector(fmt.Sprintf("request.headers.%s", headerMatch.Name)),
		Operator: PatternOperator(kuadrantv1beta2.EqualOperator),
		Value:    headerMatch.Value,
	}
}

func patternExpresionFromHostname(hostname gatewayapiv1.Hostname) PatternExpression {
	value := string(hostname)
	operator := "eq"
	if strings.HasPrefix(value, "*.") {
		operator = "endswith"
		value = value[1:]
	}
	return PatternExpression{
		Selector: "request.host",
		Operator: PatternOperator(operator),
		Value:    value,
	}
}

func patternExpresionFromWhen(when kuadrantv1beta2.WhenCondition) PatternExpression {
	return PatternExpression{
		Selector: when.Selector,
		Operator: PatternOperator(when.Operator),
		Value:    when.Value,
	}
}

func dataFromLimit(limitIdentifier string, limit *kuadrantv1beta2.Limit) (data []DataItem) {
	if limit == nil {
		return
	}

	// static key representing the limit
	data = append(data, DataItem{Static: &StaticSpec{Key: limitIdentifier, Value: "1"}})

	for _, counter := range limit.Counters {
		data = append(data, DataItem{Selector: &SelectorSpec{Selector: counter}})
	}

	return data
}

func routeFromRLP(ctx context.Context, t *kuadrantgatewayapi.TopologyIndexes, rlp *kuadrantv1beta2.RateLimitPolicy, gw *gatewayapiv1.Gateway) (*gatewayapiv1.HTTPRoute, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	route := t.GetPolicyHTTPRoute(rlp)

	if route == nil {
		// The policy is targeting a gateway
		// This gateway policy will be enforced into all HTTPRoutes that do not have a policy attached to it

		// Build imaginary route with all the routes not having a RLP targeting it
		untargetedRoutes := t.GetUntargetedRoutes(gw)

		if len(untargetedRoutes) == 0 {
			// For policies targeting a gateway, when no httproutes is attached to the gateway, skip wasm config
			// test wasm config when no http routes attached to the gateway
			logger.V(1).Info("no untargeted httproutes attached to the targeted gateway, skipping wasm config for the gateway rlp", "ratelimitpolicy", client.ObjectKeyFromObject(rlp))
			return nil, nil
		}

		untargetedRules := make([]gatewayapiv1.HTTPRouteRule, 0)
		for idx := range untargetedRoutes {
			untargetedRules = append(untargetedRules, untargetedRoutes[idx].Spec.Rules...)
		}
		gwHostnamesTmp := kuadrantgatewayapi.TargetHostnames(gw)
		gwHostnames := utils.Map(gwHostnamesTmp, func(str string) gatewayapiv1.Hostname { return gatewayapiv1.Hostname(str) })
		route = &gatewayapiv1.HTTPRoute{
			Spec: gatewayapiv1.HTTPRouteSpec{
				Hostnames: gwHostnames,
				Rules:     untargetedRules,
			},
		}
	}

	return route, nil
}

func wasmRateLimitPolicy(ctx context.Context, t *kuadrantgatewayapi.TopologyIndexes, rlp *kuadrantv1beta2.RateLimitPolicy, gw *gatewayapiv1.Gateway) (*RateLimitPolicy, error) {
	route, err := routeFromRLP(ctx, t, rlp, gw)
	if err != nil {
		return nil, err
	}
	if route == nil {
		// no need to add the policy if there are no routes;
		// a rlp can return no rules if all its limits fail to match any route rule
		// or targeting a gateway with no "free" routes. "free" meaning no route with policies targeting it
		return nil, nil
	}

	// narrow the list of hostnames specified in the route so we don't generate wasm rules that only apply to other gateways
	// this is a no-op for the gateway rlp
	gwHostnames := kuadrantgatewayapi.GatewayHostnames(gw)
	if len(gwHostnames) == 0 {
		gwHostnames = []gatewayapiv1.Hostname{"*"}
	}
	hostnames := kuadrantgatewayapi.FilterValidSubdomains(gwHostnames, route.Spec.Hostnames)
	if len(hostnames) == 0 { // it should only happen when the route specifies no hostnames
		hostnames = gwHostnames
	}

	//
	// The route selectors logic rely on the "hostnames" field of the route object.
	// However, routes effective hostname can be inherited from parent gateway,
	// hence it depends on the context as multiple gateways can be targeted by a route
	// The route selectors logic needs to be refactored
	// or just deleted as soon as the HTTPRoute has name in the route object
	//
	routeWithEffectiveHostnames := route.DeepCopy()
	routeWithEffectiveHostnames.Spec.Hostnames = hostnames

	rules := Rules(rlp, routeWithEffectiveHostnames)
	if len(rules) == 0 {
		// no need to add the policy if there are no rules; a rlp can return no rules if all its limits fail to match any route rule
		return nil, nil
	}

	return &RateLimitPolicy{
		Name:      client.ObjectKeyFromObject(rlp).String(),
		Domain:    LimitsNamespaceFromRLP(rlp),
		Hostnames: utils.HostnamesToStrings(hostnames), // we might be listing more hostnames than needed due to route selectors hostnames possibly being more restrictive
		Service:   common.KuadrantRateLimitClusterName,
		Rules:     rules,
	}, nil
}

func ConfigForGateway(
	ctx context.Context, gw *gatewayapiv1.Gateway,
	topology *kuadrantgatewayapi.Topology) (*Config, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	topologyIndex := kuadrantgatewayapi.NewTopologyIndexes(topology)

	rateLimitPolicies := topologyIndex.PoliciesFromGateway(gw)
	logger.V(1).Info("WasmConfig", "#RLPS", len(rateLimitPolicies))

	// Sort RLPs for consistent comparison with existing objects
	sort.Sort(kuadrantgatewayapi.PolicyByTargetRefKindAndCreationTimeStamp(rateLimitPolicies))

	config := &Config{
		FailureMode:       FailureModeDeny,
		RateLimitPolicies: make([]RateLimitPolicy, 0),
	}

	for _, policy := range rateLimitPolicies {
		rlp := policy.(*kuadrantv1beta2.RateLimitPolicy)
		wasmRLP, err := wasmRateLimitPolicy(ctx, topologyIndex, rlp, gw)
		if err != nil {
			return nil, err
		}

		if wasmRLP == nil {
			// skip this RLP
			continue
		}

		config.RateLimitPolicies = append(config.RateLimitPolicies, *wasmRLP)
	}

	return config, nil
}
