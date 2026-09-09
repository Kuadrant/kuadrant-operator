package cel

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"github.com/samber/lo"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

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

	requestBodyJSON := cel.Overload("request_body_json_string",
		[]*cel.Type{cel.StringType},
		cel.AnyType,
		cel.UnaryBinding(func(_ ref.Val) ref.Val {
			// just for parsing and checking purposes, not evaluation
			return nil
		},
		),
	)

	builder.AddFunction("requestBodyJSON", requestBodyJSON)

	responseBodyJSON := cel.Overload("response_body_json_string",
		[]*cel.Type{cel.StringType},
		cel.AnyType,
		cel.UnaryBinding(func(_ ref.Val) ref.Val {
			// just for parsing and checking purposes, not evaluation
			return nil
		},
	)
	builder.AddFunction("responseBodyJSON", responseBodyJSON)

	return builder
}

func ValidateWasmActionSpec(spec wasm.ActionSpec, validator *Validator) error {
	pol := policyKindFromWasmServiceName(spec.ServiceName)
	for _, predicate := range spec.Predicates {
		if err := validatePredicate(spec, pol, predicate, validator); err != nil {
			return err
		}
	}
	for _, conditionalData := range spec.ConditionalData {
		for _, predicate := range conditionalData.Predicates {
			if err := validatePredicate(spec, pol, predicate, validator); err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePredicate(spec wasm.ActionSpec, policyKind, predicate string, validator *Validator) error {
	ast, err := validator.Validate(policyKind, predicate)
	if err != nil {
		return err
	}

	// Token rate limit predicates are shared by both the pre-request check and
	// post-response report. A response-only function in the guard therefore
	// cannot be evaluated when the limit decision is made and may silently
	// bypass enforcement at runtime.
	if policyKind == TokenRateLimitPolicyKind && spec.IsGuard() {
		usesResponseBody, err := astCallsFunction(ast, "responseBodyJSON")
		if err != nil {
			return fmt.Errorf("failed to inspect CEL predicate: %w", err)
		}
		if usesResponseBody {
			return fmt.Errorf("responseBodyJSON is not available in TokenRateLimitPolicy when predicates because they are evaluated during the request phase")
		}
	}

	return nil
}

func astCallsFunction(ast *cel.Ast, function string) (bool, error) {
	parsed, err := cel.AstToParsedExpr(ast)
	if err != nil {
		return false, err
	}
	return exprCallsFunction(parsed.GetExpr(), function), nil
}

func exprCallsFunction(expr *exprpb.Expr, function string) bool {
	if expr == nil {
		return false
	}

	switch kind := expr.ExprKind.(type) {
	case *exprpb.Expr_CallExpr:
		if kind.CallExpr.GetFunction() == function {
			return true
		}
		if exprCallsFunction(kind.CallExpr.GetTarget(), function) {
			return true
		}
		for _, arg := range kind.CallExpr.GetArgs() {
			if exprCallsFunction(arg, function) {
				return true
			}
		}
	case *exprpb.Expr_SelectExpr:
		return exprCallsFunction(kind.SelectExpr.GetOperand(), function)
	case *exprpb.Expr_ListExpr:
		for _, element := range kind.ListExpr.GetElements() {
			if exprCallsFunction(element, function) {
				return true
			}
		}
	case *exprpb.Expr_StructExpr:
		for _, entry := range kind.StructExpr.GetEntries() {
			if exprCallsFunction(entry.GetMapKey(), function) || exprCallsFunction(entry.GetValue(), function) {
				return true
			}
		}
	case *exprpb.Expr_ComprehensionExpr:
		c := kind.ComprehensionExpr
		return exprCallsFunction(c.GetIterRange(), function) ||
			exprCallsFunction(c.GetAccuInit(), function) ||
			exprCallsFunction(c.GetLoopCondition(), function) ||
			exprCallsFunction(c.GetLoopStep(), function) ||
			exprCallsFunction(c.GetResult(), function)
	}

	return false
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
