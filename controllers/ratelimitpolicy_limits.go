package controllers

import (
	"context"
	"encoding/json"

	"github.com/go-logr/logr"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
)

func (r *RateLimitPolicyReconciler) reconcileLimits(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy) error {
	rlpRefs, err := r.GetAllGatewayPolicyRefs(ctx, &common.KuadrantRateLimitPolicyRefsConfig{})
	if err != nil {
		return err
	}
	return r.reconcileLimitador(ctx, rlp, append(rlpRefs, client.ObjectKeyFromObject(rlp)))
}

func (r *RateLimitPolicyReconciler) deleteLimits(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy) error {
	rlpRefs, err := r.GetAllGatewayPolicyRefs(ctx, &common.KuadrantRateLimitPolicyRefsConfig{})
	if err != nil {
		return err
	}

	rlpRefsWithoutRLP := common.Filter(rlpRefs, func(rlpRef client.ObjectKey) bool {
		return rlpRef.Name != rlp.Name || rlpRef.Namespace != rlp.Namespace
	})

	return r.reconcileLimitador(ctx, rlp, rlpRefsWithoutRLP)
}

func (r *RateLimitPolicyReconciler) reconcileLimitador(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, rlpRefs []client.ObjectKey) error {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("reconcileLimitador").WithValues("rlp refs", common.Map(rlpRefs, func(ref client.ObjectKey) string { return ref.String() }))

	rateLimitIndex, err := r.buildRateLimitIndex(ctx, rlpRefs)
	if err != nil {
		return err
	}

	// get the current limitador cr for the kuadrant instance so we can compare if it needs to be updated
	logger.V(1).Info("get kuadrant namespace")
	var kuadrantNamespace string
	kuadrantNamespace, isSet := common.GetKuadrantNamespaceFromPolicy(rlp)
	if !isSet {
		var err error
		kuadrantNamespace, err = common.GetKuadrantNamespaceFromPolicyTargetRef(ctx, r.Client(), rlp)
		if err != nil {
			logger.Error(err, "failed to get kuadrant namespace")
			return err
		}
		common.AnnotateObject(rlp, kuadrantNamespace)
		err = r.UpdateResource(ctx, rlp) // @guicassolato: not sure if this belongs to here
		if err != nil {
			logger.Error(err, "failed to update policy, re-queuing")
			return err
		}
	}
	limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantNamespace}
	limitador := &limitadorv1alpha1.Limitador{}
	err = r.Client().Get(ctx, limitadorKey, limitador)
	logger.V(1).Info("get limitador", "limitador", limitadorKey, "err", err)
	if err != nil {
		return err
	}

	updated, err := r.reconcileLimitadorBackRef(limitador, rlp)
	if err != nil {
		return err
	}
	// return if limitador is up-to-date
	if rlptools.Equal(rateLimitIndex.ToRateLimits(), limitador.Spec.Limits) && !updated {
		logger.V(1).Info("limitador is up to date, skipping update")
		return nil
	}

	// update limitador
	limitador.Spec.Limits = rateLimitIndex.ToRateLimits()
	err = r.UpdateResource(ctx, limitador)
	logger.V(1).Info("update limitador", "limitador", limitadorKey, "err", err)
	if err != nil {
		return err
	}

	return nil
}

func (r *RateLimitPolicyReconciler) buildRateLimitIndex(ctx context.Context, rlpRefs []client.ObjectKey) (*rlptools.RateLimitIndex, error) {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("buildRateLimitIndex").WithValues("ratelimitpolicies", rlpRefs)

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

		rateLimitIndex.Set(rlpKey, rlptools.LimitadorRateLimitsFromRLP(rlp))
	}

	return rateLimitIndex, nil
}

func (r *RateLimitPolicyReconciler) reconcileLimitadorBackRef(limitador *limitadorv1alpha1.Limitador, policyKey client.Object) (updated bool, err error) {
	policy := client.ObjectKeyFromObject(policyKey)
	objAnnotations := common.ReadAnnotationsFromObject(limitador)
	var refs []client.ObjectKey

	val, ok := objAnnotations[common.RateLimitPoliciesBackRefAnnotation]
	if ok {
		err := json.Unmarshal([]byte(val), &refs)
		if err != nil {
			return false, err
		}
		if common.ContainsObjectKey(refs, policy) {
			r.Logger().V(1).Info("policy references in annotations", "policy", policy)
			return false, nil
		}
	}

	refs = append(refs, policy)
	serialized, err := json.Marshal(refs)
	if err != nil {
		return false, err
	}
	objAnnotations[common.RateLimitPoliciesBackRefAnnotation] = string(serialized)
	limitador.SetAnnotations(objAnnotations)
	return true, nil
}

func (r *RateLimitPolicyReconciler) deleteLimitadorBackReference(ctx context.Context, policy client.Object) error {
	policyKey := client.ObjectKeyFromObject(policy)

	limitadorList, err := r.listLimitadorByNamespace(ctx, policyKey.Namespace)
	if err != nil {
		return err
	}

	updateList, err := rlptools.RemoveRLPLabelsFromLimitadorList(limitadorList, policyKey)
	if err != nil {
		return err
	}

	err = r.updateLimitadorCRs(ctx, updateList)
	if err != nil {
		return err
	}

	return nil
}

func (r *RateLimitPolicyReconciler) updateLimitadorCRs(ctx context.Context, updateList limitadorv1alpha1.LimitadorList) error {
	for index := range updateList.Items {
		err := r.Client().Update(ctx, &updateList.Items[index])
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *RateLimitPolicyReconciler) listLimitadorByNamespace(ctx context.Context, namespace string) (limitadorv1alpha1.LimitadorList, error) {
	limitadorList := limitadorv1alpha1.LimitadorList{}
	listOptions := &client.ListOptions{Namespace: namespace}

	err := r.Client().List(ctx, &limitadorList, listOptions)
	if err != nil {
		return limitadorv1alpha1.LimitadorList{}, err
	}
	return limitadorList, nil
}
