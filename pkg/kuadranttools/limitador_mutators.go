package kuadranttools

import (
	"fmt"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
)

// DeploymentMutateFn is a function which mutates the existing Deployment into it's desired state.
type LimitadorMutateFn func(desired, existing *limitadorv1alpha1.Limitador) bool

func LimitadorMutator(opts ...LimitadorMutateFn) reconcilers.MutateFn {
	return func(existingObj, desiredObj client.Object) (bool, error) {
		existing, ok := existingObj.(*limitadorv1alpha1.Limitador)
		if !ok {
			return false, fmt.Errorf("existingObj %T is not a *limitadorv1alpha1.Limitador", existingObj)
		}
		desired, ok := desiredObj.(*limitadorv1alpha1.Limitador)
		if !ok {
			return false, fmt.Errorf("desiredObj %T is not a *limitadorv1alpha1.Limitador", desiredObj)
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

func LimitadorAffinityMutator(desired, existing *limitadorv1alpha1.Limitador) bool {
	update := false
	if !reflect.DeepEqual(existing.Spec.Affinity, desired.Spec.Affinity) {
		existing.Spec.Affinity = desired.Spec.Affinity
		update = true
	}
	return update
}

func LimitadorReplicasMutator(desired, existing *limitadorv1alpha1.Limitador) bool {
	update := false

	var existingReplicas = 1

	if existing.Spec.Replicas != nil {
		existingReplicas = *existing.Spec.Replicas
	}

	var desiredReplicas = 1

	if desired.Spec.Replicas != nil {
		desiredReplicas = *desired.Spec.Replicas
	}

	if desiredReplicas != existingReplicas {
		existing.Spec.Replicas = &desiredReplicas
		update = true
	}

	return update
}

func LimitadorStorageMutator(desired, existing *limitadorv1alpha1.Limitador) bool {
	update := false
	if !reflect.DeepEqual(existing.Spec.Storage, desired.Spec.Storage) {
		existing.Spec.Storage = desired.Spec.Storage
		update = true
	}
	return update
}

func LimitadorOwnerRefsMutator(desired, existing *limitadorv1alpha1.Limitador) bool {
	update := false
	if !reflect.DeepEqual(existing.OwnerReferences, desired.OwnerReferences) {
		existing.OwnerReferences = desired.OwnerReferences
		update = true
	}
	return update
}

func LimitadorRateLimitHeadersMutator(desired, existing *limitadorv1alpha1.Limitador) bool {
	update := false
	if !reflect.DeepEqual(existing.Spec.RateLimitHeaders, desired.Spec.RateLimitHeaders) {
		existing.Spec.RateLimitHeaders = desired.Spec.RateLimitHeaders
		update = true
	}
	return update
}

func LimitadorTelemetryMutator(desired, existing *limitadorv1alpha1.Limitador) bool {
	update := false
	if !reflect.DeepEqual(existing.Spec.Telemetry, desired.Spec.Telemetry) {
		existing.Spec.Telemetry = desired.Spec.Telemetry
		update = true
	}
	return update
}

func LimitadorPodDisruptionBudgetMutator(desired, existing *limitadorv1alpha1.Limitador) bool {
	update := false
	if !reflect.DeepEqual(existing.Spec.PodDisruptionBudget, desired.Spec.PodDisruptionBudget) {
		existing.Spec.PodDisruptionBudget = desired.Spec.PodDisruptionBudget
		update = true
	}
	return update
}

func LimitadorResourceRequirementsMutator(desired, existing *limitadorv1alpha1.Limitador) bool {
	update := false
	if !reflect.DeepEqual(existing.Spec.ResourceRequirements, desired.Spec.ResourceRequirements) {
		existing.Spec.ResourceRequirements = desired.Spec.ResourceRequirements
		update = true
	}
	return update
}

func LimitadorVerbosityMutator(desired, existing *limitadorv1alpha1.Limitador) bool {
	update := false
	if !reflect.DeepEqual(existing.Spec.Verbosity, desired.Spec.Verbosity) {
		existing.Spec.Verbosity = desired.Spec.Verbosity
		update = true
	}
	return update
}
