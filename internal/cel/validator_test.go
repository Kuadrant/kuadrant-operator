package cel

import (
	"github.com/google/cel-go/cel"
	"testing"
)

func TestNoPolicy(t *testing.T) {
	builder := NewValidatorBuilder()
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate("foo", "1 == 1"); ast != nil {
			t.Fatal("No ast should have returned for no matching policy")
		} else if err == nil {
			t.Fatal("Should have returned an error")
		}
	}
}

func TestAddPolicyAfter(t *testing.T) {
	start := "foo"
	if _, err := NewValidatorBuilder().AddPolicyBindingAfter(&start, "bar", "second", cel.BoolType); err == nil {
		t.Fatal("Should have returned an error")
	}
	if builder, err := NewValidatorBuilder().AddPolicyBindingAfter(nil, start, "first", cel.BoolType); err != nil {
		t.Fatal(err)
	} else {
		if _, err := builder.AddPolicyBindingAfter(&start, "bar", "second", cel.BoolType); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPolicyNoDependency(t *testing.T) {
	builder := NewValidatorBuilder()
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate("foo", "1 == 1"); ast != nil {
			t.Fatal("No ast should have returned for no matching policy")
		} else if err == nil {
			t.Fatal("Should have returned an error")
		}
	}
}

func TestPolicyWithUnknownBinding(t *testing.T) {
	builder := NewValidatorBuilder()
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate("foo", "first == 1"); ast != nil {
			t.Fatal("No ast should have returned for no matching policy")
		} else if err == nil {
			t.Fatal("Should have returned an error")
		}
	}
}

func TestPolicyWithKnownRootBinding(t *testing.T) {
	builder := NewValidatorBuilder()
	builder.AddBinding("base", cel.StringType)
	builder, _ = builder.AddPolicyBindingAfter(nil, "foo", "first", cel.BoolType)
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate("foo", "base == 'value'"); err != nil {
			t.Fatalf("Should have not returned an error: %v", err)
		} else if ast == nil {
			t.Fatal("We should have a valid AST!")
		}
	}
}
func TestPolicyWithUnknownRootBinding(t *testing.T) {
	builder := NewValidatorBuilder()
	builder.AddBinding("nope", cel.StringType)
	builder, _ = builder.AddPolicyBindingAfter(nil, "foo", "first", cel.BoolType)
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate("foo", "base == 'value'"); ast != nil {
			t.Fatal("No ast should have returned for no known base")
		} else if err == nil {
			t.Fatal("Should have returned an error")
		}
	}
}
func TestPolicyWithKnownPolicyBinding(t *testing.T) {
	builder := NewValidatorBuilder()
	builder.AddBinding("nope", cel.StringType)
	builder, _ = builder.AddPolicyBindingAfter(nil, "foo", "first", cel.BoolType)
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate("foo", "!first"); err != nil {
			t.Fatalf("Should have not returned an error: %v", err)
		} else if ast == nil {
			t.Fatal("Ast should have returned for known policy binding")
		}
	}
}

func TestPolicyWithNotYetKnownPolicyBinding(t *testing.T) {
	builder := NewValidatorBuilder()
	builder.AddBinding("nope", cel.StringType)
	first := "foo"
	builder, _ = builder.AddPolicyBindingAfter(nil, first, "first", cel.BoolType)
	builder, _ = builder.AddPolicyBindingAfter(&first, "bar", "second", cel.BoolType)
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate(first, "!second"); ast != nil {
			t.Fatalf("Should have not returned an ast: %v", ast)
		} else if err == nil {
			t.Fatal("Should have returned an error")
		}
	}
}
