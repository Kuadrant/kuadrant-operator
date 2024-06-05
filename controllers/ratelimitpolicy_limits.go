package controllers

import (
	"context"
	"fmt"
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
	policies, err := r.getPolicies(ctx)
	if err != nil {
		return err
	}
	topology, err := r.buildTopology(ctx, policies)
	if err != nil {
		return err
	}
	return r.reconcileLimitador(ctx, rlp, topology)
}

func (r *RateLimitPolicyReconciler) deleteLimits(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy) error {
	policies, err := r.getPolicies(ctx)
	if err != nil {
		return err
	}
	policiesWithoutRLP := utils.Filter(policies, func(policy kuadrantgatewayapi.Policy) bool {
		return client.ObjectKeyFromObject(policy) != client.ObjectKeyFromObject(rlp)
	})
	topology, err := r.buildTopology(ctx, policiesWithoutRLP)
	if err != nil {
		return err
	}
	return r.reconcileLimitador(ctx, rlp, topology)
}

func (r *RateLimitPolicyReconciler) reconcileLimitador(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) error {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("reconcileLimitador")

	rateLimitIndex := r.buildRateLimitIndex(ctx, topology)

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

func (r *RateLimitPolicyReconciler) getPolicies(ctx context.Context) ([]kuadrantgatewayapi.Policy, error) {
	logger, _ := logr.FromContext(ctx)

	rlpList := &kuadrantv1beta2.RateLimitPolicyList{}
	err := r.Client().List(ctx, rlpList)
	logger.V(1).Info("topology: list rate limit policies", "#RLPS", len(rlpList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	policies := utils.Map(rlpList.Items, func(p kuadrantv1beta2.RateLimitPolicy) kuadrantgatewayapi.Policy { return &p })

	return policies, nil
}

func (r *RateLimitPolicyReconciler) buildTopology(ctx context.Context, policies []kuadrantgatewayapi.Policy) (*kuadrantgatewayapi.Topology, error) {
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

	return kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways(utils.Map(gwList.Items, ptr.To[gatewayapiv1.Gateway])),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
		kuadrantgatewayapi.WithPolicies(policies),
		kuadrantgatewayapi.WithLogger(logger),
	)
}

func (r *RateLimitPolicyReconciler) buildRateLimitIndex(ctx context.Context, topology *kuadrantgatewayapi.Topology) *rlptools.RateLimitIndex {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("buildRateLimitIndex")

	gateways := lo.KeyBy(topology.Gateways(), func(gateway kuadrantgatewayapi.GatewayNode) string {
		return client.ObjectKeyFromObject(gateway.Gateway).String()
	})

	// sort the gateways for deterministic output and consistent comparison against existing objects
	gatewayNames := lo.Keys(gateways)
	slices.Sort(gatewayNames)

	rateLimitIndex := rlptools.NewRateLimitIndex()

	for _, gatewayName := range gatewayNames {
		gateway := gateways[gatewayName].Gateway
		topologyWithOverrides, err := rlptools.ApplyOverrides(topology, gateway)
		if err != nil {
			logger.Error(err, "failed to apply overrides")
			return nil
		}

		// sort the policies for deterministic output and consistent comparison against existing objects
		indexes := kuadrantgatewayapi.NewTopologyIndexes(topologyWithOverrides)
		policies := indexes.PoliciesFromGateway(gateway)
		sort.Sort(kuadrantgatewayapi.PolicyByTargetRefKindAndCreationTimeStamp(policies))

		logger.V(1).Info("new rate limit index", "gateway", client.ObjectKeyFromObject(gateway), "policies", lo.Map(policies, func(p kuadrantgatewayapi.Policy, _ int) string { return client.ObjectKeyFromObject(p).String() }))

		for _, policy := range policies {
			rlpKey := client.ObjectKeyFromObject(policy)
			gatewayKey := client.ObjectKeyFromObject(gateway)
			key := rlptools.RateLimitIndexKey{
				RateLimitPolicyKey: rlpKey,
				GatewayKey:         gatewayKey,
			}
			if _, ok := rateLimitIndex.Get(key); ok { // should never happen
				logger.Error(fmt.Errorf("unexpected duplicate rate limit policy key found"), "failed do add rate limit policy to index", "RateLimitPolicy", rlpKey.String(), "Gateway", gatewayKey)
				continue
			}
			rlp := policy.(*kuadrantv1beta2.RateLimitPolicy)
			rateLimitIndex.Set(key, rlptools.LimitadorRateLimitsFromRLP(rlp))
		}
	}

	return rateLimitIndex
}
