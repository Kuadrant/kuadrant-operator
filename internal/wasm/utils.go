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

	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/internal/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	kuadrantpolicymachinery "github.com/kuadrant/kuadrant-operator/internal/policymachinery"
)

const (
	RateLimitServiceName       = "ratelimit-service"
	RateLimitCheckServiceName  = "ratelimit-check-service"
	RateLimitReportServiceName = "ratelimit-report-service"
	AuthServiceName            = "auth-service"
	TracingServiceName         = "tracing-service"
)

type LogLevel int

const (
	LogLevelError LogLevel = iota
	LogLevelWarn
	LogLevelInfo
	LogLevelDebug
)

var logLevelNames = map[LogLevel]string{
	LogLevelError: "ERROR",
	LogLevelWarn:  "WARN",
	LogLevelInfo:  "INFO",
	LogLevelDebug: "DEBUG",
}

func (ll LogLevel) String() string {
	return logLevelNames[ll]
}

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

func TracingServiceTimeout() string {
	return env.GetString("TRACING_SERVICE_TIMEOUT", "100ms")
}

func TracingServiceFailureMode(logger *logr.Logger) FailureModeType {
	return parseFailureModeValue("TRACING_SERVICE_FAILURE_MODE", FailureModeAllow, logger)
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

// BuildObservabilityConfig builds the wasm-shim observability config from the Observability spec
// For MVP: finds the highest priority level that is set to "true"
// Priority: DEBUG(4) > INFO(3) > WARN(2) > ERROR(1)
// Future: will support dynamic CEL predicates for request-time evaluation
func BuildObservabilityConfig(serviceBuilder *ServiceBuilder, observabilitySpec *v1beta1.Observability) *Observability {
	if observabilitySpec == nil || observabilitySpec.DataPlane == nil {
		return nil
	}

	dataPlane := observabilitySpec.DataPlane

	// Find the highest priority level set to "true"
	var logLevel LogLevel
	for _, level := range dataPlane.DefaultLevels {
		if level.Debug != nil {
			logLevel = LogLevelDebug
		}
		if level.Info != nil && logLevel < LogLevelInfo {
			logLevel = LogLevelInfo
		}
		if level.Warn != nil && logLevel < LogLevelWarn {
			logLevel = LogLevelWarn
		}
	}

	var tracing *Tracing
	if observabilitySpec.Tracing != nil && observabilitySpec.Tracing.DefaultEndpoint != "" {
		// Reference the tracing service that will be created in BuildConfigForActionSet
		tracing = &Tracing{
			Service: TracingServiceName,
		}

		serviceBuilder.WithTracing()
	}

	return &Observability{
		DefaultLevel:         ptr.To(logLevel.String()),
		HTTPHeaderIdentifier: dataPlane.HTTPHeaderIdentifier,
		Tracing:              tracing,
	}
}

// ServiceBuilder helps build wasm services with optional configurations using the builder pattern
type ServiceBuilder struct {
	services map[string]Service
	logger   *logr.Logger
}

// NewServiceBuilder creates a new ServiceBuilder with default services
func NewServiceBuilder(logger *logr.Logger) *ServiceBuilder {
	return &ServiceBuilder{
		services: map[string]Service{
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
		logger: logger,
	}
}

// WithTracing adds a tracing service with the specified endpoint
func (sb *ServiceBuilder) WithTracing() *ServiceBuilder {
	sb.services[TracingServiceName] = Service{
		Type:        TracingServiceType,
		Endpoint:    kuadrant.KuadrantTracingClusterName,
		FailureMode: TracingServiceFailureMode(sb.logger),
		Timeout:     ptr.To(TracingServiceTimeout()),
	}
	return sb
}

// WithService adds a custom service
func (sb *ServiceBuilder) WithService(name string, service Service) *ServiceBuilder {
	sb.services[name] = service
	return sb
}

// Build returns the built services map
func (sb *ServiceBuilder) Build() map[string]Service {
	return sb.services
}

func BuildConfigForActionSet(actionSets []ActionSet, logger *logr.Logger, observability *Observability, serviceBuilder *ServiceBuilder) Config {
	// Use provided service builder or create a new one
	if serviceBuilder == nil {
		serviceBuilder = NewServiceBuilder(logger)
	}

	return Config{
		Services:      serviceBuilder.Build(),
		ActionSets:    actionSets,
		Observability: observability,
	}
}

// BuildActionSetsForPath builds action sets for both HTTP and gRPC routes.
//
// Note: Returns HTTPRouteMatchConfig for both HTTP and gRPC routes. For gRPC routes,
// GRPCRouteMatch is converted to HTTPRouteMatch format because gRPC runs on HTTP/2 and
// gRPC methods map to HTTP/2 paths (/{service}/{method}). This conversion enables sorting
// with HTTP routes and reflects the wire-level protocol. The actual predicates sent to
// the WASM plugin are generated from the original GRPCRouteMatch.
func BuildActionSetsForPath(ctx context.Context, pathID string, path []machinery.Targetable, actions []Action) ([]kuadrantgatewayapi.HTTPRouteMatchConfig, error) {
	tracer := controller.TracerFromContext(ctx)
	_, span := tracer.Start(ctx, "wasm.BuildActionSetsForPath")
	defer span.End()

	parsed, err := kuadrantpolicymachinery.ParseTopologyPath(path)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to extract objects from request path")
		return nil, err
	}

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

	var configs []kuadrantgatewayapi.HTTPRouteMatchConfig

	switch parsed.RouteType {
	case kuadrantpolicymachinery.RouteTypeHTTP:
		configs = lo.FlatMap(kuadrantgatewayapi.HostnamesFromListenerAndHTTPRoute(parsed.Listener.Listener, parsed.HTTPRoute.HTTPRoute), func(hostname gatewayapiv1.Hostname, _ int) []kuadrantgatewayapi.HTTPRouteMatchConfig {
			// If Matches is empty or nil, use a default catch-all match (matches all requests with PathPrefix "/")
			matches := parsed.HTTPRouteRule.Matches
			if len(matches) == 0 {
				matches = []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
							Value: ptr.To("/"),
						},
					},
				}
			}
			return lo.Map(matches, func(httpRouteMatch gatewayapiv1.HTTPRouteMatch, j int) kuadrantgatewayapi.HTTPRouteMatchConfig {
				// Create a span for each ActionSet being created
				_, actionSetSpan := tracer.Start(ctx, "wasm.ActionSet.create")
				actionSetSpan.SetAttributes(
					attribute.String("hostname", string(hostname)),
					attribute.Int("match_index", j),
				)

				if httpRouteMatch.Path != nil && httpRouteMatch.Path.Value != nil {
					actionSetSpan.SetAttributes(attribute.String("path", *httpRouteMatch.Path.Value))
				}

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

				// Count actions by service type to understand policy composition for this specific match
				actionsByService := lo.GroupBy(actions, func(a Action) string {
					return a.ServiceName
				})

				actionSetSpan.SetAttributes(
					attribute.String("actionset.name", actionSet.Name),
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
					CreationTimestamp: parsed.HTTPRoute.GetCreationTimestamp(),
					Namespace:         parsed.HTTPRoute.GetNamespace(),
					Name:              parsed.HTTPRoute.GetName(),
					Config:            actionSet,
				}
			})
		})

	case kuadrantpolicymachinery.RouteTypeGRPC:
		hostnames := kuadrantgatewayapi.HostnamesFromListenerAndHTTPRoute(parsed.Listener.Listener, &gatewayapiv1.HTTPRoute{
			Spec: gatewayapiv1.HTTPRouteSpec{
				Hostnames: parsed.GRPCRoute.Spec.Hostnames,
			},
		})

		// If no matches are specified, use a default empty match (matches all requests)
		matches := parsed.GRPCRouteRule.Matches
		if len(matches) == 0 {
			matches = []gatewayapiv1.GRPCRouteMatch{{}}
		}

		configs = lo.FlatMap(hostnames, func(hostname gatewayapiv1.Hostname, _ int) []kuadrantgatewayapi.HTTPRouteMatchConfig {
			return lo.Map(matches, func(grpcRouteMatch gatewayapiv1.GRPCRouteMatch, j int) kuadrantgatewayapi.HTTPRouteMatchConfig {
				// Create a span for each ActionSet being created
				_, actionSetSpan := tracer.Start(ctx, "wasm.ActionSet.create")
				actionSetSpan.SetAttributes(
					attribute.String("hostname", string(hostname)),
					attribute.Int("match_index", j),
				)

				if grpcRouteMatch.Method != nil && grpcRouteMatch.Method.Service != nil {
					actionSetSpan.SetAttributes(attribute.String("grpc_service", *grpcRouteMatch.Method.Service))
				}
				if grpcRouteMatch.Method != nil && grpcRouteMatch.Method.Method != nil {
					actionSetSpan.SetAttributes(attribute.String("grpc_method", *grpcRouteMatch.Method.Method))
				}

				actionSet := ActionSet{
					Name:    ActionSetNameForPath(pathID, j, string(hostname)),
					Actions: actions,
				}
				routeRuleConditions := RouteRuleConditions{
					Hostnames: []string{string(hostname)},
				}
				if predicates := PredicatesFromGRPCRouteMatch(grpcRouteMatch); len(predicates) > 0 {
					routeRuleConditions.Predicates = predicates
					actionSetSpan.SetAttributes(attribute.Int("predicate_count", len(predicates)))
				}
				actionSet.RouteRuleConditions = routeRuleConditions

				// Count actions by service type to understand policy composition for this specific match
				actionsByService := lo.GroupBy(actions, func(a Action) string {
					return a.ServiceName
				})

				actionSetSpan.SetAttributes(
					attribute.String("actionset.name", actionSet.Name),
					attribute.Int("actionset.auth_actions", len(actionsByService[AuthServiceName])),
					attribute.Int("actionset.ratelimit_actions", len(actionsByService[RateLimitServiceName])),
					attribute.Int("actionset.ratelimit_check_actions", len(actionsByService[RateLimitCheckServiceName])),
					attribute.Int("actionset.ratelimit_report_actions", len(actionsByService[RateLimitReportServiceName])),
				)
				actionSetSpan.SetStatus(codes.Ok, "")
				actionSetSpan.End()

				// IMPORTANT: Why we use HTTPRouteMatch for gRPC routes
				//
				// gRPC is built on HTTP/2. At the wire level, a gRPC call is an HTTP/2 POST request
				// to a path /{service}/{method}. For example:
				//   gRPC: Service="foo.Bar", Method="Baz"
				//   → HTTP/2: POST /foo.Bar/Baz
				//
				// We convert GRPCRouteMatch to HTTPRouteMatch to:
				// 1. Represent the underlying HTTP/2 structure of gRPC traffic
				// 2. Reuse the existing SortableHTTPRouteMatchConfigs comparator which implements
				//    Gateway API's specificity rules (internal/gatewayapi/types.go)
				// 3. Allow HTTP and gRPC matches to be sorted together by hostname/specificity
				//
				// Note: The actual predicates sent to the WASM plugin at runtime are generated
				// from the original GRPCRouteMatch via PredicatesFromGRPCRouteMatch(). This HTTP
				// representation is used for sorting and reflects the HTTP/2 wire format.
				httpRouteMatch := ConvertGRPCRouteMatchToHTTP(grpcRouteMatch)

				// Return HTTPRouteMatchConfig for sorting with HTTP routes.
				// The HTTPRouteMatch represents the HTTP/2 wire format of the gRPC call.
				// The Config field (actionSet) contains the actual WASM configuration
				// with predicates generated from the original GRPCRouteMatch.
				return kuadrantgatewayapi.HTTPRouteMatchConfig{
					Hostname:          string(hostname),
					HTTPRouteMatch:    httpRouteMatch,
					CreationTimestamp: parsed.GRPCRoute.GetCreationTimestamp(),
					Namespace:         parsed.GRPCRoute.GetNamespace(),
					Name:              parsed.GRPCRoute.GetName(),
					Config:            actionSet,
				}
			})
		})
	}

	span.SetStatus(codes.Ok, "")

	return configs, nil
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

// PredicatesFromGRPCRouteMatch generates CEL predicates from a GRPCRouteMatch.
// GRPCRouteMatch fields map to CEL as follows:
//   - method.service and method.method combine to form request.url_path patterns
//   - headers use the same predicate generation as HTTPRoute (predicateFromHeader)
func PredicatesFromGRPCRouteMatch(match gatewayapiv1.GRPCRouteMatch) []string {
	predicates := make([]string, 0)

	// method (service + method)
	if match.Method != nil {
		if predicate := predicateFromGRPCMethod(*match.Method); predicate != "" {
			predicates = append(predicates, predicate)
		}
	}

	// headers
	for _, headerMatch := range match.Headers {
		// Multiple match values are ANDed together
		predicates = append(predicates, predicateFromGRPCHeader(headerMatch))
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

func predicateFromGRPCHeader(headerMatch gatewayapiv1.GRPCHeaderMatch) string {
	// As for gateway api v1, the only operation type with core support is Exact match.
	// https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.GRPCHeaderMatch
	// GRPCHeaderMatch has the same structure as HTTPHeaderMatch
	return fmt.Sprintf("request.headers.exists(h, h.lowerAscii() == '%s' && request.headers[h] == '%s')",
		strings.ToLower(string(headerMatch.Name)), headerMatch.Value)
}

func predicateFromQueryParam(queryParam gatewayapiv1.HTTPQueryParamMatch) string {
	return fmt.Sprintf("'%s' in queryMap(request.query) ? queryMap(request.query)['%s'] == '%s' : false",
		queryParam.Name, queryParam.Name, queryParam.Value)
}

// predicateFromGRPCMethod generates a CEL predicate for gRPC service/method matching.
// gRPC requests have URL paths in the format: /package.Service/Method
// The predicate uses request.url_path to match against this pattern.
//
// Per Gateway API spec, at least one of Service or Method MUST be non-empty.
// Match types default to Exact if not specified.
func predicateFromGRPCMethod(methodMatch gatewayapiv1.GRPCMethodMatch) string {
	var (
		service     = methodMatch.Service
		method      = methodMatch.Method
		serviceType = ptr.Deref(methodMatch.Type, gatewayapiv1.GRPCMethodMatchExact)
		attr        = "request.url_path"
	)

	// Handle all combinations of service/method presence and match types
	hasService := service != nil && *service != ""
	hasMethod := method != nil && *method != ""

	if !hasService && !hasMethod {
		// Empty match - should not happen per spec, but handle gracefully
		return ""
	}

	// Exact match type (default)
	if serviceType == gatewayapiv1.GRPCMethodMatchExact {
		if hasService && hasMethod {
			// Exact service + exact method: exact path match
			return fmt.Sprintf("%s == '/%s/%s'", attr, *service, *method)
		}
		if hasService {
			// Exact service only: prefix match with trailing slash
			return fmt.Sprintf("%s.startsWith('/%s/')", attr, *service)
		}
		// Exact method only: regex to match any service with specific method
		return fmt.Sprintf("%s.matches('^/[^/]+/%s$')", attr, *method)
	}

	// RegularExpression match type
	if hasService && hasMethod {
		// Regex service + regex method
		return fmt.Sprintf("%s.matches('^/%s/%s$')", attr, *service, *method)
	}
	if hasService {
		// Regex service only
		return fmt.Sprintf("%s.matches('^/%s/.*$')", attr, *service)
	}
	// Regex method only
	return fmt.Sprintf("%s.matches('^/[^/]+/%s$')", attr, *method)
}

// ConvertGRPCRouteMatchToHTTP converts a GRPCRouteMatch to an HTTPRouteMatch for sorting purposes.
//
// This conversion is necessary because gRPC is built on HTTP/2 and gRPC methods map directly to
// HTTP/2 paths (/{service}/{method}). By converting to HTTPRouteMatch, we can:
// 1. Reuse the existing HTTPRouteMatch sorting logic
// 2. Correctly represent gRPC method specificity using HTTP path match types
// 3. Sort HTTP and gRPC routes together by hostname and specificity
//
// The conversion encodes gRPC method specificity into HTTP path matches:
// - Service+Method → Exact match (most specific)
// - Service only → PathPrefix (medium specific)
// - Method only → PathPrefix (less specific, shorter path)
func ConvertGRPCRouteMatchToHTTP(grpcRouteMatch gatewayapiv1.GRPCRouteMatch) gatewayapiv1.HTTPRouteMatch {
	httpRouteMatch := gatewayapiv1.HTTPRouteMatch{
		Headers: lo.Map(grpcRouteMatch.Headers, func(h gatewayapiv1.GRPCHeaderMatch, _ int) gatewayapiv1.HTTPHeaderMatch {
			return gatewayapiv1.HTTPHeaderMatch{
				Type:  (*gatewayapiv1.HeaderMatchType)(h.Type),
				Name:  gatewayapiv1.HTTPHeaderName(h.Name),
				Value: h.Value,
			}
		}),
	}

	// Encode gRPC method specificity into the Path field.
	// The sorting algorithm ranks by:
	// 1. Path match type: Exact > RegularExpression > PathPrefix
	// 2. Path length: longer paths are more specific within the same match type
	if grpcRouteMatch.Method != nil {
		service := ptr.Deref(grpcRouteMatch.Method.Service, "")
		method := ptr.Deref(grpcRouteMatch.Method.Method, "")
		matchType := ptr.Deref(grpcRouteMatch.Method.Type, gatewayapiv1.GRPCMethodMatchExact)

		// Handle RegularExpression match type
		if matchType == gatewayapiv1.GRPCMethodMatchRegularExpression {
			if service != "" && method != "" {
				// Both service and method: regex match
				pathValue := "^/" + service + "/" + method + "$"
				httpRouteMatch.Path = &gatewayapiv1.HTTPPathMatch{
					Type:  ptr.To(gatewayapiv1.PathMatchRegularExpression),
					Value: &pathValue,
				}
			} else if service != "" {
				// Service only: regex prefix
				pathValue := "^/" + service
				httpRouteMatch.Path = &gatewayapiv1.HTTPPathMatch{
					Type:  ptr.To(gatewayapiv1.PathMatchRegularExpression),
					Value: &pathValue,
				}
			} else if method != "" {
				// Method only: regex
				pathValue := "^/" + method
				httpRouteMatch.Path = &gatewayapiv1.HTTPPathMatch{
					Type:  ptr.To(gatewayapiv1.PathMatchRegularExpression),
					Value: &pathValue,
				}
			}
		} else {
			// Exact match type (default)
			if service != "" && method != "" {
				// Both service and method specified: most specific (Exact match)
				pathValue := "/" + service + "/" + method
				httpRouteMatch.Path = &gatewayapiv1.HTTPPathMatch{
					Type:  ptr.To(gatewayapiv1.PathMatchExact),
					Value: &pathValue,
				}
			} else if service != "" {
				// Only service specified: medium specific (PathPrefix)
				pathValue := "/" + service
				httpRouteMatch.Path = &gatewayapiv1.HTTPPathMatch{
					Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
					Value: &pathValue,
				}
			} else if method != "" {
				// Only method specified: less specific (shorter PathPrefix)
				pathValue := "/" + method
				httpRouteMatch.Path = &gatewayapiv1.HTTPPathMatch{
					Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
					Value: &pathValue,
				}
			}
		}
	}

	return httpRouteMatch
}
