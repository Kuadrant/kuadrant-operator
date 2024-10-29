package controllers

import (
	"context"
	"sync"

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
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
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
			{Kind: &kuadrantv1beta3.AuthPolicyGroupKind},
			{Kind: &kuadrantv1beta3.RateLimitPolicyGroupKind},
			{Kind: &kuadrantv1alpha1.TLSPolicyGroupKind},
			{Kind: &kuadrantv1alpha1.DNSPolicyGroupKind},
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

	policyKinds := []*schema.GroupKind{
		&kuadrantv1beta3.AuthPolicyGroupKind,
		&kuadrantv1beta3.RateLimitPolicyGroupKind,
		&kuadrantv1alpha1.TLSPolicyGroupKind,
		&kuadrantv1alpha1.DNSPolicyGroupKind,
	}

	for _, route := range httpRoutes {
		routeStatusParents := route.Status.DeepCopy().Parents

		for _, policyKind := range policyKinds {
			// Path - Route + Listeners + Gateways
			path := []machinery.Targetable{route}
			for _, listener := range topology.Targetables().Parents(route) {
				path = append(path, listener)                                    // listener
				path = append(path, topology.Targetables().Parents(listener)...) // Gateway
			}

			// Remove duplicates
			uniquePath := lo.UniqBy(path, func(item machinery.Targetable) string {
				return item.GetLocator()
			})

			// Get gateways only in the path
			gws := lo.FilterMap(uniquePath, func(item machinery.Targetable, index int) (*machinery.Gateway, bool) {
				ob, ok := item.(*machinery.Gateway)
				return ob, ok
			})

			policies := kuadrantv1.PoliciesInPath(uniquePath, func(policy machinery.Policy) bool {
				// Filter for policies of kind
				return policy.GroupVersionKind().GroupKind() == *policyKind && IsPolicyAccepted(ctx, policy, s)
			})

			// No policies of kind attached - remove condition
			if len(policies) == 0 {
				for _, gw := range gws {
					i := utils.Index(routeStatusParents, FindRouteParentStatusFunc(route.HTTPRoute, client.ObjectKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}, kuadrant.ControllerName))
					if i < 0 {
						logger.V(1).Info("cannot find parent status, skipping")
						continue
					}

					conditionType := PolicyAffectedConditionType(policyKind.Kind)

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
				continue
			}

			// Policies of kind attached - add condition
			for _, gw := range gws {
				i := utils.Index(routeStatusParents, FindRouteParentStatusFunc(route.HTTPRoute, client.ObjectKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}, kuadrant.ControllerName))
				if i < 0 {
					logger.V(1).Info("cannot find parent status, creating new one")
					routeStatusParents = append(routeStatusParents, gatewayapiv1.RouteParentStatus{
						ControllerName: kuadrant.ControllerName,
						ParentRef: gatewayapiv1.ParentReference{
							Kind:      ptr.To[gatewayapiv1.Kind]("Gateway"),
							Name:      gatewayapiv1.ObjectName(gw.GetName()),
							Namespace: ptr.To(gatewayapiv1.Namespace(gw.GetNamespace())),
						},
						Conditions: []metav1.Condition{},
					})
					i = utils.Index(routeStatusParents, FindRouteParentStatusFunc(route.HTTPRoute, client.ObjectKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}, kuadrant.ControllerName))
				}

				condition := PolicyAffectedCondition(policyKind.Kind, policies)

				if c := meta.FindStatusCondition(routeStatusParents[i].Conditions, condition.Type); c != nil &&
					c.Status == condition.Status && c.Reason == condition.Reason && c.Message == condition.Message && c.ObservedGeneration == route.GetGeneration() {
					logger.V(1).Info("condition already up-to-date, skipping", "condition", condition.Type, "status", condition.Status, "name", route.GetName(), "namespace", route.GetNamespace(), "uid", route.GetUID())
					continue
				}

				condition.ObservedGeneration = route.GetGeneration()
				meta.SetStatusCondition(&(routeStatusParents[i].Conditions), condition)
				logger.V(1).Info("adding condition", "condition", condition.Type, "status", condition.Status, "name", route.GetName(), "namespace", route.GetNamespace(), "uid", route.GetUID())
			}
		}

		// Update Route Status
		equalStatus := equality.Semantic.DeepEqual(routeStatusParents, route.Status.Parents)
		if equalStatus {
			logger.V(1).Info("route parent status unchanged, skipping update", "name", route.GetName(), "namespace", route.GetNamespace(), "uid", route.GetUID())
			continue
		}

		route.Status.Parents = routeStatusParents
		obj, err := controller.Destruct(route.HTTPRoute)
		if err != nil {
			logger.Error(err, "unable to destruct route", "name", route.GetName(), "namespace", route.GetNamespace(), "uid", route.GetUID())
			continue
		}

		// Update route status once
		_, err = r.Client.Resource(controller.HTTPRoutesResource).Namespace(route.GetNamespace()).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
		if err != nil {
			logger.Error(err, "unable to update route status", "name", route.GetName(), "namespace", route.GetNamespace(), "uid", route.GetUID())
		}
	}
	return nil
}

func FindRouteParentStatusFunc(route *gatewayapiv1.HTTPRoute, gatewayKey client.ObjectKey, controllerName gatewayapiv1.GatewayController) func(gatewayapiv1.RouteParentStatus) bool {
	return func(p gatewayapiv1.RouteParentStatus) bool {
		return *p.ParentRef.Kind == ("Gateway") &&
			p.ControllerName == controllerName &&
			((p.ParentRef.Namespace == nil && route.GetNamespace() == gatewayKey.Namespace) || string(*p.ParentRef.Namespace) == gatewayKey.Namespace) &&
			string(p.ParentRef.Name) == gatewayKey.Name
	}
}
