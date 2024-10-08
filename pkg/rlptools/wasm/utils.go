package wasm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"unicode"

	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/types"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const (
	LimitadorRateLimitIdentifierPrefix = "limit."
	RateLimitPolicyExtensionName       = "limitador"
)

func LimitsNamespaceFromRoute(route *gatewayapiv1.HTTPRoute) string {
	return types.NamespacedName{Name: route.GetName(), Namespace: route.GetNamespace()}.String()
}

func LimitNameToLimitadorIdentifier(rlpKey types.NamespacedName, uniqueLimitName string) string {
	identifier := LimitadorRateLimitIdentifierPrefix

	// sanitize chars that are not allowed in limitador identifiers
	for _, c := range uniqueLimitName {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' {
			identifier += string(c)
		} else {
			identifier += "_"
		}
	}

	// to avoid breaking the uniqueness of the limit name after sanitization, we add a hash of the original name
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s", rlpKey.String(), uniqueLimitName)))
	identifier += "__" + hex.EncodeToString(hash[:4])

	return identifier
}

func RateLimitConfig(policies []Policy) Config {
	return Config{
		Extensions: map[string]Extension{
			RateLimitPolicyExtensionName: {
				Endpoint:    common.KuadrantRateLimitClusterName,
				FailureMode: FailureModeAllow,
				Type:        RateLimitExtensionType,
			},
		},
		Policies: policies,
	}
}

// RuleFromLimit builds a wasm rate-limit rule for a given limit.
// Conditions are built from the limit top-level conditions and the route rule's HTTPRouteMatches.
// The order of the conditions is as follows:
//  1. Route-level conditions: HTTP method, path, headers
//  2. Top-level conditions: 'when' conditions (blended into each block of route-level conditions)
//
// The only action of the rule is the rate-limit policy extension, whose data includes the activation of the limit
// and any counter qualifier of the limit.
func RuleFromLimit(limit kuadrantv1beta3.Limit, limitIdentifier, scope string, routeRule gatewayapiv1.HTTPRouteRule) Rule {
	rule := Rule{
		Conditions: conditionsFromLimit(limit, routeRule),
	}

	if data := dataFromLimit(limitIdentifier, limit); data != nil {
		rule.Actions = []Action{
			{
				Scope:         scope,
				ExtensionName: RateLimitPolicyExtensionName,
				Data:          data,
			},
		}
	}

	return rule
}

func conditionsFromLimit(limit kuadrantv1beta3.Limit, routeRule gatewayapiv1.HTTPRouteRule) []Condition {
	ruleConditions := conditionsFromRule(routeRule)

	// only rule conditions (or no condition at all)
	if len(limit.When) == 0 {
		if len(ruleConditions) == 0 {
			return nil
		}
		return ruleConditions
	}

	whenConditionToWasmPatternExpressionFunc := func(when kuadrantv1beta3.WhenCondition, _ int) PatternExpression {
		return patternExpresionFromWhen(when)
	}

	// top-level conditions merged into the rule conditions
	if len(ruleConditions) > 0 {
		return lo.Map(ruleConditions, func(condition Condition, _ int) Condition {
			condition.AllOf = append(condition.AllOf, lo.Map(limit.When, whenConditionToWasmPatternExpressionFunc)...)
			return condition
		})
	}

	// only top-level conditions
	return []Condition{{AllOf: lo.Map(limit.When, whenConditionToWasmPatternExpressionFunc)}}
}

// conditionsFromRule builds a list of conditions from a rule
// rules that specify no explicit match are assumed to match all request (i.e. implicit catch-all rule)
func conditionsFromRule(rule gatewayapiv1.HTTPRouteRule) []Condition {
	return utils.Map(rule.Matches, func(match gatewayapiv1.HTTPRouteMatch) Condition {
		return Condition{AllOf: patternExpresionsFromMatch(match)}
	})
}

func patternExpresionsFromMatch(match gatewayapiv1.HTTPRouteMatch) []PatternExpression {
	expressions := make([]PatternExpression, 0)

	// method
	if match.Method != nil {
		expressions = append(expressions, patternExpresionFromMethod(*match.Method))
	}

	// path
	if match.Path != nil {
		expressions = append(expressions, patternExpresionFromPathMatch(*match.Path))
	}

	// headers
	for _, headerMatch := range match.Headers {
		// Multiple match values are ANDed together
		expressions = append(expressions, patternExpresionFromHeader(headerMatch))
	}

	// TODO(eguzki): query params. Investigate integration with wasm regarding Envoy params
	// from https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/attributes
	// request.query -> string : The query portion of the URL in the format of “name1=value1&name2=value2”.

	return expressions
}

func patternExpresionFromPathMatch(pathMatch gatewayapiv1.HTTPPathMatch) PatternExpression {
	var (
		operator = PatternOperator(kuadrantv1beta3.StartsWithOperator) // default value
		value    = "/"                                                 // default value
	)

	if pathMatch.Value != nil {
		value = *pathMatch.Value
	}

	if pathMatch.Type != nil {
		if val, ok := PathMatchTypeMap[*pathMatch.Type]; ok {
			operator = val
		}
	}

	return PatternExpression{
		Selector: "request.url_path",
		Operator: operator,
		Value:    value,
	}
}

func patternExpresionFromMethod(method gatewayapiv1.HTTPMethod) PatternExpression {
	return PatternExpression{
		Selector: "request.method",
		Operator: PatternOperator(kuadrantv1beta3.EqualOperator),
		Value:    string(method),
	}
}

func patternExpresionFromHeader(headerMatch gatewayapiv1.HTTPHeaderMatch) PatternExpression {
	// As for gateway api v1, the only operation type with core support is Exact match.
	// https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPHeaderMatch

	return PatternExpression{
		Selector: kuadrantv1beta3.ContextSelector(fmt.Sprintf("request.headers.%s", headerMatch.Name)),
		Operator: PatternOperator(kuadrantv1beta3.EqualOperator),
		Value:    headerMatch.Value,
	}
}

func patternExpresionFromWhen(when kuadrantv1beta3.WhenCondition) PatternExpression {
	return PatternExpression{
		Selector: when.Selector,
		Operator: PatternOperator(when.Operator),
		Value:    when.Value,
	}
}

func dataFromLimit(limitIdentifier string, limit kuadrantv1beta3.Limit) (data []DataType) {
	// static key representing the limit
	data = append(data,
		DataType{
			Value: &Static{
				Static: StaticSpec{Key: limitIdentifier, Value: "1"},
			},
		},
	)

	for _, counter := range limit.Counters {
		data = append(data,
			DataType{
				Value: &Selector{
					Selector: SelectorSpec{Selector: counter},
				},
			},
		)
	}

	return data
}
