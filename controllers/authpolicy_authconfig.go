package controllers

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func (r *AuthPolicyReconciler) reconcileAuthConfigs(ctx context.Context, ap *kuadrantv1beta3.AuthPolicy, targetNetworkObject client.Object) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	authConfig, err := r.desiredAuthConfig(ctx, ap, targetNetworkObject)
	if err != nil {
		return err
	}

	err = r.SetOwnerReference(ap, authConfig)
	if err != nil {
		return err
	}

	err = r.ReconcileResource(ctx, &authorinoapi.AuthConfig{}, authConfig, authConfigBasicMutator)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		logger.Error(err, "ReconcileResource failed to create/update AuthConfig resource")
		return err
	}
	return nil
}

func (r *AuthPolicyReconciler) desiredAuthConfig(ctx context.Context, ap *kuadrantv1beta3.AuthPolicy, targetNetworkObject client.Object) (*authorinoapi.AuthConfig, error) {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("desiredAuthConfig")

	authConfig := &authorinoapi.AuthConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AuthConfig",
			APIVersion: authorinoapi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      AuthConfigName(client.ObjectKeyFromObject(ap)),
			Namespace: ap.Namespace,
		},
		Spec: authorinoapi.AuthConfigSpec{},
	}

	var route *gatewayapiv1.HTTPRoute
	var hosts []string

	switch obj := targetNetworkObject.(type) {
	case *gatewayapiv1.HTTPRoute:
		t, err := r.generateTopology(ctx)
		if err != nil {
			logger.V(1).Info("Failed to generate topology", "error", err)
			return nil, err
		}

		overrides := routeGatewayAuthOverrides(t, ap)
		if len(overrides) != 0 {
			logger.V(1).Info("targeted gateway has authpolicy with atomic overrides, skipping authorino authconfig for the HTTPRoute authpolicy")
			utils.TagObjectToDelete(authConfig)
			r.AffectedPolicyMap.SetAffectedPolicy(ap, overrides)
			return authConfig, nil
		}
		route = obj
		hosts, err = kuadrant.HostnamesFromHTTPRoute(ctx, obj, r.Client())
		if err != nil {
			return nil, err
		}
	case *gatewayapiv1.Gateway:
		// fake a single httproute with all rules from all httproutes accepted by the gateway,
		// that do not have an authpolicy of its own, so we can generate wasm rules for those cases
		gw := kuadrant.GatewayWrapper{Gateway: obj}
		gwHostnames := gw.Hostnames()
		if len(gwHostnames) == 0 {
			gwHostnames = []gatewayapiv1.Hostname{"*"}
		}
		hosts = utils.HostnamesToStrings(gwHostnames)

		rules := make([]gatewayapiv1.HTTPRouteRule, 0)
		routes := r.TargetRefReconciler.FetchAcceptedGatewayHTTPRoutes(ctx, obj)
		for idx := range routes {
			route := routes[idx]
			// skip routes that have an authpolicy of its own and Gateway authpolicy does not define atomic overrides
			if route.GetAnnotations()[common.AuthPolicyBackRefAnnotation] != "" && !ap.IsAtomicOverride() {
				continue
			}
			rules = append(rules, route.Spec.Rules...)
		}
		if len(rules) == 0 {
			logger.V(1).Info("no httproutes attached to the targeted gateway, skipping authorino authconfig for the gateway authpolicy")
			utils.TagObjectToDelete(authConfig)
			obj := targetNetworkObject.(*gatewayapiv1.Gateway)
			gatewayWrapper := kuadrant.GatewayWrapper{Gateway: obj, Referrer: ap}
			refs := gatewayWrapper.PolicyRefs()
			filteredRef := utils.Filter(refs, func(key client.ObjectKey) bool {
				return key != client.ObjectKeyFromObject(ap)
			})

			r.AffectedPolicyMap.SetAffectedPolicy(ap, filteredRef)
			return authConfig, nil
		}
		route = &gatewayapiv1.HTTPRoute{
			Spec: gatewayapiv1.HTTPRouteSpec{
				Hostnames: gwHostnames,
				Rules:     rules,
			},
		}
	}

	// AuthPolicy is not Affected if we still need to create an AuthConfig for it
	r.AffectedPolicyMap.RemoveAffectedPolicy(ap)

	// hosts
	authConfig.Spec.Hosts = hosts

	commonSpec := ap.Spec.CommonSpec()

	// named patterns
	if namedPatterns := commonSpec.NamedPatterns; len(namedPatterns) > 0 {
		authConfig.Spec.NamedPatterns = namedPatterns
	}

	conditionsFromHTTPRoute := authorinoConditionsFromHTTPRoute(route)
	if len(conditionsFromHTTPRoute) > 0 || len(commonSpec.Conditions) > 0 {
		authConfig.Spec.Conditions = append(commonSpec.Conditions, conditionsFromHTTPRoute...)
	}

	// return early if authScheme is nil
	if commonSpec.AuthScheme == nil {
		return authConfig, nil
	}

	// authentication
	if authentication := commonSpec.AuthScheme.Authentication; len(authentication) > 0 {
		authConfig.Spec.Authentication = authorinoSpecsFromConfigs(authentication, func(config authorinoapi.AuthenticationSpec) authorinoapi.AuthenticationSpec {
			return config
		})
	}

	// metadata
	if metadata := commonSpec.AuthScheme.Metadata; len(metadata) > 0 {
		authConfig.Spec.Metadata = authorinoSpecsFromConfigs(metadata, func(config authorinoapi.MetadataSpec) authorinoapi.MetadataSpec { return config })
	}

	// authorization
	if authorization := commonSpec.AuthScheme.Authorization; len(authorization) > 0 {
		authConfig.Spec.Authorization = authorinoSpecsFromConfigs(authorization, func(config authorinoapi.AuthorizationSpec) authorinoapi.AuthorizationSpec {
			return config
		})
	}

	// response
	if response := commonSpec.AuthScheme.Response; response != nil {
		authConfig.Spec.Response = &authorinoapi.ResponseSpec{
			Unauthenticated: response.Unauthenticated,
			Unauthorized:    response.Unauthorized,
			Success: authorinoapi.WrappedSuccessResponseSpec{
				Headers: authorinoSpecsFromConfigs(response.Success.Headers, func(config kuadrantv1beta3.HeaderSuccessResponseSpec) authorinoapi.HeaderSuccessResponseSpec {
					return authorinoapi.HeaderSuccessResponseSpec{SuccessResponseSpec: config.SuccessResponseSpec}
				}),
				DynamicMetadata: authorinoSpecsFromConfigs(response.Success.DynamicMetadata, func(config authorinoapi.SuccessResponseSpec) authorinoapi.SuccessResponseSpec {
					return config
				}),
			},
		}
	}

	// callbacks
	if callbacks := commonSpec.AuthScheme.Callbacks; len(callbacks) > 0 {
		authConfig.Spec.Callbacks = authorinoSpecsFromConfigs(callbacks, func(config authorinoapi.CallbackSpec) authorinoapi.CallbackSpec { return config })
	}

	return authConfig, nil
}

// routeGatewayAuthOverrides returns the GW auth policies that has an override field set
func routeGatewayAuthOverrides(t *kuadrantgatewayapi.Topology, ap *kuadrantv1beta3.AuthPolicy) []client.ObjectKey {
	affectedPolicies := getAffectedPolicies(t, ap)

	// Filter the policies where:
	// 1. targets a gateway
	// 2. is not the current AP that is being assessed
	// 3. is an overriding policy
	// 4. is not marked for deletion
	affectedPolicies = utils.Filter(affectedPolicies, func(policy kuadrantgatewayapi.Policy) bool {
		p, ok := policy.(*kuadrantv1beta3.AuthPolicy)
		return ok &&
			p.DeletionTimestamp == nil &&
			kuadrantgatewayapi.IsTargetRefGateway(policy.GetTargetRef()) &&
			ap.GetUID() != policy.GetUID() &&
			p.IsAtomicOverride()
	})

	return utils.Map(affectedPolicies, func(policy kuadrantgatewayapi.Policy) client.ObjectKey {
		return client.ObjectKeyFromObject(policy)
	})
}

func getAffectedPolicies(t *kuadrantgatewayapi.Topology, ap *kuadrantv1beta3.AuthPolicy) []kuadrantgatewayapi.Policy {
	topologyIndexes := kuadrantgatewayapi.NewTopologyIndexes(t)
	var affectedPolicies []kuadrantgatewayapi.Policy

	// If AP is listed within the policies from gateway, it potentially can be overridden by it
	for _, gw := range t.Gateways() {
		policyList := topologyIndexes.PoliciesFromGateway(gw.Gateway)
		if slices.Contains(utils.Map(policyList, func(p kuadrantgatewayapi.Policy) client.ObjectKey {
			return client.ObjectKeyFromObject(p)
		}), client.ObjectKeyFromObject(ap)) {
			affectedPolicies = append(affectedPolicies, policyList...)
		}
	}

	return affectedPolicies
}

// AuthConfigName returns the name of Authorino AuthConfig CR.
func AuthConfigName(apKey client.ObjectKey) string {
	return fmt.Sprintf("ap-%s-%s", apKey.Namespace, apKey.Name)
}

func authorinoSpecsFromConfigs[T, U any](configs map[string]U, extractAuthorinoSpec func(U) T) map[string]T {
	specs := make(map[string]T, len(configs))
	for name, config := range configs {
		authorinoConfig := extractAuthorinoSpec(config)
		specs[name] = authorinoConfig
	}

	if len(specs) == 0 {
		return nil
	}

	return specs
}

// authorinoConditionsFromHTTPRoute builds a list of Authorino conditions from an HTTPRoute, without using route selectors.
func authorinoConditionsFromHTTPRoute(route *gatewayapiv1.HTTPRoute) []authorinoapi.PatternExpressionOrRef {
	conditions := []authorinoapi.PatternExpressionOrRef{}
	hostnamesForConditions := []gatewayapiv1.Hostname{"*"}
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
				All: utils.Map(allOf, toAuthorinoUnstructuredPatternExpressionOrRef),
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
	return strings.Join(utils.Map(hostnames, func(hostname string) string {
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
			Any: utils.Map(oneOf, toAuthorinoUnstructuredPatternExpressionOrRef),
		},
	}
}

func authConfigBasicMutator(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*authorinoapi.AuthConfig)
	if !ok {
		return false, fmt.Errorf("%T is not an *authorinoapi.AuthConfig", existingObj)
	}
	desired, ok := desiredObj.(*authorinoapi.AuthConfig)
	if !ok {
		return false, fmt.Errorf("%T is not an *authorinoapi.AuthConfig", desiredObj)
	}

	if reflect.DeepEqual(existing.Spec, desired.Spec) {
		return false, nil
	}

	existing.Spec = desired.Spec

	return true, nil
}
