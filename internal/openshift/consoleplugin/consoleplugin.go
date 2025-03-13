package consoleplugin

import (
	consolev1 "github.com/openshift/api/console/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Name() string {
	return KuadrantConsoleName
}

func ConsolePlugin(ns string) *consolev1.ConsolePlugin {
	return &consolev1.ConsolePlugin{
		TypeMeta: metav1.TypeMeta{Kind: "ConsolePlugin", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   Name(),
			Labels: CommonLabels(),
		},
		Spec: consolev1.ConsolePluginSpec{
			DisplayName: "Kuadrant Console Plugin",
			I18n: consolev1.ConsolePluginI18n{
				LoadType: consolev1.Preload,
			},
			Backend: consolev1.ConsolePluginBackend{
				Type: consolev1.Service,
				Service: &consolev1.ConsolePluginService{
					Name:      ServiceName(),
					Namespace: ns,
					Port:      9443,
					BasePath:  "/",
				},
			},
		},
	}
}
