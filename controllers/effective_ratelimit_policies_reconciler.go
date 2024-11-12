package controllers

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/client-go/dynamic"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
)

type EffectiveRateLimitPolicy struct {
	Path []machinery.Targetable
	Spec kuadrantv1.RateLimitPolicy
}

type EffectiveRateLimitPolicies map[string]EffectiveRateLimitPolicy

type EffectiveRateLimitPolicyReconciler struct {
	client *dynamic.DynamicClient
}

// EffectiveRateLimitPolicyReconciler subscribe to the same events as auth because they are used together to compose gateway extension resources
func (r *EffectiveRateLimitPolicyReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events:        dataPlaneEffectivePoliciesEventMatchers,
	}
}

func (r *EffectiveRateLimitPolicyReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("EffectiveRateLimitPolicyReconciler")
	logger.V(1).Info("generating effective rate limit policy", "status", "started")
	defer logger.V(1).Info("generating effective rate limit policy", "status", "completed")

	kuadrant := GetKuadrantFromTopology(topology)
	if kuadrant == nil {
		return nil
	}

	effectivePolicies := r.calculateEffectivePolicies(ctx, topology, kuadrant, state)

	state.Store(StateEffectiveRateLimitPolicies, effectivePolicies)

	return nil
}

func (r *EffectiveRateLimitPolicyReconciler) calculateEffectivePolicies(ctx context.Context, topology *machinery.Topology, kuadrant machinery.Object, state *sync.Map) EffectiveRateLimitPolicies {
	logger := controller.LoggerFromContext(ctx).WithName("EffectiveRateLimitPolicyReconciler").WithName("calculateEffectivePolicies")

	targetables := topology.Targetables()
	gatewayClasses := targetables.Children(kuadrant) // assumes only and all valid gateway classes are linked to kuadrant in the topology
	httpRouteRules := targetables.Items(func(o machinery.Object) bool {
		_, ok := o.(*machinery.HTTPRouteRule)
		return ok
	})

	logger.V(1).Info("calculating effective rate limit policies", "httpRouteRules", len(httpRouteRules))

	effectivePolicies := EffectiveRateLimitPolicies{}

	for _, gatewayClass := range gatewayClasses {
		for _, httpRouteRule := range httpRouteRules {
			paths := targetables.Paths(gatewayClass, httpRouteRule) // this may be expensive in clusters with many gateway classes - an alternative is to deep search the topology for httprouterules from each gatewayclass, keeping record of the paths
			for i := range paths {
				if effectivePolicy := kuadrantv1.EffectivePolicyForPath[*kuadrantv1.RateLimitPolicy](paths[i], isRateLimitPolicyAcceptedAndNotDeletedFunc(state)); effectivePolicy != nil {
					pathID := kuadrantv1.PathID(paths[i])
					effectivePolicies[pathID] = EffectiveRateLimitPolicy{
						Path: paths[i],
						Spec: **effectivePolicy,
					}
					if logger.V(1).Enabled() {
						jsonEffectivePolicy, _ := json.Marshal(effectivePolicy)
						pathLocators := lo.Map(paths[i], machinery.MapTargetableToLocatorFunc)
						logger.V(1).Info("effective policy", "kind", kuadrantv1.RateLimitPolicyGroupKind.Kind, "pathID", pathID, "path", pathLocators, "effectivePolicy", string(jsonEffectivePolicy))
					}
				}
			}
		}
	}

	logger.V(1).Info("finished calculating effective rate limit policies", "effectivePolicies", len(effectivePolicies))

	return effectivePolicies
}
