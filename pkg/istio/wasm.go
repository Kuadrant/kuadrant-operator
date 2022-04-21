package istio

import (
	"reflect"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// wasm-shim API structs
type Rule struct {
	Operations []*apimv1alpha1.Operation       `json:"operations"`
	Actions    []*apimv1alpha1.ActionSpecifier `json:"actions"`
}

type PluginPolicy struct {
	Hosts           []string                        `json:"hosts,omitempty"`
	Rules           []*Rule                         `json:"rules"`
	GlobalActions   []*apimv1alpha1.ActionSpecifier `json:"global_actions,omitempty"`
	UpstreamCluster string                          `json:"upstream_cluster"`
	Domain          string                          `json:"domain"`
}

type PluginConfig struct {
	FailureModeDeny bool                    `json:"failure_mode_deny"`
	PluginPolicies  map[string]PluginPolicy `json:"ratelimitpolicies"`
}

func PluginPolicyFromRateLimitPolicy(rlp *apimv1alpha1.RateLimitPolicy, pluginStage apimv1alpha1.RateLimitStage, hosts []gatewayapiv1alpha2.Hostname) *PluginPolicy {
	pluginPolicy := &PluginPolicy{
		Hosts: []string{},
	}

	for _, host := range hosts {
		pluginPolicy.Hosts = append(pluginPolicy.Hosts, string(host))
	}

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

		if len(rlpRule.Operations) > 0 && len(actions) > 0 {
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

func MergeMapStringPluginPolicy(modified *bool, existing *map[string]PluginPolicy, desired map[string]PluginPolicy) {
	if *existing == nil {
		*existing = map[string]PluginPolicy{}
	}

	for desiredKey, desiredPluginPolicy := range desired {
		existingPluginPolicy, ok := (*existing)[desiredKey]
		if !ok || !reflect.DeepEqual(existingPluginPolicy, desiredPluginPolicy) {
			(*existing)[desiredKey] = desiredPluginPolicy
			*modified = true
		}
	}
}
