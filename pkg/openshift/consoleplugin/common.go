package consoleplugin

const (
	KUADRANT_CONSOLE = "kuadrant-console"
)

func CommonLabels() map[string]string {
	return map[string]string{
		"app":                         KUADRANT_CONSOLE,
		"app.kubernetes.io/component": KUADRANT_CONSOLE,
		"app.kubernetes.io/instance":  KUADRANT_CONSOLE,
		"app.kubernetes.io/name":      KUADRANT_CONSOLE,
		"app.kubernetes.io/part-of":   KUADRANT_CONSOLE,
	}
}
