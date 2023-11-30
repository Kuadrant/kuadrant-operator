package kuadranttools

import (
	"fmt"
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func AuthorinoMutator(existingObj, desiredObj client.Object) (bool, error) {
	update := false
	existing, ok := existingObj.(*authorinov1beta1.Authorino)
	if !ok {
		return false, fmt.Errorf("existingObj %T is not a *authorinoauthorinov1beta1.Authorino", existingObj)
	}
	desired, ok := desiredObj.(*authorinov1beta1.Authorino)
	if !ok {
		return false, fmt.Errorf("desiredObj %T is not a *authorinoauthorinov1beta1.Authorino", desiredObj)
	}

	existingSpec := authorinoSpecSubSet(existing.Spec)
	desiredSpec := authorinoSpecSubSet(desired.Spec)

	if !reflect.DeepEqual(existingSpec, desiredSpec) {
		update = true
		existing.Spec.EvaluatorCacheSize = desiredSpec.EvaluatorCacheSize
		existing.Spec.Listener = desiredSpec.Listener
		existing.Spec.Metrics = desiredSpec.Metrics
		existing.Spec.OIDCServer = desiredSpec.OIDCServer
		existing.Spec.Replicas = desiredSpec.Replicas
		existing.Spec.Tracing = desiredSpec.Tracing
		existing.Spec.Volumes = desiredSpec.Volumes
	}
	return update, nil
}

func authorinoSpecSubSet(spec authorinov1beta1.AuthorinoSpec) authorinov1beta1.AuthorinoSpec {
	out := authorinov1beta1.AuthorinoSpec{}

	out.EvaluatorCacheSize = spec.EvaluatorCacheSize
	out.Listener = spec.Listener
	out.Metrics = spec.Metrics
	out.OIDCServer = spec.OIDCServer
	out.Replicas = spec.Replicas
	out.Tracing = spec.Tracing
	out.Volumes = spec.Volumes

	return out
}

func MapListenerSpec(listener *authorinov1beta1.Listener, spec v1beta1.AuthorinoListener) authorinov1beta1.Listener {
	out := authorinov1beta1.Listener{}
	if listener != nil {
		out = *listener
	}
	if spec.Ports != nil {
		out.Ports = *spec.Ports
	}
	if spec.Tls != nil {
		out.Tls = *spec.Tls
	}
	if spec.Timeout != nil {
		out.Timeout = spec.Timeout
	}
	if spec.MaxHttpRequestBodySize != nil {
		out.MaxHttpRequestBodySize = spec.MaxHttpRequestBodySize
	}
	return out
}
