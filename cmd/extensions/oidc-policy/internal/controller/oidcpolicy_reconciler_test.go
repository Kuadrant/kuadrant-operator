//go:build unit

package controller

import (
	"testing"

	"github.com/google/cel-go/cel"
)

// evalClaimPredicate compiles the predicate produced by claimPredicate and
// evaluates it against a JWT identity, mirroring what authorino does at runtime.
func evalClaimPredicate(t *testing.T, k, v string, identity map[string]interface{}) bool {
	t.Helper()
	env, err := cel.NewEnv(cel.Variable("auth", cel.DynType))
	if err != nil {
		t.Fatalf("cel.NewEnv failed: %v", err)
	}
	ast, iss := env.Compile(claimPredicate(k, v))
	if iss != nil && iss.Err() != nil {
		t.Fatalf("compile failed for predicate %q: %v", claimPredicate(k, v), iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("program failed: %v", err)
	}
	out, _, err := prg.Eval(map[string]interface{}{
		"auth": map[string]interface{}{"identity": identity},
	})
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	got, ok := out.Value().(bool)
	if !ok {
		t.Fatalf("predicate did not return a bool, got %T", out.Value())
	}
	return got
}

func TestClaimPredicate(t *testing.T) {
	cases := []struct {
		name     string
		claim    string
		value    string
		identity map[string]interface{}
		want     bool
	}{
		{
			name:     "scalar string match",
			claim:    "email",
			value:    "user@example.com",
			identity: map[string]interface{}{"email": "user@example.com"},
			want:     true,
		},
		{
			name:     "scalar string mismatch",
			claim:    "email",
			value:    "user@example.com",
			identity: map[string]interface{}{"email": "other@example.com"},
			want:     false,
		},
		{
			name:     "scalar boolean-as-string match",
			claim:    "email_verified",
			value:    "true",
			identity: map[string]interface{}{"email_verified": "true"},
			want:     true,
		},
		{
			name:     "list claim contains value",
			claim:    "groups",
			value:    "admin",
			identity: map[string]interface{}{"groups": []interface{}{"dev", "admin"}},
			want:     true,
		},
		{
			name:     "list claim missing value",
			claim:    "groups",
			value:    "admin",
			identity: map[string]interface{}{"groups": []interface{}{"dev", "ops"}},
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalClaimPredicate(t, tc.claim, tc.value, tc.identity); got != tc.want {
				t.Errorf("claimPredicate(%q, %q) over %v = %v, want %v", tc.claim, tc.value, tc.identity, got, tc.want)
			}
		})
	}
}
