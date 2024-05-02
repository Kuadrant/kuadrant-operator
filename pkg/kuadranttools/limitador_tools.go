package kuadranttools

import (
	"fmt"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

func LimitadorMutator(existingObj, desiredObj client.Object) (bool, error) {
	update := false
	existing, ok := existingObj.(*limitadorv1alpha1.Limitador)
	if !ok {
		return false, fmt.Errorf("existingObj %T is not a *limitadorv1alpha1.Limitador", existingObj)
	}
	desired, ok := desiredObj.(*limitadorv1alpha1.Limitador)
	if !ok {
		return false, fmt.Errorf("desireObj %T is not a *limitadorv1alpha1.Limitador", desiredObj)
	}

	if !reflect.DeepEqual(existing.OwnerReferences, desired.OwnerReferences) {
		update = true
		existing.OwnerReferences = desired.OwnerReferences
	}

	if !reflect.DeepEqual(existing.Spec.Affinity, desired.Spec.Affinity) {
		update = true
		existing.Spec.Affinity = desired.Spec.Affinity
	}

	if !reflect.DeepEqual(existing.Spec.Replicas, desired.Spec.Replicas) {
		update = true
		existing.Spec.Replicas = desired.Spec.Replicas
	}

	if !reflect.DeepEqual(existing.Spec.Storage, desired.Spec.Storage) {
		update = true
		existing.Spec.Storage = desired.Spec.Storage
	}

	if !reflect.DeepEqual(existing.Spec.RateLimitHeaders, desired.Spec.RateLimitHeaders) {
		update = true
		existing.Spec.RateLimitHeaders = desired.Spec.RateLimitHeaders
	}

	if !reflect.DeepEqual(existing.Spec.Telemetry, desired.Spec.Telemetry) {
		update = true
		existing.Spec.Telemetry = desired.Spec.Telemetry
	}

	if !reflect.DeepEqual(existing.Spec.Tracing, desired.Spec.Tracing) {
		update = true
		existing.Spec.Tracing = desired.Spec.Tracing
	}

	if !reflect.DeepEqual(existing.Spec.PodDisruptionBudget, desired.Spec.PodDisruptionBudget) {
		update = true
		existing.Spec.PodDisruptionBudget = desired.Spec.PodDisruptionBudget
	}

	if !reflect.DeepEqual(existing.Spec.ResourceRequirements, desired.Spec.ResourceRequirements) {
		update = true
		existing.Spec.ResourceRequirements = desired.Spec.ResourceRequirements
	}

	if !reflect.DeepEqual(existing.Spec.Verbosity, desired.Spec.Verbosity) {
		update = true
		existing.Spec.Verbosity = desired.Spec.Verbosity
	}

	return update, nil
}
