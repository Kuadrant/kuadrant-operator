package mappers

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

type LimitadorToRateLimitPoliciesEventMapper struct {
	opts MapperOptions
}

func NewLimitadorToRateLimitPoliciesEventMapper(o ...MapperOption) *LimitadorToRateLimitPoliciesEventMapper {
	return &LimitadorToRateLimitPoliciesEventMapper{opts: Apply(o...)}
}

// Map limitador to RLP requests
func (m *LimitadorToRateLimitPoliciesEventMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	kuadrantList := &kuadrantv1beta1.KuadrantList{}
	if err := m.opts.Client.List(ctx, kuadrantList, &client.ListOptions{Namespace: obj.GetNamespace()}); err != nil {
		m.opts.Logger.V(1).Error(err, "failed to list kuadrant in namespace", "namespace", obj.GetNamespace())
		return []reconcile.Request{}
	}

	// No kuadrant in limitador namespace - skipping as it's not managed by kuadrant
	if len(kuadrantList.Items) == 0 {
		m.opts.Logger.V(1).Info("no kuadrant resources found in limitador namespace, skipping")
		return []reconcile.Request{}
	}

	// List all RLPs as there's been an event from Limitador which may affect RLP status
	rlpList := &kuadrantv1beta2.RateLimitPolicyList{}
	if err := m.opts.Client.List(ctx, rlpList); err != nil {
		m.opts.Logger.V(1).Error(err, "failed to list RLPs")
		return []reconcile.Request{}
	}

	return utils.Map(rlpList.Items, func(policy kuadrantv1beta2.RateLimitPolicy) reconcile.Request {
		return reconcile.Request{NamespacedName: types.NamespacedName{Namespace: policy.GetNamespace(), Name: policy.GetName()}}
	})
}
