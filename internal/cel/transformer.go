package transformer

import (
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/parser"
)

func parseExpression(expression string) (*ast.AST, error) {
	prsr, err := parser.NewParser()
	if err != nil {
		return nil, err
	}

	p, iss := prsr.Parse(common.NewTextSource(expression))
	if len(iss.GetErrors()) > 0 {
		return nil, errors.New(iss.ToDisplayString())
	}

	return p, nil
}

// TransformCounterVariable Limitador, as of v2, does expose `descriptors` explicitly. As such `Limit`'s variables
// need to be accessed through that root binding's `Ident`: `descriptors[0]`.
// This function parses the CEL `expression` and traverses its AST to "rename" all bindings that are from "well-known
// attributes" and prefixes them, e.g. `request.method` will become `descriptors[0].request.method`, so that the value
// can be successfully looked up at request time by `Limitador`.
// Note: This will _only_ replace `knownAttributes` and will leave others as is... tho these _should_ be meaningless
// anyway. This function does *NOT* try to validate or make any assumption about the expression being otherwise valid.
// Rather than transforming the AST directly, it only uses the AST to find the `Ident` that need renaming directly in
// the `expression` passed in. This keeps the resulting expression as close to the input as possible.
func TransformCounterVariable(expression string) (*string, error) {
	knownAttributes := []string{"request", "source", "destination", "connection", "metadata", "filter_state", "auth", "ratelimit"}
	var err error
	var p *ast.AST
	if p, err = parseExpression(expression); err != nil {
		return nil, err
	}

	decls := make(map[string]ast.CallExpr)
	toReplace := make([]ast.OffsetRange, 0)
	stack := newStack()
	stack.push(p.Expr())

	for next := stack.pop(); next != nil; next = stack.pop() {
		expr := *next
		if expr.Kind() == ast.IdentKind && slices.Contains(knownAttributes, expr.AsIdent()) {
			if decls[expr.AsIdent()] == nil {
				if offset, found := p.SourceInfo().GetOffsetRange(expr.ID()); found {
					toReplace = append(toReplace, offset)
				} else {
					return nil, fmt.Errorf("could not find offset range for %d", expr.ID())
				}
			}
		} else if expr.Kind() == ast.CallKind {
			call := expr.AsCall()
			stack.push(call.Target())
			if (call.FunctionName() == "all" ||
				call.FunctionName() == "exists" ||
				call.FunctionName() == "exists_one" ||
				call.FunctionName() == "map" ||
				call.FunctionName() == "filter") &&
				len(call.Args()) >= 2 &&
				call.Args()[0].Kind() == ast.IdentKind &&
				call.Args()[1].Kind() == ast.CallKind {
				decls[call.Args()[0].AsIdent()] = call.Args()[1].AsCall()
				for _, arg := range call.Args()[1:] {
					stack.push(arg)
				}
			} else {
				for _, arg := range call.Args() {
					stack.push(arg)
				}
			}
		} else if expr.Kind() == ast.SelectKind {
			stack.push(expr.AsSelect().Operand())
		} else if expr.Kind() == ast.ListKind {
			l := expr.AsList()
			for _, arg := range l.Elements() {
				stack.push(arg)
			}
		} else if expr.Kind() == ast.MapKind {
			m := expr.AsMap()
			for _, entry := range m.Entries() {
				mapEntry := entry.AsMapEntry()
				stack.push(mapEntry.Key())
				stack.push(mapEntry.Value())
			}
		}
	}

	if len(toReplace) == 0 {
		return &expression, nil
	}

	sort.Slice(toReplace, func(i, j int) bool {
		return toReplace[i].Start < toReplace[j].Start
	})

	exp := ""
	cur := int32(0)
	for _, offset := range toReplace {
		if offset.Start > cur {
			exp = exp + expression[cur:offset.Start]
		}
		exp = exp + "descriptors[0]." + expression[offset.Start:offset.Stop]
		cur = offset.Stop
	}
	if int(cur) < len(expression) {
		exp = exp + expression[cur:]
	}
	return &exp, nil
}

type stack []ast.Expr

func newStack() stack {
	return make([]ast.Expr, 0)
}

func (s *stack) push(expr ast.Expr) {
	*s = append(*s, expr)
}

func (s *stack) pop() *ast.Expr {
	var ret *ast.Expr
	if len(*s) > 0 {
		ret = &(*s)[len(*s)-1]
		*s = (*s)[:len(*s)-1]
	}
	return ret
}
