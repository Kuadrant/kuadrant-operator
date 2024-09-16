package v1alpha1

// Contains of this file allow the DNSPolicy and TLSPolicy to adhere to the machinery.Policy interface

import (
	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	DNSPoliciesResource = GroupVersion.WithResource("dnspolicies")
	DNSPolicyKind       = schema.GroupKind{Group: GroupVersion.Group, Kind: "DNSPolicy"}
	TLSPoliciesResource = GroupVersion.WithResource("tlspolicies")
	TLSPolicyKind       = schema.GroupKind{Group: GroupVersion.Group, Kind: "TLSPolicy"}
)

var _ machinery.Policy = &DNSPolicy{}

func (p *DNSPolicy) GetTargetRefs() []machinery.PolicyTargetReference {
	return []machinery.PolicyTargetReference{
		machinery.LocalPolicyTargetReference{
			LocalPolicyTargetReference: p.Spec.TargetRef,
			PolicyNamespace:            p.Namespace,
		},
	}
}

func (p *DNSPolicy) GetMergeStrategy() machinery.MergeStrategy {
	return func(policy machinery.Policy, _ machinery.Policy) machinery.Policy {
		return policy
	}
}

func (p *DNSPolicy) Merge(other machinery.Policy) machinery.Policy {
	return other
}

func (p *DNSPolicy) GetLocator() string {
	return machinery.LocatorFromObject(p)
}

var _ machinery.Policy = &TLSPolicy{}

func (p *TLSPolicy) GetTargetRefs() []machinery.PolicyTargetReference {
	return []machinery.PolicyTargetReference{
		machinery.LocalPolicyTargetReference{
			LocalPolicyTargetReference: p.Spec.TargetRef,
			PolicyNamespace:            p.Namespace,
		},
	}
}

func (p *TLSPolicy) GetMergeStrategy() machinery.MergeStrategy {
	return func(policy machinery.Policy, _ machinery.Policy) machinery.Policy {
		return policy
	}
}

func (p *TLSPolicy) Merge(other machinery.Policy) machinery.Policy {
	return other
}

func (p *TLSPolicy) GetLocator() string {
	return machinery.LocatorFromObject(p)
}
