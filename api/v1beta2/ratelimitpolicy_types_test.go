//go:build unit

package v1beta2

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

func testBuildBasicRLP(name string, kind gatewayapiv1.Kind) *RateLimitPolicy {
	return &RateLimitPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RateLimitPolicy",
			APIVersion: GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "testNS",
		},
		Spec: RateLimitPolicySpec{
			TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
				Group: "gateway.networking.k8s.io",
				Kind:  kind,
				Name:  "some-name",
			},
		},
	}
}

func testBuildBasicGatewayRLP(name string) *RateLimitPolicy {
	return testBuildBasicRLP(name, gatewayapiv1.Kind("Gateway"))
}

func testBuildBasicHTTPRouteRLP(name string) *RateLimitPolicy {
	return testBuildBasicRLP(name, gatewayapiv1.Kind("HTTPRoute"))
}

// TestRateLimitPolicyValidation calls rlp.Validate()
// for a valid return value.
func TestRateLimitPolicyValidation(t *testing.T) {
	name := "httproute-a"

	// Different namespace
	rlp := testBuildBasicHTTPRouteRLP(name)
	otherNS := gatewayapiv1.Namespace(rlp.GetNamespace() + "other")
	rlp.Spec.TargetRef.Namespace = &otherNS
	err := rlp.Validate()
	if err == nil {
		t.Fatal(`rlp.Validate() did not return error and should`)
	}
	if !strings.Contains(err.Error(), "invalid targetRef.Namespace") {
		t.Fatalf(`rlp.Validate() did not return expected error. Instead: %v`, err)
	}
}

func TestRateLimitPolicyListGetItems(t *testing.T) {
	list := &RateLimitPolicyList{}
	if len(list.GetItems()) != 0 {
		t.Errorf("Expected empty list of items")
	}
	policy := RateLimitPolicy{}
	list.Items = []RateLimitPolicy{policy}
	result := list.GetItems()
	if len(result) != 1 {
		t.Errorf("Expected 1 item, got %d", len(result))
	}
	_, ok := result[0].(common.KuadrantPolicy)
	if !ok {
		t.Errorf("Expected item to be a KuadrantPolicy")
	}
}
