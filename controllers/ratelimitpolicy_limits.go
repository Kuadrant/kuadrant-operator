package controllers

import (
	"context"
	"slices"
	"sort"

	"github.com/go-logr/logr"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
)

func (r *RateLimitPolicyReconciler) reconcileLimits(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy) error {
	rlpRefs, err := r.TargetRefReconciler.GetAllGatewayPolicyRefs(ctx, rlp)
	if err != nil {
		return err
	}
	return r.reconcileLimitador(ctx, rlp, append(rlpRefs, client.ObjectKeyFromObject(rlp)))
}

func (r *RateLimitPolicyReconciler) deleteLimits(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy) error {
	rlpRefs, err := r.TargetRefReconciler.GetAllGatewayPolicyRefs(ctx, rlp)
	if err != nil {
		return err
	}

	rlpRefsWithoutRLP := utils.Filter(rlpRefs, func(rlpRef client.ObjectKey) bool {
		return rlpRef.Name != rlp.Name || rlpRef.Namespace != rlp.Namespace
	})

	return r.reconcileLimitador(ctx, rlp, rlpRefsWithoutRLP)
}

func (r *RateLimitPolicyReconciler) reconcileLimitador(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, rlpRefs []client.ObjectKey) error {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("reconcileLimitador").WithValues("rlp refs", utils.Map(rlpRefs, func(ref client.ObjectKey) string { return ref.String() }))

	rateLimitIndex, err := r.buildRateLimitIndex(ctx, rlpRefs)
	if err != nil {
		return err
	}
	// get the current limitador cr for the kuadrant instance so we can compare if it needs to be updated
	limitador, err := r.getLimitador(ctx, rlp)
	if err != nil {
		return err
	}
	// return if limitador is up-to-date
	if rlptools.Equal(rateLimitIndex.ToRateLimits(), limitador.Spec.Limits) {
		logger.V(1).Info("limitador is up to date, skipping update")
		return nil
	}

	// update limitador
	limitador.Spec.Limits = rateLimitIndex.ToRateLimits()
	err = r.UpdateResource(ctx, limitador)
	logger.V(1).Info("update limitador", "limitador", client.ObjectKeyFromObject(limitador), "err", err)
	if err != nil {
		return err
	}

	return nil
}

func (r *RateLimitPolicyReconciler) getLimitador(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy) (*limitadorv1alpha1.Limitador, error) {
	logger, _ := logr.FromContext(ctx)

	logger.V(1).Info("get kuadrant namespace")
	var kuadrantNamespace string
	kuadrantNamespace, isSet := kuadrant.GetKuadrantNamespaceFromPolicy(rlp)
	if !isSet {
		var err error
		kuadrantNamespace, err = kuadrant.GetKuadrantNamespaceFromPolicyTargetRef(ctx, r.Client(), rlp)
		if err != nil {
			logger.Error(err, "failed to get kuadrant namespace")
			return nil, err
		}
		kuadrant.AnnotateObject(rlp, kuadrantNamespace)
		err = r.UpdateResource(ctx, rlp) // @guicassolato: not sure if this belongs to here
		if err != nil {
			logger.Error(err, "failed to update policy, re-queuing")
			return nil, err
		}
	}
	limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantNamespace}
	limitador := &limitadorv1alpha1.Limitador{}
	err := r.Client().Get(ctx, limitadorKey, limitador)
	logger.V(1).Info("get limitador", "limitador", limitadorKey, "err", err)
	if err != nil {
		return nil, err
	}

	return limitador, nil
}

func (r *RateLimitPolicyReconciler) buildRateLimitIndex(ctx context.Context, rlpRefs []client.ObjectKey) (*rlptools.RateLimitIndex, error) {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("buildRateLimitIndex").WithValues("ratelimitpolicies", rlpRefs)

	t, err := r.generateTopology(ctx)
	if err != nil {
		return nil, err
	}

	rateLimitIndex := rlptools.NewRateLimitIndex()

	for _, rlpKey := range rlpRefs {
		if _, ok := rateLimitIndex.Get(rlpKey); ok {
			continue
		}

		rlp := &kuadrantv1beta2.RateLimitPolicy{}
		err := r.Client().Get(ctx, rlpKey, rlp)
		logger.V(1).Info("get rlp", "ratelimitpolicy", rlpKey, "err", err)
		if err != nil {
			return nil, err
		}

		if err := r.applyOverrides(ctx, rlp, t); err != nil {
			return nil, err
		}

		rateLimitIndex.Set(rlpKey, rlptools.LimitadorRateLimitsFromRLP(rlp))
	}

	return rateLimitIndex, nil
}

// applyOverrides checks for any overrides set for the RateLimitPolicy.
// It iterates through the slice of policies to find overrides for the provided target HTTPRoute.
// If an override is found, it updates the limits in the RateLimitPolicySpec accordingly.
func (r *RateLimitPolicyReconciler) applyOverrides(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, t *kuadrantgatewayapi.Topology) error {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("applyOverrides")

	affectedPolicies, numUnTargetedRoutes := r.getAffectedPolicies(rlp, t)
	affectedPolicies = utils.Filter(affectedPolicies, func(policy kuadrantgatewayapi.Policy) bool {
		// Filter out current policy from affected policies
		return policy.GetUID() != rlp.GetUID()
	})
	affectedPoliciesKeys := utils.Map(affectedPolicies, func(p kuadrantgatewayapi.Policy) client.ObjectKey {
		return client.ObjectKeyFromObject(p)
	})

	filteredPolicies := utils.Filter(affectedPolicies, func(policy kuadrantgatewayapi.Policy) bool {
		if policy.GetUID() == rlp.GetUID() {
			return false
		}

		// HTTPRoute RLPs should only care about overrides from gateways
		if kuadrantgatewayapi.IsTargetRefHTTPRoute(rlp.GetTargetRef()) {
			return kuadrantgatewayapi.IsTargetRefGateway(policy.GetTargetRef())
		}
		// Gateway RLPs are not affected by other Gateway RLPs
		return false
	})

	// Is a GW policy
	if kuadrantgatewayapi.IsTargetRefGateway(rlp.GetTargetRef()) {
		// Specifies defaults and has free routes => not enforced
		if rlp.Spec.Overrides == nil && numUnTargetedRoutes == 0 {
			r.OverriddenPolicyMap.SetOverriddenPolicy(rlp, affectedPoliciesKeys)
			logger.V(1).Info("policy has no free routes to enforce policy")
		} else {
			r.OverriddenPolicyMap.RemoveOverriddenPolicy(rlp)
		}
	} else {
		// Is a Route policy
		// Sort by TargetRefKind and creation timestamp
		// Gateways RLPs are listed first in the order of oldest policy
		sort.Sort(kuadrantgatewayapi.PolicyByTargetRefKindAndCreationTimeStamp(filteredPolicies))

		// Iterate in order of precedence until finding a block of overrides
		for _, policy := range filteredPolicies {
			p := policy.(*kuadrantv1beta2.RateLimitPolicy)
			if p.Spec.Overrides != nil {
				rlp.Spec.CommonSpec().Limits = p.Spec.Overrides.Limits
				logger.V(1).Info("applying overrides from parent policy", "parentPolicy", client.ObjectKeyFromObject(p))
				// Overridden by another policy
				r.OverriddenPolicyMap.SetOverriddenPolicy(rlp, []client.ObjectKey{client.ObjectKeyFromObject(p)})
				break
			}
			r.OverriddenPolicyMap.RemoveOverriddenPolicy(rlp)
		}
	}

	// Reconcile status for all policies that are affected by this RLP
	for _, policy := range affectedPolicies {
		if policy.GetUID() != rlp.GetUID() {
			p := policy.(*kuadrantv1beta2.RateLimitPolicy)
			if kuadrantgatewayapi.IsTargetRefGateway(rlp.GetTargetRef()) && rlp.Spec.Overrides != nil {
				r.OverriddenPolicyMap.SetOverriddenPolicy(p, []client.ObjectKey{client.ObjectKeyFromObject(rlp)})
			}
			_, err := r.reconcileStatus(ctx, p, nil)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *RateLimitPolicyReconciler) getAffectedPolicies(rlp *kuadrantv1beta2.RateLimitPolicy, t *kuadrantgatewayapi.Topology) ([]kuadrantgatewayapi.Policy, int) {
	topologyIndexes := kuadrantgatewayapi.NewTopologyIndexes(t)

	var affectedPolicies []kuadrantgatewayapi.Policy
	var numUnTargetedRoutes int
	// For each gw, get all the policies if the current rlp is within the topology for this gateway
	for _, gw := range t.Gateways() {
		policyList := topologyIndexes.PoliciesFromGateway(gw.Gateway)
		policyKeys := utils.Map(policyList, func(p kuadrantgatewayapi.Policy) client.ObjectKey {
			return client.ObjectKeyFromObject(p)
		})

		numUnTargetedRoutes = numUnTargetedRoutes + len(topologyIndexes.GetUntargetedRoutes(gw.Gateway))

		// this policy is potentially affected by other policies from this gateway
		if slices.Contains(policyKeys, client.ObjectKeyFromObject(rlp)) {
			affectedPolicies = append(affectedPolicies, policyList...)
		}
	}

	return affectedPolicies, numUnTargetedRoutes
}

func (r *RateLimitPolicyReconciler) generateTopology(ctx context.Context) (*kuadrantgatewayapi.Topology, error) {
	logger, _ := logr.FromContext(ctx)

	gwList := &gatewayapiv1.GatewayList{}
	err := r.Client().List(ctx, gwList)
	logger.V(1).Info("topology: list gateways", "#Gateways", len(gwList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	routeList := &gatewayapiv1.HTTPRouteList{}
	err = r.Client().List(ctx, routeList)
	logger.V(1).Info("topology: list httproutes", "#HTTPRoutes", len(routeList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	rlpList := &kuadrantv1beta2.RateLimitPolicyList{}
	err = r.Client().List(ctx, rlpList)
	logger.V(1).Info("topology: list rate limit policies", "#RLPS", len(rlpList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	policies := utils.Map(rlpList.Items, func(p kuadrantv1beta2.RateLimitPolicy) kuadrantgatewayapi.Policy { return &p })

	return kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways(utils.Map(gwList.Items, ptr.To[gatewayapiv1.Gateway])),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
		kuadrantgatewayapi.WithPolicies(policies),
		kuadrantgatewayapi.WithLogger(logger),
	)
}
