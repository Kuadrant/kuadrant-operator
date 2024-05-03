package istio

import (
	"context"

	"github.com/go-logr/logr"
	istiocommon "istio.io/api/type/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

func WorkloadSelectorFromGateway(ctx context.Context, k8sClient client.Client, gateway *gatewayapiv1.Gateway) *istiocommon.WorkloadSelector {
	logger, _ := logr.FromContext(ctx)
	gatewayWorkloadSelector, err := kuadrantgatewayapi.GetGatewayWorkloadSelector(ctx, k8sClient, gateway)
	if err != nil {
		logger.V(1).Info("failed to build Istio WorkloadSelector from Gateway service - falling back to Gateway labels")
		gatewayWorkloadSelector = gateway.Labels
	}
	return &istiocommon.WorkloadSelector{
		MatchLabels: gatewayWorkloadSelector,
	}
}

func PolicyTargetRefFromGateway(gateway *gatewayapiv1.Gateway) *istiocommon.PolicyTargetReference {
	return &istiocommon.PolicyTargetReference{
		Group: gatewayapiv1.GroupName,
		Kind:  "Gateway",
		Name:  gateway.Name,
	}
}
