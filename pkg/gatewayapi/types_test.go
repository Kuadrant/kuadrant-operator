//go:build unit

package gatewayapi

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

var (
	_ Policy       = &TestPolicy{}
	_ PolicyStatus = &FakePolicyStatus{}
)

type TestPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReference `json:"targetRef"`
	Status    FakePolicyStatus                              `json:"status"`
}

func (p *TestPolicy) Kind() string {
	return "FakePolicy"
}

func (p *TestPolicy) List(ctx context.Context, c client.Client, namespace string) []Policy {
	return nil
}

func (p *TestPolicy) GetTargetRef() gatewayapiv1alpha2.LocalPolicyTargetReference {
	return p.TargetRef
}

func (p *TestPolicy) GetStatus() PolicyStatus {
	return &p.Status
}

func (p *TestPolicy) DeepCopyObject() runtime.Object {
	if c := p.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (p *TestPolicy) DeepCopy() *TestPolicy {
	if p == nil {
		return nil
	}
	out := new(TestPolicy)
	p.DeepCopyInto(out)
	return out
}

func (p *TestPolicy) DeepCopyInto(out *TestPolicy) {
	*out = *p
	out.TypeMeta = p.TypeMeta
	p.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	p.TargetRef.DeepCopyInto(&out.TargetRef)
}

type FakePolicyStatus struct {
	Conditions []metav1.Condition
}

func (s *FakePolicyStatus) GetConditions() []metav1.Condition {
	return s.Conditions
}
