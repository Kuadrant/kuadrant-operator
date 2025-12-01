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

type EffectiveAuthPolicy struct {
	Path           []machinery.Targetable
	Spec           kuadrantv1.AuthPolicy
	SourcePolicies []string
}

type EffectiveAuthPolicies map[string]EffectiveAuthPolicy

type EffectiveAuthPolicyReconciler struct {
	client *dynamic.DynamicClient
}

// EffectiveAuthPolicyReconciler subscribe to the same events as rate limit because they are used together to compose gateway extension resources
func (r *EffectiveAuthPolicyReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events:        dataPlaneEffectivePoliciesEventMatchers,
	}
}

func (r *EffectiveAuthPolicyReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("EffectiveAuthPolicyReconciler").WithValues("context", ctx)
	logger.V(1).Info("generate effective auth policy", "status", "started")
	defer logger.V(1).Info("generate effective auth policy", "status", "completed")

	kuadrant := GetKuadrantFromTopology(topology)
	if kuadrant == nil {
		return nil
	}

	effectivePolicies := CalculateEffectiveAuthPolicies(ctx, topology, kuadrant, state)

	state.Store(StateEffectiveAuthPolicies, effectivePolicies)

	return nil
}

func CalculateEffectiveAuthPolicies(ctx context.Context, topology *machinery.Topology, kuadrant machinery.Object, state *sync.Map) EffectiveAuthPolicies {
	logger := controller.LoggerFromContext(ctx).WithName("calculateEffectivePolicies").WithValues("context", ctx)

	targetables := topology.Targetables()
	gatewayClasses := targetables.Children(kuadrant) // assumes only and all valid gateway classes are linked to kuadrant in the topology
	httpRouteRules := targetables.Items(func(o machinery.Object) bool {
		_, ok := o.(*machinery.HTTPRouteRule)
		return ok
	})

	logger.V(1).Info("calculating effective auth policies", "httpRouteRules", len(httpRouteRules))

	effectivePolicies := EffectiveAuthPolicies{}

	for _, gatewayClass := range gatewayClasses {
		for _, httpRouteRule := range httpRouteRules {
			paths := targetables.Paths(gatewayClass, httpRouteRule) // this may be expensive in clusters with many gateway classes - an alternative is to deep search the topology for httprouterules from each gatewayclass, keeping record of the paths
			for i := range paths {
				if effectivePolicy := kuadrantv1.EffectivePolicyForPath[*kuadrantv1.AuthPolicy](paths[i], isAuthPolicyAcceptedAndNotDeletedFunc(state)); effectivePolicy != nil {
					pathID := kuadrantv1.PathID(paths[i])

					// Extract source policy locators from the effective policy rules
					// This ensures only policies that actually contributed are tracked, excluding overridden ones
					sourceLocators := kuadrantv1.SourcePoliciesFromEffectivePolicy(*effectivePolicy)

					effectivePolicies[pathID] = EffectiveAuthPolicy{
						Path:           paths[i],
						Spec:           **effectivePolicy,
						SourcePolicies: sourceLocators,
					}
					if logger.V(1).Enabled() {
						jsonEffectivePolicy, _ := json.Marshal(effectivePolicy)
						pathLocators := lo.Map(paths[i], machinery.MapTargetableToLocatorFunc)
						logger.V(1).Info("effective policy", "kind", kuadrantv1.AuthPolicyGroupKind.Kind, "pathID", pathID, "path", pathLocators, "effectivePolicy", string(jsonEffectivePolicy), "sourcePolicies", sourceLocators)
					}
				}
			}
		}
	}

	logger.V(1).Info("finished calculating effective auth policies", "effectivePolicies", len(effectivePolicies))

	return effectivePolicies
}
