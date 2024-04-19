package kuadrant

import (
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type HTTPRouteWrapper struct {
	*gatewayapiv1.HTTPRoute
	kuadrantgatewayapi.Policy
}

func (r HTTPRouteWrapper) PolicyRefs(t *kuadrantgatewayapi.Topology) []client.ObjectKey {
	if r.HTTPRoute == nil {
		return make([]client.ObjectKey, 0)
	}
	refs := make([]client.ObjectKey, 0)
	for _, gw := range t.Gateways() {
		affectedPolicies := utils.Filter(gw.AttachedPolicies(), func(policy kuadrantgatewayapi.Policy) bool {
			return kuadrantgatewayapi.IsTargetRefGateway(policy.GetTargetRef()) && r.Policy.GetUID() != policy.GetUID()
		})

		policyKeys := utils.Map(affectedPolicies, func(policy kuadrantgatewayapi.Policy) client.ObjectKey {
			return client.ObjectKeyFromObject(policy)
		})

		refs = append(refs, policyKeys...)
	}
	return refs
}

// HTTPRouteWrapperList is a list of HTTPRouteWrapper that implements sort.interface
// impl: sort.interface
type HTTPRouteWrapperList []HTTPRouteWrapper

func (l HTTPRouteWrapperList) Len() int { return len(l) }

func (l HTTPRouteWrapperList) Less(i, j int) bool {
	return l[i].CreationTimestamp.Before(&l[j].CreationTimestamp)
}

func (l HTTPRouteWrapperList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}
