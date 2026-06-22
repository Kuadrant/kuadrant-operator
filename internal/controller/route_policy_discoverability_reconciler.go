package controllers

import (
	"context"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

// discoverableRoute wraps a route (HTTPRoute or GRPCRoute) with the operations
// needed for policy discoverability, abstracting over the concrete route type.
type discoverableRoute struct {
	targetable machinery.Targetable
	getParents func() []gatewayapiv1.RouteParentStatus
	setParents func([]gatewayapiv1.RouteParentStatus)
	destruct   func() (*unstructured.Unstructured, error)
	resource   schema.GroupVersionResource
	generation int64
}

type RoutePolicyDiscoverabilityReconciler struct {
	client *dynamic.DynamicClient
}

func NewRoutePolicyDiscoverabilityReconciler(client *dynamic.DynamicClient) *RoutePolicyDiscoverabilityReconciler {
	return &RoutePolicyDiscoverabilityReconciler{client: client}
}

func (r *RoutePolicyDiscoverabilityReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &machinery.GRPCRouteGroupKind},
			{Kind: &kuadrantv1.AuthPolicyGroupKind},
			{Kind: &kuadrantv1.RateLimitPolicyGroupKind},
			{Kind: &kuadrantv1alpha1.TokenRateLimitPolicyGroupKind},
			{Kind: &kuadrantv1.TLSPolicyGroupKind},
			{Kind: &kuadrantv1.DNSPolicyGroupKind},
		},
		ReconcileFunc: r.reconcile,
	}
}

func (r *RoutePolicyDiscoverabilityReconciler) reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, s *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("RoutePolicyDiscoverabilityReconciler").WithName("reconcile").WithValues("context", ctx)

	routes := extractDiscoverableRoutes(topology)
	policyKinds := policyGroupKinds()

	for _, route := range routes {
		routeStatusParents := deepCopyParents(route.getParents())

		for _, policyKind := range policyKinds {
			path := getRoutePath(topology, route.targetable)
			gateways := lo.FilterMap(path, func(item machinery.Targetable, _ int) (*machinery.Gateway, bool) {
				ob, ok := item.(*machinery.Gateway)
				return ob, ok
			})

			policies := kuadrantv1.PoliciesInPath(path, func(policy machinery.Policy) bool {
				return policy.GroupVersionKind().GroupKind() == *policyKind && IsPolicyAccepted(ctx, policy, s)
			})

			if len(policies) == 0 {
				routeStatusParents = removePolicyConditions(routeStatusParents, gateways, policyKind, route.targetable, logger)
			} else {
				routeStatusParents = addPolicyConditions(routeStatusParents, gateways, policies, policyKind, route, logger)
			}
		}

		if !equality.Semantic.DeepEqual(routeStatusParents, route.getParents()) {
			if err := r.updateRouteStatus(ctx, route, routeStatusParents, logger); err != nil {
				if strings.Contains(err.Error(), "StorageError: invalid object") {
					logger.Info("possible error updating resource", "err", err, "possible_cause", "resource has being removed from the cluster already")
					continue
				}
				logger.Error(err, "unable to update route status", "name", route.targetable.GetName(), "namespace", route.targetable.GetNamespace())
			}
		}
	}
	return nil
}

func (r *RoutePolicyDiscoverabilityReconciler) updateRouteStatus(ctx context.Context, route discoverableRoute, parents []gatewayapiv1.RouteParentStatus, logger logr.Logger) error {
	// Set parents on the typed object before destructing, so the unstructured
	// representation includes the updated status
	route.setParents(parents)

	obj, err := route.destruct()
	if err != nil {
		logger.Error(err, "unable to destruct route", "name", route.targetable.GetName(), "namespace", route.targetable.GetNamespace())
		return err
	}

	_, err = r.client.Resource(route.resource).Namespace(route.targetable.GetNamespace()).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
	return err
}

// extractDiscoverableRoutes finds all HTTPRoutes and GRPCRoutes in the topology
// and wraps them as discoverableRoutes.
func extractDiscoverableRoutes(topology *machinery.Topology) []discoverableRoute {
	var routes []discoverableRoute

	for _, item := range topology.Targetables().Items() {
		switch route := item.(type) {
		case *machinery.HTTPRoute:
			routes = append(routes, discoverableRoute{
				targetable: route,
				getParents: func() []gatewayapiv1.RouteParentStatus { return route.Status.Parents },
				setParents: func(parents []gatewayapiv1.RouteParentStatus) { route.Status.Parents = parents },
				destruct:   func() (*unstructured.Unstructured, error) { return controller.Destruct(route.HTTPRoute) },
				resource:   controller.HTTPRoutesResource,
				generation: route.GetGeneration(),
			})
		case *machinery.GRPCRoute:
			routes = append(routes, discoverableRoute{
				targetable: route,
				getParents: func() []gatewayapiv1.RouteParentStatus { return route.Status.Parents },
				setParents: func(parents []gatewayapiv1.RouteParentStatus) { route.Status.Parents = parents },
				destruct:   func() (*unstructured.Unstructured, error) { return controller.Destruct(route.GRPCRoute) },
				resource:   controller.GRPCRoutesResource,
				generation: route.GetGeneration(),
			})
		}
	}

	return routes
}

func getRoutePath(topology *machinery.Topology, route machinery.Targetable) []machinery.Targetable {
	path := make([]machinery.Targetable, 0, 3)
	path = append(path, route)
	for _, listener := range topology.Targetables().Parents(route) {
		path = append(path, listener)
		path = append(path, topology.Targetables().Parents(listener)...)
	}
	return lo.UniqBy(path, func(item machinery.Targetable) string {
		return item.GetLocator()
	})
}

func removePolicyConditions(routeStatusParents []gatewayapiv1.RouteParentStatus, gateways []*machinery.Gateway, policyKind *schema.GroupKind, route machinery.Targetable, logger logr.Logger) []gatewayapiv1.RouteParentStatus {
	conditionType := PolicyAffectedConditionType(policyKind.Kind)
	for _, gw := range gateways {
		i := utils.Index(routeStatusParents, FindRouteParentStatusFunc(route.GetNamespace(), client.ObjectKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}, kuadrant.ControllerName))
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

func addPolicyConditions(routeStatusParents []gatewayapiv1.RouteParentStatus, gateways []*machinery.Gateway, policies []machinery.Policy, policyKind *schema.GroupKind, route discoverableRoute, logger logr.Logger) []gatewayapiv1.RouteParentStatus {
	condition := PolicyAffectedCondition(policyKind.Kind, policies)
	for _, gw := range gateways {
		i := ensureRouteParentStatus(&routeStatusParents, route.targetable, gw)
		if currentCondition := meta.FindStatusCondition(routeStatusParents[i].Conditions, condition.Type); currentCondition != nil &&
			currentCondition.Status == condition.Status && currentCondition.Reason == condition.Reason &&
			currentCondition.Message == condition.Message && currentCondition.ObservedGeneration == route.generation {
			logger.V(1).Info("condition already up-to-date, skipping", "condition", condition.Type, "status", condition.Status, "name", route.targetable.GetName(), "namespace", route.targetable.GetNamespace())
			continue
		}
		condition.ObservedGeneration = route.generation
		meta.SetStatusCondition(&(routeStatusParents[i].Conditions), condition)
		logger.V(1).Info("adding condition", "condition", condition.Type, "status", condition.Status, "name", route.targetable.GetName(), "namespace", route.targetable.GetNamespace())
	}
	return routeStatusParents
}

func ensureRouteParentStatus(routeStatusParents *[]gatewayapiv1.RouteParentStatus, route machinery.Targetable, gw *machinery.Gateway) int {
	i := utils.Index(*routeStatusParents, FindRouteParentStatusFunc(route.GetNamespace(), client.ObjectKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}, kuadrant.ControllerName))
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
		i = len(*routeStatusParents) - 1
	}
	return i
}

func deepCopyParents(parents []gatewayapiv1.RouteParentStatus) []gatewayapiv1.RouteParentStatus {
	copied := make([]gatewayapiv1.RouteParentStatus, len(parents))
	for i, p := range parents {
		copied[i] = *p.DeepCopy()
	}
	return copied
}

// FindRouteParentStatusFunc returns a predicate that matches a RouteParentStatus
// for a given route namespace and gateway. Works for any route type.
func FindRouteParentStatusFunc(routeNamespace string, gatewayKey client.ObjectKey, controllerName gatewayapiv1.GatewayController) func(gatewayapiv1.RouteParentStatus) bool {
	return func(p gatewayapiv1.RouteParentStatus) bool {
		return p.ParentRef.Kind != nil && *p.ParentRef.Kind == "Gateway" &&
			p.ControllerName == controllerName &&
			((p.ParentRef.Namespace == nil && routeNamespace == gatewayKey.Namespace) || (p.ParentRef.Namespace != nil && string(*p.ParentRef.Namespace) == gatewayKey.Namespace)) &&
			string(p.ParentRef.Name) == gatewayKey.Name
	}
}
