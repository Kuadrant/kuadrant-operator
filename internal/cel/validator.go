package cel

import (
	"fmt"
	"github.com/google/cel-go/cel"
)

type ValidatorBuilder struct {
	baseBindings []binding
	policies     []policyBinding
}

type policyBinding struct {
	policy  string
	binding binding
}

type binding struct {
	name string
}

func NewValidatorBuilder() *ValidatorBuilder {
	return &ValidatorBuilder{}
}

func (b *ValidatorBuilder) AddBinding(name string) *ValidatorBuilder {
	return b
}

func (b *ValidatorBuilder) AddPolicyBindingAfter(after *string, policy string, name string) (*ValidatorBuilder, error) {
	p := policyBinding{
		policy:  policy,
		binding: binding{name: name},
	}
	if after == nil {
		b.policies = append([]policyBinding{p}, b.policies...)
	} else {
		idx := -1
		for i, p := range b.policies {
			if p.policy == *after {
				idx = i
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("policy %s not found", *after)
		} else {
			suffix := append([]policyBinding{p}, b.policies[idx+1:]...)
			b.policies = append(b.policies[:idx], suffix...)
		}
	}
	return b, nil
}

func (b *ValidatorBuilder) Build() (*Validator, error) {
	return &Validator{
		baseBindings: b.baseBindings,
		policies:     b.policies,
	}, nil
}

type Validator struct {
	baseBindings []binding
	policies     []policyBinding
}

func (v *Validator) Validate(policy string, ast *cel.Ast) (*cel.Ast, error) {
	if ast == nil {
		return nil, fmt.Errorf("AST is nil")
	}
	var env *cel.Env
	var err error
	if env, err = cel.NewEnv(); err != nil {
		return nil, err
	}

	found := false
	for _, p := range v.policies {
		if p.policy == policy {
			found = true
		}
	}

	if !found {
		return nil, fmt.Errorf("no policy matching `%s`", policy)
	}

	if cAst, iss := env.Check(ast); iss.Err() != nil {
		return nil, iss.Err()
	} else {
		return cAst, nil
	}
}
