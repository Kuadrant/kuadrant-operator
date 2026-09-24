package translator

import (
	"strings"

	"github.com/google/cel-go/cel"
)

// TranslateCEL translates domain-specific MCP variables into data-plane variables.
func TranslateCEL(expr string) string {
	// Translate "request.mcp.tool_name" -> "(has(request.headers) && 'x-mcp-toolname' in request.headers ? request.headers['x-mcp-toolname'] : '')"
	safeHeaderCheck := "(has(request.headers) && 'x-mcp-toolname' in request.headers ? request.headers['x-mcp-toolname'] : '')"
	return strings.ReplaceAll(expr, "request.mcp.tool_name", safeHeaderCheck)
}

// ValidateCEL checks if the CEL expression has valid syntax.
// We only perform syntax validation, not semantic/type validation.
func ValidateCEL(expr string) error {
	// Initialize a minimal CEL environment
	env, err := cel.NewEnv()
	if err != nil {
		return err
	}

	ast, iss := env.Parse(expr)
	if iss != nil && iss.Err() != nil {
		return iss.Err()
	}
	_ = ast

	return nil
}
