package consoleplugin

const (
	KuadrantConsoleName              = "kuadrant-console-plugin"
	KuadrantConsolePluginImagesLabel = "kuadrant-operator-console-plugin-images"
)

var (
	AppLabelKey   = "app"
	AppLabelValue = KuadrantConsoleName
)

func CommonLabels() map[string]string {
	return map[string]string{
		AppLabelKey:                    AppLabelValue,
		"app.kubernetes.io/component":  KuadrantConsoleName,
		"app.kubernetes.io/managed-by": "kuadrant-operator",
		"app.kubernetes.io/instance":   KuadrantConsoleName,
		"app.kubernetes.io/name":       KuadrantConsoleName,
		"app.kubernetes.io/part-of":    KuadrantConsoleName,
	}
}
