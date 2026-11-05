package controllers

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantpolicymachinery "github.com/kuadrant/kuadrant-operator/internal/policymachinery"
	"github.com/kuadrant/kuadrant-operator/internal/ratelimit"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

const LimitadorLimitsReconcilerName = "LimitadorLimitsReconciler"

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
			{Kind: &machinery.GRPCRouteGroupKind},
			{Kind: &kuadrantv1.RateLimitPolicyGroupKind},
			{Kind: &kuadrantv1alpha1.TokenRateLimitPolicyGroupKind},
			{Kind: &kuadrantv1beta1.LimitadorGroupKind},
		},
	}
}

func (r *LimitadorLimitsReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("LimitadorLimitsReconciler").WithValues("context", ctx)
	logger.Info("Limitador limits reconciler", "status", "started")
	defer logger.Info("Limitador limits reconciler", "status", "completed")

	limitador := GetLimitadorFromTopology(topology, state)
	if limitador == nil {
		logger.V(1).Info("not limitador resources found in topology")
		return nil
	}

	desiredLimits, owners, sources := r.buildLimitadorLimits(ctx, state)

	if ratelimit.LimitadorRateLimits(limitador.Spec.Limits).EqualTo(desiredLimits) {
		logger.Info("limitador object is up to date, nothing to do", "status", "skipping")
		return nil
	}

	policies := lo.FilterMap(topology.Policies().Items(), func(policy machinery.Policy, _ int) (limitOwner, bool) {
		return newLimitOwner(policy)
	})
	modifiedPolicies := limitadorLimitsModifiedPolicies(limitador.Spec.Limits, desiredLimits, owners, policies)
	logger.V(1).Info("limitador limits modified", "policies", modifiedPolicies)
	state.Store(StateLimitadorLimitsModified, modifiedPolicies)

	if limitador.Annotations == nil {
		limitador.Annotations = make(map[string]string)
	}

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("namespace", limitador.Namespace),
		attribute.String("name", limitador.Name),
		attribute.StringSlice("sources", lo.Uniq(sources)),
	)

	// Clear stale trace annotations then inject fresh ones (no-op if tracing is disabled)
	delete(limitador.Annotations, "traceparent")
	delete(limitador.Annotations, "tracestate")
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(limitador.Annotations))

	limitador.Spec.Limits = desiredLimits

	obj, err := controller.Destruct(limitador)
	if err != nil {
		return err // should never happen
	}

	logger.V(1).Info("updating limitador object", "limitador", obj.Object)
	logger.Info("updating limitador object", "status", "processing")

	if _, err := r.client.Resource(kuadrantv1beta1.LimitadorsResource).Namespace(limitador.GetNamespace()).Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		logger.Error(err, "failed to update limitador object")

		// Record error for deferred retry
		errorRegistry := GetOrCreateErrorRegistry(state)
		errorRegistry.Record(
			LimitadorLimitsReconcilerName,
			OperationUpdate,
			k8stypes.NamespacedName{Name: limitador.GetName(), Namespace: limitador.GetNamespace()},
			kuadrantv1beta1.LimitadorGroupKind,
			err,
		)
	}

	logger.V(1).Info("finished updating limitador object", "limitador", (k8stypes.NamespacedName{Name: limitador.GetName(), Namespace: limitador.GetNamespace()}).String())

	return nil
}

func (r *LimitadorLimitsReconciler) buildLimitadorLimits(ctx context.Context, state *sync.Map) ([]limitadorv1alpha1.RateLimit, map[string]string, []string) {
	logger := controller.LoggerFromContext(ctx).WithName("LimitadorLimitsReconciler").WithName("buildLimitadorLimits").WithValues("context", ctx)

	rateLimitIndex := ratelimit.NewIndex()
	owners := map[string]string{}

	// both RateLimitPolicies and TokenRateLimitPolicies together
	sources := r.processEffectivePolicies(ctx, state, rateLimitIndex, owners)

	logger.V(1).Info("finished building limitador limits", "limits", rateLimitIndex.Len())

	return rateLimitIndex.ToRateLimits(), owners, sources
}

func (r *LimitadorLimitsReconciler) processEffectivePolicies(ctx context.Context, state *sync.Map, rateLimitIndex *ratelimit.Index, owners map[string]string) []string {
	logger := controller.LoggerFromContext(ctx).WithName("LimitadorLimitsReconciler").WithName("processEffectivePolicies").WithValues("context", ctx)
	var sources []string
	// RateLimitPolicies
	if effectivePolicies, ok := state.Load(StateEffectiveRateLimitPolicies); ok {
		effectivePoliciesMap := effectivePolicies.(EffectiveRateLimitPolicies)
		logger.V(1).Info("processing rate limit policies", "count", len(effectivePoliciesMap))
		for pathID, effectivePolicy := range effectivePoliciesMap {
			sources = append(sources, effectivePolicy.SourcePolicies...)
			r.processPolicyRules(ctx, pathID, effectivePolicy.Path, effectivePolicy.Spec.Rules(), state, rateLimitIndex, owners)
		}
	}

	// TokenRateLimitPolicies
	if effectivePolicies, ok := state.Load(StateEffectiveTokenRateLimitPolicies); ok {
		effectivePoliciesMap := effectivePolicies.(EffectiveTokenRateLimitPolicies)
		logger.V(1).Info("processing token rate limit policies", "count", len(effectivePoliciesMap))
		for pathID, effectivePolicy := range effectivePoliciesMap {
			sources = append(sources, effectivePolicy.SourcePolicies...)
			r.processPolicyRules(ctx, pathID, effectivePolicy.Path, effectivePolicy.Spec.Rules(), state, rateLimitIndex, owners)
		}
	}

	return sources
}

func (r *LimitadorLimitsReconciler) processPolicyRules(ctx context.Context, pathID string, path []machinery.Targetable, rules map[string]kuadrantv1.MergeableRule, state *sync.Map, rateLimitIndex *ratelimit.Index, owners map[string]string) {
	logger := controller.LoggerFromContext(ctx).WithName("LimitadorLimitsReconciler").WithName("processPolicyRules").WithValues("context", ctx)
	parsed, err := kuadrantpolicymachinery.ParseTopologyPath(path)
	if err != nil {
		logger.Error(err, "failed to parse request path", "pathID", pathID)
		return
	}
	limitsNamespace := LimitsNamespaceFromRoute(parsed.GetRoute())

	limitRules := lo.Filter(lo.Entries(rules),
		func(r lo.Entry[string, kuadrantv1.MergeableRule], _ int) bool {
			return r.Key != kuadrantv1.RulesKeyTopLevelPredicates
		},
	)

	for _, limitRule := range limitRules {
		limitKey := limitRule.Key
		mergeableLimit := limitRule.Value

		// Find policy using combined predicate that checks both RateLimitPolicy and TokenRateLimitPolicy
		policy, found := lo.Find(kuadrantv1.PoliciesInPath(path, isRateLimitingPolicyAcceptedAndNotDeletedFunc(state)), func(p machinery.Policy) bool {
			return p.GetLocator() == mergeableLimit.GetSource()
		})

		if !found { // should never happen
			logger.Error(fmt.Errorf("origin policy %s not found in path %s", mergeableLimit.GetSource(), pathID), "failed to build limitador limit definition")
			continue
		}

		policyKey := k8stypes.NamespacedName{Name: policy.GetName(), Namespace: policy.GetNamespace()}
		limitSpec := mergeableLimit.GetSpec()
		switch limit := limitSpec.(type) {
		case *kuadrantv1.Limit:
			limitIdentifier := LimitNameToLimitadorIdentifier(policyKey, limitKey)
			rateLimits := lo.Map(limit.Rates, func(rate kuadrantv1.Rate, _ int) limitadorv1alpha1.RateLimit {
				maxValue, seconds := rate.ToSeconds()
				return limitadorv1alpha1.RateLimit{
					Name:       limitKey,
					Namespace:  string(limitsNamespace),
					MaxValue:   maxValue,
					Seconds:    seconds,
					Conditions: []string{fmt.Sprintf("descriptors[0][\"%s\"] == \"1\"", limitIdentifier)},
					Variables:  utils.GetEmptySliceIfNil(limit.CountersAsStringList()),
				}
			})
			rateLimitIndex.Set(fmt.Sprintf("%s/%s", limitsNamespace, limitIdentifier), rateLimits)
			addLimitOwner(owners, rateLimits, policy.GetLocator())

		case *kuadrantv1alpha1.TokenLimit:
			limitIdentifier := TokenLimitNameToLimitadorIdentifier(policyKey, limitKey)
			rateLimits := utils.Map(limit.Rates, func(rate kuadrantv1.Rate) limitadorv1alpha1.RateLimit {
				maxValue, seconds := rate.ToSeconds()
				return limitadorv1alpha1.RateLimit{
					Name:       limitKey,
					Namespace:  string(limitsNamespace),
					MaxValue:   maxValue,
					Seconds:    seconds,
					Conditions: []string{fmt.Sprintf("descriptors[0][\"%s\"] == \"1\"", limitIdentifier)},
					Variables:  utils.GetEmptySliceIfNil(limit.CountersAsStringList()),
				}
			})
			rateLimitIndex.Set(fmt.Sprintf("%s/%s", limitsNamespace, limitIdentifier), rateLimits)
			addLimitOwner(owners, rateLimits, policy.GetLocator())

		default:
			logger.Error(fmt.Errorf("unknown limit type: %T", limitSpec), "failed to process limit")
			continue
		}
	}
}

// limitOwner is a policy that generates Limitador limits
type limitOwner struct {
	locator    string
	key        k8stypes.NamespacedName
	identifier func(policyKey k8stypes.NamespacedName, limitName string) string
}

func newLimitOwner(policy machinery.Policy) (limitOwner, bool) {
	key := k8stypes.NamespacedName{Name: policy.GetName(), Namespace: policy.GetNamespace()}
	switch policy.(type) {
	case *kuadrantv1.RateLimitPolicy:
		return limitOwner{locator: policy.GetLocator(), key: key, identifier: LimitNameToLimitadorIdentifier}, true
	case *kuadrantv1alpha1.TokenRateLimitPolicy:
		return limitOwner{locator: policy.GetLocator(), key: key, identifier: TokenLimitNameToLimitadorIdentifier}, true
	}
	return limitOwner{}, false
}

// owns tells whether the limit was generated from one of the owner's limits. The Limitador limit
// name is the policy limit name, which recomputes the identifier embedded in the condition.
func (o limitOwner) owns(limit limitadorv1alpha1.RateLimit) bool {
	return lo.Contains(limit.Conditions, fmt.Sprintf("descriptors[0][\"%s\"] == \"1\"", o.identifier(o.key, limit.Name)))
}

// addLimitOwner records the locator of the policy that generated the group of limits
func addLimitOwner(owners map[string]string, rateLimits []limitadorv1alpha1.RateLimit, locator string) {
	if len(rateLimits) == 0 {
		return
	}
	owners[limitadorLimitGroupKey(rateLimits[0])] = locator
}

// limitadorLimitGroupKey groups the Limitador limits generated from one policy limit for one route:
// they share the limits namespace and the condition on the limit identifier, and differ in rates only.
func limitadorLimitGroupKey(limit limitadorv1alpha1.RateLimit) string {
	conditions := slices.Clone(limit.Conditions)
	slices.Sort(conditions)
	return limit.Namespace + "|" + strings.Join(conditions, "|")
}

// limitadorLimitsModifiedPolicies returns the sorted locators of the policies whose Limitador limits
// differ between existing and desired. owners maps each desired group to the policy that generated it.
// A group only present in existing is matched against policies, so a policy that keeps a limit but
// clears all its rates is still attributed. Groups of deleted policies are attributed to no one.
func limitadorLimitsModifiedPolicies(existing, desired []limitadorv1alpha1.RateLimit, owners map[string]string, policies []limitOwner) []string {
	existingGroups := lo.GroupBy(existing, limitadorLimitGroupKey)
	desiredGroups := lo.GroupBy(desired, limitadorLimitGroupKey)

	var modified []string
	for key, limits := range desiredGroups {
		locator, found := owners[key]
		if found && !ratelimit.LimitadorRateLimits(existingGroups[key]).EqualTo(limits) {
			modified = append(modified, locator)
		}
	}

	for key, limits := range existingGroups {
		if _, found := desiredGroups[key]; found {
			continue
		}
		if owner, found := lo.Find(policies, func(o limitOwner) bool { return o.owns(limits[0]) }); found {
			modified = append(modified, owner.locator)
		}
	}

	modified = lo.Uniq(modified)
	slices.Sort(modified)
	return modified
}

// isRateLimitingPolicyAcceptedAndNotDeletedFunc returns a predicate that checks if a policy is either
// a RateLimitPolicy or TokenRateLimitPolicy that is accepted and not deleted
func isRateLimitingPolicyAcceptedAndNotDeletedFunc(state *sync.Map) func(machinery.Policy) bool {
	rlpPredicate := isRateLimitPolicyAcceptedAndNotDeletedFunc(state)
	trlpPredicate := isTokenRateLimitPolicyAcceptedAndNotDeletedFunc(state)

	return func(policy machinery.Policy) bool {
		// Check if it's a RateLimitPolicy or TokenRateLimitPolicy
		return rlpPredicate(policy) || trlpPredicate(policy)
	}
}
