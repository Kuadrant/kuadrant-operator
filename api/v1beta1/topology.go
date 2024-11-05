package v1beta1

import (
	authorinooperatorv1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	KuadrantGroupKind   = schema.GroupKind{Group: GroupVersion.Group, Kind: "Kuadrant"}
	LimitadorGroupKind  = schema.GroupKind{Group: limitadorv1alpha1.GroupVersion.Group, Kind: "Limitador"}
	AuthorinoGroupKind  = schema.GroupKind{Group: authorinooperatorv1beta1.GroupVersion.Group, Kind: "Authorino"}
	AuthConfigGroupKind = schema.GroupKind{Group: authorinov1beta3.GroupVersion.Group, Kind: "AuthConfig"}

	KuadrantsResource   = GroupVersion.WithResource("kuadrants")
	LimitadorsResource  = limitadorv1alpha1.GroupVersion.WithResource("limitadors")
	AuthorinosResource  = authorinooperatorv1beta1.GroupVersion.WithResource("authorinos")
	AuthConfigsResource = authorinov1beta3.GroupVersion.WithResource("authconfigs")

	AuthConfigHTTPRouteRuleAnnotation = machinery.HTTPRouteRuleGroupKind.String()
)

var _ machinery.Object = &Kuadrant{}

func (p *Kuadrant) GetLocator() string {
	return machinery.LocatorFromObject(p)
}

func LinkKuadrantToGatewayClasses(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(KuadrantGroupKind), controller.ObjectAs[*Kuadrant])

	return machinery.LinkFunc{
		From: KuadrantGroupKind,
		To:   schema.GroupKind{Group: gatewayapiv1.GroupVersion.Group, Kind: "GatewayClass"},
		Func: func(_ machinery.Object) []machinery.Object {
			parents := make([]machinery.Object, len(kuadrants))
			for _, parent := range kuadrants {
				parents = append(parents, parent)
			}
			return parents
		},
	}
}

func LinkKuadrantToLimitador(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(KuadrantGroupKind), controller.ObjectAs[machinery.Object])

	return machinery.LinkFunc{
		From: KuadrantGroupKind,
		To:   LimitadorGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			return lo.Filter(kuadrants, func(kuadrant machinery.Object, _ int) bool {
				return kuadrant.GetNamespace() == child.GetNamespace() && child.GetName() == "limitador"
			})
		},
	}
}

func LinkKuadrantToAuthorino(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(KuadrantGroupKind), controller.ObjectAs[machinery.Object])

	return machinery.LinkFunc{
		From: KuadrantGroupKind,
		To:   AuthorinoGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			return lo.Filter(kuadrants, func(kuadrant machinery.Object, _ int) bool {
				return kuadrant.GetNamespace() == child.GetNamespace() && child.GetName() == "authorino"
			})
		},
	}
}

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
