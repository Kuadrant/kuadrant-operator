package extension

import (
	"fmt"

	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/parser"
)

func extractVarFieldAccesses(expr string, knownVars map[string]string) (map[string][][]string, error) {
	prsr, err := parser.NewParser()
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL parser: %w", err)
	}
	parsed, iss := prsr.Parse(common.NewTextSource(expr))
	if len(iss.GetErrors()) > 0 {
		return nil, fmt.Errorf("failed to parse CEL expression: %s", iss.ToDisplayString())
	}

	result := make(map[string][][]string)
	collectFieldAccesses(parsed.Expr(), knownVars, result)
	return result, nil
}

func collectFieldAccesses(expr ast.Expr, knownVars map[string]string, result map[string][][]string) {
	if expr == nil {
		return
	}

	if expr.Kind() == ast.SelectKind {
		varName, fields := resolveSelectChain(expr)
		if varName != "" {
			if _, ok := knownVars[varName]; ok {
				result[varName] = append(result[varName], fields)
				return
			}
		}
	}

	switch expr.Kind() {
	case ast.SelectKind:
		collectFieldAccesses(expr.AsSelect().Operand(), knownVars, result)
	case ast.CallKind:
		call := expr.AsCall()
		if t := call.Target(); t != nil {
			collectFieldAccesses(t, knownVars, result)
		}
		for _, arg := range call.Args() {
			collectFieldAccesses(arg, knownVars, result)
		}
	case ast.ListKind:
		for _, elem := range expr.AsList().Elements() {
			collectFieldAccesses(elem, knownVars, result)
		}
	case ast.MapKind:
		for _, entry := range expr.AsMap().Entries() {
			mapEntry := entry.AsMapEntry()
			collectFieldAccesses(mapEntry.Key(), knownVars, result)
			collectFieldAccesses(mapEntry.Value(), knownVars, result)
		}
	}
}

func resolveSelectChain(expr ast.Expr) (string, []string) {
	var fields []string
	current := expr
	for current.Kind() == ast.SelectKind {
		fields = append([]string{current.AsSelect().FieldName()}, fields...)
		current = current.AsSelect().Operand()
	}
	if current.Kind() == ast.IdentKind {
		return current.AsIdent(), fields
	}
	return "", nil
}
