//go:build unit

package common

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

var (
	_ KuadrantPolicy = &TestPolicy{}
)

type TestPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`
}

func (p *TestPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return p.TargetRef
}

func (p *TestPolicy) GetWrappedNamespace() gatewayapiv1beta1.Namespace {
	return gatewayapiv1beta1.Namespace(p.Namespace)
}

func (p *TestPolicy) GetRulesHostnames() []string {
	return nil
}

func (p *TestPolicy) Kind() string {
	return p.TypeMeta.Kind
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
