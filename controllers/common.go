package controllers

const (
	KuadrantAppName = "kuadrant"
)

var (
	AppLabelKey   = "app"
	AppLabelValue = KuadrantAppName
)

func CommonLabels() map[string]string {
	return map[string]string{
		AppLabelKey:                    AppLabelValue,
		"app.kubernetes.io/component":  KuadrantAppName,
		"app.kubernetes.io/managed-by": "kuadrant-operator",
		"app.kubernetes.io/instance":   KuadrantAppName,
		"app.kubernetes.io/name":       KuadrantAppName,
		"app.kubernetes.io/part-of":    KuadrantAppName,
	}
}
