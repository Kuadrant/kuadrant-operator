package v1beta2

// Contains of this file allow the AuthPolicy and RateLimitPolicy to adhere to the machinery.Policy interface

import (
	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	AuthPolicyResource       = GroupVersion.WithResource("authpolicies")
	AuthPolicyGroupKind      = schema.GroupKind{Group: GroupVersion.Group, Kind: "AuthPolicy"}
	RateLimitPolicyResource  = GroupVersion.WithResource("ratelimitpolicies")
	RateLimitPolicyGroupKind = schema.GroupKind{Group: GroupVersion.Group, Kind: "RateLimitPolicy"}
)

var _ machinery.Policy = &AuthPolicy{}

func (ap *AuthPolicy) GetTargetRefs() []machinery.PolicyTargetReference {
	return []machinery.PolicyTargetReference{
		machinery.LocalPolicyTargetReference{
			LocalPolicyTargetReference: ap.Spec.TargetRef,
			PolicyNamespace:            ap.Namespace,
		},
	}
}

func (ap *AuthPolicy) GetMergeStrategy() machinery.MergeStrategy {
	return func(policy machinery.Policy, _ machinery.Policy) machinery.Policy {
		return policy
	}
}

func (ap *AuthPolicy) Merge(other machinery.Policy) machinery.Policy {
	return other
}

func (ap *AuthPolicy) GetLocator() string {
	return machinery.LocatorFromObject(ap)
}

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
