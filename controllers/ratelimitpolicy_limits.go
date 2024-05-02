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

		// If rlp is targeting a route, limits may be overridden by other policies
		if kuadrantgatewayapi.IsTargetRefHTTPRoute(rlp.GetTargetRef()) {
			r.applyOverrides(ctx, rlp, t)
		}

		rateLimitIndex.Set(rlpKey, rlptools.LimitadorRateLimitsFromRLP(rlp))
	}

	return rateLimitIndex, nil
}

func (r *RateLimitPolicyReconciler) applyOverrides(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, t *kuadrantgatewayapi.Topology) {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("applyOverrides")

	// Retrieve affected policies
	affectedPolicies := r.getAffectedPolicies(rlp, t)

	// Filter out current policy from affected policies
	// Only gateway RLPs can affect Route RLP and these must not be marked for deletion
	affectedPolicies = utils.Filter(affectedPolicies, func(policy kuadrantgatewayapi.Policy) bool {
		return kuadrantgatewayapi.IsTargetRefGateway(policy.GetTargetRef()) && policy.GetUID() != rlp.GetUID() && policy.GetDeletionTimestamp() == nil
	})

	sort.Sort(kuadrantgatewayapi.PolicyByTargetRefKindAndCreationTimeStamp(affectedPolicies))

	for _, policy := range affectedPolicies {
		p := policy.(*kuadrantv1beta2.RateLimitPolicy)
		if p.Spec.Overrides != nil {
			rlp.Spec.CommonSpec().Limits = p.Spec.Overrides.Limits
			logger.V(1).Info("applying overrides from parent policy", "parentPolicy", client.ObjectKeyFromObject(p))
			break
		}
	}
}

func (r *RateLimitPolicyReconciler) getAffectedPolicies(rlp *kuadrantv1beta2.RateLimitPolicy, t *kuadrantgatewayapi.Topology) []kuadrantgatewayapi.Policy {
	topologyIndexes := kuadrantgatewayapi.NewTopologyIndexes(t)
	var affectedPolicies []kuadrantgatewayapi.Policy

	for _, gw := range t.Gateways() {
		policyList := topologyIndexes.PoliciesFromGateway(gw.Gateway)

		if slices.Contains(utils.Map(policyList, func(p kuadrantgatewayapi.Policy) client.ObjectKey {
			return client.ObjectKeyFromObject(p)
		}), client.ObjectKeyFromObject(rlp)) {
			affectedPolicies = append(affectedPolicies, policyList...)
		}
	}

	return affectedPolicies
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
