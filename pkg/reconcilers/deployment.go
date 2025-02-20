package reconcilers

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func DeploymentImageMutator(desired, existing *appsv1.Deployment) bool {
	update := false

	if existing.Spec.Template.Spec.Containers[0].Image != desired.Spec.Template.Spec.Containers[0].Image {
		existing.Spec.Template.Spec.Containers[0].Image = desired.Spec.Template.Spec.Containers[0].Image
		update = true
	}

	return update
}

func DeploymentTemplateLabelIstioInjectMutator(_, existing *appsv1.Deployment) bool {
	update := false
	MergeMapStringString(&update, &existing.Spec.Template.Labels, map[string]string{"sidecar.istio.io/inject": "true"})
	return update
}

func MergeMapStringString(modified *bool, existing *map[string]string, desired map[string]string) {
	if *existing == nil {
		*existing = map[string]string{}
	}

	// for each desired key value set, e.g. labels
	// check if it's present in existing. if not add it to existing.
	// e.g. preserving existing labels while adding those that are in the desired set.
	for desiredKey, desiredValue := range desired {
		if existingValue, exists := (*existing)[desiredKey]; !exists || existingValue != desiredValue {
			(*existing)[desiredKey] = desiredValue
			*modified = true
		}
	}
}

// DeploymentMutateFn is a function which mutates the existing Deployment into it's desired state.
type DeploymentMutateFn func(desired, existing *appsv1.Deployment) bool

func DeploymentMutator(opts ...DeploymentMutateFn) MutateFn {
	return func(existingObj, desiredObj client.Object) (bool, error) {
		existing, ok := existingObj.(*appsv1.Deployment)
		if !ok {
			return false, fmt.Errorf("%T is not a *appsv1.Deployment", existingObj)
		}
		desired, ok := desiredObj.(*appsv1.Deployment)
		if !ok {
			return false, fmt.Errorf("%T is not a *appsv1.Deployment", desiredObj)
		}

		update := false

		// Loop through each option
		for _, opt := range opts {
			tmpUpdate := opt(desired, existing)
			update = update || tmpUpdate
		}

		return update, nil
	}
}
