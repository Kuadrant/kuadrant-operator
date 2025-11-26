package wasm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/logr"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/protobuf/types/known/structpb"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/utils/env"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/internal/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	kuadrantpolicymachinery "github.com/kuadrant/kuadrant-operator/internal/policymachinery"
)

const (
	RateLimitServiceName       = "ratelimit-service"
	RateLimitCheckServiceName  = "ratelimit-check-service"
	RateLimitReportServiceName = "ratelimit-report-service"
	AuthServiceName            = "auth-service"
)

func AuthServiceTimeout() string {
	return env.GetString("AUTH_SERVICE_TIMEOUT", "200ms")
}

func AuthServiceFailureMode(logger *logr.Logger) FailureModeType {
	return parseFailureModeValue("AUTH_SERVICE_FAILURE_MODE", FailureModeDeny, logger)
}

func RatelimitServiceTimeout() string {
	return env.GetString("RATELIMIT_SERVICE_TIMEOUT", "100ms")
}

func RatelimitServiceFailureMode(logger *logr.Logger) FailureModeType {
	return parseFailureModeValue("RATELIMIT_SERVICE_FAILURE_MODE", FailureModeAllow, logger)
}

func RatelimitCheckServiceTimeout() string {
	return env.GetString("RATELIMIT_CHECK_SERVICE_TIMEOUT", "100ms")
}

func RatelimitCheckServiceFailureMode(logger *logr.Logger) FailureModeType {
	return parseFailureModeValue("RATELIMIT_CHECK_SERVICE_FAILURE_MODE", FailureModeAllow, logger)
}

func RatelimitReportServiceTimeout() string {
	return env.GetString("RATELIMIT_REPORT_SERVICE_TIMEOUT", "100ms")
}

func RatelimitReportServiceFailureMode(logger *logr.Logger) FailureModeType {
	return parseFailureModeValue("RATELIMIT_REPORT_SERVICE_FAILURE_MODE", FailureModeAllow, logger)
}

func parseFailureModeValue(envVarName string, defaultValue FailureModeType, logger *logr.Logger) FailureModeType {
	value := os.Getenv(envVarName)
	if value == "" {
		return defaultValue
	}

	switch value {
	case string(FailureModeAllow), string(FailureModeDeny):
		return FailureModeType(value)
	default:
		logger.Info("Warning: Invalid FailureMode value '%s' for %s. Using default value '%s'.\n", value, envVarName, defaultValue)
		return defaultValue
	}
}

func ExtensionName(gatewayName string) string {
	return fmt.Sprintf("kuadrant-%s", gatewayName)
}

func BuildConfigForActionSet(actionSets []ActionSet, logger *logr.Logger) Config {
	return Config{
		Services: map[string]Service{
			AuthServiceName: {
				Type:        AuthServiceType,
				Endpoint:    kuadrant.KuadrantAuthClusterName,
				FailureMode: AuthServiceFailureMode(logger),
				Timeout:     ptr.To(AuthServiceTimeout()),
			},
			RateLimitServiceName: {
				Type:        RateLimitServiceType,
				Endpoint:    kuadrant.KuadrantRateLimitClusterName,
				FailureMode: RatelimitServiceFailureMode(logger),
				Timeout:     ptr.To(RatelimitServiceTimeout()),
			},
			RateLimitCheckServiceName: {
				Type:        RateLimitCheckServiceType,
				Endpoint:    kuadrant.KuadrantRateLimitClusterName,
				FailureMode: RatelimitCheckServiceFailureMode(logger),
				Timeout:     ptr.To(RatelimitCheckServiceTimeout()),
			},
			RateLimitReportServiceName: {
				Type:        RateLimitReportServiceType,
				Endpoint:    kuadrant.KuadrantRateLimitClusterName,
				FailureMode: RatelimitReportServiceFailureMode(logger),
				Timeout:     ptr.To(RatelimitReportServiceTimeout()),
			},
		},
		ActionSets: actionSets,
	}
}

func BuildActionSetsForPath(ctx context.Context, pathID string, path []machinery.Targetable, actions []Action) ([]kuadrantgatewayapi.HTTPRouteMatchConfig, error) {
	tracer := controller.TracerFromContext(ctx)
	_, span := tracer.Start(ctx, "wasm.BuildActionSetsForPath")
	defer span.End()

	span.SetAttributes(
		attribute.String("path_id", pathID),
		attribute.Int("action_count", len(actions)),
	)

	_, _, listener, httpRoute, httpRouteRule, err := kuadrantpolicymachinery.ObjectsInRequestPath(path)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to extract objects from request path")
		return nil, err
	}

	span.SetAttributes(
		attribute.String("gateway.name", listener.Gateway.GetName()),
		attribute.String("gateway.namespace", listener.Gateway.GetNamespace()),
		attribute.String("listener.name", string(listener.Name)),
		attribute.String("httproute.name", httpRoute.GetName()),
		attribute.String("httproute.namespace", httpRoute.GetNamespace()),
	)

	// Add action type attributes for observability
	actionTypes := lo.Map(actions, func(action Action, _ int) string {
		if action.ServiceName != "" {
			return action.ServiceName
		}
		return "unknown"
	})
	if len(actionTypes) > 0 {
		span.SetAttributes(attribute.StringSlice("action_types", actionTypes))
	}

	configs := lo.FlatMap(kuadrantgatewayapi.HostnamesFromListenerAndHTTPRoute(listener.Listener, httpRoute.HTTPRoute), func(hostname gatewayapiv1.Hostname, _ int) []kuadrantgatewayapi.HTTPRouteMatchConfig {
		return lo.Map(httpRouteRule.Matches, func(httpRouteMatch gatewayapiv1.HTTPRouteMatch, j int) kuadrantgatewayapi.HTTPRouteMatchConfig {
			// Create a span for each ActionSet being created
			_, actionSetSpan := tracer.Start(ctx, "wasm.ActionSet.create")
			actionSetSpan.SetAttributes(
				attribute.String("path_id", pathID),
				attribute.String("hostname", string(hostname)),
				attribute.Int("match_index", j),
				attribute.String("httproute.name", httpRoute.GetName()),
				attribute.String("httproute.namespace", httpRoute.GetNamespace()),
			)

			actionSet := ActionSet{
				Name:    ActionSetNameForPath(pathID, j, string(hostname)),
				Actions: actions,
			}
			routeRuleConditions := RouteRuleConditions{
				Hostnames: []string{string(hostname)},
			}
			if predicates := PredicatesFromHTTPRouteMatch(httpRouteMatch); len(predicates) > 0 {
				routeRuleConditions.Predicates = predicates
				actionSetSpan.SetAttributes(attribute.Int("predicate_count", len(predicates)))
			}
			actionSet.RouteRuleConditions = routeRuleConditions

			// Track which policies contributed to this ActionSet
			// Flatten all SourcePolicyLocators from all actions and get unique values
			sourcePolicies := lo.Uniq(lo.FlatMap(actions, func(a Action, _ int) []string {
				return a.SourcePolicyLocators
			}))

			// Count actions by service type to understand policy composition
			actionsByService := lo.GroupBy(actions, func(a Action) string {
				return a.ServiceName
			})

			actionSetSpan.SetAttributes(
				attribute.String("actionset.name", actionSet.Name),
				attribute.Int("actionset.action_count", len(actionSet.Actions)),
				attribute.StringSlice("actionset.source_policies", sourcePolicies),
				attribute.Int("actionset.auth_actions", len(actionsByService[AuthServiceName])),
				attribute.Int("actionset.ratelimit_actions", len(actionsByService[RateLimitServiceName])),
				attribute.Int("actionset.ratelimit_check_actions", len(actionsByService[RateLimitCheckServiceName])),
				attribute.Int("actionset.ratelimit_report_actions", len(actionsByService[RateLimitReportServiceName])),
			)
			actionSetSpan.SetStatus(codes.Ok, "")
			actionSetSpan.End()

			return kuadrantgatewayapi.HTTPRouteMatchConfig{
				Hostname:          string(hostname),
				HTTPRouteMatch:    httpRouteMatch,
				CreationTimestamp: httpRoute.GetCreationTimestamp(),
				Namespace:         httpRoute.GetNamespace(),
				Name:              httpRoute.GetName(),
				Config:            actionSet,
			}
		})
	})

	span.SetAttributes(attribute.Int("config_count", len(configs)))
	span.SetStatus(codes.Ok, "")

	return configs, err
}

func ActionSetNameForPath(pathID string, httpRouteMatchIndex int, hostname string) string {
	source := fmt.Sprintf("%s|%d|%s", pathID, httpRouteMatchIndex+1, hostname)
	hash := sha256.Sum256([]byte(source))
	return hex.EncodeToString(hash[:])
}

func ConfigFromStruct(structure *structpb.Struct) (*Config, error) {
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

// PredicatesFromHTTPRouteMatch builds a list of conditions from a rule match
func PredicatesFromHTTPRouteMatch(match gatewayapiv1.HTTPRouteMatch) []string {
	predicates := make([]string, 0)

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

	// query param, only consider the first in case of repetition, as per spec
	queryParams := make(map[gatewayapiv1.HTTPHeaderName]bool)
	for _, queryParamMatch := range match.QueryParams {
		if !queryParams[queryParamMatch.Name] {
			queryParams[queryParamMatch.Name] = true
			predicates = append(predicates, predicateFromQueryParam(queryParamMatch))
		}
	}

	return predicates
}

func predicateFromPathMatch(pathMatch gatewayapiv1.HTTPPathMatch) string {
	var (
		attr          = "request.url_path"
		pathMatchType = gatewayapiv1.PathMatchPathPrefix // default value
		value         = "/"                              // default value
	)

	if pathMatch.Value != nil {
		value = *pathMatch.Value
	}

	if pathMatch.Type != nil {
		pathMatchType = *pathMatch.Type
	}

	switch pathMatchType {
	case gatewayapiv1.PathMatchExact:
		return fmt.Sprintf("%s == '%s'", attr, value)
	case gatewayapiv1.PathMatchPathPrefix:
		return fmt.Sprintf("%s.startsWith('%s')", attr, value)
	case gatewayapiv1.PathMatchRegularExpression:
		return fmt.Sprintf("%s.matches('%s')", attr, value)
	default:
		return fmt.Sprintf("%s == '%s'", attr, value)
	}
}

func predicateFromMethod(method gatewayapiv1.HTTPMethod) string {
	return fmt.Sprintf("request.method == '%s'", string(method))
}

func predicateFromHeader(headerMatch gatewayapiv1.HTTPHeaderMatch) string {
	// As for gateway api v1, the only operation type with core support is Exact match.
	// https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPHeaderMatch
	return fmt.Sprintf("request.headers.exists(h, h.lowerAscii() == '%s' && request.headers[h] == '%s')",
		strings.ToLower(string(headerMatch.Name)), headerMatch.Value)
}

func predicateFromQueryParam(queryParam gatewayapiv1.HTTPQueryParamMatch) string {
	return fmt.Sprintf("'%s' in queryMap(request.query) ? queryMap(request.query)['%s'] == '%s' : false",
		queryParam.Name, queryParam.Name, queryParam.Value)
}
