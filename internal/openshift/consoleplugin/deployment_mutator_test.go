//go:build unit

package consoleplugin

import (
	"testing"

	"gotest.tools/assert"
	"gotest.tools/assert/cmp"
	corev1 "k8s.io/api/core/v1"
)

func TestDeploymentConfigMutatorUpgradesNginxDeployment(t *testing.T) {
	desired := Deployment("test-namespace", "example.test/plugin:new", "topology")
	desired.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullIfNotPresent
	existing := desired.DeepCopy()
	existing.Spec.Template.Spec.Containers[0].Ports[0].Name = ""
	existing.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullAlways
	existing.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
		{Name: "TOPOLOGY_CONFIGMAP_NAME", Value: "old-topology"},
		{Name: "MCP_PROXY_DIAL_ADDRESS", Value: "gateway.example:80"},
	}
	existing.Spec.Template.Spec.Containers[0].VolumeMounts = append(
		existing.Spec.Template.Spec.Containers[0].VolumeMounts,
		corev1.VolumeMount{Name: "nginx-conf", MountPath: "/etc/nginx/nginx.conf"},
	)
	existing.Spec.Template.Spec.Volumes = append(
		existing.Spec.Template.Spec.Volumes,
		corev1.Volume{Name: "nginx-conf"},
	)

	assert.Assert(t, DeploymentConfigMutator(desired, existing))
	container := existing.Spec.Template.Spec.Containers[0]
	assert.DeepEqual(t, container.Ports, desired.Spec.Template.Spec.Containers[0].Ports)
	assert.Equal(t, container.ImagePullPolicy, corev1.PullIfNotPresent)
	assert.DeepEqual(t, container.VolumeMounts, desired.Spec.Template.Spec.Containers[0].VolumeMounts)
	assert.DeepEqual(t, existing.Spec.Template.Spec.Volumes, desired.Spec.Template.Spec.Volumes)
	assert.Assert(t, cmp.Contains(container.Env, corev1.EnvVar{Name: "TLS_CERTIFICATE_FILE", Value: "/var/serving-cert/tls.crt"}))
	assert.Assert(t, cmp.Contains(container.Env, corev1.EnvVar{Name: "TLS_KEY_FILE", Value: "/var/serving-cert/tls.key"}))
	assert.Assert(t, cmp.Contains(container.Env, corev1.EnvVar{Name: "MCP_PROXY_DIAL_ADDRESS", Value: "gateway.example:80"}))
	assert.Assert(t, !DeploymentConfigMutator(desired, existing))
}
