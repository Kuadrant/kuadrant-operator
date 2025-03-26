package transformer

import (
	"errors"

	//"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	//"github.com/google/cel-go/common/types/ref"
	//"github.com/google/cel-go/ext"
	"github.com/google/cel-go/parser"
)

func parseExpression(expression string) (*ast.AST, error) {
	prsr, err := parser.NewParser()
	if err != nil {
		return nil, err
	}

	p, iss := prsr.Parse(common.NewTextSource(expression))
	if len(iss.GetErrors()) > 0 {
		return nil, errors.New("got errors parsing")
	}

	return p, nil
}

func SafeToSimplyPrefix(expression string) bool {
	if p, err := parseExpression(expression); err != nil {
		return false
	} else {
		expr := p.Expr()
		for {
			if expr.Kind() == ast.IdentKind && expr.AsIdent() != "descriptors" {
				return true
			} else if expr.Kind() == ast.CallKind && len(expr.AsCall().Args()) == 2 && expr.AsCall().FunctionName() == "_[_]" {
				expr = expr.AsCall().Args()[0]
			} else if expr.Kind() == ast.SelectKind {
				expr = expr.AsSelect().Operand()
			} else {
				return false
			}
		}
	}
}
