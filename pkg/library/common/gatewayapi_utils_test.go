package common

import (
	"fmt"
	"testing"

	"gotest.tools/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

type FakePolicy struct {
	client.Object
	Hosts []string
}

func (p *FakePolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return gatewayapiv1alpha2.PolicyTargetReference{}
}

func (p *FakePolicy) GetWrappedNamespace() gatewayapiv1beta1.Namespace {
	return ""
}

func (p *FakePolicy) GetRulesHostnames() []string {
	return p.Hosts
}

func TestValidateHierarchicalRules(t *testing.T) {
	hostname := gatewayapiv1beta1.Hostname("*.example.com")
	gateway := &gatewayapiv1beta1.Gateway{
		Spec: gatewayapiv1beta1.GatewaySpec{Listeners: []gatewayapiv1beta1.Listener{
			{
				Hostname: &hostname,
			},
		}},
	}
	httpRoute := &gatewayapiv1beta1.HTTPRoute{
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			Hostnames: []gatewayapiv1beta1.Hostname{hostname},
		},
	}

	policy1 := FakePolicy{Hosts: []string{"this.example.com", "*.example.com"}}
	policy2 := FakePolicy{Hosts: []string{"*.z.com"}}

	t.Run("gateway - contains host", func(subT *testing.T) {
		assert.NilError(subT, ValidateHierarchicalRules(&policy1, gateway))
	})

	t.Run("gateway error - host has no match", func(subT *testing.T) {
		expectedError := fmt.Sprintf("rule host (%s) does not follow any hierarchical constraints, "+
			"for the %T to be validated, it must match with at least one of the target network hostnames %+q",
			"*.z.com",
			&policy2,
			[]string{"*.example.com"},
		)
		assert.Error(subT, ValidateHierarchicalRules(&policy2, gateway), expectedError)
	})

	t.Run("gateway - no hosts", func(subT *testing.T) {
		assert.NilError(subT, ValidateHierarchicalRules(&policy1, &gatewayapiv1beta1.Gateway{}))
	})

	t.Run("httpRoute - contains host ", func(subT *testing.T) {
		assert.NilError(subT, ValidateHierarchicalRules(&policy1, httpRoute))
	})
}
