package controllers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/ratelimit"
)

type LimitadorLimitsReconciler struct {
	client *dynamic.DynamicClient
}

// LimitadorLimitsReconciler reconciles to events with impact to change the state of the Limitador custom resources regarding the definitions for the effective rate limit policies
func (r *LimitadorLimitsReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1beta3.RateLimitPolicyGroupKind},
			{Kind: &kuadrantv1beta1.LimitadorGroupKind},
		},
	}
}

func (r *LimitadorLimitsReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("LimitadorLimitsReconciler")

	limitador, err := GetLimitadorFromTopology(topology)
	if err != nil {
		if errors.Is(err, ErrMissingKuadrant) || errors.Is(err, ErrMissingLimitador) {
			logger.V(1).Info(err.Error())
			return nil
		}
		return err
	}

	desiredLimits, err := r.buildLimitadorLimits(ctx, state)
	if err != nil {
		logger.Error(err, "failed to build limitador limits")
		return nil
	}

	if ratelimit.LimitadorRateLimits(limitador.Spec.Limits).EqualTo(desiredLimits) {
		logger.V(1).Info("limitador object is up to date, nothing to do")
		return nil
	}

	state.Store(StateLimitadorLimitsModified, true)

	limitador.Spec.Limits = desiredLimits

	obj, err := controller.Destruct(limitador)
	if err != nil {
		return err // should never happen
	}

	logger.V(1).Info("updating limitador object", "limitador", obj.Object)

	if _, err := r.client.Resource(kuadrantv1beta1.LimitadorsResource).Namespace(limitador.GetNamespace()).Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		logger.Error(err, "failed to update limitador object")
		// TODO: handle error
	}

	logger.V(1).Info("finished updating limitador object", "limitador", (k8stypes.NamespacedName{Name: limitador.GetName(), Namespace: limitador.GetNamespace()}).String())

	return nil
}

func (r *LimitadorLimitsReconciler) buildLimitadorLimits(ctx context.Context, state *sync.Map) ([]limitadorv1alpha1.RateLimit, error) {
	logger := controller.LoggerFromContext(ctx).WithName("LimitadorLimitsReconciler").WithName("buildLimitadorLimits")

	effectivePolicies, ok := state.Load(StateEffectiveRateLimitPolicies)
	if !ok {
		return nil, ErrMissingStateEffectiveRateLimitPolicies
	}
	effectivePoliciesMap := effectivePolicies.(EffectiveRateLimitPolicies)

	logger.V(1).Info("building limitador limits", "effectivePolicies", len(effectivePoliciesMap))

	rateLimitIndex := ratelimit.NewIndex()

	for pathID, effectivePolicy := range effectivePoliciesMap {
		_, _, _, httpRoute, _, _ := common.ObjectsInRequestPath(effectivePolicy.Path)
		limitsNamespace := LimitsNamespaceFromRoute(httpRoute.HTTPRoute)
		for limitKey, mergeableLimit := range effectivePolicy.Spec.Rules() {
			policy, found := lo.Find(kuadrantv1.PoliciesInPath(effectivePolicy.Path, isRateLimitPolicyAcceptedAndNotDeletedFunc(state)), func(p machinery.Policy) bool {
				return p.GetLocator() == mergeableLimit.GetSource()
			})
			if !found { // should never happen
				logger.Error(fmt.Errorf("origin policy %s not found in path %s", mergeableLimit.GetSource(), pathID), "failed to build limitador limit definition")
				continue
			}
			limitIdentifier := LimitNameToLimitadorIdentifier(k8stypes.NamespacedName{Name: policy.GetName(), Namespace: policy.GetNamespace()}, limitKey)
			limit := mergeableLimit.GetSpec().(*kuadrantv1beta3.Limit)
			rateLimits := lo.Map(limit.Rates, func(rate kuadrantv1beta3.Rate, _ int) limitadorv1alpha1.RateLimit {
				maxValue, seconds := rate.ToSeconds()
				return limitadorv1alpha1.RateLimit{
					Namespace:  limitsNamespace,
					MaxValue:   maxValue,
					Seconds:    seconds,
					Conditions: []string{fmt.Sprintf("%s == \"1\"", limitIdentifier)},
					Variables:  utils.GetEmptySliceIfNil(limit.CountersAsStringList()),
				}
			})
			rateLimitIndex.Set(fmt.Sprintf("%s/%s", limitsNamespace, limitIdentifier), rateLimits)
		}
	}

	logger.V(1).Info("finished building limitador limits", "limits", rateLimitIndex.Len())

	return rateLimitIndex.ToRateLimits(), nil
}
