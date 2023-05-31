//go:build unit

package v1beta2

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func testBuildBasicRLP(name string, kind gatewayapiv1alpha2.Kind) *RateLimitPolicy {
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
				Group: gatewayapiv1alpha2.Group("gateway.networking.k8s.io"),
				Kind:  kind,
				Name:  "some-name",
			},
		},
	}
}

func testBuildBasicGatewayRLP(name string) *RateLimitPolicy {
	return testBuildBasicRLP(name, gatewayapiv1alpha2.Kind("Gateway"))
}

func testBuildBasicHTTPRouteRLP(name string) *RateLimitPolicy {
	return testBuildBasicRLP(name, gatewayapiv1alpha2.Kind("HTTPRoute"))
}

// TestRateLimitPolicyValidation calls rlp.Validate()
// for a valid return value.
func TestRateLimitPolicyValidation(t *testing.T) {
	// valid httproute rlp
	name := "httproute-a"
	rlp := testBuildBasicHTTPRouteRLP(name)
	err := rlp.Validate()
	if err != nil {
		t.Fatalf(`rlp.Validate() returned error "%v", wanted nil`, err)
	}

	// valid gateway rlp
	name = "gateway-a"
	rlp = testBuildBasicGatewayRLP(name)
	err = rlp.Validate()
	if err != nil {
		t.Fatalf(`rlp.Validate() returned error "%v", wanted nil`, err)
	}

	// invalid group
	rlp = testBuildBasicHTTPRouteRLP(name)
	rlp.Spec.TargetRef.Group = gatewayapiv1alpha2.Group("foo.example.com")
	err = rlp.Validate()
	if err == nil {
		t.Fatal(`rlp.Validate() did not return error and should`)
	}
	if !strings.Contains(err.Error(), "invalid targetRef.Group") {
		t.Fatalf(`rlp.Validate() did not return expected error. Instead: %v`, err)
	}

	// invalid kind
	rlp = testBuildBasicHTTPRouteRLP(name)
	rlp.Spec.TargetRef.Kind = gatewayapiv1alpha2.Kind("Foo")
	err = rlp.Validate()
	if err == nil {
		t.Fatal(`rlp.Validate() did not return error and should`)
	}
	if !strings.Contains(err.Error(), "invalid targetRef.Kind") {
		t.Fatalf(`rlp.Validate() did not return expected error. Instead: %v`, err)
	}

	// Different namespace
	rlp = testBuildBasicHTTPRouteRLP(name)
	otherNS := gatewayapiv1alpha2.Namespace(rlp.GetNamespace() + "other")
	rlp.Spec.TargetRef.Namespace = &otherNS
	err = rlp.Validate()
	if err == nil {
		t.Fatal(`rlp.Validate() did not return error and should`)
	}
	if !strings.Contains(err.Error(), "invalid targetRef.Namespace") {
		t.Fatalf(`rlp.Validate() did not return expected error. Instead: %v`, err)
	}
}
