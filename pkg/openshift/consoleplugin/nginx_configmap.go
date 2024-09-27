package consoleplugin

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NginxConfigMapName() string {
	return "kuadrant-console-nginx-conf"
}

func NginxConfigMap(ns string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      NginxConfigMapName(),
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
		add_header oauth_token "$http_Authorization";
		location / {
			root                /usr/share/nginx/html;
		}
	}
}
`,
		},
	}
}
