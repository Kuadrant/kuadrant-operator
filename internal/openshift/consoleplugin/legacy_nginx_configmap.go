package consoleplugin

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const legacyNginxConfigMapName = "kuadrant-console-nginx-conf"

// LegacyNginxConfigMap configures HTTPS for older plugin images that run nginx.
func LegacyNginxConfigMap(ns string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      legacyNginxConfigMapName,
			Namespace: ns,
			Labels:    CommonLabels(),
		},
		Data: map[string]string{
			"nginx.conf": `error_log /dev/stdout;
events {}
http {
	access_log         /dev/stdout;
	include            /etc/nginx/mime.types;
	default_type       application/octet-stream;
	keepalive_timeout  65;
	server {
		listen              9443 ssl;
		listen              [::]:9443 ssl;
		ssl_certificate     /var/serving-cert/tls.crt;
		ssl_certificate_key /var/serving-cert/tls.key;
		location / {
			root                /usr/share/nginx/html;
		}
		location /config.js {
			root /tmp;
		}
	}
}
`,
		},
	}
}
