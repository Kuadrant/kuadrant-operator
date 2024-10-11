//go:build unit

package istio

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestPolicyTargetRefFromGateway(t *testing.T) {
	gateway := &gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-gw",
		},
	}

	ref := PolicyTargetRefFromGateway(gateway)
	if ref == nil || ref.Group != "gateway.networking.k8s.io" || ref.Kind != "Gateway" || ref.Name != "my-gw" {
		t.Error("should have built the istio policy target reference from the gateway")
	}
}
