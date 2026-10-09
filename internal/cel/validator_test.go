package cel

import (
	"testing"

	"github.com/google/cel-go/cel"
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

func TestPolicyWithUnknownPolicyBinding(t *testing.T) {
	builder := NewValidatorBuilder()
	builder.AddBinding("nope", cel.StringType)
	builder, _ = builder.AddPolicyBindingAfter(nil, "foo", "first", cel.BoolType)
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate("fo", "!first"); ast != nil {
			t.Fatal("No ast should have returned for unknown policy")
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

func TestPolicyWithAnyKnownPolicyBinding(t *testing.T) {
	builder := NewValidatorBuilder()
	builder.AddBinding("nope", cel.StringType)
	first := "foo"
	second := "bar"
	builder, _ = builder.AddPolicyBindingAfter(nil, first, "first", cel.AnyType)
	builder, _ = builder.AddPolicyBindingAfter(&first, second, "second", cel.AnyType)
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate(first, "!first.randomField"); err != nil {
			t.Fatalf("Should have not returned an error: %v", err)
		} else if ast == nil {
			t.Fatal("Ast should have returned for known policy binding")
		}
		if ast, err := validator.Validate(second, "!first.randomField"); err != nil {
			t.Fatalf("Should have not returned an error: %v", err)
		} else if ast == nil {
			t.Fatal("Ast should have returned for known policy binding")
		}
		if ast, err := validator.Validate(second, "!second.randomField"); err != nil {
			t.Fatalf("Should have not returned an error: %v", err)
		} else if ast == nil {
			t.Fatal("Ast should have returned for known policy binding")
		}
	}
}

func TestPushPolicyBinding(t *testing.T) {
	builder := NewValidatorBuilder()
	builder.AddBinding("root", cel.StringType)
	first := "foo"
	second := "bar"
	builder = builder.PushPolicyBinding(first, "first", cel.AnyType)
	builder = builder.PushPolicyBinding(second, "second", cel.AnyType)
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate(first, "!first.randomField"); err != nil {
			t.Fatalf("Should have not returned an error: %v", err)
		} else if ast == nil {
			t.Fatal("Ast should have returned for known policy binding")
		}
		if ast, err := validator.Validate(second, "!first.randomField"); err != nil {
			t.Fatalf("Should have not returned an error: %v", err)
		} else if ast == nil {
			t.Fatal("Ast should have returned for known policy binding")
		}
		if ast, err := validator.Validate(second, "!second.randomField"); err != nil {
			t.Fatalf("Should have not returned an error: %v", err)
		} else if ast == nil {
			t.Fatal("Ast should have returned for known policy binding")
		}
		if ast, err := validator.Validate(first, "!second.randomField"); err == nil {
			t.Fatalf("Should have returned an error")
		} else if ast != nil {
			t.Fatalf("Should have not returned an ast: %v", ast)
		}
		if ast, err := validator.Validate("baz", "!second.randomField"); err == nil {
			t.Fatalf("Should have returned an error")
		} else if ast != nil {
			t.Fatal("Ast should have not returned for unknown policy binding")
		}
	}
}

func TestValidatorSupportsStringExtensions(t *testing.T) {
	builder := NewValidatorBuilder()
	if _, err := builder.AddPolicyBindingAfter(nil, "foo", "first", cel.AnyType); err != nil {
		t.Fatal(err)
	}

	validator, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		expr string
	}{
		{name: "format", expr: "'Hello, %s!'.format(['World'])"},
		{name: "lowerAscii", expr: "'ABC'.lowerAscii()"},
		{name: "upperAscii", expr: "'abc'.upperAscii()"},
		{name: "charAt", expr: "'abc'.charAt(1)"},
		{name: "indexOf", expr: "'foobarbaz'.indexOf('bar')"},
		{name: "replace", expr: "'foobarbaz'.replace('bar', 'qux')"},
		{name: "substring", expr: "'foobarbaz'.substring(3, 6)"},
		{name: "split", expr: "'a,b,c'.split(',')"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if ast, err := validator.Validate("foo", tc.expr); err != nil {
				t.Fatalf("unexpected error validating expr '%s': %v", tc.expr, err)
			} else if ast == nil {
				t.Fatalf("expected non-nil AST for expr '%s'", tc.expr)
			}
		})
	}
}

func TestValidatorSupportsOptionalExpression(t *testing.T) {
	builder := NewValidatorBuilder()
	if _, err := builder.AddPolicyBindingAfter(nil, "foo", "first", cel.AnyType); err != nil {
		t.Fatal(err)
	}

	validator, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		expr string
	}{
		{name: "optional syntax", expr: "first.?randomField"},
		{name: "optional orValue", expr: "first.?randomField.orValue('none')"},
		{name: "optional of", expr: "optional.of(10)"},
		{name: "optional ofNonZeroValue", expr: "optional.ofNonZeroValue([])"},
		{name: "optional hasValue", expr: "optional.of(first.randomField).hasValue()"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if ast, err := validator.Validate("foo", tc.expr); err != nil {
				t.Fatalf("unexpected error validating expr '%s': %v", tc.expr, err)
			} else if ast == nil {
				t.Fatalf("expected non-nil AST for expr '%s'", tc.expr)
			}
		})
	}
}
