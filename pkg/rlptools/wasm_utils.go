package rlptools

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	_struct "google.golang.org/protobuf/types/known/structpb"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	"k8s.io/utils/env"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

var (
	WASMFilterImageURL = env.GetString("RELATED_IMAGE_WASMSHIM", "oci://quay.io/kuadrant/wasm-shim:latest")
)

// WasmRules computes WASM rules from the policy and the targeted route.
// It returns an empty list of wasm rules if the policy specifies no limits or if all limits specified in the policy
// fail to match any route rule according to the limits route selectors.
func WasmRules(rlp *kuadrantv1beta2.RateLimitPolicy, route *gatewayapiv1.HTTPRoute) []wasm.Rule {
	rules := make([]wasm.Rule, 0)
	if rlp == nil {
		return rules
	}

	for limitName := range rlp.Spec.Limits {
		// 1 RLP limit <---> 1 WASM rule
		limit := rlp.Spec.Limits[limitName]
		limitIdentifier := LimitNameToLimitadorIdentifier(limitName)
		rule, err := ruleFromLimit(limitIdentifier, &limit, route)
		if err == nil {
			rules = append(rules, rule)
		}
	}

	return rules
}

func ruleFromLimit(limitIdentifier string, limit *kuadrantv1beta2.Limit, route *gatewayapiv1.HTTPRoute) (wasm.Rule, error) {
	rule := wasm.Rule{}

	conditions, err := conditionsFromLimit(limit, route)
	if err != nil {
		return rule, err
	}

	rule.Conditions = conditions

	if data := dataFromLimt(limitIdentifier, limit); data != nil {
		rule.Data = data
	}

	return rule, nil
}

func conditionsFromLimit(limit *kuadrantv1beta2.Limit, route *gatewayapiv1.HTTPRoute) ([]wasm.Condition, error) {
	if limit == nil {
		return nil, errors.New("limit should not be nil")
	}

	routeConditions := make([]wasm.Condition, 0)

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

// conditionsFromRule builds a list of conditions from a rule and a list of hostnames
// each combination of a rule match and hostname yields one condition
// rules that specify no explicit match are assumed to match all request (i.e. implicit catch-all rule)
// empty list of hostnames yields a condition without a hostname pattern expression
func conditionsFromRule(rule gatewayapiv1.HTTPRouteRule, hostnames []gatewayapiv1.Hostname) (conditions []wasm.Condition) {
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

func patternExpresionsFromMatch(match gatewayapiv1.HTTPRouteMatch) []wasm.PatternExpression {
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

func patternExpresionFromPathMatch(pathMatch gatewayapiv1.HTTPPathMatch) wasm.PatternExpression {
	var (
		operator = wasm.PatternOperator(kuadrantv1beta2.StartsWithOperator) // default value
		value    = "/"                                                      // default value
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

func patternExpresionFromMethod(method gatewayapiv1.HTTPMethod) wasm.PatternExpression {
	return wasm.PatternExpression{
		Selector: "request.method",
		Operator: wasm.PatternOperator(kuadrantv1beta2.EqualOperator),
		Value:    string(method),
	}
}

func patternExpresionFromHostname(hostname gatewayapiv1.Hostname) wasm.PatternExpression {
	value := string(hostname)
	operator := "eq"
	if strings.HasPrefix(value, "*.") {
		operator = "endswith"
		value = value[1:]
	}
	return wasm.PatternExpression{
		Selector: "request.host",
		Operator: wasm.PatternOperator(operator),
		Value:    value,
	}
}

func patternExpresionFromWhen(when kuadrantv1beta2.WhenCondition) wasm.PatternExpression {
	return wasm.PatternExpression{
		Selector: when.Selector,
		Operator: wasm.PatternOperator(when.Operator),
		Value:    when.Value,
	}
}

func dataFromLimt(limitIdentifier string, limit *kuadrantv1beta2.Limit) (data []wasm.DataItem) {
	if limit == nil {
		return
	}

	// static key representing the limit
	data = append(data, wasm.DataItem{Static: &wasm.StaticSpec{Key: limitIdentifier, Value: "1"}})

	for _, counter := range limit.Counters {
		data = append(data, wasm.DataItem{Selector: &wasm.SelectorSpec{Selector: counter}})
	}

	return data
}

func WASMPluginFromStruct(structure *_struct.Struct) (*wasm.Plugin, error) {
	if structure == nil {
		return nil, errors.New("cannot desestructure WASMPlugin from nil")
	}
	// Serialize struct into json
	configJSON, err := structure.MarshalJSON()
	if err != nil {
		return nil, err
	}
	// Deserialize struct into PluginConfig struct
	wasmPlugin := &wasm.Plugin{}
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
