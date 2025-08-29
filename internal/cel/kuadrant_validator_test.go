package cel

import (
	"fmt"
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

func TestNewIssue(t *testing.T) {
	action := wasm.Action{
		ServiceName: wasm.RateLimitServiceName,
		Scope:       "scope",
		Predicates:  []string{"auth.identity == 'anonymous'"},
	}
	issue := NewIssue(action, "/test/pathID", fmt.Errorf("auth not there"))

	assert.ErrorContains(t, issue.GetError(), "auth not there")
	assert.Equal(t, issue.policyKind, RateLimitPolicyKind)
	assert.Equal(t, issue.pathID, "/test/pathID")
}

func TestIssueCollectionIsEmpty(t *testing.T) {
	collection := NewIssueCollection()
	assert.Equal(t, collection.IsEmpty(), true)

	action := wasm.Action{
		ServiceName: wasm.RateLimitServiceName,
		Scope:       "scope",
		Predicates:  []string{"auth.identity == 'anonymous'"},
	}
	issue := NewIssue(action, "/test/path", nil)

	collection.Add(issue)
	assert.Equal(t, collection.IsEmpty(), false)
}

func TestIssueCollectionGetByPolicyKind(t *testing.T) {
	collection := NewIssueCollection()
	action := wasm.Action{
		ServiceName: wasm.RateLimitServiceName,
		Scope:       "scope",
		Predicates:  []string{"auth.identity == 'anonymous'"},
	}
	issue := NewIssue(action, "/test/path", nil)
	collection.Add(issue)

	issues, found := collection.GetByPolicyKind(RateLimitPolicyKind)
	assert.Equal(t, found, true)
	assert.Equal(t, len(issues), 1)

	// Test non-existent policy kind
	_, found = collection.GetByPolicyKind("non-existent")
	assert.Equal(t, found, false)
}
