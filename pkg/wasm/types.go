package wasm

import (
	"bytes"
	"encoding/json"
	"errors"

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

type Config struct {
	Services   map[string]Service `json:"services"`
	ActionSets []ActionSet        `json:"actionSets"`
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
	if len(c.Services) != len(other.Services) || len(c.ActionSets) != len(other.ActionSets) {
		return false
	}

	for key, service := range c.Services {
		if otherService, ok := other.Services[key]; !ok || service != otherService {
			return false
		}
	}

	for i := range c.ActionSets {
		if !c.ActionSets[i].EqualTo(other.ActionSets[i]) {
			return false
		}
	}

	return true
}

type Service struct {
	Endpoint    string          `json:"endpoint"`
	Type        ServiceType     `json:"type"`
	FailureMode FailureModeType `json:"failureMode"`
	Timeout     *string         `json:"timeout,omitempty"`
}

// +kubebuilder:validation:Enum:=ratelimit;auth
type ServiceType string

const (
	RateLimitServiceType ServiceType = "ratelimit"
	AuthServiceType      ServiceType = "auth"
)

// +kubebuilder:validation:Enum:=deny;allow
type FailureModeType string

const (
	FailureModeDeny  FailureModeType = "deny"
	FailureModeAllow FailureModeType = "allow"
)

type ActionSet struct {
	Name string `json:"name"`

	// Conditions that activate the action set
	RouteRuleConditions RouteRuleConditions `json:"routeRuleConditions,omitempty"`

	// Actions that will be invoked when the conditions are met
	// +optional
	Actions []Action `json:"actions,omitempty"`
}

func (s *ActionSet) EqualTo(other ActionSet) bool {
	if s.Name != other.Name || !s.RouteRuleConditions.EqualTo(other.RouteRuleConditions) || len(s.Actions) != len(other.Actions) {
		return false
	}

	for i := range s.Actions {
		if !s.Actions[i].EqualTo(other.Actions[i]) {
			return false
		}
	}

	return true
}

type RouteRuleConditions struct {
	Hostnames []string    `json:"hostnames"`
	Matches   []Predicate `json:"matches,omitempty"`
}

func (r *RouteRuleConditions) EqualTo(other RouteRuleConditions) bool {
	if len(r.Hostnames) != len(other.Hostnames) || len(r.Matches) != len(other.Matches) {
		return false
	}

	for i := range r.Hostnames {
		if r.Hostnames[i] != other.Hostnames[i] {
			return false
		}
	}

	for i := range r.Matches {
		if !r.Matches[i].EqualTo(other.Matches[i]) {
			return false
		}
	}

	return true
}

type Predicate struct {
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

func (p *Predicate) EqualTo(other Predicate) bool {
	return p.Selector == other.Selector &&
		p.Operator == other.Operator &&
		p.Value == other.Value
}

type PatternOperator kuadrantv1beta3.WhenConditionOperator

type Action struct {
	ServiceName string `json:"service"`
	Scope       string `json:"scope"`

	// Conditions that activate the action
	Conditions []Predicate `json:"conditions,omitempty"`

	// Data to be sent to the service
	// +optional
	Data []DataType `json:"data,omitempty"`
}

func (a *Action) EqualTo(other Action) bool {
	if a.Scope != other.Scope || a.ServiceName != other.ServiceName || len(a.Conditions) != len(other.Conditions) || len(a.Data) != len(other.Data) {
		return false
	}

	for i := range a.Conditions {
		if !a.Conditions[i].EqualTo(other.Conditions[i]) {
			return false
		}
	}

	for i := range a.Data {
		if !a.Data[i].EqualTo(other.Data[i]) {
			return false
		}
	}

	return true
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

type Static struct {
	Static StaticSpec `json:"static"`
}

type Selector struct {
	Selector SelectorSpec `json:"selector"`
}

type StaticSpec struct {
	Value string `json:"value"`
	Key   string `json:"key"`
}

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
