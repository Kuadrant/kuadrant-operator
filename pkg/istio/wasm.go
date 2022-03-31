package istio

import (
	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
)

// wasm-shim API structs
type Rule struct {
	Operations []*apimv1alpha1.Operation       `json:"operations"`
	Actions    []*apimv1alpha1.ActionSpecifier `json:"actions"`
}

type PluginPolicy struct {
	Rules           []*Rule                         `json:"rules"`
	GlobalActions   []*apimv1alpha1.ActionSpecifier `json:"global_actions,omitempty"`
	UpstreamCluster string                          `json:"upstream_cluster"`
	Domain          string                          `json:"domain"`
}

type PluginConfig struct {
	FailureModeDeny bool                    `json:"failure_mode_deny"`
	PluginPolicies  map[string]PluginPolicy `json:"ratelimitpolicies"`
}

func PluginPolicyFromRateLimitPolicy(rlp *apimv1alpha1.RateLimitPolicy, pluginStage apimv1alpha1.RateLimitStage) *PluginPolicy {
	pluginPolicy := &PluginPolicy{}

	// Filter through global ratelimits
	for _, ratelimit := range rlp.Spec.RateLimits {
		if ratelimit.Stage == pluginStage || ratelimit.Stage == apimv1alpha1.RateLimitStageBOTH {
			pluginPolicy.GlobalActions = append(pluginPolicy.GlobalActions, ratelimit.Actions...)
		}
	}

	// Filter through route-level ratelimits
	for _, rlpRule := range rlp.Spec.Rules {
		actions := []*apimv1alpha1.ActionSpecifier{}
		for _, ratelimit := range rlpRule.RateLimits {
			if ratelimit.Stage == pluginStage || ratelimit.Stage == apimv1alpha1.RateLimitStageBOTH {
				actions = append(actions, ratelimit.Actions...)
			}
		}

		if len(rlpRule.Operations) > 0 {
			pluginRule := &Rule{
				Operations: rlpRule.Operations,
				Actions:    actions,
			}
			pluginPolicy.Rules = append(pluginPolicy.Rules, pluginRule)
		}
	}

	// Pass the domain from RateLimitPolicy to PluginPolicy
	pluginPolicy.Domain = rlp.Spec.Domain

	pluginPolicy.UpstreamCluster = PatchedLimitadorClusterName
	return pluginPolicy
}
