package wasm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	_struct "google.golang.org/protobuf/types/known/structpb"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

type Config struct {
	Services          map[string]Service `json:"services"`
	ActionSets        []ActionSet        `json:"actionSets"`
	Observability     *Observability     `json:"observability,omitempty"`
	DescriptorService string             `json:"descriptorService,omitempty"`
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

// EqualTo performs semantic equality comparison between two Config instances.
// This is not a strict equality check - it considers two configs equal if they are
// functionally equivalent, even if some collection orderings differ.
//
// Order-sensitive fields:
//   - ActionSets: Order matters - compared by index position
//   - Services: Map comparison (order doesn't apply)
//   - DescriptorService: String comparison
//   - Observability: Strict equality via nested EqualTo
//
// Note: ActionSets order is significant because it affects the evaluation order
// in the data plane.
func (c *Config) EqualTo(other *Config) bool {
	if other == nil {
		return false
	}

	if len(c.Services) != len(other.Services) || len(c.ActionSets) != len(other.ActionSets) {
		return false
	}

	if c.DescriptorService != other.DescriptorService {
		return false
	}

	for key, service := range c.Services {
		if otherService, ok := other.Services[key]; !ok || !service.EqualTo(otherService) {
			return false
		}
	}

	for i := range c.ActionSets {
		if !c.ActionSets[i].EqualTo(other.ActionSets[i]) {
			return false
		}
	}

	if (c.Observability == nil) != (other.Observability == nil) {
		return false
	}
	if c.Observability != nil && other.Observability != nil && !c.Observability.EqualTo(other.Observability) {
		return false
	}

	return true
}

type Service struct {
	Endpoint    string          `json:"endpoint"`
	Type        ServiceType     `json:"type"`
	FailureMode FailureModeType `json:"failureMode"`
	Timeout     *string         `json:"timeout,omitempty"`
	GrpcService *string         `json:"grpcService,omitempty"`
	GrpcMethod  *string         `json:"grpcMethod,omitempty"`
}

func (s Service) EqualTo(other Service) bool {
	if s.Endpoint != other.Endpoint ||
		s.Type != other.Type ||
		s.FailureMode != other.FailureMode ||
		((s.Timeout == nil) != (other.Timeout == nil)) ||
		(s.Timeout != nil && other.Timeout != nil && *s.Timeout != *other.Timeout) ||
		((s.GrpcService == nil) != (other.GrpcService == nil)) ||
		(s.GrpcService != nil && other.GrpcService != nil && *s.GrpcService != *other.GrpcService) ||
		((s.GrpcMethod == nil) != (other.GrpcMethod == nil)) ||
		(s.GrpcMethod != nil && other.GrpcMethod != nil && *s.GrpcMethod != *other.GrpcMethod) {
		return false
	}
	return true
}

// +kubebuilder:validation:Enum:=ratelimit;auth;ratelimit-check;ratelimit-report;tracing;dynamic
type ServiceType string

const (
	RateLimitServiceType       ServiceType = "ratelimit"
	RateLimitCheckServiceType  ServiceType = "ratelimit-check"
	RateLimitReportServiceType ServiceType = "ratelimit-report"
	AuthServiceType            ServiceType = "auth"
	TracingServiceType         ServiceType = "tracing"
	DynamicServiceType         ServiceType = "dynamic"
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

	// Actions holds the typed pipeline actions for this action set.
	// +optional
	Actions []Action `json:"-"`

	// SourceRoute records which route (Kind/Namespace/Name) this action set was
	// created from. Not serialized — used only to match pipeline actions to the
	// correct action sets at reconcile time.
	SourceRoute string `json:"-"`
}

// actionSetJSON is the intermediate representation used for custom JSON marshaling.
type actionSetJSON struct {
	Name                string              `json:"name"`
	RouteRuleConditions RouteRuleConditions `json:"routeRuleConditions,omitempty"`
	Actions             []json.RawMessage   `json:"actions,omitempty"`
}

func (s ActionSet) MarshalJSON() ([]byte, error) {
	alias := actionSetJSON{
		Name:                s.Name,
		RouteRuleConditions: s.RouteRuleConditions,
	}

	for _, action := range s.Actions {
		raw, err := json.Marshal(action)
		if err != nil {
			return nil, err
		}
		alias.Actions = append(alias.Actions, raw)
	}

	return json.Marshal(alias)
}

func (s *ActionSet) UnmarshalJSON(data []byte) error {
	var alias actionSetJSON
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}

	s.Name = alias.Name
	s.RouteRuleConditions = alias.RouteRuleConditions
	s.Actions = nil

	for _, raw := range alias.Actions {
		action, err := UnmarshalAction(raw)
		if err != nil {
			return err
		}
		s.Actions = append(s.Actions, action)
	}

	return nil
}

// EqualTo performs semantic equality comparison between two ActionSet instances.
//
// Order-sensitive fields:
//   - Name: String comparison
//   - RouteRuleConditions: Uses RouteRuleConditions.EqualTo (which has its own ordering rules)
//   - Actions: Order matters - compared by index position
//
// Note: Actions order is significant because it affects the evaluation order
// in the data plane.
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
	Hostnames []string `json:"hostnames"`

	// +optional
	Predicates []string `json:"predicates,omitempty"`
}

// EqualTo performs semantic equality comparison between two RouteRuleConditions instances.
//
// Order-sensitive fields:
//   - Hostnames: Order matters - compared by index position
//   - Predicates: Order matters - compared by index position
//
// Note: Hostname order is preserved as it may reflect priority or specificity,
// while predicates are order-insensitive as they are evaluated independently.
func (r *RouteRuleConditions) EqualTo(other RouteRuleConditions) bool {
	if len(r.Hostnames) != len(other.Hostnames) || len(r.Predicates) != len(other.Predicates) {
		return false
	}

	for i := range r.Hostnames {
		if r.Hostnames[i] != other.Hostnames[i] {
			return false
		}
	}

	for i := range r.Predicates {
		if r.Predicates[i] != other.Predicates[i] {
			return false
		}
	}

	return true
}

type ConditionalData struct {
	// Predicates holds a list of CEL predicates
	// +optional
	Predicates []string `json:"predicates,omitempty"`

	// Data to be sent to the service
	// +optional
	Data []DataType `json:"data,omitempty"`
}

type DataType struct {
	Value any
}

func (d *DataType) UnmarshalJSON(data []byte) error {
	// Precisely one of "static", "selector" must be set.
	types := []any{
		&Static{},
		&Expression{},
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
	case *Expression:
		return json.Marshal(val)
	default:
		return nil, errors.New("DataType.Value has unknown type")
	}
}

type Static struct {
	Static StaticSpec `json:"static"`
}

type StaticSpec struct {
	Value string `json:"value"`
	Key   string `json:"key"`
}

type ExpressionItem struct {
	// Key holds the expression key
	Key string `json:"key"`

	// Value holds the CEL expression
	Value string `json:"value"`
}

type Expression struct {
	// Data to be sent to the service
	ExpressionItem ExpressionItem `json:"expression"`
}

type ActionKind string

const (
	ActionKindGrpc    ActionKind = "grpc"
	ActionKindDeny    ActionKind = "deny"
	ActionKindHeaders ActionKind = "headers"
	ActionKindStore   ActionKind = "store"
	ActionKindFail    ActionKind = "fail"
)

// Action is the interface for typed pipeline actions in the wasm-shim format.
// Concrete implementations: GrpcAction, DenyAction, HeadersAction, StoreAction, FailAction.
type Action interface {
	ActionType() ActionKind
	Base() *ActionBase
	EqualTo(other Action) bool
	sealedAction()
}

type ActionBase struct {
	Predicate            string   `json:"predicate"`
	Terminal             bool     `json:"terminal"`
	IsGuard              bool     `json:"isGuard"`
	SourcePolicyLocators []string `json:"sources,omitempty"`
}

func (b *ActionBase) equalBase(other *ActionBase) bool {
	return b.Predicate == other.Predicate &&
		b.Terminal == other.Terminal &&
		b.IsGuard == other.IsGuard &&
		slices.Equal(b.SourcePolicyLocators, other.SourcePolicyLocators)
}

// GrpcAction sends a gRPC request to a service and processes the reply.
type GrpcAction struct {
	ActionBase
	Var            string
	Service        string
	Label          string
	MessageBuilder string
	OnReply        []Action
}

func (a *GrpcAction) ActionType() ActionKind { return ActionKindGrpc }
func (a *GrpcAction) Base() *ActionBase      { return &a.ActionBase }
func (a *GrpcAction) sealedAction()          {}
func (a *GrpcAction) EqualTo(other Action) bool {
	o, ok := other.(*GrpcAction)
	if !ok {
		return false
	}
	if !a.equalBase(&o.ActionBase) ||
		a.Var != o.Var ||
		a.Service != o.Service ||
		a.Label != o.Label ||
		a.MessageBuilder != o.MessageBuilder ||
		len(a.OnReply) != len(o.OnReply) {
		return false
	}
	for i := range a.OnReply {
		if !a.OnReply[i].EqualTo(o.OnReply[i]) {
			return false
		}
	}
	return true
}

type DenyAction struct {
	ActionBase
	DenyWith string
}

func (a *DenyAction) ActionType() ActionKind { return ActionKindDeny }
func (a *DenyAction) Base() *ActionBase      { return &a.ActionBase }
func (a *DenyAction) sealedAction()          {}
func (a *DenyAction) EqualTo(other Action) bool {
	o, ok := other.(*DenyAction)
	if !ok {
		return false
	}
	return a.equalBase(&o.ActionBase) && a.DenyWith == o.DenyWith
}

type HeadersAction struct {
	ActionBase
	Target  string
	Headers string
}

func (a *HeadersAction) ActionType() ActionKind { return ActionKindHeaders }
func (a *HeadersAction) Base() *ActionBase      { return &a.ActionBase }
func (a *HeadersAction) sealedAction()          {}
func (a *HeadersAction) EqualTo(other Action) bool {
	o, ok := other.(*HeadersAction)
	if !ok {
		return false
	}
	return a.equalBase(&o.ActionBase) && a.Target == o.Target && a.Headers == o.Headers
}

type StoreAction struct {
	ActionBase
	Path         string
	Value        string
	ExportToHost bool
}

func (a *StoreAction) ActionType() ActionKind { return ActionKindStore }
func (a *StoreAction) Base() *ActionBase      { return &a.ActionBase }
func (a *StoreAction) sealedAction()          {}
func (a *StoreAction) EqualTo(other Action) bool {
	o, ok := other.(*StoreAction)
	if !ok {
		return false
	}
	return a.equalBase(&o.ActionBase) &&
		a.Path == o.Path && a.Value == o.Value && a.ExportToHost == o.ExportToHost
}

type FailAction struct {
	ActionBase
	LogMessage string
}

func (a *FailAction) ActionType() ActionKind { return ActionKindFail }
func (a *FailAction) Base() *ActionBase      { return &a.ActionBase }
func (a *FailAction) sealedAction()          {}
func (a *FailAction) EqualTo(other Action) bool {
	o, ok := other.(*FailAction)
	if !ok {
		return false
	}
	return a.equalBase(&o.ActionBase) && a.LogMessage == o.LogMessage
}

// actionWire is the flat JSON representation used for wire serialization.
type actionWire struct {
	Type           ActionKind        `json:"type"`
	Predicate      string            `json:"predicate"`
	Terminal       bool              `json:"terminal"`
	IsGuard        bool              `json:"isGuard"`
	Var            string            `json:"var,omitempty"`
	Service        string            `json:"service,omitempty"`
	Label          string            `json:"label,omitempty"`
	MessageBuilder string            `json:"messageBuilder,omitempty"`
	OnReply        []json.RawMessage `json:"onReply,omitempty"`
	DenyWith       string            `json:"denyWith,omitempty"`
	Target         string            `json:"target,omitempty"`
	Headers        string            `json:"headers,omitempty"`
	Path           string            `json:"path,omitempty"`
	Value          string            `json:"value,omitempty"`
	ExportToHost   bool              `json:"exportToHost,omitempty"`
	LogMessage     string            `json:"logMessage,omitempty"`
	Sources        []string          `json:"sources,omitempty"`
}

func marshalOnReply(actions []Action) ([]json.RawMessage, error) {
	if len(actions) == 0 {
		return nil, nil
	}
	raw := make([]json.RawMessage, len(actions))
	for i, a := range actions {
		data, err := json.Marshal(a)
		if err != nil {
			return nil, err
		}
		raw[i] = data
	}
	return raw, nil
}

func (a *GrpcAction) MarshalJSON() ([]byte, error) {
	onReply, err := marshalOnReply(a.OnReply)
	if err != nil {
		return nil, err
	}
	return json.Marshal(actionWire{
		Type: ActionKindGrpc, Predicate: a.Predicate, Terminal: a.Terminal, IsGuard: a.IsGuard,
		Var: a.Var, Service: a.Service, Label: a.Label, MessageBuilder: a.MessageBuilder,
		OnReply: onReply, Sources: a.SourcePolicyLocators,
	})
}

func (a *DenyAction) MarshalJSON() ([]byte, error) {
	return json.Marshal(actionWire{
		Type: ActionKindDeny, Predicate: a.Predicate, Terminal: a.Terminal, IsGuard: a.IsGuard,
		DenyWith: a.DenyWith, Sources: a.SourcePolicyLocators,
	})
}

func (a *HeadersAction) MarshalJSON() ([]byte, error) {
	return json.Marshal(actionWire{
		Type: ActionKindHeaders, Predicate: a.Predicate, Terminal: a.Terminal, IsGuard: a.IsGuard,
		Target: a.Target, Headers: a.Headers, Sources: a.SourcePolicyLocators,
	})
}

func (a *StoreAction) MarshalJSON() ([]byte, error) {
	return json.Marshal(actionWire{
		Type: ActionKindStore, Predicate: a.Predicate, Terminal: a.Terminal, IsGuard: a.IsGuard,
		Path: a.Path, Value: a.Value, ExportToHost: a.ExportToHost, Sources: a.SourcePolicyLocators,
	})
}

func (a *FailAction) MarshalJSON() ([]byte, error) {
	return json.Marshal(actionWire{
		Type: ActionKindFail, Predicate: a.Predicate, Terminal: a.Terminal, IsGuard: a.IsGuard,
		LogMessage: a.LogMessage, Sources: a.SourcePolicyLocators,
	})
}

func UnmarshalAction(data []byte) (Action, error) {
	var w actionWire
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	base := ActionBase{
		Predicate: w.Predicate, Terminal: w.Terminal, IsGuard: w.IsGuard,
		SourcePolicyLocators: w.Sources,
	}
	switch w.Type {
	case ActionKindGrpc:
		var onReply []Action
		for _, raw := range w.OnReply {
			r, err := UnmarshalAction(raw)
			if err != nil {
				return nil, err
			}
			onReply = append(onReply, r)
		}
		return &GrpcAction{
			ActionBase: base, Var: w.Var, Service: w.Service,
			Label: w.Label, MessageBuilder: w.MessageBuilder, OnReply: onReply,
		}, nil
	case ActionKindDeny:
		return &DenyAction{ActionBase: base, DenyWith: w.DenyWith}, nil
	case ActionKindHeaders:
		return &HeadersAction{ActionBase: base, Target: w.Target, Headers: w.Headers}, nil
	case ActionKindStore:
		return &StoreAction{ActionBase: base, Path: w.Path, Value: w.Value, ExportToHost: w.ExportToHost}, nil
	case ActionKindFail:
		return &FailAction{ActionBase: base, LogMessage: w.LogMessage}, nil
	default:
		return nil, fmt.Errorf("unknown action type: %q", w.Type)
	}
}

type Observability struct {
	HTTPHeaderIdentifier *string  `json:"httpHeaderIdentifier,omitempty"`
	DefaultLevel         *string  `json:"defaultLevel,omitempty"`
	Tracing              *Tracing `json:"tracing,omitempty"`
}

type Tracing struct {
	Service string `json:"service,omitempty"`
}

func (o *Observability) EqualTo(other *Observability) bool {
	if o == nil && other == nil {
		return true
	}
	if o == nil || other == nil {
		return false
	}

	if (o.HTTPHeaderIdentifier == nil) != (other.HTTPHeaderIdentifier == nil) {
		return false
	}
	if o.HTTPHeaderIdentifier != nil && other.HTTPHeaderIdentifier != nil && *o.HTTPHeaderIdentifier != *other.HTTPHeaderIdentifier {
		return false
	}

	if (o.DefaultLevel == nil) != (other.DefaultLevel == nil) {
		return false
	}
	if o.DefaultLevel != nil && other.DefaultLevel != nil && *o.DefaultLevel != *other.DefaultLevel {
		return false
	}

	if (o.Tracing == nil) != (other.Tracing == nil) {
		return false
	}
	if o.Tracing != nil && other.Tracing != nil && !o.Tracing.EqualTo(other.Tracing) {
		return false
	}

	return true
}

func (t *Tracing) EqualTo(other *Tracing) bool {
	if t == nil && other == nil {
		return true
	}
	if t == nil || other == nil {
		return false
	}
	return t.Service == other.Service
}
