package consoleplugin

import (
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func DeploymentName() string {
	return KuadrantConsoleName
}

func DeploymentStrategy() appsv1.DeploymentStrategy {
	return appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{
			MaxUnavailable: ptr.To(intstr.FromString("25%")),
			MaxSurge:       ptr.To(intstr.FromString("25%")),
		},
	}
}
func DeploymentSelector() *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchLabels: CommonLabels(),
	}
}

func DeploymentVolumeMounts(runtime Runtime) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{
			Name:      "plugin-serving-cert",
			ReadOnly:  true,
			MountPath: "/var/serving-cert",
		},
	}
	if runtime == RuntimeNginx {
		mounts = append(mounts, corev1.VolumeMount{
			Name: "nginx-conf", ReadOnly: true,
			MountPath: "/etc/nginx/nginx.conf", SubPath: "nginx.conf",
		})
	}
	return mounts
}

func DeploymentVolumes(runtime Runtime) []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name: "plugin-serving-cert",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  "plugin-serving-cert",
					DefaultMode: ptr.To(int32(420)),
				},
			},
		},
	}
	if runtime == RuntimeNginx {
		volumes = append(volumes, corev1.Volume{
			Name: "nginx-conf",
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: legacyNginxConfigMapName},
				DefaultMode:          ptr.To(int32(420)),
			}},
		})
	}
	return volumes
}

func DeploymentLabels(namespace string) map[string]string {
	result := map[string]string{
		"app.openshift.io/runtime-namespace": namespace,
	}

	maps.Copy(result, CommonLabels())

	return result
}

func Deployment(ns string, image Image, topologyName string) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName(),
			Namespace: ns,
			Labels:    DeploymentLabels(ns),
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: DeploymentStrategy(),
			Selector: DeploymentSelector(),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: CommonLabels(),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  KuadrantConsoleName,
							Image: image.URL,
							Ports: []corev1.ContainerPort{
								{
									Name:          "https",
									ContainerPort: 9443,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							ImagePullPolicy: corev1.PullAlways,
							VolumeMounts:    DeploymentVolumeMounts(image.Runtime),
							Env:             deploymentEnvironment(ns, topologyName, image.Runtime),
						},
					},
					Volumes:       DeploymentVolumes(image.Runtime),
					RestartPolicy: corev1.RestartPolicyAlways,
					DNSPolicy:     corev1.DNSClusterFirst,
				},
			},
		},
	}
}

func deploymentEnvironment(namespace, topologyName string, runtime Runtime) []corev1.EnvVar {
	variables := []corev1.EnvVar{
		{Name: "TOPOLOGY_CONFIGMAP_NAME", Value: topologyName},
		{Name: "TOPOLOGY_CONFIGMAP_NAMESPACE", Value: namespace},
	}
	if runtime == RuntimeGo {
		variables = append(variables,
			corev1.EnvVar{Name: "TLS_CERTIFICATE_FILE", Value: "/var/serving-cert/tls.crt"},
			corev1.EnvVar{Name: "TLS_KEY_FILE", Value: "/var/serving-cert/tls.key"},
		)
	}
	return variables
}
