package transformer

import (
	"errors"
	"fmt"
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
		return nil, errors.New("got errors parsing")
	}

	return p, nil
}

func TransformCounterVariable(expression string) (*string, error) {
	var err error
	var p *ast.AST
	if p, err = parseExpression(expression); err != nil {
		return nil, err
	}

	toReplace := make([]ast.OffsetRange, 0)
	stack := newStack()
	stack.push(p.Expr())

	for next := stack.pop(); next != nil; next = stack.pop() {
		expr := *next
		if expr.Kind() == ast.IdentKind && expr.AsIdent() != "descriptors" {
			if offset, found := p.SourceInfo().GetOffsetRange(expr.ID()); found {
				toReplace = append(toReplace, offset)
			} else {
				return nil, fmt.Errorf("could not find offset range for %d", expr.ID())
			}
		} else if expr.Kind() == ast.CallKind {
			call := expr.AsCall()
			for _, arg := range call.Args() {
				stack.push(arg)
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
