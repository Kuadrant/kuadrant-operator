package wasm

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	_struct "google.golang.org/protobuf/types/known/structpb"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

const (
	// TODO(guicassolato): move these to github.com/kuadrant/kuadrant-operator/pkg/common (?)
	RateLimitExtensionName = "limitador"
	AuthExtensionName      = "authorino"
)

func ExtensionName(gatewayName string) string {
	return fmt.Sprintf("kuadrant-%s", gatewayName)
}

func BuildWasmConfigForPolicies(policies []Policy) Config {
	return Config{
		Extensions: map[string]Extension{
			RateLimitExtensionName: {
				Endpoint:    common.KuadrantRateLimitClusterName,
				FailureMode: FailureModeAllow,
				Type:        RateLimitExtensionType,
			},
			// TODO: add auth extension
		},
		Policies: policies,
	}
}

type RuleBuilderFunc func(httpRouteMatch gatewayapiv1.HTTPRouteMatch, uniquePolicyRuleKey string, policyRule kuadrantv1.MergeableRule) (Rule, error)

func BuildWasmPoliciesForPath(pathID string, path []machinery.Targetable, policyRules map[string]kuadrantv1.MergeableRule, wasmRuleBuilder RuleBuilderFunc) ([]kuadrantgatewayapi.HTTPRouteMatchConfig, error) {
	// assumes the path is always [gatewayclass, gateway, listener, httproute, httprouterule]
	listener, _ := path[2].(*machinery.Listener)
	httpRoute, _ := path[3].(*machinery.HTTPRoute)
	httpRouteRule, _ := path[4].(*machinery.HTTPRouteRule)

	var err error

	return lo.FlatMap(kuadrantgatewayapi.HostnamesFromListenerAndHTTPRoute(listener.Listener, httpRoute.HTTPRoute), func(hostname gatewayapiv1.Hostname, i int) []kuadrantgatewayapi.HTTPRouteMatchConfig {
		return lo.Map(httpRouteRule.Matches, func(httpRouteMatch gatewayapiv1.HTTPRouteMatch, j int) kuadrantgatewayapi.HTTPRouteMatchConfig {
			var wasmRules []Rule
			for uniquePolicyRuleKey, mergeablePolicyRule := range policyRules {
				wasmRule, err := wasmRuleBuilder(httpRouteMatch, uniquePolicyRuleKey, mergeablePolicyRule)
				if err != nil {
					errors.Join(err)
					continue
				}
				wasmRules = append(wasmRules, wasmRule)
			}

			return kuadrantgatewayapi.HTTPRouteMatchConfig{
				Hostname:          string(hostname),
				HTTPRouteMatch:    httpRouteMatch,
				CreationTimestamp: httpRoute.GetCreationTimestamp(),
				Namespace:         httpRoute.GetNamespace(),
				Name:              httpRoute.GetName(),
				Config: Policy{
					Name:      fmt.Sprintf("%d-%s-%d", i, pathID, j),
					Hostnames: []string{string(hostname)},
					Rules:     wasmRules,
				},
			}
		})
	}), err
}

func ConfigFromStruct(structure *_struct.Struct) (*Config, error) {
	if structure == nil {
		return nil, errors.New("cannot desestructure config from nil")
	}
	// Serialize struct into json
	configJSON, err := structure.MarshalJSON()
	if err != nil {
		return nil, err
	}
	// Deserialize protobuf struct into Config struct
	config := &Config{}
	if err := json.Unmarshal(configJSON, config); err != nil {
		return nil, err
	}

	return config, nil
}

func ConfigFromJSON(configJSON *apiextensionsv1.JSON) (*Config, error) {
	if configJSON == nil {
		return nil, errors.New("cannot desestructure config from nil")
	}

	config := &Config{}
	if err := json.Unmarshal(configJSON.Raw, config); err != nil {
		return nil, err
	}

	return config, nil
}

func ConditionsFromHTTPRouteMatch(routeMatch gatewayapiv1.HTTPRouteMatch, otherConditions ...kuadrantv1beta3.WhenCondition) []Condition {
	var ruleConditions []Condition
	if routeMatch.Path != nil || routeMatch.Method != nil || len(routeMatch.Headers) > 0 || len(routeMatch.QueryParams) > 0 {
		ruleConditions = append(ruleConditions, conditionsFromHTTPRouteMatch(routeMatch))
	}

	// only rule conditions (or no condition at all)
	if len(otherConditions) == 0 {
		if len(ruleConditions) == 0 {
			return nil
		}
		return ruleConditions
	}

	whenConditionToWasmPatternExpressionFunc := func(when kuadrantv1beta3.WhenCondition, _ int) PatternExpression {
		return patternExpresionFromWhenCondition(when)
	}

	// top-level conditions merged into the rule conditions
	if len(ruleConditions) > 0 {
		return lo.Map(ruleConditions, func(condition Condition, _ int) Condition {
			condition.AllOf = append(condition.AllOf, lo.Map(otherConditions, whenConditionToWasmPatternExpressionFunc)...)
			return condition
		})
	}

	// only top-level conditions
	return []Condition{{AllOf: lo.Map(otherConditions, whenConditionToWasmPatternExpressionFunc)}}
}

// conditionsFromHTTPRouteMatch builds a list of conditions from a rule match
func conditionsFromHTTPRouteMatch(match gatewayapiv1.HTTPRouteMatch) Condition {
	return Condition{AllOf: patternExpresionsFromMatch(match)}
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

func patternExpresionFromWhenCondition(when kuadrantv1beta3.WhenCondition) PatternExpression {
	return PatternExpression{
		Selector: when.Selector,
		Operator: PatternOperator(when.Operator),
		Value:    when.Value,
	}
}
