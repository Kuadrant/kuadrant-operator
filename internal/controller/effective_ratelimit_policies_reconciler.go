package controllers

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"k8s.io/client-go/dynamic"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
)

type EffectiveRateLimitPolicy struct {
	Path []machinery.Targetable
	Spec kuadrantv1.RateLimitPolicy
	// SourcePolicies contains the locators of all policies that were evaluated/merged
	// to create this effective policy. This is important for tracing which parent policies
	// (e.g., Gateway-level and HTTPRoute-level) contributed to the final configuration.
	SourcePolicies []string
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
	logger := controller.LoggerFromContext(ctx).WithName("EffectiveRateLimitPolicyReconciler").WithValues("context", ctx)
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
	logger := controller.LoggerFromContext(ctx).WithName("EffectiveRateLimitPolicyReconciler").WithName("calculateEffectivePolicies").WithValues("context", ctx)
	tracer := controller.TracerFromContext(ctx)

	targetables := topology.Targetables()
	gatewayClasses := targetables.Children(kuadrant) // assumes only and all valid gateway classes are linked to kuadrant in the topology
	httpRouteRules := targetables.Items(func(o machinery.Object) bool {
		_, ok := o.(*machinery.HTTPRouteRule)
		return ok
	})

	logger.V(1).Info("calculating effective rate limit policies", "httpRouteRules", len(httpRouteRules))

	effectivePolicies := EffectiveRateLimitPolicies{}

	// Track which policies have been processed to create spans only once per policy
	processedPolicies := make(map[string]bool)

	for _, gatewayClass := range gatewayClasses {
		for _, httpRouteRule := range httpRouteRules {
			paths := targetables.Paths(gatewayClass, httpRouteRule) // this may be expensive in clusters with many gateway classes - an alternative is to deep search the topology for httprouterules from each gatewayclass, keeping record of the paths
			for i := range paths {
				// Get all policies in this path to trace each one
				policiesInPath := kuadrantv1.PoliciesInPath(paths[i], isRateLimitPolicyAcceptedAndNotDeletedFunc(state))

				// Create a span for each policy in the path (only once per policy)
				for _, p := range policiesInPath {
					policy := p.(*kuadrantv1.RateLimitPolicy)
					policyKey := string(policy.GetUID())

					if !processedPolicies[policyKey] {
						_, span := tracer.Start(ctx, "policy.RateLimitPolicy.effective")
						span.SetAttributes(
							attribute.String("policy.name", policy.GetName()),
							attribute.String("policy.namespace", policy.GetNamespace()),
							attribute.String("policy.kind", kuadrantv1.RateLimitPolicyGroupKind.Kind),
							attribute.String("policy.uid", policyKey),
						)
						span.AddEvent("policy evaluated in effective policy calculation")
						span.SetStatus(codes.Ok, "")
						span.End()

						processedPolicies[policyKey] = true
					}
				}

				if effectivePolicy := kuadrantv1.EffectivePolicyForPath[*kuadrantv1.RateLimitPolicy](paths[i], isRateLimitPolicyAcceptedAndNotDeletedFunc(state)); effectivePolicy != nil {
					pathID := kuadrantv1.PathID(paths[i])

					// Collect all source policy locators that contributed to this effective policy
					sourceLocators := lo.Map(policiesInPath, func(p machinery.Policy, _ int) string {
						return p.GetLocator()
					})

					effectivePolicies[pathID] = EffectiveRateLimitPolicy{
						Path:           paths[i],
						Spec:           **effectivePolicy,
						SourcePolicies: sourceLocators,
					}
					if logger.V(1).Enabled() {
						jsonEffectivePolicy, _ := json.Marshal(effectivePolicy)
						pathLocators := lo.Map(paths[i], machinery.MapTargetableToLocatorFunc)
						logger.V(1).Info("effective policy", "kind", kuadrantv1.RateLimitPolicyGroupKind.Kind, "pathID", pathID, "path", pathLocators, "effectivePolicy", string(jsonEffectivePolicy), "sourcePolicies", sourceLocators)
					}
				}
			}
		}
	}

	logger.V(1).Info("finished calculating effective rate limit policies", "effectivePolicies", len(effectivePolicies))

	return effectivePolicies
}
