//go:build unit

package kuadrant

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

var _ Referrer = &PolicyKindStub{}

type PolicyKindStub struct{}

func (tpk *PolicyKindStub) Kind() string {
	return "TestPolicy"
}

func (tpk *PolicyKindStub) BackReferenceAnnotationName() string {
	return "kuadrant.io/testpolicies"
}

func (tpk *PolicyKindStub) DirectReferenceAnnotationName() string {
	return "kuadrant.io/testpolicy"
}

const (
	NS = "nsA"
)

type FakePolicy struct {
	client.Object
	Hosts     []string
	targetRef gatewayapiv1alpha2.PolicyTargetReference
}

func (p *FakePolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return p.targetRef
}

func (p *FakePolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &FakePolicyStatus{}
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

func (p *FakePolicy) List(ctx context.Context, c client.Client, namespace string) []kuadrantgatewayapi.Policy {
	return nil
}

func (p *FakePolicy) BackReferenceAnnotationName() string {
	return ""
}

func (p *FakePolicy) DirectReferenceAnnotationName() string {
	return ""
}

func (_ *FakePolicy) PolicyClass() kuadrantgatewayapi.PolicyClass {
	return kuadrantgatewayapi.DirectPolicy
}

type FakePolicyStatus struct{}

func (s *FakePolicyStatus) GetConditions() []metav1.Condition {
	return nil
}
