package controllers

import "strings"

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
	case "NetworkPolicy":
		return "networkpolicies"
	case "Role":
		return "roles"
	case "RoleBinding":
		return "rolebindings"
	default:
		return strings.ToLower(kind) + "s"
	}
}
