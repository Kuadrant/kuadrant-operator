package controllers

import (
	"context"
	"errors"
	"reflect"
	"sort"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
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

	t, err := BuildTopology(ctx, r.Client(), gw, (&kuadrantv1beta2.RateLimitPolicy{}).Kind(), &kuadrantv1beta2.RateLimitPolicyList{})
	if err != nil {
		return ctrl.Result{}, err
	}

	policies := t.PoliciesFromGateway(gw)
	unTargetedRoutes := len(t.GetUntargetedRoutes(gw))

	sort.Sort(kuadrantgatewayapi.PolicyByTargetRefKindAndCreationTimeStamp(policies))

	for i := range policies {
		policy := policies[i]
		p := policy.(*kuadrantv1beta2.RateLimitPolicy)
		conditions := p.GetStatus().GetConditions()

		// Skip policies if accepted condition is false
		if meta.IsStatusConditionFalse(policy.GetStatus().GetConditions(), string(gatewayapiv1alpha2.PolicyConditionAccepted)) {
			continue
		}

		// Policy has been accepted
		// Ensure no error on underlying subresource (i.e. Limitador)
		if cond := r.hasErrCondOnSubResource(ctx, logger, p); cond != nil {
			if err := r.setCondition(ctx, logger, p, &conditions, *cond); err != nil {
				return ctrl.Result{}, err
			}
			continue
		}

		if kuadrantgatewayapi.IsTargetRefGateway(p.GetTargetRef()) {
			if p.Spec.Overrides != nil {
				if err := r.setConditionForGWPolicyWithOverrides(ctx, logger, p, conditions, policies, unTargetedRoutes); err != nil {
					return ctrl.Result{}, err
				}
				break
			}
			if err := r.setConditionForGWPolicyWithDefaults(ctx, logger, p, conditions, policies, unTargetedRoutes); err != nil {
				return ctrl.Result{}, err
			}
			continue
		}

		// Route Policy
		if err := r.setCondition(ctx, logger, p, &conditions, *kuadrant.EnforcedCondition(p, nil, true)); err != nil {
			return ctrl.Result{}, err
		}
	}

	logger.Info("Policy status reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyEnforcedStatusReconciler) setConditionForGWPolicyWithOverrides(ctx context.Context, logger logr.Logger, p *kuadrantv1beta2.RateLimitPolicy, conditions []metav1.Condition, policies []kuadrantgatewayapi.Policy, unTargetedRoutes int) error {
	// Only have this policy and no free routes
	if len(policies) == 1 && unTargetedRoutes == 0 {
		if err := r.setCondition(ctx, logger, p, &conditions, *kuadrant.EnforcedCondition(p, kuadrant.NewErrUnknown(p.Kind(), errors.New("no free routes to enforce policy")), true)); err != nil {
			return err
		}
	}

	// Has free routes or is overriding policies
	// Set Enforced true condition for this GW policy
	if err := r.setCondition(ctx, logger, p, &conditions, *kuadrant.EnforcedCondition(p, nil, true)); err != nil {
		return err
	}

	// Update the rest of the policies as overridden
	affectedPolices := utils.Filter(policies, func(ap kuadrantgatewayapi.Policy) bool {
		return p != ap && ap.GetDeletionTimestamp() == nil
	})

	for i := range affectedPolices {
		af := affectedPolices[i]
		afp := af.(*kuadrantv1beta2.RateLimitPolicy)
		afConditions := afp.GetStatus().GetConditions()

		if err := r.setCondition(ctx, logger, afp, &afConditions, *kuadrant.EnforcedCondition(afp, kuadrant.NewErrOverridden(afp.Kind(), []client.ObjectKey{client.ObjectKeyFromObject(p)}), true)); err != nil {
			return err
		}
	}

	return nil
}

func (r *RateLimitPolicyEnforcedStatusReconciler) setConditionForGWPolicyWithDefaults(ctx context.Context, logger logr.Logger, p *kuadrantv1beta2.RateLimitPolicy, conditions []metav1.Condition, policies []kuadrantgatewayapi.Policy, unTargetedRoutes int) error {
	// GW Policy defaults is defined
	// Only have this policy or no free routes -> nothing to enforce policy
	if len(policies) == 1 && unTargetedRoutes == 0 {
		if err := r.setCondition(ctx, logger, p, &conditions, *kuadrant.EnforcedCondition(p, kuadrant.NewErrUnknown(p.Kind(), errors.New("no free routes to enforce policy")), true)); err != nil {
			return err
		}
	} else if len(policies) > 1 && unTargetedRoutes == 0 {
		// GW policy defaults are overridden by child policies
		affectedPolices := utils.Filter(policies, func(ap kuadrantgatewayapi.Policy) bool {
			return p != ap && ap.GetDeletionTimestamp() == nil
		})

		if err := r.setCondition(ctx, logger, p, &conditions, *kuadrant.EnforcedCondition(p, kuadrant.NewErrOverridden(p.Kind(), utils.Map(affectedPolices, func(ap kuadrantgatewayapi.Policy) client.ObjectKey {
			return client.ObjectKeyFromObject(ap)
		})), true)); err != nil {
			return err
		}
	} else {
		// Is enforcing default policy on a free route
		if err := r.setCondition(ctx, logger, p, &conditions, *kuadrant.EnforcedCondition(p, nil, true)); err != nil {
			return err
		}
	}

	return nil
}

func (r *RateLimitPolicyEnforcedStatusReconciler) hasErrCondOnSubResource(ctx context.Context, logger logr.Logger, p *kuadrantv1beta2.RateLimitPolicy) *metav1.Condition {
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

func (r *RateLimitPolicyEnforcedStatusReconciler) setCondition(ctx context.Context, logger logr.Logger, p *kuadrantv1beta2.RateLimitPolicy, conditions *[]metav1.Condition, cond metav1.Condition) error {
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
