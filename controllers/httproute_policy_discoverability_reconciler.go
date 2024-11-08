package controllers

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

type HTTPRoutePolicyDiscoverabilityReconciler struct {
	Client *dynamic.DynamicClient
}

func NewHTTPRoutePolicyDiscoverabilityReconciler(client *dynamic.DynamicClient) *HTTPRoutePolicyDiscoverabilityReconciler {
	return &HTTPRoutePolicyDiscoverabilityReconciler{Client: client}
}

func (r *HTTPRoutePolicyDiscoverabilityReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1.AuthPolicyGroupKind},
			{Kind: &kuadrantv1.RateLimitPolicyGroupKind},
			{Kind: &kuadrantv1.TLSPolicyGroupKind},
			{Kind: &kuadrantv1.DNSPolicyGroupKind},
		},
		ReconcileFunc: r.reconcile,
	}
}

func (r *HTTPRoutePolicyDiscoverabilityReconciler) reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, s *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("HTTPRoutePolicyDiscoverabilityReconciler").WithName("reconcile")

	httpRoutes := lo.FilterMap(topology.Targetables().Items(), func(item machinery.Targetable, index int) (*machinery.HTTPRoute, bool) {
		ob, ok := item.(*machinery.HTTPRoute)
		return ob, ok
	})

	policyKinds := policyGroupKinds()

	for _, route := range httpRoutes {
		routeStatusParents := route.Status.DeepCopy().Parents

		for _, policyKind := range policyKinds {
			path := getRoutePath(topology, route)
			gateways := lo.FilterMap(path, func(item machinery.Targetable, index int) (*machinery.Gateway, bool) {
				ob, ok := item.(*machinery.Gateway)
				return ob, ok
			})

			policies := kuadrantv1.PoliciesInPath(path, func(policy machinery.Policy) bool {
				return policy.GroupVersionKind().GroupKind() == *policyKind && IsPolicyAccepted(ctx, policy, s)
			})

			if len(policies) == 0 {
				routeStatusParents = removePolicyConditions(routeStatusParents, gateways, policyKind, route, logger)
			} else {
				routeStatusParents = addPolicyConditions(routeStatusParents, gateways, policies, policyKind, route, logger)
			}
		}

		if !equality.Semantic.DeepEqual(routeStatusParents, route.Status.Parents) {
			route.Status.Parents = routeStatusParents
			if err := r.updateRouteStatus(ctx, route, logger); err != nil {
				logger.Error(err, "unable to update route status", "name", route.GetName(), "namespace", route.GetNamespace(), "uid", route.GetUID())
			}
		}
	}
	return nil
}

func (r *HTTPRoutePolicyDiscoverabilityReconciler) updateRouteStatus(ctx context.Context, route *machinery.HTTPRoute, logger logr.Logger) error {
	obj, err := controller.Destruct(route.HTTPRoute)
	if err != nil {
		logger.Error(err, "unable to destruct route", "name", route.GetName(), "namespace", route.GetNamespace(), "uid", route.GetUID())
		return err
	}
	_, err = r.Client.Resource(controller.HTTPRoutesResource).Namespace(route.GetNamespace()).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
	return err
}

func getRoutePath(topology *machinery.Topology, route *machinery.HTTPRoute) []machinery.Targetable {
	path := []machinery.Targetable{route}
	for _, listener := range topology.Targetables().Parents(route) {
		path = append(path, listener)
		path = append(path, topology.Targetables().Parents(listener)...)
	}
	return lo.UniqBy(path, func(item machinery.Targetable) string {
		return item.GetLocator()
	})
}

func removePolicyConditions(routeStatusParents []gatewayapiv1.RouteParentStatus, gateways []*machinery.Gateway, policyKind *schema.GroupKind, route *machinery.HTTPRoute, logger logr.Logger) []gatewayapiv1.RouteParentStatus {
	conditionType := PolicyAffectedConditionType(policyKind.Kind)
	for _, gw := range gateways {
		i := utils.Index(routeStatusParents, FindRouteParentStatusFunc(route.HTTPRoute, client.ObjectKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}, kuadrant.ControllerName))
		if i < 0 {
			logger.V(1).Info("cannot find parent status, skipping")
			continue
		}
		if c := meta.FindStatusCondition(routeStatusParents[i].Conditions, conditionType); c == nil {
			logger.V(1).Info("condition already absent, skipping")
			continue
		}
		logger.V(1).Info("removing condition from route")
		meta.RemoveStatusCondition(&(routeStatusParents[i].Conditions), conditionType)
		if len(routeStatusParents[i].Conditions) == 0 {
			routeStatusParents = append(routeStatusParents[:i], routeStatusParents[i+1:]...)
		}
	}
	return routeStatusParents
}

func addPolicyConditions(routeStatusParents []gatewayapiv1.RouteParentStatus, gateways []*machinery.Gateway, policies []machinery.Policy, policyKind *schema.GroupKind, route *machinery.HTTPRoute, logger logr.Logger) []gatewayapiv1.RouteParentStatus {
	condition := PolicyAffectedCondition(policyKind.Kind, policies)
	for _, gw := range gateways {
		i := ensureRouteParentStatus(&routeStatusParents, route, gw)
		if currentCondition := meta.FindStatusCondition(routeStatusParents[i].Conditions, condition.Type); currentCondition != nil &&
			currentCondition.Status == condition.Status && currentCondition.Reason == condition.Reason &&
			currentCondition.Message == condition.Message && currentCondition.ObservedGeneration == route.GetGeneration() {
			logger.V(1).Info("condition already up-to-date, skipping", "condition", condition.Type, "status", condition.Status, "name", route.GetName(), "namespace", route.GetNamespace(), "uid", route.GetUID())
			continue
		}
		condition.ObservedGeneration = route.GetGeneration()
		meta.SetStatusCondition(&(routeStatusParents[i].Conditions), condition)
		logger.V(1).Info("adding condition", "condition", condition.Type, "status", condition.Status, "name", route.GetName(), "namespace", route.GetNamespace(), "uid", route.GetUID())
	}
	return routeStatusParents
}

func ensureRouteParentStatus(routeStatusParents *[]gatewayapiv1.RouteParentStatus, route *machinery.HTTPRoute, gw *machinery.Gateway) int {
	i := utils.Index(*routeStatusParents, FindRouteParentStatusFunc(route.HTTPRoute, client.ObjectKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}, kuadrant.ControllerName))
	if i < 0 {
		*routeStatusParents = append(*routeStatusParents, gatewayapiv1.RouteParentStatus{
			ControllerName: kuadrant.ControllerName,
			ParentRef: gatewayapiv1.ParentReference{
				Kind:      ptr.To[gatewayapiv1.Kind]("Gateway"),
				Name:      gatewayapiv1.ObjectName(gw.GetName()),
				Namespace: ptr.To(gatewayapiv1.Namespace(gw.GetNamespace())),
			},
			Conditions: []metav1.Condition{},
		})
		i = utils.Index(*routeStatusParents, FindRouteParentStatusFunc(route.HTTPRoute, client.ObjectKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}, kuadrant.ControllerName))
	}
	return i
}

func FindRouteParentStatusFunc(route *gatewayapiv1.HTTPRoute, gatewayKey client.ObjectKey, controllerName gatewayapiv1.GatewayController) func(gatewayapiv1.RouteParentStatus) bool {
	return func(p gatewayapiv1.RouteParentStatus) bool {
		return *p.ParentRef.Kind == "Gateway" &&
			p.ControllerName == controllerName &&
			((p.ParentRef.Namespace == nil && route.GetNamespace() == gatewayKey.Namespace) || string(*p.ParentRef.Namespace) == gatewayKey.Namespace) &&
			string(p.ParentRef.Name) == gatewayKey.Name
	}
}
