package controllers

/*
Copyright 2021 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/fieldindexers"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const PolicyAffectedConditionPattern = "kuadrant.io/%sAffected" // Policy kinds are expected to be named XPolicy

// TargetStatusReconciler reconciles a the status stanzas of objects targeted by Kuadrant policies
type TargetStatusReconciler struct {
	*reconcilers.BaseReconciler
}

func (r *TargetStatusReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("gateway", req.NamespacedName, "request id", uuid.NewString())
	logger.Info("Reconciling target status")
	ctx := logr.NewContext(eventCtx, logger)

	gw := &gatewayapiv1.Gateway{}
	if err := r.Client().Get(ctx, req.NamespacedName, gw); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no gateway found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get gateway")
		return ctrl.Result{}, err
	}

	if err := r.reconcileResources(ctx, gw); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Target status reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *TargetStatusReconciler) reconcileResources(ctx context.Context, gw *gatewayapiv1.Gateway) error {
	policyKinds := map[kuadrantgatewayapi.Policy]client.ObjectList{
		&kuadrantv1beta2.AuthPolicy{TypeMeta: ctrl.TypeMeta{Kind: "AuthPolicy"}}:           &kuadrantv1beta2.AuthPolicyList{},
		&kuadrantv1alpha1.DNSPolicy{TypeMeta: ctrl.TypeMeta{Kind: "DNSPolicy"}}:            &kuadrantv1alpha1.DNSPolicyList{},
		&kuadrantv1alpha1.TLSPolicy{TypeMeta: ctrl.TypeMeta{Kind: "TLSPolicy"}}:            &kuadrantv1alpha1.TLSPolicyList{},
		&kuadrantv1beta2.RateLimitPolicy{TypeMeta: ctrl.TypeMeta{Kind: "RateLimitPolicy"}}: &kuadrantv1beta2.RateLimitPolicyList{},
	}

	var errs error

	for policy, policyListKind := range policyKinds {
		err := r.reconcileResourcesForPolicyKind(ctx, gw, policy, policyListKind)
		if err != nil {
			errs = errors.Join(errs, err)
		}
	}

	return errs
}

func (r *TargetStatusReconciler) reconcileResourcesForPolicyKind(parentCtx context.Context, gw *gatewayapiv1.Gateway, policy kuadrantgatewayapi.Policy, listPolicyKind client.ObjectList) error {
	logger, err := logr.FromContext(parentCtx)
	if err != nil {
		return err
	}
	gatewayKey := client.ObjectKeyFromObject(gw)
	policyKind := policy.GetObjectKind().GroupVersionKind().Kind
	ctx := logr.NewContext(parentCtx, logger.WithValues("kind", policyKind))

	topology, err := r.buildTopology(ctx, gw, policyKind, listPolicyKind)
	if err != nil {
		return err
	}
	policies := topology.PoliciesFromGateway(gw)

	sort.Sort(kuadrantgatewayapi.PolicyByTargetRefKindAndAcceptedStatus(policies))

	var errs error

	// if no policies of a kind affecting the gateway → remove condition from the gateway and routes
	gatewayPolicyExists := len(policies) > 0 && utils.Index(policies, func(p kuadrantgatewayapi.Policy) bool { return kuadrantgatewayapi.IsTargetRefGateway(p.GetTargetRef()) }) >= 0
	if !gatewayPolicyExists {
		// remove the condition from the gateway
		conditionType := PolicyAffectedConditionType(policyKind)
		if c := meta.FindStatusCondition(gw.Status.Conditions, conditionType); c == nil {
			logger.V(1).Info("condition already absent, skipping", "condition", conditionType)
		} else {
			meta.RemoveStatusCondition(&gw.Status.Conditions, conditionType)
			logger.V(1).Info("removing condition from gateway", "condition", conditionType)
			if err := r.Client().Status().Update(ctx, gw); err != nil {
				errs = errors.Join(errs, err)
			}
		}

		// remove the condition from the routes not targeted by any policy
		if policy.PolicyClass() == kuadrantgatewayapi.InheritedPolicy {
			if err := r.updateInheritedGatewayCondition(ctx, topology.GetUntargetedRoutes(gw), metav1.Condition{Type: conditionType}, gatewayKey, r.removeRouteCondition); err != nil {
				errs = errors.Join(errs, err)
			}
		}
	}

	reconciledTargets := make(map[string]struct{})

	reconcileFunc := func(policy kuadrantgatewayapi.Policy) error {
		targetRefKey := targetRefKey(policy)

		// update status of targeted route
		if route := topology.GetPolicyHTTPRoute(policy); route != nil {
			if _, updated := reconciledTargets[targetRefKey]; updated { // do not update the same route twice
				return nil
			}
			if err := r.addRouteCondition(ctx, route, buildPolicyAffectedCondition(policy), gatewayKey, kuadrant.ControllerName); err != nil {
				errs = errors.Join(errs, err)
			}
			reconciledTargets[targetRefKey] = struct{}{}
			return nil
		}

		// update status of targeted gateway and routes not targeted by any policy
		if _, updated := reconciledTargets[targetRefKey]; updated { // do not update the same gateway twice
			return nil
		}
		condition := buildPolicyAffectedCondition(policy)
		if c := meta.FindStatusCondition(gw.Status.Conditions, condition.Type); c != nil && c.Status == condition.Status && c.Reason == condition.Reason && c.Message == condition.Message && c.ObservedGeneration == gw.GetGeneration() {
			logger.V(1).Info("condition already up-to-date, skipping", "condition", condition.Type, "status", condition.Status)
		} else {
			gwCondition := condition.DeepCopy()
			gwCondition.ObservedGeneration = gw.GetGeneration()
			meta.SetStatusCondition(&gw.Status.Conditions, *gwCondition)
			logger.V(1).Info("adding condition to gateway", "condition", condition.Type, "status", condition.Status)
			if err := r.Client().Status().Update(ctx, gw); err != nil {
				return err
			}
		}
		// update status of all untargeted routes accepted by the gateway
		if policy.PolicyClass() == kuadrantgatewayapi.InheritedPolicy {
			if err := r.updateInheritedGatewayCondition(ctx, topology.GetUntargetedRoutes(gw), condition, gatewayKey, r.addRouteCondition); err != nil {
				return err
			}
		}
		reconciledTargets[targetRefKey] = struct{}{}

		return nil
	}

	// update for policies with status condition Accepted: true
	for i := range utils.Filter(policies, kuadrantgatewayapi.IsPolicyAccepted) {
		policy := policies[i]
		errs = errors.Join(errs, reconcileFunc(policy))
	}

	// update for policies with status condition Accepted: false
	for i := range utils.Filter(policies, kuadrantgatewayapi.IsNotPolicyAccepted) {
		policy := policies[i]
		errs = errors.Join(errs, reconcileFunc(policy))
	}

	return errs
}

func (r *TargetStatusReconciler) buildTopology(ctx context.Context, gw *gatewayapiv1.Gateway, policyKind string, listPolicyKind client.ObjectList) (*kuadrantgatewayapi.TopologyIndexes, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	routeList := &gatewayapiv1.HTTPRouteList{}
	// Get all the routes having the gateway as parent
	err = r.Client().List(ctx, routeList, client.MatchingFields{fieldindexers.HTTPRouteGatewayParentField: client.ObjectKeyFromObject(gw).String()})
	logger.V(1).Info("list routes by gateway", "#routes", len(routeList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	policies, err := r.getPoliciesByKind(ctx, policyKind, listPolicyKind)
	if err != nil {
		return nil, err
	}

	t, err := kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways([]*gatewayapiv1.Gateway{gw}),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
		kuadrantgatewayapi.WithPolicies(policies),
		kuadrantgatewayapi.WithLogger(logger),
	)
	if err != nil {
		return nil, err
	}

	return kuadrantgatewayapi.NewTopologyIndexes(t), nil
}

func (r *TargetStatusReconciler) getPoliciesByKind(ctx context.Context, policyKind string, listKind client.ObjectList) ([]kuadrantgatewayapi.Policy, error) {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithValues("kind", policyKind)

	// Get all policies of the given kind
	err := r.Client().List(ctx, listKind)
	policyList, ok := listKind.(kuadrant.PolicyList)
	if !ok {
		return nil, fmt.Errorf("%T is not a kuadrant.PolicyList", listKind)
	}
	logger.V(1).Info("list policies by kind", "#policies", len(policyList.GetItems()), "err", err)
	if err != nil {
		return nil, err
	}

	return utils.Map(policyList.GetItems(), func(p kuadrant.Policy) kuadrantgatewayapi.Policy { return p }), nil
}

func (r *TargetStatusReconciler) updateInheritedGatewayCondition(ctx context.Context, routes []*gatewayapiv1.HTTPRoute, condition metav1.Condition, gatewayKey client.ObjectKey, update updateRouteConditionFunc) error {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithValues("condition", condition.Type, "status", condition.Status)

	logger.V(1).Info("update inherited gateway condition", "#routes", len(routes))

	var errs error

	for i := range routes {
		route := routes[i]
		if err := update(ctx, route, condition, gatewayKey, kuadrant.ControllerName); err != nil {
			errs = errors.Join(errs, err)
		}
	}

	return errs
}

type updateRouteConditionFunc func(ctx context.Context, route *gatewayapiv1.HTTPRoute, condition metav1.Condition, gatewayKey client.ObjectKey, controllerName gatewayapiv1.GatewayController) error

func (r *TargetStatusReconciler) addRouteCondition(ctx context.Context, route *gatewayapiv1.HTTPRoute, condition metav1.Condition, gatewayKey client.ObjectKey, controllerName gatewayapiv1.GatewayController) error {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithValues("route", client.ObjectKeyFromObject(route), "condition", condition.Type, "status", condition.Status)

	i := utils.Index(route.Status.RouteStatus.Parents, FindRouteParentStatusFunc(route, gatewayKey, controllerName))
	if i < 0 {
		logger.V(1).Info("cannot find parent status, creating new one")
		route.Status.RouteStatus.Parents = append(route.Status.RouteStatus.Parents, gatewayapiv1.RouteParentStatus{
			ControllerName: controllerName,
			ParentRef: gatewayapiv1.ParentReference{
				Kind:      ptr.To(gatewayapiv1.Kind("Gateway")),
				Name:      gatewayapiv1.ObjectName(gatewayKey.Name),
				Namespace: ptr.To(gatewayapiv1.Namespace(gatewayKey.Namespace)),
			},
			Conditions: []metav1.Condition{},
		})
		i = utils.Index(route.Status.RouteStatus.Parents, FindRouteParentStatusFunc(route, gatewayKey, controllerName))
	}

	if c := meta.FindStatusCondition(route.Status.RouteStatus.Parents[i].Conditions, condition.Type); c != nil && c.Status == condition.Status && c.Reason == condition.Reason && c.Message == condition.Message && c.ObservedGeneration == route.GetGeneration() {
		logger.V(1).Info("condition already up-to-date, skipping")
		return nil
	}

	routeCondition := condition.DeepCopy()
	routeCondition.ObservedGeneration = route.GetGeneration()
	meta.SetStatusCondition(&(route.Status.RouteStatus.Parents[i].Conditions), *routeCondition) // Istio will merge the conditions from Kuadrant's parent status into its own parent status. See https://github.com/istio/istio/issues/50484
	logger.V(1).Info("adding condition to route")
	return r.Client().Status().Update(ctx, route)
}

func (r *TargetStatusReconciler) removeRouteCondition(ctx context.Context, route *gatewayapiv1.HTTPRoute, condition metav1.Condition, gatewayKey client.ObjectKey, controllerName gatewayapiv1.GatewayController) error {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithValues("route", client.ObjectKeyFromObject(route), "condition", condition.Type, "status", condition.Status)

	i := utils.Index(route.Status.RouteStatus.Parents, FindRouteParentStatusFunc(route, gatewayKey, controllerName))
	if i < 0 {
		logger.V(1).Info("cannot find parent status, skipping")
		return nil
	}

	if c := meta.FindStatusCondition(route.Status.RouteStatus.Parents[i].Conditions, condition.Type); c == nil {
		logger.V(1).Info("condition already absent, skipping")
		return nil
	}

	logger.V(1).Info("removing condition from route")
	meta.RemoveStatusCondition(&(route.Status.RouteStatus.Parents[i].Conditions), condition.Type)
	if len(route.Status.RouteStatus.Parents[i].Conditions) == 0 {
		route.Status.RouteStatus.Parents = append(route.Status.RouteStatus.Parents[:i], route.Status.RouteStatus.Parents[i+1:]...)
	}
	return r.Client().Status().Update(ctx, route)
}

func FindRouteParentStatusFunc(route *gatewayapiv1.HTTPRoute, gatewayKey client.ObjectKey, controllerName gatewayapiv1.GatewayController) func(gatewayapiv1.RouteParentStatus) bool {
	return func(p gatewayapiv1.RouteParentStatus) bool {
		return *p.ParentRef.Kind == ("Gateway") &&
			p.ControllerName == controllerName &&
			((p.ParentRef.Namespace == nil && route.GetNamespace() == gatewayKey.Namespace) || string(*p.ParentRef.Namespace) == gatewayKey.Namespace) &&
			string(p.ParentRef.Name) == gatewayKey.Name
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TargetStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("TargetStatus controller disabled. GatewayAPI was not found")
		return nil
	}

	httpRouteToParentGatewaysEventMapper := mappers.NewHTTPRouteToParentGatewaysEventMapper(
		mappers.WithLogger(r.Logger().WithName("httpRouteToParentGatewaysEventMapper")),
	)

	policyToParentGatewaysEventMapper := mappers.NewPolicyToParentGatewaysEventMapper(
		mappers.WithLogger(r.Logger().WithName("policyToParentGatewaysEventMapper")),
		mappers.WithClient(r.Client()),
	)

	policyStatusChangedPredicate := predicate.Funcs{
		UpdateFunc: func(ev event.UpdateEvent) bool {
			oldPolicy, ok := ev.ObjectOld.(kuadrantgatewayapi.Policy)
			if !ok {
				return false
			}
			newPolicy, ok := ev.ObjectNew.(kuadrantgatewayapi.Policy)
			if !ok {
				return false
			}
			oldStatus := oldPolicy.GetStatus()
			newStatus := newPolicy.GetStatus()
			return !reflect.DeepEqual(oldStatus, newStatus)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayapiv1.Gateway{}).
		Watches(
			&gatewayapiv1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(httpRouteToParentGatewaysEventMapper.Map),
		).
		Watches(
			&kuadrantv1beta2.AuthPolicy{},
			handler.EnqueueRequestsFromMapFunc(policyToParentGatewaysEventMapper.Map),
			builder.WithPredicates(policyStatusChangedPredicate),
		).
		Watches(
			&kuadrantv1alpha1.DNSPolicy{},
			handler.EnqueueRequestsFromMapFunc(policyToParentGatewaysEventMapper.Map),
			builder.WithPredicates(policyStatusChangedPredicate),
		).
		Watches(
			&kuadrantv1beta2.RateLimitPolicy{},
			handler.EnqueueRequestsFromMapFunc(policyToParentGatewaysEventMapper.Map),
			builder.WithPredicates(policyStatusChangedPredicate),
		).
		Watches(
			&kuadrantv1alpha1.TLSPolicy{},
			handler.EnqueueRequestsFromMapFunc(policyToParentGatewaysEventMapper.Map),
			builder.WithPredicates(policyStatusChangedPredicate),
		).
		Complete(r)
}

func buildPolicyAffectedCondition(policy kuadrantgatewayapi.Policy) metav1.Condition {
	policyKind := policy.GetObjectKind().GroupVersionKind().Kind

	condition := metav1.Condition{
		Type:    PolicyAffectedConditionType(policyKind),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayapiv1alpha2.PolicyReasonAccepted),
		Message: fmt.Sprintf("Object affected by %s %s", policyKind, client.ObjectKeyFromObject(policy)),
	}

	if c := meta.FindStatusCondition(policy.GetStatus().GetConditions(), string(gatewayapiv1alpha2.PolicyConditionAccepted)); c == nil || c.Status != metav1.ConditionTrue { // should we aim for 'Enforced' instead?
		condition.Status = metav1.ConditionFalse
		condition.Message = fmt.Sprintf("Object unaffected by %s %s, policy is not accepted", policyKind, client.ObjectKeyFromObject(policy))
		condition.Reason = PolicyReasonUnknown
		if c != nil {
			condition.Reason = c.Reason
		}
	}

	return condition
}

func PolicyAffectedConditionType(policyKind string) string {
	return fmt.Sprintf(PolicyAffectedConditionPattern, policyKind)
}

func targetRefKey(policy kuadrantgatewayapi.Policy) string {
	targetRef := policy.GetTargetRef()
	return fmt.Sprintf("%s.%s/%s/%s", targetRef.Group, targetRef.Kind, ptr.Deref(targetRef.Namespace, gatewayapiv1.Namespace(policy.GetNamespace())), targetRef.Name)
}
