package controllers

import (
	"testing"

	"gotest.tools/assert"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestGetGatewayControllerNames(t *testing.T) {
	t.Setenv("ISTIO_GATEWAY_CONTROLLER_NAMES", "istio-alpha1 , istio-alpha2  ")

	istioGwCtrlNames := getGatewayControllerNames("ISTIO_GATEWAY_CONTROLLER_NAMES", "default-istio")
	envoyGwGwCtrlNames := getGatewayControllerNames("ENVOY_GATEWAY_GATEWAY_CONTROLLER_NAMES", "default-envoy")

	assert.Equal(t, len(istioGwCtrlNames), 3)
	assert.Equal(t, istioGwCtrlNames[0], gatewayapiv1.GatewayController("istio-alpha1"))
	assert.Equal(t, istioGwCtrlNames[1], gatewayapiv1.GatewayController("istio-alpha2"))
	assert.Equal(t, istioGwCtrlNames[2], gatewayapiv1.GatewayController("default-istio"))

	assert.Equal(t, len(envoyGwGwCtrlNames), 1)
	assert.Equal(t, envoyGwGwCtrlNames[0], gatewayapiv1.GatewayController("default-envoy"))
}

func TestDefaultGatewayControllerNames(t *testing.T) {
	istioGatewayControllerNames = []gatewayapiv1.GatewayController{"istio-alpha1"}
	envoyGatewayGatewayControllerNames = []gatewayapiv1.GatewayController{"envoy-alpha1"}

	assert.Equal(t, defaultGatewayControllerName("istio-alpha1"), gatewayapiv1.GatewayController("istio.io/gateway-controller"))
	assert.Equal(t, defaultGatewayControllerName("envoy-alpha1"), gatewayapiv1.GatewayController("gateway.envoyproxy.io/gatewayclass-controller"))
	assert.Equal(t, defaultGatewayControllerName("envoy-alpha2"), gatewayapiv1.GatewayController("Unknown"))
}
