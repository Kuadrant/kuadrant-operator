package wasm

import (
	"encoding/json"

	_struct "github.com/golang/protobuf/ptypes/struct"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
)

var (
	PathMatchTypeMap = map[gatewayapiv1beta1.PathMatchType]PatternOperator{
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
