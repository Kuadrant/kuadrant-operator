package controllers

import (
	"context"
	"sort"

	"github.com/go-logr/logr"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	return r.reconcileLimitador(ctx, rlp, policies)
}

func (r *RateLimitPolicyReconciler) deleteLimits(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy) error {
	policies, err := r.getPolicies(ctx)
	if err != nil {
		return err
	}
	policiesWithoutRLP := utils.Filter(policies, func(policy kuadrantgatewayapi.Policy) bool {
		return client.ObjectKeyFromObject(policy) != client.ObjectKeyFromObject(rlp)
	})
	return r.reconcileLimitador(ctx, rlp, policiesWithoutRLP)
}

func (r *RateLimitPolicyReconciler) reconcileLimitador(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, policies []kuadrantgatewayapi.Policy) error {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("reconcileLimitador").WithValues("policies", utils.Map(policies, func(p kuadrantgatewayapi.Policy) string { return client.ObjectKeyFromObject(p).String() }))

	rateLimitIndex := r.buildRateLimitIndex(policies)

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

func (r *RateLimitPolicyReconciler) buildRateLimitIndex(policies []kuadrantgatewayapi.Policy) *rlptools.RateLimitIndex {
	rateLimitIndex := rlptools.NewRateLimitIndex()

	sort.Sort(kuadrantgatewayapi.PolicyByTargetRefKindAndCreationTimeStamp(policies))

	for i := range policies {
		policy := policies[i]
		rlpKey := client.ObjectKeyFromObject(policy)
		rlp := policy.(*kuadrantv1beta2.RateLimitPolicy)
		rateLimitIndex.Set(rlpKey, rlptools.LimitadorRateLimitsFromRLP(rlp))
	}

	return rateLimitIndex
}
