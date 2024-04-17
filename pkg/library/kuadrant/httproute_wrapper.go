package kuadrant

import (
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type HTTPRouteWrapper struct {
	*gatewayapiv1.HTTPRoute
	Referrer
}

func (r HTTPRouteWrapper) PolicyRefs(t *kuadrantgatewayapi.Topology) []string {
	if r.HTTPRoute == nil {
		return make([]string, 0)
	}
	refs := make([]string, 0)
	for _, gw := range t.Gateways() {
		authPolicyRefs, ok := gw.GetAnnotations()[common.AuthPolicyBackRefAnnotation]
		if !ok {
			continue
		}
		refs = append(refs, authPolicyRefs)
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
