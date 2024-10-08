package controllers

import (
	"context"
	"fmt"
	"sync"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

type limitadorLimitsReconciler struct {
	client *dynamic.DynamicClient
}

func (r *limitadorLimitsReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events:        rateLimitEventMatchers,
	}
}

func (r *limitadorLimitsReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("limitadorLimitsReconciler")

	kuadrant, err := GetKuadrantFromTopology(topology)
	if err != nil {
		logger.Error(err, "failed to get kuadrant from topology")
		return nil
	}

	limitadorObj, found := lo.Find(topology.Objects().Children(kuadrant), func(child machinery.Object) bool {
		return child.GroupVersionKind().GroupKind() == kuadrantv1beta1.LimitadorGroupKind
	})
	if !found {
		logger.Error(ErrMissingLimitador, "failed to get limitador from topology")
		return nil
	}

	limitador := limitadorObj.(*controller.RuntimeObject).Object.(*limitadorv1alpha1.Limitador)

	desiredLimits, err := r.buildLimitadorLimits(ctx, state)
	if err != nil {
		logger.Error(err, "failed to build limitador limits")
		return nil
	}

	if rlptools.Equal(limitador.Spec.Limits, desiredLimits) {
		logger.Info("limitador object is up to date, nothing to do")
		return nil
	}

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

func (r *limitadorLimitsReconciler) buildLimitadorLimits(ctx context.Context, state *sync.Map) ([]limitadorv1alpha1.RateLimit, error) {
	logger := controller.LoggerFromContext(ctx).WithName("limitadorLimitsReconciler").WithName("buildLimitadorLimits")

	effectivePolicies, ok := state.Load(StateEffectiveRateLimitPolicies)
	if !ok {
		return nil, ErrMissingStateEffectiveRateLimitPolicies
	}

	logger.V(1).Info("building limitador limits", "effectivePolicies", len(effectivePolicies.(EffectiveRateLimitPolicies)))

	rateLimitIndex := rlptools.NewRateLimitIndex()

	for pathID, effectivePolicy := range effectivePolicies.(EffectiveRateLimitPolicies) {
		httpRoute, _ := effectivePolicy.Path[3].(*machinery.HTTPRoute) // assumes the path is always [gatewayclass, gateway, listener, httproute, httprouterule]
		limitsNamespace := wasm.LimitsNamespaceFromRoute(httpRoute.HTTPRoute)
		for limitKey, mergeableLimit := range effectivePolicy.Spec.Rules() {
			policy, found := lo.Find(kuadrantv1.PoliciesInPath(effectivePolicy.Path, acceptedRateLimitPolicyFunc(state)), func(p machinery.Policy) bool {
				return p.GetLocator() == mergeableLimit.Source
			})
			if !found { // should never happen
				logger.Error(fmt.Errorf("origin policy %s not found in path %s", mergeableLimit.Source, pathID), "failed to build limitador limit definition")
				continue
			}
			limitIdentifier := wasm.LimitNameToLimitadorIdentifier(k8stypes.NamespacedName{Name: policy.GetName(), Namespace: policy.GetNamespace()}, limitKey)
			limit := mergeableLimit.Spec.(kuadrantv1beta3.Limit)
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
