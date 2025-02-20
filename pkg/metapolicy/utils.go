package metapolicy

import (
	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/utils"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
)

func LinkPlanPolicyToRateLimitPolicy(objs controller.Store) machinery.LinkFunc {
	planPolicies := lo.Map(objs.FilterByGroupKind(kuadrantv1alpha1.PlanPolicyGroupKind), controller.ObjectAs[*kuadrantv1alpha1.PlanPolicy])

	return machinery.LinkFunc{
		From: kuadrantv1alpha1.PlanPolicyGroupKind,
		To:   kuadrantv1.RateLimitPolicyGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			if rlp, ok := child.(*kuadrantv1.RateLimitPolicy); ok {
				return lo.FilterMap(planPolicies, func(planPolicy *kuadrantv1alpha1.PlanPolicy, _ int) (machinery.Object, bool) {
					return planPolicy, utils.IsOwnedBy(rlp, planPolicy)
				})
			}
			return nil
		},
	}
}
