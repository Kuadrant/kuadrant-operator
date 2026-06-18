package cel

import "testing"

func TestStringLiteral(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "normal value", input: "hello", want: `"hello"`},
		{name: "double quote injection", input: `admin"||true`, want: `"admin\"||true"`},
		{name: "claim key with special characters", input: "custom:role", want: `"custom:role"`},
		{name: "backslash", input: `foo\bar`, want: `"foo\\bar"`},
		{name: "empty string", input: "", want: `""`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StringLiteral(tt.input)
			if got != tt.want {
				t.Errorf("StringLiteral(%q) = %s; want %s", tt.input, got, tt.want)
			}
		})
	}
}
