package consoleplugin

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func ServiceName() string {
	return KUADRANT_CONSOLE
}

func ServiceAnnotations() map[string]string {
	return map[string]string{
		"service.alpha.openshift.io/serving-cert-secret-name": "plugin-serving-cert",
	}
}

func ServiceSelector() map[string]string {
	return map[string]string{
		"app": KUADRANT_CONSOLE,
	}
}

func Service(ns string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        ServiceName(),
			Namespace:   ns,
			Labels:      CommonLabels(),
			Annotations: ServiceAnnotations(),
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "9443-tcp",
					Protocol:   corev1.ProtocolTCP,
					Port:       9443,
					TargetPort: intstr.FromInt32(9443),
				},
			},
			Selector: ServiceSelector(),
			Type:     corev1.ServiceTypeClusterIP,
		},
	}
}
