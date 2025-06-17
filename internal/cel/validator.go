package cel

import (
	"fmt"

	"github.com/google/cel-go/cel"
)

type ValidatorBuilder struct {
	baseBindings map[string]binding
	policies     []policyBinding
}

type policyBinding struct {
	policy  string
	binding binding
}

type binding struct {
	name string
	t    *cel.Type
}

func NewValidatorBuilder() *ValidatorBuilder {
	return &ValidatorBuilder{
		baseBindings: make(map[string]binding),
	}
}

func (b *ValidatorBuilder) AddBinding(name string, t *cel.Type) *ValidatorBuilder {
	b.baseBindings[name] = binding{
		name: name,
		t:    t,
	}
	return b
}

func (b *ValidatorBuilder) AddPolicyBindingAfter(after *string, policy string, name string, t *cel.Type) (*ValidatorBuilder, error) {
	p := policyBinding{
		policy: policy,
		binding: binding{
			name: name,
			t:    t,
		},
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
		}
		suffix := append([]policyBinding{p}, b.policies[idx+1:]...)
		b.policies = append(b.policies[:idx], suffix...)
	}
	return b, nil
}

func (b *ValidatorBuilder) Build() (*Validator, error) {
	var envs = make(map[string]*cel.Env)

	for _, policy := range b.policies {
		var env *cel.Env
		var err error
		var opts []cel.EnvOption

		for _, binding := range b.baseBindings {
			opts = append(opts, cel.Variable(binding.name, binding.t))
		}

		for _, p := range b.policies {
			opts = append(opts,
				cel.Types(p.binding.t),
				cel.Variable(p.binding.name, p.binding.t),
			)
			if p.policy == policy.policy {
				break
			}
		}

		if env, err = cel.NewEnv(opts...); err != nil {
			return nil, err
		}

		envs[policy.policy] = env
	}

	return &Validator{
		envs: envs,
	}, nil
}

type Validator struct {
	envs map[string]*cel.Env
}

func (v *Validator) Validate(policy string, expr string) (*cel.Ast, error) {
	env := v.envs[policy]
	if env == nil {
		return nil, fmt.Errorf("no policy matching `%s`", policy)
	}

	var ast *cel.Ast
	var iss *cel.Issues

	if ast, iss = env.Parse(expr); iss.Err() != nil {
		return nil, iss.Err()
	}

	if ast, iss = env.Check(ast); iss.Err() != nil {
		return nil, iss.Err()
	}

	return ast, nil
}
