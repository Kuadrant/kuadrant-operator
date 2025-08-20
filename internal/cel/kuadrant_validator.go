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

var StateCELValidationErrors = "CELValidationErrors"

type Issue struct {
	policyKind string
	pathID     string
	err        error
}

func NewIssue(action wasm.Action, pathID string, err error) *Issue {
	return &Issue{
		policyKind: policyKindFromWasmServiceName(action.ServiceName),
		pathID:     pathID,
		err:        err,
	}
}

func (i *Issue) GetError() error {
	return i.err
}

type IssueCollection struct {
	issues map[string]map[string][]*Issue
}

func NewIssueCollection() *IssueCollection {
	return &IssueCollection{
		issues: make(map[string]map[string][]*Issue),
	}
}

func (c *IssueCollection) IsEmpty() bool {
	return len(c.issues) == 0
}

func (c *IssueCollection) GetByPolicyKind(policyKind string) (map[string][]*Issue, bool) {
	issues := c.issues[policyKind]
	return issues, len(issues) > 0
}

func (c *IssueCollection) Add(issue *Issue) {
	if _, exists := c.issues[issue.policyKind]; !exists {
		c.issues[issue.policyKind] = make(map[string][]*Issue)
	}

	if _, exists := c.issues[issue.policyKind][issue.pathID]; !exists {
		c.issues[issue.policyKind][issue.pathID] = make([]*Issue, 0)
	}

	c.issues[issue.policyKind][issue.pathID] = append(c.issues[issue.policyKind][issue.pathID], issue)
}

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
