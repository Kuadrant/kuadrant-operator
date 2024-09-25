package v1beta3

// Contains of this file allow the AuthPolicy and RateLimitPolicy to adhere to the machinery.Policy interface

import (
	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	RateLimitPoliciesResource = GroupVersion.WithResource("ratelimitpolicies")
	RateLimitPolicyKind       = schema.GroupKind{Group: GroupVersion.Group, Kind: "RateLimitPolicy"}
)

var _ machinery.Policy = &RateLimitPolicy{}

func (r *RateLimitPolicy) GetTargetRefs() []machinery.PolicyTargetReference {
	return []machinery.PolicyTargetReference{
		machinery.LocalPolicyTargetReference{
			LocalPolicyTargetReference: r.Spec.TargetRef,
			PolicyNamespace:            r.Namespace,
		},
	}
}

func (r *RateLimitPolicy) GetMergeStrategy() machinery.MergeStrategy {
	return func(policy machinery.Policy, _ machinery.Policy) machinery.Policy {
		return policy
	}
}

func (r *RateLimitPolicy) Merge(other machinery.Policy) machinery.Policy {
	return other
}

func (r *RateLimitPolicy) GetLocator() string {
	return machinery.LocatorFromObject(r)
}
