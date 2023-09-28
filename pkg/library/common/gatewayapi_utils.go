package common

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// ValidateHierarchicalRules returns error if the policy rules hostnames fail to match the target network hosts
func ValidateHierarchicalRules(policy KuadrantPolicy, targetNetworkObject client.Object) error {
	targetHostnames, err := TargetHostnames(targetNetworkObject)
	if err != nil {
		return err
	}

	if valid, invalidHost := ValidSubdomains(targetHostnames, policy.GetRulesHostnames()); !valid {
		return fmt.Errorf(
			"rule host (%s) does not follow any hierarchical constraints, "+
				"for the %T to be validated, it must match with at least one of the target network hostnames %+q",
			invalidHost,
			policy,
			targetHostnames,
		)
	}

	return nil
}

// TargetHostnames returns an array of hostnames coming from the network object (HTTPRoute, Gateway)
func TargetHostnames(targetNetworkObject client.Object) ([]string, error) {
	hosts := make([]string, 0)
	switch obj := targetNetworkObject.(type) {
	case *gatewayapiv1beta1.HTTPRoute:
		for _, hostname := range obj.Spec.Hostnames {
			hosts = append(hosts, string(hostname))
		}
	case *gatewayapiv1beta1.Gateway:
		for idx := range obj.Spec.Listeners {
			if obj.Spec.Listeners[idx].Hostname != nil {
				hosts = append(hosts, string(*obj.Spec.Listeners[idx].Hostname))
			}
		}
	}

	if len(hosts) == 0 {
		hosts = append(hosts, "*")
	}

	return hosts, nil
}
