package controllers

import (
	"context"
	"slices"
	"sort"

	"github.com/go-logr/logr"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/samber/lo"
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
	topology, policies, err := r.generateTopology(ctx)
	if err != nil {
		return err
	}
	return r.reconcileLimitador(ctx, rlp, topology, policies)
}

func (r *RateLimitPolicyReconciler) deleteLimits(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy) error {
	topology, policies, err := r.generateTopology(ctx)
	if err != nil {
		return err
	}
	policiesWithoutRLP := utils.Filter(policies, func(policy kuadrantgatewayapi.Policy) bool {
		return client.ObjectKeyFromObject(policy) != client.ObjectKeyFromObject(rlp)
	})
	return r.reconcileLimitador(ctx, rlp, topology, policiesWithoutRLP)
}

func (r *RateLimitPolicyReconciler) reconcileLimitador(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology, policies []kuadrantgatewayapi.Policy) error {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("reconcileLimitador").WithValues("policies", utils.Map(policies, func(p kuadrantgatewayapi.Policy) string { return client.ObjectKeyFromObject(p).String() }))

	rateLimitIndex := r.buildRateLimitIndex(ctx, topology, policies)

	// get the current limitador cr for the kuadrant instance so we can compare if it needs to be updated
	limitador, err := GetLimitador(ctx, r.Client(), rlp)
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

func GetLimitador(ctx context.Context, k8sclient client.Client, rlp *kuadrantv1beta2.RateLimitPolicy) (*limitadorv1alpha1.Limitador, error) {
	logger, _ := logr.FromContext(ctx)

	logger.V(1).Info("get kuadrant namespace")
	var kuadrantNamespace string
	kuadrantNamespace, isSet := kuadrant.GetKuadrantNamespaceFromPolicy(rlp)
	if !isSet {
		var err error
		kuadrantNamespace, err = kuadrant.GetKuadrantNamespaceFromPolicyTargetRef(ctx, k8sclient, rlp)
		if err != nil {
			logger.Error(err, "failed to get kuadrant namespace")
			return nil, err
		}
		kuadrant.AnnotateObject(rlp, kuadrantNamespace)
		err = k8sclient.Update(ctx, rlp) // @guicassolato: not sure if this belongs to here
		if err != nil {
			logger.Error(err, "failed to update policy, re-queuing")
			return nil, err
		}
	}
	limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantNamespace}
	limitador := &limitadorv1alpha1.Limitador{}
	err := k8sclient.Get(ctx, limitadorKey, limitador)
	logger.V(1).Info("get limitador", "limitador", limitadorKey, "err", err)
	if err != nil {
		return nil, err
	}

	return limitador, nil
}

func (r *RateLimitPolicyReconciler) buildRateLimitIndex(ctx context.Context, topology *kuadrantgatewayapi.Topology, policies []kuadrantgatewayapi.Policy) *rlptools.RateLimitIndex {
	rateLimitIndex := rlptools.NewRateLimitIndex()

	for _, policy := range policies {
		rlpKey := client.ObjectKeyFromObject(policy)
		if _, ok := rateLimitIndex.Get(rlpKey); ok {
			continue
		}

		rlp := policy.(*kuadrantv1beta2.RateLimitPolicy)

		// If rlp is targeting a route, limits may be overridden by other policies
		if r.overridden(ctx, rlp, topology) {
			continue
		}

		rateLimitIndex.Set(rlpKey, rlptools.LimitadorRateLimitsFromRLP(rlp))
	}

	return rateLimitIndex
}

func (r *RateLimitPolicyReconciler) overridden(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) bool {
	// Only route policies can be overridden
	if !kuadrantgatewayapi.IsTargetRefHTTPRoute(rlp.GetTargetRef()) {
		return false
	}

	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("overridden")

	// Only gateway policies can override route policies and those must not be marked for deletion
	gatewayPolicies := utils.Filter(r.getRelatedPolicies(rlp, topology), func(policy kuadrantgatewayapi.Policy) bool {
		return policy.GetDeletionTimestamp() == nil &&
			kuadrantgatewayapi.IsTargetRefGateway(policy.GetTargetRef()) &&
			policy.GetUID() != rlp.GetUID()
	})

	for _, policy := range gatewayPolicies {
		p := policy.(*kuadrantv1beta2.RateLimitPolicy)
		if p.Spec.Overrides != nil {
			logger.V(1).Info("policy has been overridden, skipping corresponding limits config", "overridden by", client.ObjectKeyFromObject(p))
			return true
		}
	}

	return false
}

func (r *RateLimitPolicyReconciler) getRelatedPolicies(rlp *kuadrantv1beta2.RateLimitPolicy, t *kuadrantgatewayapi.Topology) []kuadrantgatewayapi.Policy {
	topologyIndexes := kuadrantgatewayapi.NewTopologyIndexes(t)
	policyKeyFunc := func(p kuadrantgatewayapi.Policy) client.ObjectKey { return client.ObjectKeyFromObject(p) }
	var relatedPolicies []kuadrantgatewayapi.Policy
	for _, gw := range t.Gateways() {
		policyList := topologyIndexes.PoliciesFromGateway(gw.Gateway)
		if slices.Contains(utils.Map(policyList, policyKeyFunc), client.ObjectKeyFromObject(rlp)) {
			relatedPolicies = append(relatedPolicies, policyList...)
		}
	}
	sort.Sort(kuadrantgatewayapi.PolicyByTargetRefKindAndCreationTimeStamp(relatedPolicies))
	return lo.Uniq(relatedPolicies)
}

func (r *RateLimitPolicyReconciler) generateTopology(ctx context.Context) (*kuadrantgatewayapi.Topology, []kuadrantgatewayapi.Policy, error) {
	logger, _ := logr.FromContext(ctx)

	gwList := &gatewayapiv1.GatewayList{}
	err := r.Client().List(ctx, gwList)
	logger.V(1).Info("topology: list gateways", "#Gateways", len(gwList.Items), "err", err)
	if err != nil {
		return nil, nil, err
	}

	routeList := &gatewayapiv1.HTTPRouteList{}
	err = r.Client().List(ctx, routeList)
	logger.V(1).Info("topology: list httproutes", "#HTTPRoutes", len(routeList.Items), "err", err)
	if err != nil {
		return nil, nil, err
	}

	rlpList := &kuadrantv1beta2.RateLimitPolicyList{}
	err = r.Client().List(ctx, rlpList)
	logger.V(1).Info("topology: list rate limit policies", "#RLPS", len(rlpList.Items), "err", err)
	if err != nil {
		return nil, nil, err
	}

	policies := utils.Map(rlpList.Items, func(p kuadrantv1beta2.RateLimitPolicy) kuadrantgatewayapi.Policy { return &p })
	sort.Sort(kuadrantgatewayapi.PolicyByTargetRefKindAndCreationTimeStamp(policies))

	topology, err := kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways(utils.Map(gwList.Items, ptr.To[gatewayapiv1.Gateway])),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
		kuadrantgatewayapi.WithPolicies(policies),
		kuadrantgatewayapi.WithLogger(logger),
	)
	if err != nil {
		return nil, nil, err
	}

	return topology, policies, nil
}
