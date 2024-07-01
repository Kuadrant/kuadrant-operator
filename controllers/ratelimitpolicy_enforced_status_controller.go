package controllers

import (
	"context"
	"errors"
	"reflect"
	"sort"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/samber/lo"
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

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/fieldindexers"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

type RateLimitPolicyEnforcedStatusReconciler struct {
	*reconcilers.BaseReconciler
}

func (r *RateLimitPolicyEnforcedStatusReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("gateway", req.NamespacedName, "request id", uuid.NewString())
	logger.Info("Reconciling policy status")
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

	topology, err := r.buildTopology(ctx, gw)
	if err != nil {
		return ctrl.Result{}, err
	}

	indexes := kuadrantgatewayapi.NewTopologyIndexes(topology)
	policies := indexes.PoliciesFromGateway(gw)
	numRoutes := len(topology.Routes())
	numUntargetedRoutes := len(indexes.GetUntargetedRoutes(gw))

	sort.Sort(kuadrantgatewayapi.PolicyByTargetRefKindAndCreationTimeStamp(policies))

	// for each policy:
	//   if the policy is a gateway policy:
	//     and no route exists (numRoutes == 0) → set the Enforced condition of the gateway policy to 'false' (unknown)
	//     and the gateway contains routes (numRoutes > 0)
	//       and the gateway policy contains overrides → set the Enforced condition of the gateway policy to 'true'
	//       and the gateway policy contains defaults:
	//         and no routes have an attached policy (numUntargetedRoutes == numRoutes) → set the Enforced condition of the gateway policy to 'true'
	//         and some routes have attached policy (numUntargetedRoutes < numRoutes && numUntargetedRoutes > 1) → set the Enforced condition of the gateway policy to 'true' (partially enforced)
	//         and all routes have attached policy (numUntargetedRoutes == 0) → set the Enforced condition of the gateway policy to 'false' (overridden)
	//   if the policy is a route policy:
	//     and the route has no gateway parent (numGatewayParents == 0) → set the Enforced condition of the route policy to 'false' (unknown)
	//     and the route has gateway parents (numGatewayParents > 0)
	//       and all gateway parents of the route have gateway policies with overrides (numGatewayParentsWithOverrides == numGatewayParents) → set the Enforced condition of the route policy to 'false' (overridden)
	//       and some gateway parents of the route have gateway policies with overrides (numGatewayParentsWithOverrides < numGatewayParents && numGatewayParentsWithOverrides > 1) → set the Enforced condition of the route policy to 'true' (partially enforced)
	//       and no gateway parent of the route has gateway policies with overrides (numGatewayParentsWithOverrides == 0) → set the Enforced condition of the route policy to 'true'

	for i := range policies {
		policy := policies[i]
		rlpKey := client.ObjectKeyFromObject(policy)
		rlp := policy.(*kuadrantv1beta2.RateLimitPolicy)
		conditions := rlp.GetStatus().GetConditions()

		// skip policy if accepted condition is false
		if meta.IsStatusConditionFalse(policy.GetStatus().GetConditions(), string(gatewayapiv1alpha2.PolicyConditionAccepted)) {
			continue
		}

		// ensure no error on underlying subresource (i.e. Limitador)
		if condition := r.hasErrCondOnSubResource(ctx, rlp); condition != nil {
			if err := r.setCondition(ctx, rlp, &conditions, *condition); err != nil {
				return ctrl.Result{}, err
			}
			continue
		}

		var condition *metav1.Condition

		if kuadrantgatewayapi.IsTargetRefGateway(rlp.GetTargetRef()) { // gateway policy
			if numRoutes == 0 {
				condition = kuadrant.EnforcedCondition(rlp, kuadrant.NewErrUnknown(rlp.Kind(), errors.New("no free routes to enforce policy")), true) // unknown
			} else {
				if rlp.Spec.Overrides != nil {
					condition = kuadrant.EnforcedCondition(rlp, nil, true) // fully enforced
				} else {
					if numUntargetedRoutes == numRoutes {
						condition = kuadrant.EnforcedCondition(rlp, nil, true) // fully enforced
					} else if numUntargetedRoutes > 0 {
						condition = kuadrant.EnforcedCondition(rlp, nil, false) // partially enforced
					} else {
						otherPolicies := lo.FilterMap(policies, func(p kuadrantgatewayapi.Policy, _ int) (client.ObjectKey, bool) {
							key := client.ObjectKeyFromObject(p)
							return key, key != rlpKey
						})
						condition = kuadrant.EnforcedCondition(rlp, kuadrant.NewErrOverridden(rlp.Kind(), otherPolicies), true) // overridden
					}
				}
			}
		} else { // route policy
			route := indexes.GetPolicyHTTPRoute(rlp)
			gatewayParents := lo.FilterMap(kuadrantgatewayapi.GetRouteAcceptedGatewayParentKeys(route), func(parentKey client.ObjectKey, _ int) (*gatewayapiv1.Gateway, bool) {
				g, found := utils.Find(topology.Gateways(), func(g kuadrantgatewayapi.GatewayNode) bool { return client.ObjectKeyFromObject(g.Gateway) == parentKey })
				if !found {
					return nil, false
				}
				return g.Gateway, true
			})
			numGatewayParents := len(gatewayParents)
			if numGatewayParents == 0 {
				condition = kuadrant.EnforcedCondition(rlp, kuadrant.NewErrUnknown(rlp.Kind(), errors.New("the targeted route has not been accepted by any gateway parent")), true) // unknown
			} else {
				var gatewayParentOverridePolicies []kuadrantgatewayapi.Policy
				gatewayParentsWithOverrides := utils.Filter(gatewayParents, func(gatewayParent *gatewayapiv1.Gateway) bool {
					_, found := utils.Find(indexes.PoliciesFromGateway(gatewayParent), func(p kuadrantgatewayapi.Policy) bool {
						rlp := p.(*kuadrantv1beta2.RateLimitPolicy)
						if kuadrantgatewayapi.IsTargetRefGateway(p.GetTargetRef()) && rlp != nil && rlp.Spec.Overrides != nil {
							gatewayParentOverridePolicies = append(gatewayParentOverridePolicies, p)
							return true
						}
						return false
					})
					return found
				})
				numGatewayParentsWithOverrides := len(gatewayParentsWithOverrides)
				if numGatewayParentsWithOverrides == numGatewayParents {
					sort.Sort(kuadrantgatewayapi.PolicyByTargetRefKindAndCreationTimeStamp(gatewayParentOverridePolicies))
					condition = kuadrant.EnforcedCondition(rlp, kuadrant.NewErrOverridden(rlp.Kind(), utils.Map(gatewayParentOverridePolicies, func(p kuadrantgatewayapi.Policy) client.ObjectKey { return client.ObjectKeyFromObject(p) })), true) // overridden
				} else if numGatewayParentsWithOverrides > 0 {
					condition = kuadrant.EnforcedCondition(rlp, nil, false) // partially enforced
				} else {
					condition = kuadrant.EnforcedCondition(rlp, nil, true) // fully enforced
				}
			}
		}

		if err := r.setCondition(ctx, rlp, &conditions, *condition); err != nil {
			return ctrl.Result{}, err
		}
	}

	logger.Info("Policy status reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyEnforcedStatusReconciler) buildTopology(ctx context.Context, gw *gatewayapiv1.Gateway) (*kuadrantgatewayapi.Topology, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	gatewayList := &gatewayapiv1.GatewayList{}
	err = r.Client().List(ctx, gatewayList)
	logger.V(1).Info("list gateways", "#gateways", len(gatewayList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	routeList := &gatewayapiv1.HTTPRouteList{}
	// Get all the routes having the gateway as parent
	err = r.Client().List(
		ctx,
		routeList,
		client.MatchingFields{
			fieldindexers.HTTPRouteGatewayParentField: client.ObjectKeyFromObject(gw).String(),
		})
	logger.V(1).Info("list routes by gateway", "#routes", len(routeList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	policyList := &kuadrantv1beta2.RateLimitPolicyList{}
	err = r.Client().List(ctx, policyList)
	logger.V(1).Info("list rate limit policies", "#policies", len(policyList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	return kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways(utils.Map(gatewayList.Items, ptr.To[gatewayapiv1.Gateway])),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
		kuadrantgatewayapi.WithPolicies(utils.Map(policyList.Items, func(p kuadrantv1beta2.RateLimitPolicy) kuadrantgatewayapi.Policy { return &p })),
		kuadrantgatewayapi.WithLogger(logger),
	)
}

func (r *RateLimitPolicyEnforcedStatusReconciler) hasErrCondOnSubResource(ctx context.Context, p *kuadrantv1beta2.RateLimitPolicy) *metav1.Condition {
	logger, err := logr.FromContext(ctx)
	logger.WithName("hasErrCondOnSubResource")
	if err != nil {
		logger = r.Logger()
	}

	limitador, err := GetLimitador(ctx, r.Client(), p)
	if err != nil {
		logger.V(1).Error(err, "failed to get limitador")
		return kuadrant.EnforcedCondition(p, kuadrant.NewErrUnknown(p.Kind(), err), false)
	}
	if meta.IsStatusConditionFalse(limitador.Status.Conditions, "Ready") {
		logger.V(1).Info("Limitador is not ready")
		return kuadrant.EnforcedCondition(p, kuadrant.NewErrUnknown(p.Kind(), errors.New("limitador is not ready")), false)
	}

	logger.V(1).Info("limitador is ready and enforcing limits")
	return nil
}

func (r *RateLimitPolicyEnforcedStatusReconciler) setCondition(ctx context.Context, p *kuadrantv1beta2.RateLimitPolicy, conditions *[]metav1.Condition, cond metav1.Condition) error {
	logger, err := logr.FromContext(ctx)
	logger.WithName("setCondition")
	if err != nil {
		logger = r.Logger()
	}

	idx := utils.Index(*conditions, func(c metav1.Condition) bool {
		return c.Type == cond.Type && c.Status == cond.Status && c.Reason == cond.Reason && c.Message == cond.Message
	})
	if idx == -1 {
		meta.SetStatusCondition(conditions, cond)
		p.Status.Conditions = *conditions
		if err := r.Client().Status().Update(ctx, p); err != nil {
			logger.Error(err, "failed to update policy status")
			return err
		}
		return nil
	}

	logger.V(1).Info("skipping policy enforced condition status update - already up to date")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitPolicyEnforcedStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("RateLimitPolicyEnforcedStatus controller disabled. GatewayAPI was not found")
		return nil
	}

	httpRouteToParentGatewaysEventMapper := mappers.NewHTTPRouteToParentGatewaysEventMapper(
		mappers.WithLogger(r.Logger().WithName("httpRouteToParentGatewaysEventMapper")),
	)

	policyToParentGatewaysEventMapper := mappers.NewPolicyToParentGatewaysEventMapper(
		mappers.WithLogger(r.Logger().WithName("policyToParentGatewaysEventMapper")),
		mappers.WithClient(r.Client()),
	)

	limitadorStatusToParentGatewayEventMapper := limitadorStatusRLPGatewayEventHandler{
		Client: r.Client(),
		Logger: r.Logger().WithName("limitadorStatusToRLPsEventHandler"),
	}

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
			&kuadrantv1beta2.RateLimitPolicy{},
			handler.EnqueueRequestsFromMapFunc(policyToParentGatewaysEventMapper.Map),
			builder.WithPredicates(policyStatusChangedPredicate),
		).
		Watches(&limitadorv1alpha1.Limitador{}, limitadorStatusToParentGatewayEventMapper).
		Complete(r)
}
