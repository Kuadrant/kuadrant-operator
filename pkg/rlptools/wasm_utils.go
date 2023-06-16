package rlptools

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	_struct "github.com/golang/protobuf/ptypes/struct"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

var (
	WASMFilterImageURL = common.FetchEnv("RELATED_IMAGE_WASMSHIM", "oci://quay.io/kuadrant/wasm-shim:latest")

	PathMatchTypeMap = map[gatewayapiv1alpha2.PathMatchType]PatternOperator{
		gatewayapiv1beta1.PathMatchExact:             PatternOperator(kuadrantv1beta2.EqualOperator),
		gatewayapiv1beta1.PathMatchPathPrefix:        PatternOperator(kuadrantv1beta2.StartsWithOperator),
		gatewayapiv1beta1.PathMatchRegularExpression: PatternOperator(kuadrantv1beta2.MatchesOperator),
	}
)

type SelectorSpec struct {
	// Selector of an attribute from the contextual properties provided by Envoy
	// during request and connection processing
	// https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/attributes
	// They are named by a dot-separated path (e.g. request.path)
	// Examples:
	// "request.path" -> The path portion of the URL
	Selector string `json:"selector"`

	// If not set it defaults to `selector` field value as the descriptor key.
	// +optional
	Key *string `json:"key,omitempty"`

	// An optional value to use if the selector is not found in the context.
	// If not set and the selector is not found in the context, then no descriptor is generated.
	// +optional
	Default *string `json:"default,omitempty"`
}

type StaticSpec struct {
	Value string `json:"value"`
	Key   string `json:"key"`
}

// TODO implement one of constraint
// Precisely one of "static", "selector" must be set.
type DataItem struct {
	// +optional
	Static *StaticSpec `json:"static,omitempty"`

	// +optional
	Selector *SelectorSpec `json:"selector,omitempty"`
}

type PatternOperator kuadrantv1beta2.WhenConditionOperator

type PatternExpression struct {
	// Selector of an attribute from the contextual properties provided by Envoy
	// during request and connection processing
	// https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/attributes
	// They are named by a dot-separated path (e.g. request.path)
	// Examples:
	// "request.path" -> The path portion of the URL
	Selector string `json:"selector"`

	// The binary operator to be applied to the content fetched from context, for comparison with "value".
	// Possible values are: "eq" (equal to), "neq" (not equal to), "incl" (includes; for arrays), "excl" (excludes; for arrays), "matches" (regex)
	// TODO build comprehensive list of operators
	Operator PatternOperator `json:"operator"`

	// The value of reference for the comparison with the content fetched from the context.
	Value string `json:"value"`
}

// Condition defines traffic matching rules
type Condition struct {
	// All of the expressions defined must match to match this condition
	// +optional
	AllOf []PatternExpression `json:"allOf,omitempty"`
}

// Rule defines one rate limit configuration. When conditions are met,
// it uses `data` section to generate one RLS descriptor.
type Rule struct {
	// +optional
	Conditions []Condition `json:"conditions,omitempty"`
	// +optional
	Data []DataItem `json:"data,omitempty"`
}

type RateLimitPolicy struct {
	Name      string   `json:"name"`
	Domain    string   `json:"domain"`
	Service   string   `json:"service"`
	Hostnames []string `json:"hostnames"`

	// +optional
	Rules []Rule `json:"rules,omitempty"`
}

// +kubebuilder:validation:Enum:=deny;allow
type FailureModeType string

const (
	FailureModeDeny  FailureModeType = "deny"
	FailureModeAllow FailureModeType = "allow"
)

type WASMPlugin struct {
	FailureMode       FailureModeType   `json:"failureMode"`
	RateLimitPolicies []RateLimitPolicy `json:"rateLimitPolicies"`
}

func (w *WASMPlugin) ToStruct() (*_struct.Struct, error) {
	wasmPluginJSON, err := json.Marshal(w)
	if err != nil {
		return nil, err
	}

	pluginConfigStruct := &_struct.Struct{}
	if err := pluginConfigStruct.UnmarshalJSON(wasmPluginJSON); err != nil {
		return nil, err
	}
	return pluginConfigStruct, nil
}

// WasmRules computes WASM rules from the policy and the targeted Route (which can be nil when a gateway is targeted)
func WasmRules(rlp *kuadrantv1beta2.RateLimitPolicy, route *gatewayapiv1alpha2.HTTPRoute) []Rule {
	rules := make([]Rule, 0)
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

func ruleFromLimit(limitFullName string, limit *kuadrantv1beta2.Limit, route *gatewayapiv1alpha2.HTTPRoute) Rule {
	if limit == nil {
		return Rule{}
	}

	return Rule{
		Conditions: conditionsFromLimit(limit, route),
		Data:       dataFromLimt(limitFullName, limit),
	}
}

func conditionsFromLimit(limit *kuadrantv1beta2.Limit, route *gatewayapiv1alpha2.HTTPRoute) []Condition {
	if limit == nil {
		return make([]Condition, 0)
	}

	// TODO(eastizle): review this implementation. This is a first naive implementation.
	// The conditions must always be a subset of the route's matching rules.

	conditions := make([]Condition, 0)

	for routeSelectorIdx := range limit.RouteSelectors {
		// TODO(eastizle): what if there are only Hostnames (i.e. empty "matches" list)
		for matchIdx := range limit.RouteSelectors[routeSelectorIdx].Matches {
			condition := Condition{
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

func conditionsFromRoute(route *gatewayapiv1alpha2.HTTPRoute) []Condition {
	if route == nil {
		return make([]Condition, 0)
	}

	conditions := make([]Condition, 0)

	for ruleIdx := range route.Spec.Rules {
		// One condition per match
		for matchIdx := range route.Spec.Rules[ruleIdx].Matches {
			conditions = append(conditions, Condition{
				AllOf: patternExpresionsFromMatch(&route.Spec.Rules[ruleIdx].Matches[matchIdx]),
			})
		}
	}

	return conditions
}

func patternExpresionsFromMatch(match *gatewayapiv1alpha2.HTTPRouteMatch) []PatternExpression {
	// TODO(eastizle): only paths and methods implemented

	if match == nil {
		return make([]PatternExpression, 0)
	}

	expressions := make([]PatternExpression, 0)

	if match.Path != nil {
		expressions = append(expressions, patternExpresionFromPathMatch(*match.Path))
	}

	if match.Method != nil {
		expressions = append(expressions, patternExpresionFromMethod(*match.Method))
	}

	return expressions
}

func patternExpresionFromPathMatch(pathMatch gatewayapiv1alpha2.HTTPPathMatch) PatternExpression {

	var (
		operator PatternOperator = PatternOperator(kuadrantv1beta2.StartsWithOperator) // default value
		value    string          = "/"                                                 // default value
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

func patternExpresionFromMethod(method gatewayapiv1alpha2.HTTPMethod) PatternExpression {
	return PatternExpression{
		Selector: "request.method",
		Operator: PatternOperator(kuadrantv1beta2.EqualOperator),
		Value:    string(method),
	}
}

func patternExpresionFromWhen(when kuadrantv1beta2.WhenCondition) PatternExpression {
	return PatternExpression{
		Selector: string(when.Selector),
		Operator: PatternOperator(when.Operator),
		Value:    when.Value,
	}
}

func patternExpresionFromHostname(hostname gatewayapiv1alpha2.Hostname) PatternExpression {
	return PatternExpression{
		Selector: "request.host",
		Operator: "eq",
		Value:    string(hostname),
	}
}

func dataFromLimt(limitFullName string, limit *kuadrantv1beta2.Limit) []DataItem {
	if limit == nil {
		return make([]DataItem, 0)
	}

	data := make([]DataItem, 0)

	// static key representing the limit
	data = append(data, DataItem{Static: &StaticSpec{Key: limitFullName, Value: "1"}})

	for _, counter := range limit.Counters {
		data = append(data, DataItem{Selector: &SelectorSpec{Selector: string(counter)}})
	}

	return data
}

func WASMPluginFromStruct(structure *_struct.Struct) (*WASMPlugin, error) {
	if structure == nil {
		return nil, errors.New("cannot desestructure WASMPlugin from nil")
	}
	// Serialize struct into json
	configJSON, err := structure.MarshalJSON()
	if err != nil {
		return nil, err
	}
	// Deserialize struct into PluginConfig struct
	wasmPlugin := &WASMPlugin{}
	if err := json.Unmarshal(configJSON, wasmPlugin); err != nil {
		return nil, err
	}

	return wasmPlugin, nil
}

type WasmRulesByDomain map[string][]Rule

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
