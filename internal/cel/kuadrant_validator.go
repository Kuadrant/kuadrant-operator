package cel

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"github.com/samber/lo"

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

func NewIssue(spec wasm.ActionSpec, pathID string, err error) *Issue {
	return &Issue{
		policyKind: policyKindFromWasmServiceName(spec.ServiceName),
		pathID:     pathID,
		err:        err,
	}
}

func (i *Issue) GetError() error {
	return i.err
}

type IssueCollection struct {
	issues []*Issue
}

func NewIssueCollection() *IssueCollection {
	return &IssueCollection{}
}

func (c *IssueCollection) IsEmpty() bool {
	return len(c.issues) == 0
}

func (c *IssueCollection) GetByPolicyKind(policyKind string) (map[string][]*Issue, bool) {
	filteredIssues := lo.Filter(c.issues, func(issue *Issue, _ int) bool {
		return issue.policyKind == policyKind
	})

	if len(filteredIssues) == 0 {
		return nil, false
	}

	groupedByPathID := lo.GroupBy(filteredIssues, func(issue *Issue) string {
		return issue.pathID
	})

	return groupedByPathID, true
}

func (c *IssueCollection) Add(issue *Issue) {
	c.issues = append(c.issues, issue)
}

func NewRootValidatorBuilder() *ValidatorBuilder {
	builder := NewValidatorBuilder()
	// TODO: correct cel types
	builder.AddBinding("request", cel.AnyType)
	builder.AddBinding("source", cel.AnyType)
	builder.AddBinding("destination", cel.AnyType)
	builder.AddBinding("connection", cel.AnyType)

	noopUnary := func(_ ref.Val) ref.Val {
		// just for parsing and checking purposes, not evaluation
		return nil
	}
	noopBinary := func(_, _ ref.Val) ref.Val {
		// just for parsing and checking purposes, not evaluation
		return nil
	}

	requestBodyJSONString := cel.Overload("request_body_json_string",
		[]*cel.Type{cel.StringType}, cel.AnyType, cel.UnaryBinding(noopUnary))
	requestBodyJSONList := cel.Overload("request_body_json_list",
		[]*cel.Type{cel.ListType(cel.StringType)}, cel.AnyType, cel.UnaryBinding(noopUnary))
	requestBodyJSONListWithHint := cel.Overload("request_body_json_list_with_type_hint",
		[]*cel.Type{cel.ListType(cel.StringType), cel.StringType}, cel.AnyType, cel.BinaryBinding(noopBinary))

	builder.AddFunction("requestBodyJSON", requestBodyJSONString, requestBodyJSONList, requestBodyJSONListWithHint)

	responseBodyJSONString := cel.Overload("response_body_json_string",
		[]*cel.Type{cel.StringType}, cel.AnyType, cel.UnaryBinding(noopUnary))
	responseBodyJSONList := cel.Overload("response_body_json_list",
		[]*cel.Type{cel.ListType(cel.StringType)}, cel.AnyType, cel.UnaryBinding(noopUnary))
	responseBodyJSONListWithHint := cel.Overload("response_body_json_list_with_type_hint",
		[]*cel.Type{cel.ListType(cel.StringType), cel.StringType}, cel.AnyType, cel.BinaryBinding(noopBinary))

	builder.AddFunction("responseBodyJSON", responseBodyJSONString, responseBodyJSONList, responseBodyJSONListWithHint)

	return builder
}

func ValidateWasmActionSpec(spec wasm.ActionSpec, validator *Validator) error {
	pol := policyKindFromWasmServiceName(spec.ServiceName)
	for _, predicate := range spec.Predicates {
		if _, err := validator.Validate(pol, predicate); err != nil {
			return err
		}
	}
	for _, conditionalData := range spec.ConditionalData {
		for _, predicate := range conditionalData.Predicates {
			if _, err := validator.Validate(pol, predicate); err != nil {
				return err
			}
		}
	}
	if amount := spec.ReservationAmountCEL(); amount != "" {
		if _, err := validator.Validate(pol, amount); err != nil {
			return err
		}
	}
	if ttl := spec.ReservationTTLCEL(); ttl != "" {
		ast, err := validator.Validate(pol, ttl)
		if err != nil {
			return err
		}
		if ast.OutputType() != cel.DurationType {
			return fmt.Errorf("reservation ttl expression must evaluate to duration, got %s", ast.OutputType())
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
	case wasm.RateLimitCheckServiceName, wasm.RateLimitReserveServiceName:
		return TokenRateLimitPolicyKind
	case wasm.RateLimitReportServiceName, wasm.RateLimitCommitServiceName:
		return TokenRateLimitPolicyKind
	default:
		return RateLimitPolicyKind
	}
}
