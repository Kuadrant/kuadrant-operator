//go:build unit

package v1alpha1

import (
	"reflect"
	"testing"

	"github.com/google/cel-go/cel"
)

func evalCEL(t *testing.T, expr string, plan string) interface{} {
	t.Helper()
	env, err := cel.NewEnv(cel.Variable("auth", cel.DynType))
	if err != nil {
		t.Fatalf("cel.NewEnv failed: %v", err)
	}
	ast, iss := env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		t.Fatalf("compile failed for expression %q: %v", expr, iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("program failed: %v", err)
	}
	out, _, err := prg.Eval(map[string]interface{}{"auth": map[string]interface{}{"kuadrant": map[string]interface{}{"plan": plan}}})
	if err != nil {
		t.Fatalf("eval failed for expression %q: %v", expr, err)
	}
	return out.Value()
}

func TestToRateLimitsQuotesTier(t *testing.T) {
	for _, tier := range []string{"gold", `gold" || true || "`} {
		policy := &PlanPolicy{Spec: PlanPolicySpec{Plans: []Plan{{Tier: tier, Predicate: "true"}}}}
		predicate := policy.ToRateLimits()[tier].When[0].Predicate

		if got := evalCEL(t, predicate, tier); got != true {
			t.Errorf("predicate %q with plan %q = %v, want true", predicate, tier, got)
		}
		if got := evalCEL(t, predicate, "other"); got != false {
			t.Errorf("predicate %q with plan %q = %v, want false", predicate, "other", got)
		}
	}
}

func TestBuildCelExpressionQuotesTier(t *testing.T) {
	policy := &PlanPolicy{Spec: PlanPolicySpec{Plans: []Plan{
		{Tier: `gold"`, Predicate: "false"},
		{Tier: `silver\`, Predicate: "true"},
		{Tier: "bronze", Predicate: "true"},
	}}}
	expr := policy.BuildCelExpression()

	if got, want := evalCEL(t, expr, ""), `silver\`; !reflect.DeepEqual(got, want) {
		t.Errorf("expression %q = %v, want %q", expr, got, want)
	}
}
