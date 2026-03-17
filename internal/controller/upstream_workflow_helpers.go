package controllers

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/kuadrant/kuadrant-operator/internal/extension"
)

const upstreamObjectLabelKey = "kuadrant.io/upstream"

func UpstreamObjectLabels() labels.Set {
	m := KuadrantManagedObjectLabels()
	m[upstreamObjectLabelKey] = "true"
	return m
}

func UpstreamClusterName(gatewayName string) string {
	return fmt.Sprintf("kuadrant-upstream-%s", gatewayName)
}

// upstreamTargetsGateway checks if a registered upstream's TargetRef resolves to the given gateway.
func upstreamTargetsGateway(entry extension.RegisteredUpstreamEntry, gateway *machinery.Gateway) bool {
	ref := entry.TargetRef
	if ref.Group == "gateway.networking.k8s.io" && ref.Kind == "Gateway" {
		return ref.Name == gateway.GetName() && ref.Namespace == gateway.GetNamespace()
	}
	return false
}

// parseUpstreamURL extracts host and port from a grpc:// URL.
func parseUpstreamURL(rawURL string) (string, int, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", 0, err
	}
	host := parsed.Hostname()
	if host == "" {
		return "", 0, fmt.Errorf("no host in URL %q", rawURL)
	}
	portStr := parsed.Port()
	if portStr == "" {
		return host, 80, nil
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	return host, port, nil
}
