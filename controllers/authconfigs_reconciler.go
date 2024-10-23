package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	authorinov1beta2 "github.com/kuadrant/authorino/api/v1beta2"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

type AuthConfigsReconciler struct {
	client *dynamic.DynamicClient
}

// AuthConfigsReconciler subscribes to events with potential to change Authorino AuthConfig custom resources
func (r *AuthConfigsReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1beta3.AuthPolicyGroupKind},
			{Kind: &kuadrantv1beta1.AuthConfigGroupKind},
		},
	}
}

func (r *AuthConfigsReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("AuthConfigsReconciler")

	authorino, err := GetAuthorinoFromTopology(topology)
	if err != nil {
		if errors.Is(err, ErrMissingKuadrant) || errors.Is(err, ErrMissingAuthorino) {
			logger.V(1).Info(err.Error())
			return nil
		}
		return err
	}
	authConfigsNamespace := authorino.GetNamespace()

	effectivePolicies, ok := state.Load(StateEffectiveAuthPolicies)
	if !ok {
		logger.Error(ErrMissingStateEffectiveAuthPolicies, "failed to build limitador limits")
		return nil
	}
	effectivePoliciesMap := effectivePolicies.(EffectiveAuthPolicies)

	logger.V(1).Info("reconciling authconfig objects", "effectivePolicies", len(effectivePoliciesMap))
	defer logger.V(1).Info("finished reconciling authconfig objects")

	desiredAuthConfigs := make(map[k8stypes.NamespacedName]struct{})
	var modifiedAuthConfigs []string

	for pathID, effectivePolicy := range effectivePoliciesMap {
		_, _, _, httpRoute, httpRouteRule, _ := common.ObjectsInRequestPath(effectivePolicy.Path)
		httpRouteKey := k8stypes.NamespacedName{Name: httpRoute.GetName(), Namespace: httpRoute.GetNamespace()}
		httpRouteRuleKey := httpRouteRule.Name

		authConfigName := authConfigNameForPath(pathID)
		desiredAuthConfig := r.buildDesiredAuthConfig(effectivePolicy, authConfigName, authConfigsNamespace)
		desiredAuthConfigs[k8stypes.NamespacedName{Name: desiredAuthConfig.GetName(), Namespace: desiredAuthConfig.GetNamespace()}] = struct{}{}

		resource := r.client.Resource(kuadrantv1beta1.AuthConfigsResource).Namespace(desiredAuthConfig.GetNamespace())

		existingAuthConfigObj, found := lo.Find(topology.Objects().Children(httpRouteRule), func(child machinery.Object) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantv1beta1.AuthConfigGroupKind && child.GetName() == authConfigName && labels.Set(child.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(labels.Set(desiredAuthConfig.GetLabels()))
		})

		// create
		if !found {
			modifiedAuthConfigs = append(modifiedAuthConfigs, authConfigName)
			desiredAuthConfigUnstructured, err := common.Destruct(desiredAuthConfig)
			if err != nil {
				logger.Error(err, "failed to destruct authconfig object", "httpRoute", httpRouteKey.String(), "httpRouteRule", httpRouteRuleKey, "authconfig", desiredAuthConfig)
				continue
			}

			if _, err = resource.Create(ctx, desiredAuthConfigUnstructured, metav1.CreateOptions{}); err != nil {
				logger.Error(err, "failed to create authconfig object", "httpRoute", httpRouteKey.String(), "httpRouteRule", httpRouteRuleKey, "authconfig", desiredAuthConfigUnstructured.Object)
				// TODO: handle error
			}
			continue
		}

		existingAuthConfig := existingAuthConfigObj.(*controller.RuntimeObject).Object.(*authorinov1beta2.AuthConfig)

		if equalAuthConfigs(existingAuthConfig, desiredAuthConfig) {
			logger.V(1).Info("authconfig object is up to date, nothing to do")
			continue
		}

		modifiedAuthConfigs = append(modifiedAuthConfigs, authConfigName)

		// delete
		if utils.IsObjectTaggedToDelete(desiredAuthConfig) && !utils.IsObjectTaggedToDelete(existingAuthConfig) {
			if err := resource.Delete(ctx, existingAuthConfig.GetName(), metav1.DeleteOptions{}); err != nil {
				logger.Error(err, "failed to delete wasmplugin object", "httpRoute", httpRouteKey.String(), "httpRouteRule", httpRouteRuleKey, "authconfig", fmt.Sprintf("%s/%s", existingAuthConfig.GetNamespace(), existingAuthConfig.GetName()))
				// TODO: handle error
			}
			continue
		}

		// update
		existingAuthConfig.Spec = desiredAuthConfig.Spec

		existingAuthConfigUnstructured, err := common.Destruct(existingAuthConfig)
		if err != nil {
			logger.Error(err, "failed to destruct authconfig object", "httpRoute", httpRouteKey.String(), "httpRouteRule", httpRouteRuleKey, "authconfig", existingAuthConfig)
			continue
		}
		if _, err = resource.Update(ctx, existingAuthConfigUnstructured, metav1.UpdateOptions{}); err != nil {
			logger.Error(err, "failed to update authconfig object", "httpRoute", httpRouteKey.String(), "httpRouteRule", httpRouteRuleKey, "authconfig", existingAuthConfigUnstructured.Object)
			// TODO: handle error
		}
	}

	state.Store(StateModifiedAuthConfigs, modifiedAuthConfigs)

	// cleanup authconfigs that are not in the effective policies
	staleAuthConfigs := topology.Objects().Items(func(o machinery.Object) bool {
		_, desired := desiredAuthConfigs[k8stypes.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()}]
		return o.GroupVersionKind().GroupKind() == kuadrantv1beta1.AuthConfigGroupKind && labels.Set(o.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(AuthObjectLabels()) && !desired
	})
	for _, authConfig := range staleAuthConfigs {
		if err := r.client.Resource(kuadrantv1beta1.AuthConfigsResource).Namespace(authConfig.GetNamespace()).Delete(ctx, authConfig.GetName(), metav1.DeleteOptions{}); err != nil {
			logger.Error(err, "failed to delete authconfig object", "authconfig", fmt.Sprintf("%s/%s", authConfig.GetNamespace(), authConfig.GetName()))
			// TODO: handle error
		}
	}

	return nil
}

func (r *AuthConfigsReconciler) buildDesiredAuthConfig(effectivePolicy EffectiveAuthPolicy, name, namespace string) *authorinov1beta2.AuthConfig {
	_, _, _, _, httpRouteRule, _ := common.ObjectsInRequestPath(effectivePolicy.Path)

	authConfig := &authorinov1beta2.AuthConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AuthConfig",
			APIVersion: authorinov1beta2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				kuadrantv1beta1.AuthConfigHTTPRouteRuleAnnotation: httpRouteRule.GetLocator(),
			},
			Labels: AuthObjectLabels(),
		},
		Spec: authorinov1beta2.AuthConfigSpec{
			Hosts: []string{name},
		},
	}

	spec := effectivePolicy.Spec.Spec.Proper()

	// named patterns
	if namedPatterns := spec.NamedPatterns; namedPatterns != nil {
		authConfig.Spec.NamedPatterns = lo.MapValues(spec.NamedPatterns, func(v kuadrantv1beta3.MergeablePatternExpressions, _ string) authorinov1beta2.PatternExpressions {
			return v.PatternExpressions
		})
	}

	// top-level conditions
	if conditions := spec.Conditions; conditions != nil {
		authConfig.Spec.Conditions = lo.Map(spec.Conditions, func(v kuadrantv1beta3.MergeablePatternExpressionOrRef, _ int) authorinov1beta2.PatternExpressionOrRef {
			return v.PatternExpressionOrRef
		})
	}

	// return early if authScheme is nil
	authScheme := spec.AuthScheme
	if authScheme == nil {
		return authConfig
	}

	// authentication
	if authentication := authScheme.Authentication; authentication != nil {
		authConfig.Spec.Authentication = lo.MapValues(authentication, func(v kuadrantv1beta3.MergeableAuthenticationSpec, _ string) authorinov1beta2.AuthenticationSpec {
			return v.AuthenticationSpec
		})
	}

	// metadata
	if metadata := authScheme.Metadata; metadata != nil {
		authConfig.Spec.Metadata = lo.MapValues(metadata, func(v kuadrantv1beta3.MergeableMetadataSpec, _ string) authorinov1beta2.MetadataSpec {
			return v.MetadataSpec
		})
	}

	// authorization
	if authorization := authScheme.Authorization; authorization != nil {
		authConfig.Spec.Authorization = lo.MapValues(authorization, func(v kuadrantv1beta3.MergeableAuthorizationSpec, _ string) authorinov1beta2.AuthorizationSpec {
			return v.AuthorizationSpec
		})
	}

	// response
	if response := authScheme.Response; response != nil {
		var unauthenticated *authorinov1beta2.DenyWithSpec
		if response.Unauthenticated != nil {
			unauthenticated = &response.Unauthenticated.DenyWithSpec
		}

		var unauthorized *authorinov1beta2.DenyWithSpec
		if response.Unauthorized != nil {
			unauthorized = &response.Unauthorized.DenyWithSpec
		}

		authConfig.Spec.Response = &authorinov1beta2.ResponseSpec{
			Unauthenticated: unauthenticated,
			Unauthorized:    unauthorized,
			Success: authorinov1beta2.WrappedSuccessResponseSpec{
				Headers: authorinoSpecsFromConfigs(response.Success.Headers, func(config kuadrantv1beta3.MergeableHeaderSuccessResponseSpec) authorinov1beta2.HeaderSuccessResponseSpec {
					return authorinov1beta2.HeaderSuccessResponseSpec{SuccessResponseSpec: config.SuccessResponseSpec}
				}),
				DynamicMetadata: authorinoSpecsFromConfigs(response.Success.DynamicMetadata, func(config kuadrantv1beta3.MergeableSuccessResponseSpec) authorinov1beta2.SuccessResponseSpec {
					return config.SuccessResponseSpec
				}),
			},
		}
	}

	// callbacks
	if callbacks := authScheme.Callbacks; callbacks != nil {
		authConfig.Spec.Callbacks = lo.MapValues(callbacks, func(v kuadrantv1beta3.MergeableCallbackSpec, _ string) authorinov1beta2.CallbackSpec {
			return v.CallbackSpec
		})
	}

	return authConfig
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

func equalAuthConfigs(existing, desired *authorinov1beta2.AuthConfig) bool {
	// httprouterule back ref annotation
	existingAnnotations := existing.GetAnnotations()
	desiredAnnotations := desired.GetAnnotations()
	if existingAnnotations == nil || desiredAnnotations == nil || existingAnnotations[kuadrantv1beta1.AuthConfigHTTPRouteRuleAnnotation] != desiredAnnotations[kuadrantv1beta1.AuthConfigHTTPRouteRuleAnnotation] {
		return false
	}

	// labels
	existingLabels := existing.GetLabels()
	desiredLabels := desired.GetLabels()
	if len(existingLabels) != len(desiredLabels) || !labels.Set(existingLabels).AsSelector().Matches(labels.Set(desiredLabels)) {
		return false
	}

	// spec
	return reflect.DeepEqual(existing.Spec, desired.Spec)
}

// TODO(guicassolato): remove these functions below if we decide not to build conditions from the HTTPRouteRule + hostnames

// authorinoConditionsFromHTTPRouteRule builds a list of Authorino conditions from a HTTPRouteRule and a list of hostnames
// * Each combination of HTTPRouteMatch and hostname yields one condition.
// * Rules that specify no explicit HTTPRouteMatch are assumed to match all requests (i.e. implicit catch-all rule.)
// * Empty list of hostnames yields a condition without a hostname pattern expression.
func authorinoConditionsFromHTTPRouteRule(rule gatewayapiv1.HTTPRouteRule, hostnames []gatewayapiv1.Hostname) []authorinov1beta2.PatternExpressionOrRef {
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
		return []authorinov1beta2.PatternExpressionOrRef{hostnameRuleToAuthorinoCondition(hosts)}
	}

	var oneOf []authorinov1beta2.PatternExpressionOrRef

	// http route matches and possibly hostnames → we need one authorino rule per http route match
	for _, match := range rule.Matches {
		var allOf []authorinov1beta2.PatternExpressionOrRef

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
			oneOf = append(oneOf, authorinov1beta2.PatternExpressionOrRef{
				All: utils.Map(allOf, toAuthorinoUnstructuredPatternExpressionOrRef),
			})
		}
	}
	return toAuthorinoOneOfPatternExpressionsOrRefs(oneOf)
}

func hostnameRuleToAuthorinoCondition(hostnames []string) authorinov1beta2.PatternExpressionOrRef {
	return authorinov1beta2.PatternExpressionOrRef{
		PatternExpression: authorinov1beta2.PatternExpression{
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

func httpMethodRuleToAuthorinoCondition(method gatewayapiv1.HTTPMethod) authorinov1beta2.PatternExpressionOrRef {
	return authorinov1beta2.PatternExpressionOrRef{
		PatternExpression: authorinov1beta2.PatternExpression{
			Selector: "request.method",
			Operator: "eq",
			Value:    string(method),
		},
	}
}

func httpPathRuleToAuthorinoCondition(path gatewayapiv1.HTTPPathMatch) authorinov1beta2.PatternExpressionOrRef {
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

	return authorinov1beta2.PatternExpressionOrRef{
		PatternExpression: authorinov1beta2.PatternExpression{
			Selector: `request.url_path`,
			Operator: authorinov1beta2.PatternExpressionOperator(operator),
			Value:    value,
		},
	}
}

func httpHeadersRuleToAuthorinoConditions(headers []gatewayapiv1.HTTPHeaderMatch) []authorinov1beta2.PatternExpressionOrRef {
	conditions := make([]authorinov1beta2.PatternExpressionOrRef, 0, len(headers))
	for _, header := range headers {
		condition := httpHeaderRuleToAuthorinoCondition(header)
		conditions = append(conditions, condition)
	}
	return conditions
}

func httpHeaderRuleToAuthorinoCondition(header gatewayapiv1.HTTPHeaderMatch) authorinov1beta2.PatternExpressionOrRef {
	operator := "eq" // gateway api defaults to HeaderMatchExact
	if header.Type != nil && *header.Type == gatewayapiv1.HeaderMatchRegularExpression {
		operator = "matches"
	}
	return authorinov1beta2.PatternExpressionOrRef{
		PatternExpression: authorinov1beta2.PatternExpression{
			Selector: fmt.Sprintf("request.headers.%s", strings.ToLower(string(header.Name))),
			Operator: authorinov1beta2.PatternExpressionOperator(operator),
			Value:    header.Value,
		},
	}
}

func httpQueryParamsRuleToAuthorinoConditions(queryParams []gatewayapiv1.HTTPQueryParamMatch) []authorinov1beta2.PatternExpressionOrRef {
	conditions := make([]authorinov1beta2.PatternExpressionOrRef, 0, len(queryParams))
	for _, queryParam := range queryParams {
		condition := httpQueryParamRuleToAuthorinoCondition(queryParam)
		conditions = append(conditions, condition)
	}
	return conditions
}

func httpQueryParamRuleToAuthorinoCondition(queryParam gatewayapiv1.HTTPQueryParamMatch) authorinov1beta2.PatternExpressionOrRef {
	operator := "eq" // gateway api defaults to QueryParamMatchExact
	if queryParam.Type != nil && *queryParam.Type == gatewayapiv1.QueryParamMatchRegularExpression {
		operator = "matches"
	}
	return authorinov1beta2.PatternExpressionOrRef{
		Any: []authorinov1beta2.UnstructuredPatternExpressionOrRef{
			{
				PatternExpressionOrRef: authorinov1beta2.PatternExpressionOrRef{
					PatternExpression: authorinov1beta2.PatternExpression{
						Selector: fmt.Sprintf(`request.path.@extract:{"sep":"?%s=","pos":1}|@extract:{"sep":"&"}`, queryParam.Name),
						Operator: authorinov1beta2.PatternExpressionOperator(operator),
						Value:    queryParam.Value,
					},
				},
			},
			{
				PatternExpressionOrRef: authorinov1beta2.PatternExpressionOrRef{
					PatternExpression: authorinov1beta2.PatternExpression{
						Selector: fmt.Sprintf(`request.path.@extract:{"sep":"&%s=","pos":1}|@extract:{"sep":"&"}`, queryParam.Name),
						Operator: authorinov1beta2.PatternExpressionOperator(operator),
						Value:    queryParam.Value,
					},
				},
			},
		},
	}
}

func toAuthorinoUnstructuredPatternExpressionOrRef(patternExpressionOrRef authorinov1beta2.PatternExpressionOrRef) authorinov1beta2.UnstructuredPatternExpressionOrRef {
	return authorinov1beta2.UnstructuredPatternExpressionOrRef{PatternExpressionOrRef: patternExpressionOrRef}
}

func toAuthorinoOneOfPatternExpressionsOrRefs(oneOf []authorinov1beta2.PatternExpressionOrRef) []authorinov1beta2.PatternExpressionOrRef {
	return []authorinov1beta2.PatternExpressionOrRef{
		{
			Any: utils.Map(oneOf, toAuthorinoUnstructuredPatternExpressionOrRef),
		},
	}
}
