package common

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "cool-namespace",
			Name:      "cool-gateway",
		},
		Spec: gatewayapiv1beta1.GatewaySpec{Listeners: []gatewayapiv1beta1.Listener{
			{
				Hostname: &hostname,
			},
		}},
	}
	policy1 := FakePolicy{Hosts: []string{"this.example.com", "*.example.com"}}
	policy2 := FakePolicy{Hosts: []string{"*.z.com"}}

	if err := ValidateHierarchicalRules(&policy1, gateway); err != nil {
		t.Fatal(err)
	}

	expectedError := fmt.Errorf(
		"rule host (%s) does not follow any hierarchical constraints, "+
			"for the %T to be validated, it must match with at least one of the target network hostnames %+q",
		"*.z.com",
		&policy2,
		[]string{"*.example.com"},
	)

	if err := ValidateHierarchicalRules(&policy2, gateway); err.Error() != expectedError.Error() {
		t.Fatal("the error message does not match the expected error one", expectedError.Error(), err.Error())
	}
}
