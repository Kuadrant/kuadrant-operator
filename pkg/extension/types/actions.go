package types

import "context"

// ActionType discriminates how the wasm-shim dispatches an action.
type ActionType string

const (
	ActionTypeGRPCMethod ActionType = "grpc_method"
	ActionTypeDeny       ActionType = "deny"
	ActionTypeFail       ActionType = "fail"
	ActionTypeAddHeaders ActionType = "add_headers"
)

// Action is the interface implemented by all pipeline action types.
// Actions can be used in either the request or response phase.
type Action interface {
	actionType() ActionType
}

// GRPCMethodAction invokes a registered gRPC action method and optionally
// stores the response in a named variable for use by subsequent actions.
type GRPCMethodAction struct {
	Predicate string // CEL — if true, call the gRPC method
	Method    string // Name of a registered ActionMethod
	Var       string // Variable name to store gRPC response (optional)
}

func (a GRPCMethodAction) actionType() ActionType { return ActionTypeGRPCMethod }

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
	WithBody    string // Response body string; optional
}

func (a DenyAction) actionType() ActionType { return ActionTypeDeny }

// FailAction logs an error message and terminates the action chain when
// the predicate evaluates to true. Maps to the wasm-shim's "fail" type.
type FailAction struct {
	Predicate  string // CEL — if true, fail with log message
	LogMessage string // Error message to log
}

func (a FailAction) actionType() ActionType { return ActionTypeFail }

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

func (a AddHeadersAction) actionType() ActionType { return ActionTypeAddHeaders }

// Pipeline provides a builder for composing ordered actions on HTTP request
// and response phases. Actions accumulate locally with immediate ordering
// validation. Commit sends all actions atomically to the operator.
type Pipeline interface {
	OnHTTPRequest(actions ...Action) error
	OnHTTPResponse(actions ...Action) error
	Commit(ctx context.Context) error
}
