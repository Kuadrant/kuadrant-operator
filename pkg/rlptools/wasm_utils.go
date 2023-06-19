package rlptools

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

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

// WasmRules computes WASM rules from the policy and the targeted Route (which can be nil when a gateway is targeted)
func WasmRules(rlp *kuadrantv1beta2.RateLimitPolicy, route *gatewayapiv1beta1.HTTPRoute) []wasm.Rule {
	rules := make([]wasm.Rule, 0)
	if rlp == nil {
		return rules
	}

	for limitName, limit := range rlp.Spec.Limits {
		// 1 RLP limit <---> 1 WASM rule
		limitFullName := FullLimitName(rlp, limitName)
		rule := ruleFromLimit(limitFullName, &limit, route)

		rules = append(rules, rule)
	}

	return rules
}

func ruleFromLimit(limitFullName string, limit *kuadrantv1beta2.Limit, route *gatewayapiv1beta1.HTTPRoute) wasm.Rule {
	if limit == nil {
		return wasm.Rule{}
	}

	return wasm.Rule{
		Conditions: conditionsFromLimit(limit, route),
		Data:       dataFromLimt(limitFullName, limit),
	}
}

func conditionsFromLimit(limit *kuadrantv1beta2.Limit, route *gatewayapiv1beta1.HTTPRoute) []wasm.Condition {
	if limit == nil {
		return make([]wasm.Condition, 0)
	}

	// TODO(eastizle): review this implementation. This is a first naive implementation.
	// The conditions must always be a subset of the route's matching rules.

	conditions := make([]wasm.Condition, 0)

	for routeSelectorIdx := range limit.RouteSelectors {
		// TODO(eastizle): what if there are only Hostnames (i.e. empty "matches" list)
		for matchIdx := range limit.RouteSelectors[routeSelectorIdx].Matches {
			condition := wasm.Condition{
				AllOf: patternExpresionsFromMatch(&limit.RouteSelectors[routeSelectorIdx].Matches[matchIdx]),
			}

			// merge hostnames expression in the same condition
			for _, hostname := range limit.RouteSelectors[routeSelectorIdx].Hostnames {
				condition.AllOf = append(condition.AllOf, patternExpresionFromHostname(hostname))
			}

			conditions = append(conditions, condition)
		}
	}

	if len(conditions) == 0 {
		conditions = append(conditions, conditionsFromRoute(route)...)
	}

	// merge when expression in the same condition
	// must be done after adding route level conditions when no route selector are available
	// prevent conditions only filled with "when" definitions
	for whenIdx := range limit.When {
		for idx := range conditions {
			conditions[idx].AllOf = append(conditions[idx].AllOf, patternExpresionFromWhen(limit.When[whenIdx]))
		}
	}

	return conditions
}

func conditionsFromRoute(route *gatewayapiv1beta1.HTTPRoute) []wasm.Condition {
	if route == nil {
		return make([]wasm.Condition, 0)
	}

	conditions := make([]wasm.Condition, 0)

	for ruleIdx := range route.Spec.Rules {
		// One condition per match
		for matchIdx := range route.Spec.Rules[ruleIdx].Matches {
			conditions = append(conditions, wasm.Condition{
				AllOf: patternExpresionsFromMatch(&route.Spec.Rules[ruleIdx].Matches[matchIdx]),
			})
		}
	}

	return conditions
}

func patternExpresionsFromMatch(match *gatewayapiv1beta1.HTTPRouteMatch) []wasm.PatternExpression {
	// TODO(eastizle): only paths and methods implemented

	if match == nil {
		return make([]wasm.PatternExpression, 0)
	}

	expressions := make([]wasm.PatternExpression, 0)

	if match.Path != nil {
		expressions = append(expressions, patternExpresionFromPathMatch(*match.Path))
	}

	if match.Method != nil {
		expressions = append(expressions, patternExpresionFromMethod(*match.Method))
	}

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

func patternExpresionFromWhen(when kuadrantv1beta2.WhenCondition) wasm.PatternExpression {
	return wasm.PatternExpression{
		Selector: string(when.Selector),
		Operator: wasm.PatternOperator(when.Operator),
		Value:    when.Value,
	}
}

func patternExpresionFromHostname(hostname gatewayapiv1beta1.Hostname) wasm.PatternExpression {
	return wasm.PatternExpression{
		Selector: "request.host",
		Operator: "eq",
		Value:    string(hostname),
	}
}

func dataFromLimt(limitFullName string, limit *kuadrantv1beta2.Limit) []wasm.DataItem {
	if limit == nil {
		return make([]wasm.DataItem, 0)
	}

	data := make([]wasm.DataItem, 0)

	// static key representing the limit
	data = append(data, wasm.DataItem{Static: &wasm.StaticSpec{Key: limitFullName, Value: "1"}})

	for _, counter := range limit.Counters {
		data = append(data, wasm.DataItem{Selector: &wasm.SelectorSpec{Selector: string(counter)}})
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
