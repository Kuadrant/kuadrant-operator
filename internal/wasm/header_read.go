package wasm

import (
	"fmt"

	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/parser"
)

// bareHeaderRead reports whether expr is nothing but a read of one request header, either
// request.headers.<name> (select form, index=false) or request.headers["<name>"] (index form,
// index=true). request["headers"] is accepted as a spelling of request.headers. The name is
// taken from the parsed AST, never from the source text. Anything else reports ok=false: an
// expression that does not parse, a presence test such as has(request.headers.<name>), a
// compound expression, a wrapped read such as string(request.headers.<name>), or an index
// with a non-literal key.
func bareHeaderRead(expr string) (name string, index bool, ok bool) {
	// The standard macros must be registered so that has() parses as a presence test rather
	// than as a function call wrapping a select.
	prsr, err := parser.NewParser(parser.Macros(parser.AllMacros...))
	if err != nil {
		return "", false, false
	}
	parsed, iss := prsr.Parse(common.NewTextSource(expr))
	if len(iss.GetErrors()) > 0 {
		return "", false, false
	}

	root := parsed.Expr()
	switch root.Kind() {
	case ast.SelectKind:
		sel := root.AsSelect()
		if sel.IsTestOnly() || !isRequestHeadersExpr(sel.Operand()) {
			return "", false, false
		}
		return sel.FieldName(), false, true
	case ast.CallKind:
		call := root.AsCall()
		if call.FunctionName() != operators.Index || len(call.Args()) != 2 || !isRequestHeadersExpr(call.Args()[0]) {
			return "", false, false
		}
		key, isLiteral := stringLiteralExpr(call.Args()[1])
		if !isLiteral {
			return "", false, false
		}
		return key, true, true
	}
	return "", false, false
}

// guardedHeaderReadCEL renders a presence-guarded read of one request header. It evaluates
// to the header value when present and to "" when absent, instead of failing the enclosing
// expression. The index form embeds the name through celString so a name containing quotes
// or backslashes cannot break out of the literal.
func guardedHeaderReadCEL(name string, index bool) string {
	if index {
		lit := celString(name)
		return fmt.Sprintf(`%s in request.headers ? string(request.headers[%s]) : ""`, lit, lit)
	}
	return fmt.Sprintf(`has(request.headers.%s) ? string(request.headers.%s) : ""`, name, name)
}

// isRequestHeadersExpr reports whether e is request.headers, written as a field selection or
// as request["headers"].
func isRequestHeadersExpr(e ast.Expr) bool {
	switch e.Kind() {
	case ast.SelectKind:
		sel := e.AsSelect()
		return !sel.IsTestOnly() && sel.FieldName() == "headers" && isIdentExpr(sel.Operand(), "request")
	case ast.CallKind:
		call := e.AsCall()
		if call.FunctionName() != operators.Index || len(call.Args()) != 2 {
			return false
		}
		key, ok := stringLiteralExpr(call.Args()[1])
		return ok && key == "headers" && isIdentExpr(call.Args()[0], "request")
	}
	return false
}

func isIdentExpr(e ast.Expr, name string) bool {
	return e.Kind() == ast.IdentKind && e.AsIdent() == name
}

func stringLiteralExpr(e ast.Expr) (string, bool) {
	if e.Kind() != ast.LiteralKind {
		return "", false
	}
	s, ok := e.AsLiteral().(types.String)
	return string(s), ok
}
