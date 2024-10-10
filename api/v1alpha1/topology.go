package v1alpha1

// Contains of this file allow the DNSPolicy to adhere to the machinery.Policy interface

import (
	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	DNSPoliciesResource = GroupVersion.WithResource("dnspolicies")
	DNSPolicyGroupKind  = schema.GroupKind{Group: GroupVersion.Group, Kind: "DNSPolicy"}
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
