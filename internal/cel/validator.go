package cel

import (
	"fmt"
	"slices"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
)

type ValidatorBuilder struct {
	baseBindings  map[string]binding
	baseFunctions map[string]funcBinding
	policies      []policyBinding
}

type policyBinding struct {
	policy  string
	binding binding
}

type binding struct {
	name string
	t    *cel.Type
}

type funcBinding struct {
	name    string
	funcOpt cel.FunctionOpt
}

func NewValidatorBuilder() *ValidatorBuilder {
	return &ValidatorBuilder{
		baseBindings:  make(map[string]binding),
		baseFunctions: make(map[string]funcBinding),
	}
}

func (b *ValidatorBuilder) AddBinding(name string, t *cel.Type) *ValidatorBuilder {
	b.baseBindings[name] = binding{
		name: name,
		t:    t,
	}
	return b
}

func (b *ValidatorBuilder) AddFunction(name string, funcOpt cel.FunctionOpt) *ValidatorBuilder {
	b.baseFunctions[name] = funcBinding{
		name:    name,
		funcOpt: funcOpt,
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
		idx := slices.IndexFunc(b.policies, func(pb policyBinding) bool {
			return pb.policy == *after
		})
		if idx < 0 {
			return nil, fmt.Errorf("policy %s not found", *after)
		}
		suffix := append([]policyBinding{p}, b.policies[idx+1:]...)
		b.policies = append(b.policies[:idx+1], suffix...)
	}
	return b, nil
}

func (b *ValidatorBuilder) PushPolicyBinding(policy string, name string, t *cel.Type) *ValidatorBuilder {
	p := policyBinding{
		policy: policy,
		binding: binding{
			name: name,
			t:    t,
		},
	}

	b.policies = append(b.policies, p)
	return b
}

func (b *ValidatorBuilder) Build() (*Validator, error) {
	var envs = make(map[string]*cel.Env)

	for _, policy := range b.policies {
		var env *cel.Env
		var err error
		opts := []cel.EnvOption{
			ext.Strings(),
			cel.OptionalTypes(),
		}

		for _, binding := range b.baseBindings {
			opts = append(opts, cel.Variable(binding.name, binding.t))
		}

		for _, binding := range b.baseFunctions {
			opts = append(opts, cel.Function(binding.name, binding.funcOpt))
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
