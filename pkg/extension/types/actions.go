package types

import (
	"context"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

// Action is the interface implemented by all pipeline action types.
// Actions can be used in either the request or response phase.
type Action interface {
	sealedAction()
	CelExpressions() []string
	PopulateProtobuf(entry *extpb.ActionEntry)
}

// GRPCAction invokes a registered gRPC action method and optionally
// stores the response in a named variable for use by subsequent actions.
type GRPCAction struct {
	Predicate string // CEL — if true, call the gRPC method
	Method    string // Name of a registered ActionMethod
	Var       string // Variable name to store gRPC response (optional)
}

func (a GRPCAction) sealedAction() {}

func (a GRPCAction) CelExpressions() []string {
	if a.Predicate != "" {
		return []string{a.Predicate}
	}
	return nil
}

func (a GRPCAction) PopulateProtobuf(entry *extpb.ActionEntry) {
	entry.Predicate = a.Predicate
	entry.Action = &extpb.ActionEntry_Grpc{Grpc: &extpb.GrpcAction{
		Method: a.Method,
		Var:    a.Var,
	}}
}

// DenyAction denies the request or response when the predicate evaluates
// to true. All response fields are optional.
//
// Phase semantics:
//   - Request phase: deny sends the response to the origin
//     (request never reaches backend)
//   - Response phase: deny sends the response to the destination
//     (backend response replaced before reaching client)
type DenyAction struct {
	Predicate   string // CEL — if true, deny
	WithStatus  int    // HTTP status code (e.g. 403); optional
	WithHeaders string // CEL expression — array of [name, value] pairs; optional
	WithBody    string // CEL expression; optional
}

func (a DenyAction) sealedAction() {}

func (a DenyAction) CelExpressions() []string {
	var exprs []string
	if a.Predicate != "" {
		exprs = append(exprs, a.Predicate)
	}
	if a.WithHeaders != "" {
		exprs = append(exprs, a.WithHeaders)
	}
	if a.WithBody != "" {
		exprs = append(exprs, a.WithBody)
	}
	return exprs
}

func (a DenyAction) PopulateProtobuf(entry *extpb.ActionEntry) {
	entry.Predicate = a.Predicate
	entry.Action = &extpb.ActionEntry_Deny{Deny: &extpb.DenyAction{
		WithStatus:  int32(a.WithStatus), //nolint:gosec
		WithHeaders: a.WithHeaders,
		WithBody:    a.WithBody,
	}}
}

// FailAction logs an error message and terminates the action chain when
// the predicate evaluates to true. Maps to the wasm-shim's "fail" type.
type FailAction struct {
	Predicate  string // CEL — if true, fail with log message
	LogMessage string // Error message to log
}

func (a FailAction) sealedAction() {}

func (a FailAction) CelExpressions() []string {
	if a.Predicate != "" {
		return []string{a.Predicate}
	}
	return nil
}

func (a FailAction) PopulateProtobuf(entry *extpb.ActionEntry) {
	entry.Predicate = a.Predicate
	entry.Action = &extpb.ActionEntry_Fail{Fail: &extpb.FailAction{
		LogMessage: a.LogMessage,
	}}
}

// AddHeadersAction adds headers to the request or response depending on
// the phase in which it is used, when the predicate evaluates to true.
//
// Phase semantics:
//   - Request phase: headers added to the request before it reaches the backend
//   - Response phase: headers added to the response before it reaches the client
type AddHeadersAction struct {
	Predicate    string // CEL — if true, add the headers
	HeadersToAdd string // CEL expression evaluating to a map of headers
}

func (a AddHeadersAction) sealedAction() {}

func (a AddHeadersAction) CelExpressions() []string {
	var exprs []string
	if a.Predicate != "" {
		exprs = append(exprs, a.Predicate)
	}
	if a.HeadersToAdd != "" {
		exprs = append(exprs, a.HeadersToAdd)
	}
	return exprs
}

func (a AddHeadersAction) PopulateProtobuf(entry *extpb.ActionEntry) {
	entry.Predicate = a.Predicate
	entry.Action = &extpb.ActionEntry_AddHeaders{AddHeaders: &extpb.AddHeadersAction{
		HeadersToAdd: a.HeadersToAdd,
	}}
}

type StoreAction struct {
	Predicate    string // CEL — if true, store the value
	Path         string
	Value        string // CEL expression
	ExportToHost bool
}

func (a StoreAction) sealedAction() {}

func (a StoreAction) CelExpressions() []string {
	var exprs []string
	if a.Predicate != "" {
		exprs = append(exprs, a.Predicate)
	}
	if a.Value != "" {
		exprs = append(exprs, a.Value)
	}
	return exprs
}

func (a StoreAction) PopulateProtobuf(entry *extpb.ActionEntry) {
	entry.Predicate = a.Predicate
	entry.Action = &extpb.ActionEntry_Store{Store: &extpb.StoreAction{
		Path:         a.Path,
		Value:        a.Value,
		ExportToHost: a.ExportToHost,
	}}
}

// Pipeline provides a builder for composing ordered actions on HTTP request
// and response phases. Actions accumulate locally with immediate ordering
// validation. Commit sends all actions atomically to the operator.
type Pipeline interface {
	OnHTTPRequest(actions ...Action) error
	OnHTTPResponse(actions ...Action) error
	Commit(ctx context.Context) error
}
