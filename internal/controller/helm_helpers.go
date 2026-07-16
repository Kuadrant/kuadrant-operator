package controllers

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
