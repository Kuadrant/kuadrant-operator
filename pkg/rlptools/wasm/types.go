package wasm

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/samber/lo"
	_struct "google.golang.org/protobuf/types/known/structpb"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
)

var (
	PathMatchTypeMap = map[gatewayapiv1.PathMatchType]PatternOperator{
		gatewayapiv1.PathMatchExact:             PatternOperator(kuadrantv1beta3.EqualOperator),
		gatewayapiv1.PathMatchPathPrefix:        PatternOperator(kuadrantv1beta3.StartsWithOperator),
		gatewayapiv1.PathMatchRegularExpression: PatternOperator(kuadrantv1beta3.MatchesOperator),
	}
)

type SelectorSpec struct {
	// Selector of an attribute from the contextual properties provided by kuadrant
	// during request and connection processing
	Selector kuadrantv1beta3.ContextSelector `json:"selector"`

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

type Static struct {
	Static StaticSpec `json:"static"`
}

type Selector struct {
	Selector SelectorSpec `json:"selector"`
}

type DataType struct {
	Value interface{}
}

func (d *DataType) UnmarshalJSON(data []byte) error {
	// Precisely one of "static", "selector" must be set.
	types := []interface{}{
		&Static{},
		&Selector{},
	}

	var err error

	for idx := range types {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields() // Force errors
		err = dec.Decode(types[idx])
		if err == nil {
			d.Value = types[idx]
			return nil
		}
	}

	return err
}

func (d *DataType) MarshalJSON() ([]byte, error) {
	switch val := d.Value.(type) {
	case *Static:
		return json.Marshal(val)
	case *Selector:
		return json.Marshal(val)
	default:
		return nil, errors.New("DataType.Value has unknown type")
	}
}

func (d *DataType) EqualTo(other DataType) bool {
	dt, err := d.MarshalJSON()
	if err != nil {
		return false
	}
	odt, err := other.MarshalJSON()
	if err != nil {
		return false
	}
	return bytes.Equal(dt, odt)
}

type PatternOperator kuadrantv1beta3.WhenConditionOperator

type PatternExpression struct {
	// Selector of an attribute from the contextual properties provided by kuadrant
	// during request and connection processing
	Selector kuadrantv1beta3.ContextSelector `json:"selector"`

	// The binary operator to be applied to the content fetched from context, for comparison with "value".
	// Possible values are: "eq" (equal to), "neq" (not equal to), "incl" (includes; for arrays), "excl" (excludes; for arrays), "matches" (regex)
	// TODO build comprehensive list of operators
	Operator PatternOperator `json:"operator"`

	// The value of reference for the comparison with the content fetched from the context.
	Value string `json:"value"`
}

func (p *PatternExpression) EqualTo(other PatternExpression) bool {
	return p.Selector == other.Selector &&
		p.Operator == other.Operator &&
		p.Value == other.Value
}

type Condition struct {
	// All the expressions defined must match to match this rule
	// +optional
	AllOf []PatternExpression `json:"allOf,omitempty"`
}

func (c *Condition) EqualTo(other Condition) bool {
	return len(c.AllOf) == len(other.AllOf) &&
		lo.EveryBy(c.AllOf, func(expression PatternExpression) bool {
			return lo.ContainsBy(other.AllOf, expression.EqualTo)
		})
}

type Rule struct {
	// Top level conditions for the rule. At least one of the conditions must be met.
	// Empty conditions evaluate to true, so actions will be invoked.
	// +optional
	Conditions []Condition `json:"conditions,omitempty"`

	// Actions defines which extensions will be invoked when any of the top level conditions match.
	Actions []Action `json:"actions"`
}

func (r *Rule) EqualTo(other Rule) bool {
	return len(r.Conditions) == len(other.Conditions) &&
		len(r.Actions) == len(other.Actions) &&
		lo.EveryBy(r.Conditions, func(condition Condition) bool {
			return lo.ContainsBy(other.Conditions, condition.EqualTo)
		}) &&
		lo.EveryBy(r.Actions, func(action Action) bool {
			return lo.ContainsBy(other.Actions, action.EqualTo)
		})
}

type Policy struct {
	Name      string   `json:"name"`
	Hostnames []string `json:"hostnames"`

	// Rules includes top level conditions and actions to be invoked
	// +optional
	Rules []Rule `json:"rules,omitempty"`
}

func (p *Policy) EqualTo(other Policy) bool {
	return p.Name == other.Name &&
		len(p.Hostnames) == len(other.Hostnames) &&
		len(p.Rules) == len(other.Rules) &&
		lo.Every(p.Hostnames, other.Hostnames) &&
		lo.EveryBy(p.Rules, func(rule Rule) bool {
			return lo.ContainsBy(other.Rules, rule.EqualTo)
		})
}

type Action struct {
	Scope         string `json:"scope"`
	ExtensionName string `json:"extension"`

	// +optional
	Data []DataType `json:"data,omitempty"`
}

func (a *Action) EqualTo(other Action) bool {
	return a.Scope == other.Scope &&
		a.ExtensionName == other.ExtensionName &&
		len(a.Data) == len(other.Data) &&
		lo.EveryBy(a.Data, func(data DataType) bool {
			return lo.ContainsBy(other.Data, data.EqualTo)
		})
}

// +kubebuilder:validation:Enum:=ratelimit;auth
type ExtensionType string

const (
	RateLimitExtensionType ExtensionType = "ratelimit"
	AuthExtensionType      ExtensionType = "auth"
)

// +kubebuilder:validation:Enum:=deny;allow
type FailureModeType string

const (
	FailureModeDeny  FailureModeType = "deny"
	FailureModeAllow FailureModeType = "allow"
)

type Extension struct {
	Endpoint    string          `json:"endpoint"`
	FailureMode FailureModeType `json:"failureMode"`
	Type        ExtensionType   `json:"type"`
}

type LimitadorExtension struct {
	Endpoint string `json:"endpoint"`
}

type Config struct {
	Extensions map[string]Extension `json:"extensions"`
	Policies   []Policy             `json:"policies"`
}

func (c *Config) ToStruct() (*_struct.Struct, error) {
	configJSON, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}

	configStruct := &_struct.Struct{}
	if err := configStruct.UnmarshalJSON(configJSON); err != nil {
		return nil, err
	}
	return configStruct, nil
}

func (c *Config) ToJSON() (*apiextensionsv1.JSON, error) {
	configJSON, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}

	return &apiextensionsv1.JSON{Raw: configJSON}, nil
}

func (c *Config) EqualTo(other *Config) bool {
	if len(c.Extensions) != len(other.Extensions) || len(c.Policies) != len(other.Policies) {
		return false
	}

	for key, extension := range c.Extensions {
		if otherExtension, ok := other.Extensions[key]; !ok || extension != otherExtension {
			return false
		}
	}

	return lo.EveryBy(c.Policies, func(policy Policy) bool {
		return lo.ContainsBy(other.Policies, policy.EqualTo)
	})
}

func ConfigFromStruct(structure *_struct.Struct) (*Config, error) {
	if structure == nil {
		return nil, errors.New("cannot desestructure config from nil")
	}
	// Serialize struct into json
	configJSON, err := structure.MarshalJSON()
	if err != nil {
		return nil, err
	}
	// Deserialize protobuf struct into Config struct
	config := &Config{}
	if err := json.Unmarshal(configJSON, config); err != nil {
		return nil, err
	}

	return config, nil
}

func ConfigFromJSON(configJSON *apiextensionsv1.JSON) (*Config, error) {
	if configJSON == nil {
		return nil, errors.New("cannot desestructure config from nil")
	}

	config := &Config{}
	if err := json.Unmarshal(configJSON.Raw, config); err != nil {
		return nil, err
	}

	return config, nil
}
