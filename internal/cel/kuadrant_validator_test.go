package cel

import (
	"testing"

	"github.com/google/cel-go/cel"
	"gotest.tools/assert"

	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

func TestNewRootValidatorBuilder(t *testing.T) {
	builder := NewRootValidatorBuilder()
	pol := "foo"
	builder.PushPolicyBinding(pol, "foo", cel.AnyType)
	if validator, err := builder.Build(); err != nil {
		t.Fatal(err)
	} else {
		if ast, err := validator.Validate("foo", "other == 1"); ast != nil {
			t.Fatal("No ast should have returned for not known root expression")
		} else if err == nil {
			t.Fatal("Should have returned an error")
		}
		if ast, err := validator.Validate("foo", "request.id == 1"); ast == nil {
			t.Fatal("Should have return a valid ast")
		} else if err != nil {
			t.Fatalf("Should not have returned an error %v", err)
		}
	}
}

func TestValidateWasmActionInvalidNoAuth(t *testing.T) {
	wasmAction := wasm.Action{
		ServiceName: wasm.RateLimitServiceName,
		Scope:       "scope",
		Predicates:  []string{"request.id == 1"},
		ConditionalData: []wasm.ConditionalData{
			{
				Predicates: []string{"auth.identity == 'anonymous'"},
			},
		},
	}
	builder := NewRootValidatorBuilder()
	builder.PushPolicyBinding(RateLimitPolicyKind, RateLimitName, cel.AnyType)
	validator, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}

	assert.ErrorContains(t, ValidateWasmAction(wasmAction, validator), "undeclared reference to 'auth'")
}

func TestValidateWasmActionInvalidWrongDependency(t *testing.T) {
	wasmAction := wasm.Action{
		ServiceName: wasm.RateLimitServiceName,
		Scope:       "scope",
		Predicates:  []string{"auth == 'something'"},
	}
	builder := NewRootValidatorBuilder()
	builder.PushPolicyBinding(RateLimitPolicyKind, RateLimitName, cel.AnyType)
	builder.PushPolicyBinding(AuthPolicyKind, AuthPolicyName, cel.AnyType)
	validator, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}

	assert.ErrorContains(t, ValidateWasmAction(wasmAction, validator), "undeclared reference to 'auth'")
}

func TestValidateWasmActionValid(t *testing.T) {
	wasmAction := wasm.Action{
		ServiceName: wasm.RateLimitServiceName,
		Scope:       "scope",
		Predicates:  []string{"request.id == 1"},
		ConditionalData: []wasm.ConditionalData{
			{
				Predicates: []string{"auth.identity == 'anonymous'"},
			},
		},
	}
	builder := NewRootValidatorBuilder()
	builder.PushPolicyBinding(AuthPolicyKind, AuthPolicyName, cel.AnyType)
	builder.PushPolicyBinding(RateLimitPolicyKind, RateLimitName, cel.AnyType)
	validator, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}

	assert.NilError(t, ValidateWasmAction(wasmAction, validator))
}
