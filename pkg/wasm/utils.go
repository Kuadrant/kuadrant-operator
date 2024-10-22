package wasm

import (
	"crypto/sha256"
	"encoding/hex"
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
	RateLimitServiceName = "ratelimit-service"
	AuthServiceName      = "auth-service"
)

func ExtensionName(gatewayName string) string {
	return fmt.Sprintf("kuadrant-%s", gatewayName)
}

func BuildConfigForActionSet(actionSets []ActionSet) Config {
	return Config{
		Services: map[string]Service{
			RateLimitServiceName: {
				Type:        RateLimitServiceType,
				Endpoint:    common.KuadrantRateLimitClusterName,
				FailureMode: FailureModeAllow,
			},
			// TODO: add auth extension
		},
		ActionSets: actionSets,
	}
}

type ActionBuilderFunc func(uniquePolicyRuleKey string, policyRule kuadrantv1.MergeableRule) (Action, error)

func BuildActionSetsForPath(pathID string, path []machinery.Targetable, policyRules map[string]kuadrantv1.MergeableRule, actionBuilder ActionBuilderFunc) ([]kuadrantgatewayapi.HTTPRouteMatchConfig, error) {
	_, _, listener, httpRoute, httpRouteRule, err := common.ObjectsInRequestPath(path)
	if err != nil {
		return nil, err
	}

	actions := lo.FilterMap(lo.Entries(policyRules), func(r lo.Entry[string, kuadrantv1.MergeableRule], _ int) (Action, bool) {
		action, err := actionBuilder(r.Key, r.Value)
		if err != nil {
			errors.Join(err)
			return Action{}, false
		}
		return action, true
	})

	return lo.FlatMap(kuadrantgatewayapi.HostnamesFromListenerAndHTTPRoute(listener.Listener, httpRoute.HTTPRoute), func(hostname gatewayapiv1.Hostname, _ int) []kuadrantgatewayapi.HTTPRouteMatchConfig {
		return lo.Map(httpRouteRule.Matches, func(httpRouteMatch gatewayapiv1.HTTPRouteMatch, j int) kuadrantgatewayapi.HTTPRouteMatchConfig {
			actionSet := ActionSet{
				Name:    ActionSetNameForPath(pathID, j, string(hostname)),
				Actions: actions,
			}
			routeRuleConditions := RouteRuleConditions{
				Hostnames: []string{string(hostname)},
			}
			if predicates := PredicatesFromHTTPRouteMatch(httpRouteMatch); len(predicates) > 0 {
				routeRuleConditions.Matches = predicates
			}
			actionSet.RouteRuleConditions = routeRuleConditions
			return kuadrantgatewayapi.HTTPRouteMatchConfig{
				Hostname:          string(hostname),
				HTTPRouteMatch:    httpRouteMatch,
				CreationTimestamp: httpRoute.GetCreationTimestamp(),
				Namespace:         httpRoute.GetNamespace(),
				Name:              httpRoute.GetName(),
				Config:            actionSet,
			}
		})
	}), err
}

func ActionSetNameForPath(pathID string, httpRouteMatchIndex int, hostname string) string {
	source := fmt.Sprintf("%s|%d|%s", pathID, httpRouteMatchIndex+1, hostname)
	hash := sha256.Sum256([]byte(source))
	return hex.EncodeToString(hash[:])
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

// PredicatesFromWhenConditions builds a list of predicates from a list of (selector, operator, value) when conditions
func PredicatesFromWhenConditions(when ...kuadrantv1beta3.WhenCondition) []Predicate {
	return lo.Map(when, func(when kuadrantv1beta3.WhenCondition, _ int) Predicate {
		return Predicate{
			Selector: when.Selector,
			Operator: PatternOperator(when.Operator),
			Value:    when.Value,
		}
	})
}

// PredicatesFromHTTPRouteMatch builds a list of conditions from a rule match
func PredicatesFromHTTPRouteMatch(match gatewayapiv1.HTTPRouteMatch) []Predicate {
	predicates := make([]Predicate, 0)

	// method
	if match.Method != nil {
		predicates = append(predicates, predicateFromMethod(*match.Method))
	}

	// path
	if match.Path != nil {
		predicates = append(predicates, predicateFromPathMatch(*match.Path))
	}

	// headers
	for _, headerMatch := range match.Headers {
		// Multiple match values are ANDed together
		predicates = append(predicates, predicateFromHeader(headerMatch))
	}

	// TODO(eguzki): query params. Investigate integration with wasm regarding Envoy params
	// from https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/attributes
	// request.query -> string : The query portion of the URL in the format of “name1=value1&name2=value2”.

	return predicates
}

func predicateFromPathMatch(pathMatch gatewayapiv1.HTTPPathMatch) Predicate {
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

	return Predicate{
		Selector: "request.url_path",
		Operator: operator,
		Value:    value,
	}
}

func predicateFromMethod(method gatewayapiv1.HTTPMethod) Predicate {
	return Predicate{
		Selector: "request.method",
		Operator: PatternOperator(kuadrantv1beta3.EqualOperator),
		Value:    string(method),
	}
}

func predicateFromHeader(headerMatch gatewayapiv1.HTTPHeaderMatch) Predicate {
	// As for gateway api v1, the only operation type with core support is Exact match.
	// https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPHeaderMatch

	return Predicate{
		Selector: kuadrantv1beta3.ContextSelector(fmt.Sprintf("request.headers.%s", headerMatch.Name)),
		Operator: PatternOperator(kuadrantv1beta3.EqualOperator),
		Value:    headerMatch.Value,
	}
}
