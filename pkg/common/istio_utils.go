package common

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	istiocommon "istio.io/api/type/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func IstioWorkloadSelectorFromGateway(ctx context.Context, k8sClient client.Client, gateway *gatewayapiv1.Gateway) *istiocommon.WorkloadSelector {
	logger, _ := logr.FromContext(ctx)
	gatewayWorkloadSelector, err := GetGatewayWorkloadSelector(ctx, k8sClient, gateway)
	if err != nil {
		logger.V(1).Info("failed to build Istio WorkloadSelector from Gateway service - falling back to Gateway labels")
		gatewayWorkloadSelector = gateway.Labels
	}
	return &istiocommon.WorkloadSelector{
		MatchLabels: gatewayWorkloadSelector,
	}
}

func RateLimitingWASMPluginName(gw *gatewayapiv1.Gateway) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      fmt.Sprintf("kuadrant-%s", gw.Name),
		Namespace: gw.Namespace,
	}
}
