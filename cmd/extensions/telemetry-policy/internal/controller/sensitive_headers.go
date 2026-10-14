package controller

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/parser"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// sensitiveRequestHeaders lists the request headers a TelemetryPolicy expression must not read.
// It mirrors Authorino's default log redaction list (pkg/log/redact.go). Metric labels and
// logging fields that do not reference auth.* are evaluated in the gateway's wasm filter and
// their values are forwarded verbatim, outside that redaction, so a header Authorino would
// redact in its own records must never be referenced by a TelemetryPolicy expression.
var sensitiveRequestHeaders = []string{"authorization", "cookie", "x-api-key", "apikey"}

const (
	requestAttribute = "request"
	headersAttribute = "headers"
)

var (
	errWholeRequest     = errors.New("expression reads request as a whole, which includes the request headers")
	errWholeHeaders     = errors.New("expression reads request.headers as a whole, which includes sensitive headers")
	errNonLiteralHeader = errors.New("expression indexes request.headers with a non-literal key, which cannot be checked for sensitive headers")
)

// errInvalidExpression reports a metric label or logging field expression that was rejected.
// It satisfies the kuadrant PolicyError contract (Error plus Reason), so the Accepted
// condition carries the Invalid reason rather than Unknown.
type errInvalidExpression struct {
	path string
	err  error
}

func (e errInvalidExpression) Error() string {
	return fmt.Sprintf("%s: %s", e.path, e.err)
}

func (e errInvalidExpression) Unwrap() error {
	return e.err
}

func (e errInvalidExpression) Reason() gatewayapiv1alpha2.PolicyConditionReason {
	return gatewayapiv1alpha2.PolicyReasonInvalid
}

// checkSensitiveHeaders parses expression and returns an error when evaluating it could expose
// a sensitive request header. It rejects:
//   - a field selection or string-literal index on request.headers naming a sensitive header,
//     matched case-insensitively;
//   - request.headers used as a whole value, for example string(request.headers) or
//     request.headers.map(k, v, v);
//   - request.headers indexed with a key that is not a string literal, which cannot be checked
//     statically;
//   - request used as a whole value, which includes the headers.
//
// Presence tests read no value. has(request.headers.<name>) is accepted for any name, and a
// membership test such as "x-tenant" in request.headers, the only presence spelling for
// hyphenated names, is checked like an index: accepted for a non-sensitive literal name,
// rejected for a sensitive one or a non-literal key.
// An expression that does not parse is rejected as well, since it could not be checked.
func checkSensitiveHeaders(expression string) error {
	// The standard macros must be registered for has() to parse as a presence test and for
	// map(), filter() and friends to parse as comprehensions over their range.
	prsr, err := parser.NewParser(parser.Macros(parser.AllMacros...))
	if err != nil {
		return fmt.Errorf("failed to create CEL parser: %w", err)
	}
	parsed, iss := prsr.Parse(common.NewTextSource(expression))
	if errs := iss.GetErrors(); len(errs) > 0 {
		return fmt.Errorf("invalid CEL expression: %s", errs[0].Message)
	}
	return visitRequestReads(ast.NavigateAST(parsed))
}

func visitRequestReads(node ast.NavigableExpr) error {
	if err := checkRequestRead(node); err != nil {
		return err
	}
	for _, child := range node.Children() {
		if err := visitRequestReads(child); err != nil {
			return err
		}
	}
	return nil
}

// checkRequestRead inspects a single node. Only two shapes matter: the request.headers map and
// the bare request identifier. Whether either is acceptable depends on how the parent node
// consumes it.
func checkRequestRead(node ast.NavigableExpr) error {
	switch {
	case isRequestHeaders(node):
		return checkHeadersUse(node)
	case isIdent(node, requestAttribute):
		return checkRequestUse(node)
	}
	return nil
}

func checkHeadersUse(node ast.NavigableExpr) error {
	if node.Kind() == ast.SelectKind && node.AsSelect().IsTestOnly() {
		return nil // has(request.headers)
	}
	parent, ok := node.Parent()
	if !ok {
		return errWholeHeaders
	}
	switch parent.Kind() {
	case ast.SelectKind:
		// request.headers.<name>
		sel := parent.AsSelect()
		if sel.IsTestOnly() {
			return nil // has(request.headers.<name>)
		}
		return checkHeaderName(sel.FieldName())
	case ast.CallKind:
		// request.headers[<key>] reads a header; <key> in request.headers tests its presence.
		// Both name a single header, so both are checked against the sensitive list.
		call := parent.AsCall()
		key, ok := indexKey(call, node)
		if !ok {
			key, ok = membershipKey(call, node)
		}
		if ok {
			name, isLiteral := stringLiteral(key)
			if !isLiteral {
				return errNonLiteralHeader
			}
			return checkHeaderName(name)
		}
	}
	return errWholeHeaders
}

func checkRequestUse(node ast.NavigableExpr) error {
	parent, ok := node.Parent()
	if !ok {
		return errWholeRequest
	}
	switch parent.Kind() {
	case ast.SelectKind:
		return nil // request.<attribute>; request.headers is checked when that node is visited
	case ast.CallKind:
		if key, ok := indexKey(parent.AsCall(), node); ok {
			if _, isLiteral := stringLiteral(key); isLiteral {
				return nil // request["<attribute>"]; request["headers"] is checked when that node is visited
			}
		}
	}
	return errWholeRequest
}

func checkHeaderName(name string) error {
	for _, sensitive := range sensitiveRequestHeaders {
		if strings.EqualFold(name, sensitive) {
			return fmt.Errorf("expression reads sensitive request header %q", sensitive)
		}
	}
	return nil
}

// isRequestHeaders reports whether node is request.headers, written as a field selection or
// as request["headers"].
func isRequestHeaders(node ast.NavigableExpr) bool {
	switch node.Kind() {
	case ast.SelectKind:
		sel := node.AsSelect()
		return sel.FieldName() == headersAttribute && isIdent(sel.Operand(), requestAttribute)
	case ast.CallKind:
		call := node.AsCall()
		if call.FunctionName() != operators.Index || len(call.Args()) != 2 {
			return false
		}
		name, ok := stringLiteral(call.Args()[1])
		return ok && name == headersAttribute && isIdent(call.Args()[0], requestAttribute)
	}
	return false
}

func isIdent(e ast.Expr, name string) bool {
	return e.Kind() == ast.IdentKind && e.AsIdent() == name
}

// indexKey returns the key expression when call is operand[key] for the given operand.
func indexKey(call ast.CallExpr, operand ast.NavigableExpr) (ast.Expr, bool) {
	if call.FunctionName() != operators.Index || len(call.Args()) != 2 || call.Args()[0].ID() != operand.ID() {
		return nil, false
	}
	return call.Args()[1], true
}

// membershipKey returns the key expression when call is `key in operand` for the given operand.
func membershipKey(call ast.CallExpr, operand ast.NavigableExpr) (ast.Expr, bool) {
	if call.FunctionName() != operators.In || len(call.Args()) != 2 || call.Args()[1].ID() != operand.ID() {
		return nil, false
	}
	return call.Args()[0], true
}

func stringLiteral(e ast.Expr) (string, bool) {
	if e.Kind() != ast.LiteralKind {
		return "", false
	}
	s, ok := e.AsLiteral().(types.String)
	return string(s), ok
}
