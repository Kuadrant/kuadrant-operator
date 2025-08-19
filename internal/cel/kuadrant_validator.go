package cel

import (
	"github.com/google/cel-go/cel"

	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

const (
	AuthPolicyKind           = "AuthPolicy"
	RateLimitPolicyKind      = "RateLimitPolicy"
	TokenRateLimitPolicyKind = "TokenRateLimitPolicy"

	AuthPolicyName = "auth"
	RateLimitName  = "ratelimit"
)

func NewRootValidatorBuilder() *ValidatorBuilder {
	builder := NewValidatorBuilder()
	// TODO: correct cel types
	builder.AddBinding("request", cel.AnyType)
	builder.AddBinding("source", cel.AnyType)
	builder.AddBinding("destination", cel.AnyType)
	builder.AddBinding("connection", cel.AnyType)
	return builder
}

func ValidateWasmAction(action wasm.Action, validator *Validator) error {
	pol := policyKindFromWasmServiceName(action.ServiceName)
	for _, predicate := range action.Predicates {
		if _, err := validator.Validate(pol, predicate); err != nil {
			return err
		}
	}
	for _, conditionalData := range action.ConditionalData {
		for _, predicate := range conditionalData.Predicates {
			if _, err := validator.Validate(pol, predicate); err != nil {
				return err
			}
		}
	}
	return nil
}

func policyKindFromWasmServiceName(serviceName string) string {
	switch serviceName {
	case wasm.AuthServiceName:
		return AuthPolicyKind
	case wasm.RateLimitServiceName:
		return RateLimitPolicyKind
	case wasm.RateLimitCheckServiceName:
		return TokenRateLimitPolicyKind
	case wasm.RateLimitReportServiceName:
		return TokenRateLimitPolicyKind
	default:
		return RateLimitPolicyKind
	}
}
