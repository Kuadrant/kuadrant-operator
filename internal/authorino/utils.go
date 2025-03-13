package authorino

import (
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	AuthConfigGroupKind = schema.GroupKind{Group: authorinov1beta3.GroupVersion.Group, Kind: "AuthConfig"}
	AuthConfigsResource = authorinov1beta3.GroupVersion.WithResource("authconfigs")

	AuthConfigHTTPRouteRuleAnnotation = machinery.HTTPRouteRuleGroupKind.String()
)

func LinkHTTPRouteRuleToAuthConfig(objs controller.Store) machinery.LinkFunc {
	httpRoutes := lo.Map(objs.FilterByGroupKind(machinery.HTTPRouteGroupKind), controller.ObjectAs[*gatewayapiv1.HTTPRoute])
	httpRouteRules := lo.FlatMap(lo.Map(httpRoutes, func(r *gatewayapiv1.HTTPRoute, _ int) *machinery.HTTPRoute {
		return &machinery.HTTPRoute{HTTPRoute: r}
	}), machinery.HTTPRouteRulesFromHTTPRouteFunc)

	return machinery.LinkFunc{
		From: machinery.HTTPRouteRuleGroupKind,
		To:   AuthConfigGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			return lo.FilterMap(httpRouteRules, func(httpRouteRule *machinery.HTTPRouteRule, _ int) (machinery.Object, bool) {
				authConfig := child.(*controller.RuntimeObject).Object.(*authorinov1beta3.AuthConfig)
				annotations := authConfig.GetAnnotations()
				return httpRouteRule, annotations != nil && annotations[AuthConfigHTTPRouteRuleAnnotation] == httpRouteRule.GetLocator()
			})
		},
	}
}
