package controllers

import (
	"fmt"
	"net/url"
	"strconv"
	"sync"

	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/kuadrant/kuadrant-operator/internal/extension"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

const (
	tracingObjectLabelKey = "kuadrant.io/tracing"

	// State keys
	StateEnvoyGatewayTracingClustersModified = "EnvoyGatewayTracingClustersModified"
	StateIstioTracingClustersModified        = "IstioTracingClustersModified"
)

// TracingObjectLabels returns labels for tracing-related objects
func TracingObjectLabels() labels.Set {
	m := KuadrantManagedObjectLabels()
	m[tracingObjectLabelKey] = "true"
	return m
}

// TracingClusterName returns the name for the tracing cluster EnvoyFilter/EnvoyPatchPolicy
func TracingClusterName(gatewayName string) string {
	return fmt.Sprintf("kuadrant-tracing-%s", gatewayName)
}

// tracingClusterPatch returns the Envoy cluster configuration for the tracing service
func tracingClusterPatch(host string, port int, mTLS bool) map[string]any {
	return buildClusterPatch(kuadrant.KuadrantTracingClusterName, host, port, mTLS)
}

// parseTracingEndpoint parses a tracing endpoint URL and returns host and port
func parseTracingEndpoint(endpoint string) (string, int, error) {
	parsedURL, err := url.Parse(endpoint)
	if err != nil {
		return "", 0, fmt.Errorf("invalid URL: %w", err)
	}

	host := parsedURL.Hostname()
	if host == "" {
		return "", 0, fmt.Errorf("no host found in URL")
	}

	// Default ports based on scheme
	var port int
	portStr := parsedURL.Port()
	if portStr != "" {
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return "", 0, fmt.Errorf("invalid port: %w", err)
		}
	} else {
		// Use common default ports for tracing
		switch parsedURL.Scheme {
		case "https":
			port = 443
		case "http":
			port = 80
		default:
			// For OTLP gRPC (common default)
			port = 4317
		}
	}

	return host, port, nil
}

// targetIsInEffectivePolicyPath checks if a gateway is in the path of any effective
// core policies (AuthPolicy, RateLimitPolicy, TokenRateLimitPolicy) or has extension
// policies with pipeline actions targeting it.
func targetIsInEffectivePolicyPath(gateway *machinery.Gateway, topology *machinery.Topology, state *sync.Map) bool {
	locator := gateway.GetLocator()

	if effectiveAuthPolicies, ok := state.Load(StateEffectiveAuthPolicies); ok {
		for _, policy := range effectiveAuthPolicies.(EffectiveAuthPolicies) {
			for _, targetable := range policy.Path {
				if targetable.GetLocator() == locator {
					return true
				}
			}
		}
	}

	if effectiveRateLimitPolicies, ok := state.Load(StateEffectiveRateLimitPolicies); ok {
		for _, policy := range effectiveRateLimitPolicies.(EffectiveRateLimitPolicies) {
			for _, targetable := range policy.Path {
				if targetable.GetLocator() == locator {
					return true
				}
			}
		}
	}

	if effectiveTokenRateLimitPolicies, ok := state.Load(StateEffectiveTokenRateLimitPolicies); ok {
		for _, policy := range effectiveTokenRateLimitPolicies.(EffectiveTokenRateLimitPolicies) {
			for _, targetable := range policy.Path {
				if targetable.GetLocator() == locator {
					return true
				}
			}
		}
	}

	if extension.HasPipelineForTarget(gateway, topology) {
		return true
	}

	return false
}
