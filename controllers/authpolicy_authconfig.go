package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

func (r *AuthPolicyReconciler) reconcileAuthConfigs(ctx context.Context, ap *api.AuthPolicy, targetNetworkObject client.Object) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	authConfig, err := r.desiredAuthConfig(ctx, ap, targetNetworkObject)
	if err != nil {
		return err
	}

	err = r.ReconcileResource(ctx, &authorinoapi.AuthConfig{}, authConfig, alwaysUpdateAuthConfig)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		logger.Error(err, "ReconcileResource failed to create/update AuthConfig resource")
		return err
	}
	return nil
}

func (r *AuthPolicyReconciler) deleteAuthConfigs(ctx context.Context, ap *api.AuthPolicy) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	logger.Info("Removing Authorino's AuthConfigs")

	authConfig := &authorinoapi.AuthConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      authConfigName(client.ObjectKeyFromObject(ap)),
			Namespace: ap.Namespace,
		},
	}

	if err := r.DeleteResource(ctx, authConfig); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		logger.Error(err, "failed to delete Authorino's AuthConfig")
		return err
	}

	return nil
}

func (r *AuthPolicyReconciler) desiredAuthConfig(ctx context.Context, ap *api.AuthPolicy, targetNetworkObject client.Object) (*authorinoapi.AuthConfig, error) {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("desiredAuthConfig")

	authConfig := &authorinoapi.AuthConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AuthConfig",
			APIVersion: authorinoapi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      authConfigName(client.ObjectKeyFromObject(ap)),
			Namespace: ap.Namespace,
		},
		Spec: authorinoapi.AuthConfigSpec{},
	}

	var route *gatewayapiv1.HTTPRoute
	var hosts []string

	switch obj := targetNetworkObject.(type) {
	case *gatewayapiv1.HTTPRoute:
		route = obj
		var err error
		hosts, err = common.HostnamesFromHTTPRoute(ctx, obj, r.Client())
		if err != nil {
			return nil, err
		}
	case *gatewayapiv1.Gateway:
		// fake a single httproute with all rules from all httproutes accepted by the gateway,
		// that do not have an authpolicy of its own, so we can generate wasm rules for those cases
		gw := common.GatewayWrapper{Gateway: obj}
		gwHostnames := gw.Hostnames()
		if len(hosts) == 0 {
			gwHostnames = []gatewayapiv1.Hostname{"*"}
		}
		hosts = common.HostnamesToStrings(gwHostnames)

		rules := make([]gatewayapiv1.HTTPRouteRule, 0)
		routes := r.FetchAcceptedGatewayHTTPRoutes(ctx, ap.TargetKey())
		for idx := range routes {
			route := routes[idx]
			// skip routes that have an authpolicy of its own
			if route.GetAnnotations()[common.AuthPolicyBackRefAnnotation] != "" {
				continue
			}
			rules = append(rules, route.Spec.Rules...)
		}
		if len(rules) == 0 {
			logger.V(1).Info("no httproutes attached to the targeted gateway, skipping authorino authconfig for the gateway authpolicy")
			common.TagObjectToDelete(authConfig)
			return authConfig, nil
		}
		route = &gatewayapiv1.HTTPRoute{
			Spec: gatewayapiv1.HTTPRouteSpec{
				Hostnames: gwHostnames,
				Rules:     rules,
			},
		}
	}

	// hosts
	authConfig.Spec.Hosts = hosts

	// named patterns
	if namedPatterns := ap.Spec.NamedPatterns; len(namedPatterns) > 0 {
		authConfig.Spec.NamedPatterns = namedPatterns
	}

	// top-level conditions
	topLevelConditionsFromRouteSelectors, err := authorinoConditionsFromRouteSelectors(route, ap.Spec)
	if err != nil {
		return nil, err
	}
	if len(topLevelConditionsFromRouteSelectors) == 0 {
		topLevelConditionsFromRouteSelectors = authorinoConditionsFromHTTPRoute(route)
	}
	if len(topLevelConditionsFromRouteSelectors) > 0 || len(ap.Spec.Conditions) > 0 {
		authConfig.Spec.Conditions = append(ap.Spec.Conditions, topLevelConditionsFromRouteSelectors...)
	}

	// authentication
	if authentication := ap.Spec.AuthScheme.Authentication; len(authentication) > 0 {
		authConfig.Spec.Authentication = authorinoSpecsFromConfigs(authentication, func(config api.AuthenticationSpec) authorinoapi.AuthenticationSpec { return config.AuthenticationSpec })
	}

	// metadata
	if metadata := ap.Spec.AuthScheme.Metadata; len(metadata) > 0 {
		authConfig.Spec.Metadata = authorinoSpecsFromConfigs(metadata, func(config api.MetadataSpec) authorinoapi.MetadataSpec { return config.MetadataSpec })
	}

	// authorization
	if authorization := ap.Spec.AuthScheme.Authorization; len(authorization) > 0 {
		authConfig.Spec.Authorization = authorinoSpecsFromConfigs(authorization, func(config api.AuthorizationSpec) authorinoapi.AuthorizationSpec { return config.AuthorizationSpec })
	}

	// response
	if response := ap.Spec.AuthScheme.Response; response != nil {
		authConfig.Spec.Response = &authorinoapi.ResponseSpec{
			Unauthenticated: response.Unauthenticated,
			Unauthorized:    response.Unauthorized,
			Success: authorinoapi.WrappedSuccessResponseSpec{
				Headers: authorinoSpecsFromConfigs(response.Success.Headers, func(config api.HeaderSuccessResponseSpec) authorinoapi.HeaderSuccessResponseSpec {
					return authorinoapi.HeaderSuccessResponseSpec{SuccessResponseSpec: config.SuccessResponseSpec.SuccessResponseSpec}
				}),
				DynamicMetadata: authorinoSpecsFromConfigs(response.Success.DynamicMetadata, func(config api.SuccessResponseSpec) authorinoapi.SuccessResponseSpec {
					return config.SuccessResponseSpec
				}),
			},
		}
	}

	// callbacks
	if callbacks := ap.Spec.AuthScheme.Callbacks; len(callbacks) > 0 {
		authConfig.Spec.Callbacks = authorinoSpecsFromConfigs(callbacks, func(config api.CallbackSpec) authorinoapi.CallbackSpec { return config.CallbackSpec })
	}

	return mergeConditionsFromRouteSelectorsIntoConfigs(ap, route, authConfig)
}

// authConfigName returns the name of Authorino AuthConfig CR.
func authConfigName(apKey client.ObjectKey) string {
	return fmt.Sprintf("ap-%s-%s", apKey.Namespace, apKey.Name)
}

func authorinoSpecsFromConfigs[T, U any](configs map[string]U, extractAuthorinoSpec func(U) T) map[string]T {
	specs := make(map[string]T, len(configs))
	for name, config := range configs {
		authorinoConfig := extractAuthorinoSpec(config)
		specs[name] = authorinoConfig
	}
	return specs
}

func mergeConditionsFromRouteSelectorsIntoConfigs(ap *api.AuthPolicy, route *gatewayapiv1.HTTPRoute, authConfig *authorinoapi.AuthConfig) (*authorinoapi.AuthConfig, error) {
	// authentication
	for name, config := range ap.Spec.AuthScheme.Authentication {
		conditions, err := authorinoConditionsFromRouteSelectors(route, config)
		if err != nil {
			return nil, err
		}
		if len(conditions) == 0 {
			continue
		}
		c := authConfig.Spec.Authentication[name]
		c.Conditions = append(c.Conditions, conditions...)
		authConfig.Spec.Authentication[name] = c
	}

	// metadata
	for name, config := range ap.Spec.AuthScheme.Metadata {
		conditions, err := authorinoConditionsFromRouteSelectors(route, config)
		if err != nil {
			return nil, err
		}
		if len(conditions) == 0 {
			continue
		}
		c := authConfig.Spec.Metadata[name]
		c.Conditions = append(c.Conditions, conditions...)
		authConfig.Spec.Metadata[name] = c
	}

	// authorization
	for name, config := range ap.Spec.AuthScheme.Authorization {
		conditions, err := authorinoConditionsFromRouteSelectors(route, config)
		if err != nil {
			return nil, err
		}
		if len(conditions) == 0 {
			continue
		}
		c := authConfig.Spec.Authorization[name]
		c.Conditions = append(c.Conditions, conditions...)
		authConfig.Spec.Authorization[name] = c
	}

	// response
	if response := ap.Spec.AuthScheme.Response; response != nil {
		// response success headers
		for name, config := range response.Success.Headers {
			conditions, err := authorinoConditionsFromRouteSelectors(route, config)
			if err != nil {
				return nil, err
			}
			if len(conditions) == 0 {
				continue
			}
			c := authConfig.Spec.Response.Success.Headers[name]
			c.Conditions = append(c.Conditions, conditions...)
			authConfig.Spec.Response.Success.Headers[name] = c
		}

		// response success dynamic metadata
		for name, config := range response.Success.DynamicMetadata {
			conditions, err := authorinoConditionsFromRouteSelectors(route, config)
			if err != nil {
				return nil, err
			}
			if len(conditions) == 0 {
				continue
			}
			c := authConfig.Spec.Response.Success.DynamicMetadata[name]
			c.Conditions = append(c.Conditions, conditions...)
			authConfig.Spec.Response.Success.DynamicMetadata[name] = c
		}
	}

	// callbacks
	for name, config := range ap.Spec.AuthScheme.Callbacks {
		conditions, err := authorinoConditionsFromRouteSelectors(route, config)
		if err != nil {
			return nil, err
		}
		if len(conditions) == 0 {
			continue
		}
		c := authConfig.Spec.Callbacks[name]
		c.Conditions = append(c.Conditions, conditions...)
		authConfig.Spec.Callbacks[name] = c
	}

	return authConfig, nil
}

// authorinoConditionFromRouteSelectors builds a list of Authorino conditions from a config that may specify route selectors
func authorinoConditionsFromRouteSelectors(route *gatewayapiv1.HTTPRoute, config api.RouteSelectorsGetter) ([]authorinoapi.PatternExpressionOrRef, error) {
	routeSelectors := config.GetRouteSelectors()

	if len(routeSelectors) == 0 {
		return nil, nil
	}

	// build conditions from the rules selected by the route selectors
	conditions := []authorinoapi.PatternExpressionOrRef{}
	for idx := range routeSelectors {
		routeSelector := routeSelectors[idx]
		hostnamesForConditions := routeSelector.HostnamesForConditions(route)
		for _, rule := range routeSelector.SelectRules(route) {
			conditions = append(conditions, authorinoConditionsFromHTTPRouteRule(rule, hostnamesForConditions)...)
		}
	}
	if len(conditions) == 0 {
		return nil, errors.New("cannot match any route rules, check for invalid route selectors in the policy")
	}
	return toAuthorinoOneOfPatternExpressionsOrRefs(conditions), nil
}

// authorinoConditionsFromHTTPRoute builds a list of Authorino conditions from an HTTPRoute, without using route selectors.
func authorinoConditionsFromHTTPRoute(route *gatewayapiv1.HTTPRoute) []authorinoapi.PatternExpressionOrRef {
	conditions := []authorinoapi.PatternExpressionOrRef{}
	hostnamesForConditions := (&api.RouteSelector{}).HostnamesForConditions(route)
	for _, rule := range route.Spec.Rules {
		conditions = append(conditions, authorinoConditionsFromHTTPRouteRule(rule, hostnamesForConditions)...)
	}
	return toAuthorinoOneOfPatternExpressionsOrRefs(conditions)
}

// authorinoConditionsFromHTTPRouteRule builds a list of Authorino conditions from a HTTPRouteRule and a list of hostnames
// * Each combination of HTTPRouteMatch and hostname yields one condition.
// * Rules that specify no explicit HTTPRouteMatch are assumed to match all requests (i.e. implicit catch-all rule.)
// * Empty list of hostnames yields a condition without a hostname pattern expression.
func authorinoConditionsFromHTTPRouteRule(rule gatewayapiv1.HTTPRouteRule, hostnames []gatewayapiv1.Hostname) []authorinoapi.PatternExpressionOrRef {
	hosts := []string{}
	for _, hostname := range hostnames {
		if hostname == "*" {
			continue
		}
		hosts = append(hosts, string(hostname))
	}

	// no http route matches → we only need one simple authorino condition or even no condition at all
	if len(rule.Matches) == 0 {
		if len(hosts) == 0 {
			return nil
		}
		return []authorinoapi.PatternExpressionOrRef{hostnameRuleToAuthorinoCondition(hosts)}
	}

	var oneOf []authorinoapi.PatternExpressionOrRef

	// http route matches and possibly hostnames → we need one authorino rule per http route match
	for _, match := range rule.Matches {
		var allOf []authorinoapi.PatternExpressionOrRef

		// hosts
		if len(hosts) > 0 {
			allOf = append(allOf, hostnameRuleToAuthorinoCondition(hosts))
		}

		// method
		if method := match.Method; method != nil {
			allOf = append(allOf, httpMethodRuleToAuthorinoCondition(*method))
		}

		// path
		if path := match.Path; path != nil {
			allOf = append(allOf, httpPathRuleToAuthorinoCondition(*path))
		}

		// headers
		if headers := match.Headers; len(headers) > 0 {
			allOf = append(allOf, httpHeadersRuleToAuthorinoConditions(headers)...)
		}

		// query params
		if queryParams := match.QueryParams; len(queryParams) > 0 {
			allOf = append(allOf, httpQueryParamsRuleToAuthorinoConditions(queryParams)...)
		}

		if len(allOf) > 0 {
			oneOf = append(oneOf, authorinoapi.PatternExpressionOrRef{
				All: common.Map(allOf, toAuthorinoUnstructuredPatternExpressionOrRef),
			})
		}
	}
	return toAuthorinoOneOfPatternExpressionsOrRefs(oneOf)
}

func hostnameRuleToAuthorinoCondition(hostnames []string) authorinoapi.PatternExpressionOrRef {
	return authorinoapi.PatternExpressionOrRef{
		PatternExpression: authorinoapi.PatternExpression{
			Selector: "request.host",
			Operator: "matches",
			Value:    hostnamesToRegex(hostnames),
		},
	}
}

func hostnamesToRegex(hostnames []string) string {
	return strings.Join(common.Map(hostnames, func(hostname string) string {
		return strings.ReplaceAll(strings.ReplaceAll(hostname, ".", `\.`), "*", ".*")
	}), "|")
}

func httpMethodRuleToAuthorinoCondition(method gatewayapiv1.HTTPMethod) authorinoapi.PatternExpressionOrRef {
	return authorinoapi.PatternExpressionOrRef{
		PatternExpression: authorinoapi.PatternExpression{
			Selector: "request.method",
			Operator: "eq",
			Value:    string(method),
		},
	}
}

func httpPathRuleToAuthorinoCondition(path gatewayapiv1.HTTPPathMatch) authorinoapi.PatternExpressionOrRef {
	value := "/"
	if path.Value != nil {
		value = *path.Value
	}
	var operator string

	matchType := path.Type
	if matchType == nil {
		p := gatewayapiv1.PathMatchPathPrefix
		matchType = &p // gateway api defaults to PathMatchPathPrefix
	}

	switch *matchType {
	case gatewayapiv1.PathMatchExact:
		operator = "eq"
	case gatewayapiv1.PathMatchPathPrefix:
		operator = "matches"
		value += ".*"
	case gatewayapiv1.PathMatchRegularExpression:
		operator = "matches"
	}

	return authorinoapi.PatternExpressionOrRef{
		PatternExpression: authorinoapi.PatternExpression{
			Selector: `request.url_path`,
			Operator: authorinoapi.PatternExpressionOperator(operator),
			Value:    value,
		},
	}
}

func httpHeadersRuleToAuthorinoConditions(headers []gatewayapiv1.HTTPHeaderMatch) []authorinoapi.PatternExpressionOrRef {
	conditions := make([]authorinoapi.PatternExpressionOrRef, 0, len(headers))
	for _, header := range headers {
		condition := httpHeaderRuleToAuthorinoCondition(header)
		conditions = append(conditions, condition)
	}
	return conditions
}

func httpHeaderRuleToAuthorinoCondition(header gatewayapiv1.HTTPHeaderMatch) authorinoapi.PatternExpressionOrRef {
	operator := "eq" // gateway api defaults to HeaderMatchExact
	if header.Type != nil && *header.Type == gatewayapiv1.HeaderMatchRegularExpression {
		operator = "matches"
	}
	return authorinoapi.PatternExpressionOrRef{
		PatternExpression: authorinoapi.PatternExpression{
			Selector: fmt.Sprintf("request.headers.%s", strings.ToLower(string(header.Name))),
			Operator: authorinoapi.PatternExpressionOperator(operator),
			Value:    header.Value,
		},
	}
}

func httpQueryParamsRuleToAuthorinoConditions(queryParams []gatewayapiv1.HTTPQueryParamMatch) []authorinoapi.PatternExpressionOrRef {
	conditions := make([]authorinoapi.PatternExpressionOrRef, 0, len(queryParams))
	for _, queryParam := range queryParams {
		condition := httpQueryParamRuleToAuthorinoCondition(queryParam)
		conditions = append(conditions, condition)
	}
	return conditions
}

func httpQueryParamRuleToAuthorinoCondition(queryParam gatewayapiv1.HTTPQueryParamMatch) authorinoapi.PatternExpressionOrRef {
	operator := "eq" // gateway api defaults to QueryParamMatchExact
	if queryParam.Type != nil && *queryParam.Type == gatewayapiv1.QueryParamMatchRegularExpression {
		operator = "matches"
	}
	return authorinoapi.PatternExpressionOrRef{
		Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
			{
				PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
					PatternExpression: authorinoapi.PatternExpression{
						Selector: fmt.Sprintf(`request.path.@extract:{"sep":"?%s=","pos":1}|@extract:{"sep":"&"}`, queryParam.Name),
						Operator: authorinoapi.PatternExpressionOperator(operator),
						Value:    queryParam.Value,
					},
				},
			},
			{
				PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
					PatternExpression: authorinoapi.PatternExpression{
						Selector: fmt.Sprintf(`request.path.@extract:{"sep":"&%s=","pos":1}|@extract:{"sep":"&"}`, queryParam.Name),
						Operator: authorinoapi.PatternExpressionOperator(operator),
						Value:    queryParam.Value,
					},
				},
			},
		},
	}
}

func toAuthorinoUnstructuredPatternExpressionOrRef(patternExpressionOrRef authorinoapi.PatternExpressionOrRef) authorinoapi.UnstructuredPatternExpressionOrRef {
	return authorinoapi.UnstructuredPatternExpressionOrRef{PatternExpressionOrRef: patternExpressionOrRef}
}

func toAuthorinoOneOfPatternExpressionsOrRefs(oneOf []authorinoapi.PatternExpressionOrRef) []authorinoapi.PatternExpressionOrRef {
	return []authorinoapi.PatternExpressionOrRef{
		{
			Any: common.Map(oneOf, toAuthorinoUnstructuredPatternExpressionOrRef),
		},
	}
}

func alwaysUpdateAuthConfig(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*authorinoapi.AuthConfig)
	if !ok {
		return false, fmt.Errorf("%T is not an *authorinoapi.AuthConfig", existingObj)
	}
	desired, ok := desiredObj.(*authorinoapi.AuthConfig)
	if !ok {
		return false, fmt.Errorf("%T is not an *authorinoapi.AuthConfig", desiredObj)
	}

	if reflect.DeepEqual(existing.Spec, desired.Spec) && reflect.DeepEqual(existing.Annotations, desired.Annotations) {
		return false, nil
	}

	existing.Spec = desired.Spec
	existing.Annotations = desired.Annotations
	return true, nil
}
