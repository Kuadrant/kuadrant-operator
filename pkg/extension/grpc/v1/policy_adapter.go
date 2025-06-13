package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// PolicyAdapter wraps the protobuf Policy to implement types.Policy interface
type PolicyAdapter struct {
	*Policy
}

func NewPolicyAdapter(p *Policy) *PolicyAdapter {
	return &PolicyAdapter{Policy: p}
}

func (p *PolicyAdapter) GetName() string {
	if p.Policy == nil || p.Metadata == nil {
		return ""
	}
	return p.Metadata.Name
}

func (p *PolicyAdapter) GetNamespace() string {
	if p.Policy == nil || p.Metadata == nil {
		return ""
	}
	return p.Metadata.Namespace
}

func (p *PolicyAdapter) GetObjectKind() schema.ObjectKind {
	if p.Policy == nil || p.Metadata == nil {
		return &policyObjectKind{}
	}
	return &policyObjectKind{
		group: p.Metadata.Group,
		kind:  p.Metadata.Kind,
	}
}

func (p *PolicyAdapter) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	if p.Policy == nil {
		return nil
	}

	refs := make([]gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName, len(p.TargetRefs))
	for i, ref := range p.TargetRefs {
		refs[i] = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: gatewayapiv1alpha2.Group(ref.Group),
				Kind:  gatewayapiv1alpha2.Kind(ref.Kind),
				Name:  gatewayapiv1alpha2.ObjectName(ref.Name),
			},
		}
		if ref.SectionName != "" {
			refs[i].SectionName = (*gatewayapiv1alpha2.SectionName)(&ref.SectionName)
		}
	}
	return refs
}

type policyObjectKind struct {
	group string
	kind  string
}

func (p *policyObjectKind) SetGroupVersionKind(gvk schema.GroupVersionKind) {
	p.group = gvk.Group
	p.kind = gvk.Kind
}

func (p *policyObjectKind) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group: p.group,
		Kind:  p.kind,
	}
}
