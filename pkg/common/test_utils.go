package common

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type FakePolicy struct {
	client.Object
	Hosts     []string
	targetRef gatewayapiv1alpha2.PolicyTargetReference
}

func (p *FakePolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return p.targetRef
}

func (p *FakePolicy) GetWrappedNamespace() gatewayapiv1.Namespace {
	return gatewayapiv1.Namespace(p.GetNamespace())
}

func (p *FakePolicy) GetRulesHostnames() []string {
	return p.Hosts
}

func (p *FakePolicy) Kind() string {
	return "FakePolicy"
}
