package rlptools

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	_struct "github.com/golang/protobuf/ptypes/struct"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

var (
	WASMFilterImageURL = common.FetchEnv("RELATED_IMAGE_WASMSHIM", "oci://quay.io/kuadrant/wasm-shim:latest")
)

// WasmRules computes WASM rules from the policy and the targeted route.
// Pass a list of target hostnames to ensure that only rules those are computed;
// otherwise, the hostnames specified in the route will set the boundaries for the rules.
// This is useful to compute rules for a specific gateway.
func WasmRules(rlp *kuadrantv1beta2.RateLimitPolicy, route *gatewayapiv1beta1.HTTPRoute, targetHostnames []gatewayapiv1beta1.Hostname) []wasm.Rule {
	rules := make([]wasm.Rule, 0)
	if rlp == nil {
		return rules
	}

	if len(targetHostnames) == 0 {
		targetHostnames = route.Spec.Hostnames
	}

	for limitName, limit := range rlp.Spec.Limits {
		// 1 RLP limit <---> 1 WASM rule
		limitFullName := FullLimitName(rlp, limitName)
		rule, err := ruleFromLimit(limitFullName, &limit, route, targetHostnames)
		if err == nil {
			rules = append(rules, rule)
		} else {
			// TODO(guicassolato): log the error
		}
	}

	return rules
}

func ruleFromLimit(limitFullName string, limit *kuadrantv1beta2.Limit, route *gatewayapiv1beta1.HTTPRoute, targetHostnames []gatewayapiv1beta1.Hostname) (wasm.Rule, error) {
	rule := wasm.Rule{}

	if conditions, err := conditionsFromLimit(limit, route, targetHostnames); err != nil {
		return rule, err
	} else {
		rule.Conditions = conditions
	}

	if data := dataFromLimt(limitFullName, limit); data != nil {
		rule.Data = data
	}

	return rule, nil
}

func conditionsFromLimit(limit *kuadrantv1beta2.Limit, route *gatewayapiv1beta1.HTTPRoute, targetHostnames []gatewayapiv1beta1.Hostname) ([]wasm.Condition, error) {
	if limit == nil {
		return nil, errors.New("limit should not be nil")
	}

	routeConditions := make([]wasm.Condition, 0)

	if len(limit.RouteSelectors) > 0 {
		// build conditions from the rules selected by the route selectors
		for _, routeSelector := range limit.RouteSelectors {
			hostnamesForConditions := hostnamesForConditions(targetHostnames, route, &routeSelector)
			for _, rule := range HTTPRouteRulesFromRouteSelector(routeSelector, route, targetHostnames) {
				routeConditions = append(routeConditions, conditionsFromRule(rule, hostnamesForConditions)...)
			}
		}
		if len(routeConditions) == 0 {
			return nil, errors.New("cannot match any route rules, check for invalid route selectors in the policy")
		}
	} else {
		// build conditions from the route if no route selectors are defined
		for _, rule := range route.Spec.Rules {
			routeConditions = append(routeConditions, conditionsFromRule(rule, hostnamesForConditions(targetHostnames, route, nil))...)
		}
	}

	if len(limit.When) == 0 {
		return routeConditions, nil
	}

	if len(routeConditions) > 0 {
		// merge the 'when' conditions into each route level one
		mergedConditions := make([]wasm.Condition, len(routeConditions))
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
	whenConditions := make([]wasm.Condition, len(limit.When))
	for idx, when := range limit.When {
		whenConditions[idx] = wasm.Condition{AllOf: []wasm.PatternExpression{patternExpresionFromWhen(when)}}
	}
	return whenConditions, nil
}

// hostnamesForConditions allows avoiding building conditions for hostnames that are excluded by the selector
// or when the hostname is irrelevant (i.e. matches all hostnames)
func hostnamesForConditions(targetHostnames []gatewayapiv1beta1.Hostname, route *gatewayapiv1beta1.HTTPRoute, routeSelector *kuadrantv1beta2.RouteSelector) []gatewayapiv1beta1.Hostname {
	hostnames := targetHostnames

	if routeSelector != nil && len(routeSelector.Hostnames) > 0 {
		hostnames = common.Intersection(routeSelector.Hostnames, hostnames)
	}

	if common.SameElements(hostnames, targetHostnames) {
		return []gatewayapiv1beta1.Hostname{"*"}
	}

	return hostnames
}

// conditionsFromRule builds a list of conditions from a rule and a list of hostnames
// each combination of a rule match and hostname yields one condition
// rules that specify no explicit match are assumed to match all request (i.e. implicit catch-all rule)
// empty list of hostnames yields a condition without a hostname pattern expression
func conditionsFromRule(rule gatewayapiv1beta1.HTTPRouteRule, hostnames []gatewayapiv1beta1.Hostname) (conditions []wasm.Condition) {
	if len(rule.Matches) == 0 {
		for _, hostname := range hostnames {
			if hostname == "*" {
				continue
			}
			condition := wasm.Condition{AllOf: []wasm.PatternExpression{patternExpresionFromHostname(hostname)}}
			conditions = append(conditions, condition)
		}
		return
	}

	for _, match := range rule.Matches {
		condition := wasm.Condition{AllOf: patternExpresionsFromMatch(match)}

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

func patternExpresionsFromMatch(match gatewayapiv1beta1.HTTPRouteMatch) []wasm.PatternExpression {
	expressions := make([]wasm.PatternExpression, 0)

	if match.Path != nil {
		expressions = append(expressions, patternExpresionFromPathMatch(*match.Path))
	}

	if match.Method != nil {
		expressions = append(expressions, patternExpresionFromMethod(*match.Method))
	}

	// TODO(eastizle): only paths and methods implemented

	return expressions
}

func patternExpresionFromPathMatch(pathMatch gatewayapiv1beta1.HTTPPathMatch) wasm.PatternExpression {

	var (
		operator wasm.PatternOperator = wasm.PatternOperator(kuadrantv1beta2.StartsWithOperator) // default value
		value    string               = "/"                                                      // default value
	)

	if pathMatch.Value != nil {
		value = *pathMatch.Value
	}

	if pathMatch.Type != nil {
		if val, ok := wasm.PathMatchTypeMap[*pathMatch.Type]; ok {
			operator = val
		}
	}

	return wasm.PatternExpression{
		Selector: "request.url_path",
		Operator: operator,
		Value:    value,
	}
}

func patternExpresionFromMethod(method gatewayapiv1beta1.HTTPMethod) wasm.PatternExpression {
	return wasm.PatternExpression{
		Selector: "request.method",
		Operator: wasm.PatternOperator(kuadrantv1beta2.EqualOperator),
		Value:    string(method),
	}
}

func patternExpresionFromHostname(hostname gatewayapiv1beta1.Hostname) wasm.PatternExpression {
	value := string(hostname)
	operator := "eq"
	if strings.HasPrefix(value, "*.") {
		operator = "endswith"
		value = value[1:]
	}
	return wasm.PatternExpression{
		Selector: "request.host",
		Operator: wasm.PatternOperator(operator),
		Value:    string(value),
	}
}

func patternExpresionFromWhen(when kuadrantv1beta2.WhenCondition) wasm.PatternExpression {
	return wasm.PatternExpression{
		Selector: when.Selector,
		Operator: wasm.PatternOperator(when.Operator),
		Value:    when.Value,
	}
}

func dataFromLimt(limitFullName string, limit *kuadrantv1beta2.Limit) (data []wasm.DataItem) {
	if limit == nil {
		return
	}

	// static key representing the limit
	data = append(data, wasm.DataItem{Static: &wasm.StaticSpec{Key: limitFullName, Value: "1"}})

	for _, counter := range limit.Counters {
		data = append(data, wasm.DataItem{Selector: &wasm.SelectorSpec{Selector: counter}})
	}

	return data
}

func WASMPluginFromStruct(structure *_struct.Struct) (*wasm.WASMPlugin, error) {
	if structure == nil {
		return nil, errors.New("cannot desestructure WASMPlugin from nil")
	}
	// Serialize struct into json
	configJSON, err := structure.MarshalJSON()
	if err != nil {
		return nil, err
	}
	// Deserialize struct into PluginConfig struct
	wasmPlugin := &wasm.WASMPlugin{}
	if err := json.Unmarshal(configJSON, wasmPlugin); err != nil {
		return nil, err
	}

	return wasmPlugin, nil
}

type WasmRulesByDomain map[string][]wasm.Rule

func WASMPluginMutator(existingObj, desiredObj client.Object) (bool, error) {
	update := false
	existing, ok := existingObj.(*istioclientgoextensionv1alpha1.WasmPlugin)
	if !ok {
		return false, fmt.Errorf("%T is not a *istioclientgoextensionv1alpha1.WasmPlugin", existingObj)
	}
	desired, ok := desiredObj.(*istioclientgoextensionv1alpha1.WasmPlugin)
	if !ok {
		return false, fmt.Errorf("%T is not a *istioclientgoextensionv1alpha1.WasmPlugin", desiredObj)
	}

	existingWASMPlugin, err := WASMPluginFromStruct(existing.Spec.PluginConfig)
	if err != nil {
		return false, err
	}

	desiredWASMPlugin, err := WASMPluginFromStruct(desired.Spec.PluginConfig)
	if err != nil {
		return false, err
	}

	// TODO(eastizle): reflect.DeepEqual does not work well with lists without order
	if !reflect.DeepEqual(desiredWASMPlugin, existingWASMPlugin) {
		update = true
		existing.Spec.PluginConfig = desired.Spec.PluginConfig
	}

	return update, nil
}
