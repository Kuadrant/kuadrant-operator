package consoleplugin

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const legacyNginxConfigMapName = "kuadrant-console-nginx-conf"

// LegacyNginxConfigMap identifies the ConfigMap used by plugin releases whose
// runtime image was served by nginx. The backend image no longer consumes it.
func LegacyNginxConfigMap(namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: legacyNginxConfigMapName, Namespace: namespace},
	}
}
