package consoleplugin

import (
	"reflect"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// DeploymentConfigMutator reconciles the parts of the plugin pod that changed
// when its image became both the asset server and the backend. Environment
// variables not owned by the operator are retained so development overrides
// can still be injected without being removed on every reconcile.
func DeploymentConfigMutator(desired, existing *appsv1.Deployment) bool {
	if len(desired.Spec.Template.Spec.Containers) == 0 || len(existing.Spec.Template.Spec.Containers) == 0 {
		return false
	}

	updated := false
	desiredContainer := desired.Spec.Template.Spec.Containers[0]
	existingContainer := &existing.Spec.Template.Spec.Containers[0]

	if !reflect.DeepEqual(existingContainer.Ports, desiredContainer.Ports) {
		existingContainer.Ports = desiredContainer.Ports
		updated = true
	}
	if existingContainer.ImagePullPolicy != desiredContainer.ImagePullPolicy {
		existingContainer.ImagePullPolicy = desiredContainer.ImagePullPolicy
		updated = true
	}
	if !reflect.DeepEqual(existingContainer.VolumeMounts, desiredContainer.VolumeMounts) {
		existingContainer.VolumeMounts = desiredContainer.VolumeMounts
		updated = true
	}
	if mergeOwnedEnvironment(existingContainer, desiredContainer.Env) {
		updated = true
	}
	if !reflect.DeepEqual(existing.Spec.Template.Spec.Volumes, desired.Spec.Template.Spec.Volumes) {
		existing.Spec.Template.Spec.Volumes = desired.Spec.Template.Spec.Volumes
		updated = true
	}

	return updated
}

func mergeOwnedEnvironment(container *corev1.Container, desired []corev1.EnvVar) bool {
	updated := false
	// A switch back to nginx must remove the Go server's TLS settings while
	// retaining environment variables supplied by administrators.
	container.Env = slices.DeleteFunc(container.Env, func(variable corev1.EnvVar) bool {
		if variable.Name != "TLS_CERTIFICATE_FILE" && variable.Name != "TLS_KEY_FILE" {
			return false
		}
		remove := !slices.ContainsFunc(desired, func(wanted corev1.EnvVar) bool {
			return wanted.Name == variable.Name
		})
		updated = updated || remove
		return remove
	})
	for _, desiredVariable := range desired {
		found := false
		for index := range container.Env {
			if container.Env[index].Name != desiredVariable.Name {
				continue
			}
			found = true
			if !reflect.DeepEqual(container.Env[index], desiredVariable) {
				container.Env[index] = desiredVariable
				updated = true
			}
			break
		}
		if !found {
			container.Env = append(container.Env, desiredVariable)
			updated = true
		}
	}
	return updated
}
