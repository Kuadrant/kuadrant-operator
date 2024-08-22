package v1beta2

// Contains of this file allow the AuthPolicy and RateLimitPolicy to adhere to the machinery.Policy interface

import (
	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	AuthPoliciesResource      = GroupVersion.WithResource("authpolicies")
	AuthPolicyKind            = schema.GroupKind{Group: GroupVersion.Group, Kind: "AuthPolicy"}
	RateLimitPoliciesResource = GroupVersion.WithResource("ratelimitpolicies")
	RateLimitPolicyKind       = schema.GroupKind{Group: GroupVersion.Group, Kind: "RateLimitPolicy"}
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

func (ap *AuthPolicy) GetURL() string {
	return machinery.UrlFromObject(ap)
}

func (ap *AuthPolicySpec) CommonSpec() *AuthPolicyCommonSpec {
	if ap.Defaults != nil {
		return ap.Defaults
	}

	if ap.Overrides != nil {
		return ap.Overrides
	}

	return &ap.AuthPolicyCommonSpec
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

func (r *RateLimitPolicy) GetURL() string {
	return machinery.UrlFromObject(r)
}
