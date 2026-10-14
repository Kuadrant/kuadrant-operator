package translator

import (
	"testing"
)

func TestTranslateCEL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no translation needed",
			input:    "request.headers['x-custom'] == 'val'",
			expected: "request.headers['x-custom'] == 'val'",
		},
		{
			name:     "translate tool_name",
			input:    "request.mcp.tool_name == 'search'",
			expected: "(has(request.headers) && 'x-mcp-toolname' in request.headers ? request.headers['x-mcp-toolname'] : '') == 'search'",
		},
		{
			name:     "multiple occurrences",
			input:    "request.mcp.tool_name == 'a' || request.mcp.tool_name == 'b'",
			expected: "(has(request.headers) && 'x-mcp-toolname' in request.headers ? request.headers['x-mcp-toolname'] : '') == 'a' || (has(request.headers) && 'x-mcp-toolname' in request.headers ? request.headers['x-mcp-toolname'] : '') == 'b'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TranslateCEL(tt.input)
			if got != tt.expected {
				t.Errorf("TranslateCEL() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestValidateCEL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid syntax",
			input:   "request.headers['x-mcp-toolname'] == 'search'",
			wantErr: false,
		},
		{
			name:    "valid syntax with translated code",
			input:   "(has(request.headers) && 'x-mcp-toolname' in request.headers ? request.headers['x-mcp-toolname'] : '') == 'search'",
			wantErr: false,
		},
		{
			name:    "invalid syntax",
			input:   "request.headers['x' == 'search'",
			wantErr: true,
		},
		{
			name:    "unsupported variable still validates if valid syntax (no semantic check in this simple env)",
			input:   "some_undefined_var == 'value'",
			wantErr: false, // Wait, cel-go might fail if undefined variables are used and we didn't declare them?
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCEL(tt.input)
			if (err != nil) != tt.wantErr {
				// cel-go env.Compile fails if undeclared variables are referenced (if not using cel.SetTypeProvider or DisableTypeChecking).
				// We'll see how it behaves.
				t.Errorf("ValidateCEL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
