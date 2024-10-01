package consoleplugin

const (
	KuadrantConsoleName     = "kuadrant-console"
	KuadrantPluginComponent = "kuadrant-plugin"
)

func CommonLabels() map[string]string {
	return map[string]string{
		"app":                          KuadrantConsoleName,
		"app.kubernetes.io/component":  KuadrantPluginComponent,
		"app.kubernetes.io/managed-by": "kuadrant-operator",
		"app.kubernetes.io/instance":   KuadrantConsoleName,
		"app.kubernetes.io/name":       KuadrantConsoleName,
		"app.kubernetes.io/part-of":    KuadrantConsoleName,
	}
}
