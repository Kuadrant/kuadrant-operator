package wasm

import (
	"bytes"
	"encoding/json"
	"errors"

	_struct "google.golang.org/protobuf/types/known/structpb"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
)

var (
	PathMatchTypeMap = map[gatewayapiv1.PathMatchType]PatternOperator{
		gatewayapiv1.PathMatchExact:             PatternOperator(kuadrantv1beta2.EqualOperator),
		gatewayapiv1.PathMatchPathPrefix:        PatternOperator(kuadrantv1beta2.StartsWithOperator),
		gatewayapiv1.PathMatchRegularExpression: PatternOperator(kuadrantv1beta2.MatchesOperator),
	}
)

type SelectorSpec struct {
	// Selector of an attribute from the contextual properties provided by kuadrant
	// during request and connection processing
	Selector kuadrantv1beta2.ContextSelector `json:"selector"`

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

type PatternOperator kuadrantv1beta2.WhenConditionOperator

type PatternExpression struct {
	// Selector of an attribute from the contextual properties provided by kuadrant
	// during request and connection processing
	Selector kuadrantv1beta2.ContextSelector `json:"selector"`

	// The binary operator to be applied to the content fetched from context, for comparison with "value".
	// Possible values are: "eq" (equal to), "neq" (not equal to), "incl" (includes; for arrays), "excl" (excludes; for arrays), "matches" (regex)
	// TODO build comprehensive list of operators
	Operator PatternOperator `json:"operator"`

	// The value of reference for the comparison with the content fetched from the context.
	Value string `json:"value"`
}

type Condition struct {
	// All the expressions defined must match to match this rule
	// +optional
	AllOf []PatternExpression `json:"allOf,omitempty"`
}

// Rule defines conditions that are evaluated using patter expressions.
// The rule evaluates to true when all the pattern expressions are evaluated to true.
type Rule struct {
	// Top level conditions for the rule. At least one of the conditions must be met.
	// Empty conditions evaluate to true, so actions will be invoked.
	// +optional
	Conditions []Condition `json:"conditions,omitempty"`

	// Actions defines which extensions will be invoked when any of the top level conditions match.
	Actions []Action `json:"actions"`
}

type Policy struct {
	Name      string   `json:"name"`
	Hostnames []string `json:"hostnames"`

	// Rules includes top level conditions and actions to be invoked
	// +optional
	Rules []Rule `json:"rules,omitempty"`
}

type Action struct {
	Scope         string `json:"scope"`
	ExtensionName string `json:"extension"`

	// +optional
	Data []DataType `json:"data,omitempty"`
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

func (w *Config) ToStruct() (*_struct.Struct, error) {
	configJSON, err := json.Marshal(w)
	if err != nil {
		return nil, err
	}

	configStruct := &_struct.Struct{}
	if err := configStruct.UnmarshalJSON(configJSON); err != nil {
		return nil, err
	}
	return configStruct, nil
}

func (w *Config) ToJSON() (*apiextensionsv1.JSON, error) {
	configJSON, err := json.Marshal(w)
	if err != nil {
		return nil, err
	}

	return &apiextensionsv1.JSON{Raw: configJSON}, nil
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
