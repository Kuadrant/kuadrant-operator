package controllers

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/env"
)

// clusterScopedKindsSkipped lists resource kinds that should not be applied by
// the operator at runtime. ClusterRoles are managed by the installation method
// (Helm chart or OLM bundle), not by the operator. If a chart template renders
// one, it indicates a sync issue that should be investigated.
var clusterScopedKindsSkipped = map[string]bool{
	"ClusterRole":              true,
	"CustomResourceDefinition": true,
}

func shouldSkipResource(kind string) bool {
	return clusterScopedKindsSkipped[kind]
}

// Component image env vars. These are read from the operator Deployment's
// environment (set via RELATED_IMAGE_* in config/manager/manager.yaml).
// In disconnected environments, the downstream build system overrides these
// with mirrored image references.
var (
	AuthorinoOperatorImage = env.GetString("RELATED_IMAGE_AUTHORINO_OPERATOR", "quay.io/kuadrant/authorino-operator:latest")
	AuthorinoImage         = env.GetString("RELATED_IMAGE_AUTHORINO", "quay.io/kuadrant/authorino:latest")
	LimitadorOperatorImage = env.GetString("RELATED_IMAGE_LIMITADOR_OPERATOR", "quay.io/kuadrant/limitador-operator:latest")
	LimitadorImage         = env.GetString("RELATED_IMAGE_LIMITADOR", "quay.io/kuadrant/limitador:latest")
	DNSOperatorImage       = env.GetString("RELATED_IMAGE_DNS_OPERATOR", "quay.io/kuadrant/dns-operator:latest")
	MCPGatewayImage        = env.GetString("RELATED_IMAGE_MCP_GATEWAY", "ghcr.io/kuadrant/mcp-controller:latest")
)

// patchDeploymentImage overrides the container image on rendered Deployment
// objects and propagates RELATED_IMAGE_* env vars to child operator containers.
// This ensures child operators use the correct (potentially mirrored) images
// in disconnected environments.
func patchDeploymentImage(obj *unstructured.Unstructured, operatorImage string, relatedImageEnvVars map[string]string) {
	if obj.GetKind() != "Deployment" {
		return
	}

	containers, found, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	if !found || len(containers) == 0 {
		return
	}

	container := containers[0].(map[string]interface{})
	container["image"] = operatorImage

	if len(relatedImageEnvVars) > 0 {
		envList, _, _ := unstructured.NestedSlice(container, "env")
		for name, value := range relatedImageEnvVars {
			envList = appendOrUpdateEnv(envList, name, value)
		}
		container["env"] = envList
	}

	containers[0] = container
	unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers")
}

// splitImageRef splits "repo:tag" into repository and tag parts.
func splitImageRef(ref string) (string, string) {
	if i := strings.LastIndex(ref, ":"); i > 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, "latest"
}

func appendOrUpdateEnv(envList []interface{}, name, value string) []interface{} {
	for i, e := range envList {
		envMap := e.(map[string]interface{})
		if envMap["name"] == name {
			envMap["value"] = value
			envList[i] = envMap
			return envList
		}
	}
	return append(envList, map[string]interface{}{"name": name, "value": value})
}

// kindToResource converts Kubernetes Kind to resource name (simple pluralization)
func kindToResource(kind string) string {
	switch kind {
	case "Service":
		return "services"
	case "ServiceAccount":
		return "serviceaccounts"
	case "Deployment":
		return "deployments"
	case "ConfigMap":
		return "configmaps"
	case "ClusterRole":
		return "clusterroles"
	case "ClusterRoleBinding":
		return "clusterrolebindings"
	case "Role":
		return "roles"
	case "RoleBinding":
		return "rolebindings"
	default:
		// Simple pluralization
		return kind + "s"
	}
}
