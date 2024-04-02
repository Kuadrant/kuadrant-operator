//go:build unit

package v1beta2

import (
	"strings"
	"testing"

	"gotest.tools/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

func testBuildBasicRLP(name string, kind gatewayapiv1.Kind, mutateFn func(*RateLimitPolicy)) *RateLimitPolicy {
	p := &RateLimitPolicy{
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
				Group: gatewayapiv1.GroupName,
				Kind:  kind,
				Name:  "some-name",
			},
		},
	}

	if mutateFn != nil {
		mutateFn(p)
	}

	return p
}

func testBuildBasicHTTPRouteRLP(name string, mutateFn func(*RateLimitPolicy)) *RateLimitPolicy {
	return testBuildBasicRLP(name, "HTTPRoute", mutateFn)
}

// TestRateLimitPolicyValidation calls rlp.Validate()
// for a valid return value.
func TestRateLimitPolicyValidation(t *testing.T) {
	name := "httproute-a"

	t.Run("Invalid - Different namespace", func(subT *testing.T) {
		rlp := testBuildBasicHTTPRouteRLP(name, func(policy *RateLimitPolicy) {
			otherNS := gatewayapiv1.Namespace(policy.GetNamespace() + "other")
			policy.Spec.TargetRef.Namespace = &otherNS
		})
		err := rlp.Validate()
		if err == nil {
			subT.Fatal(`rlp.Validate() did not return error and should`)
		}
		if !strings.Contains(err.Error(), "invalid targetRef.Namespace") {
			subT.Fatalf(`rlp.Validate() did not return expected error. Instead: %v`, err)
		}
	})
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
	_, ok := result[0].(kuadrant.Policy)
	if !ok {
		t.Errorf("Expected item to be a Policy")
	}
}

func TestRateLimitPolicy_GetLimits(t *testing.T) {
	const name = "policy"
	var (
		defaultLimits = map[string]Limit{
			"default": {
				Rates: []Rate{{Limit: 10, Duration: 1, Unit: "seconds"}},
			},
		}
		implicitLimits = map[string]Limit{
			"implicit": {
				Rates: []Rate{{Limit: 20, Duration: 2, Unit: "minutes"}},
			},
		}
	)

	t.Run("No limits defined", func(subT *testing.T) {
		r := testBuildBasicHTTPRouteRLP(name, nil)
		assert.DeepEqual(subT, r.Spec.CommonSpec().Limits, map[string]Limit(nil))
	})
	t.Run("Defaults defined", func(subT *testing.T) {
		r := testBuildBasicHTTPRouteRLP(name, func(policy *RateLimitPolicy) {
			policy.Spec.Defaults = &RateLimitPolicyCommonSpec{
				Limits: defaultLimits,
			}
		})
		assert.DeepEqual(subT, r.Spec.CommonSpec().Limits, defaultLimits)
	})
	t.Run("Implicit rules defined", func(subT *testing.T) {
		r := testBuildBasicHTTPRouteRLP(name, func(policy *RateLimitPolicy) {
			policy.Spec.Limits = implicitLimits
		})
		assert.DeepEqual(subT, r.Spec.CommonSpec().Limits, implicitLimits)
	})
	t.Run("Default rules takes precedence over implicit rules if validation is somehow bypassed", func(subT *testing.T) {
		r := testBuildBasicHTTPRouteRLP(name, func(policy *RateLimitPolicy) {
			policy.Spec.Defaults = &RateLimitPolicyCommonSpec{
				Limits: defaultLimits,
			}
			policy.Spec.Limits = implicitLimits
		})
		assert.DeepEqual(subT, r.Spec.CommonSpec().Limits, defaultLimits)
	})
}
