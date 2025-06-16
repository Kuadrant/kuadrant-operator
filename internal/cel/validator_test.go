package cel

import (
	"github.com/google/cel-go/cel"
	"testing"
)

func TestNoAST(t *testing.T) {
	builder := NewValidatorBuilder()
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate("foo", nil); ast != nil {
			t.Fatal("No ast should have returned for no matching policy")
		} else if err == nil {
			t.Fatal("Should have returned an error")
		}
	}
}

func TestNoPolicy(t *testing.T) {
	builder := NewValidatorBuilder()
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate("foo", astFor("1 == 1")); ast != nil {
			t.Fatal("No ast should have returned for no matching policy")
		} else if err == nil {
			t.Fatal("Should have returned an error")
		}
	}
}

func TestAddPolicyAfter(t *testing.T) {
	start := "foo"
	if _, err := NewValidatorBuilder().AddPolicyBindingAfter(&start, "bar", "second"); err == nil {
		t.Fatal("Should have returned an error")
	}
	if builder, err := NewValidatorBuilder().AddPolicyBindingAfter(nil, start, "first"); err != nil {
		t.Fatal(err)
	} else {
		if _, err := builder.AddPolicyBindingAfter(&start, "bar", "second"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPolicyNoDependency(t *testing.T) {
	builder := NewValidatorBuilder()
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate("foo", astFor("1 == 1")); ast != nil {
			t.Fatal("No ast should have returned for no matching policy")
		} else if err == nil {
			t.Fatal("Should have returned an error")
		}
	}
}

func astFor(expr string) *cel.Ast {
	env, _ := cel.NewEnv()
	ast, _ := env.Parse(expr)
	return ast
}
