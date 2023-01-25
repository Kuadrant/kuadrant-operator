package controllers

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	authorinoapi "github.com/kuadrant/authorino/api/v1beta1"
	api "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

func (r *AuthPolicyReconciler) reconcileAuthConfigs(ctx context.Context, ap *api.AuthPolicy, targetNetworkObject client.Object) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	authConfig, err := r.desiredAuthConfig(ctx, ap, targetNetworkObject)
	if err != nil {
		return err
	}

	err = r.ReconcileResource(ctx, &authorinoapi.AuthConfig{}, authConfig, alwaysUpdateAuthConfig)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		logger.Error(err, "ReconcileResource failed to create/update AuthConfig resource")
		return err
	}
	return nil
}

func (r *AuthPolicyReconciler) deleteAuthConfigs(ctx context.Context, ap *api.AuthPolicy) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	logger.Info("Removing Authorino's AuthConfigs")

	authConfig := &authorinoapi.AuthConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      authConfigName(client.ObjectKeyFromObject(ap)),
			Namespace: ap.Namespace,
		},
	}

	if err := r.DeleteResource(ctx, authConfig); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		logger.Error(err, "failed to delete Authorino's AuthConfig")
		return err
	}

	return nil
}

func (r *AuthPolicyReconciler) desiredAuthConfig(ctx context.Context, ap *api.AuthPolicy, targetNetworkObject client.Object) (*authorinoapi.AuthConfig, error) {
	hosts, err := r.policyHosts(ctx, ap, targetNetworkObject)
	if err != nil {
		return nil, err
	}

	return &authorinoapi.AuthConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AuthConfig",
			APIVersion: authorinoapi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      authConfigName(client.ObjectKeyFromObject(ap)),
			Namespace: ap.Namespace,
		},
		Spec: authorinoapi.AuthConfigSpec{
			Hosts:         hosts,
			Patterns:      ap.Spec.AuthScheme.Patterns,
			Conditions:    ap.Spec.AuthScheme.Conditions,
			Identity:      ap.Spec.AuthScheme.Identity,
			Metadata:      ap.Spec.AuthScheme.Metadata,
			Authorization: ap.Spec.AuthScheme.Authorization,
			Response:      ap.Spec.AuthScheme.Response,
			DenyWith:      ap.Spec.AuthScheme.DenyWith,
		},
	}, nil
}

func (r *AuthPolicyReconciler) policyHosts(ctx context.Context, ap *api.AuthPolicy, targetNetworkObject client.Object) ([]string, error) {
	if len(ap.Spec.AuthRules) == 0 {
		return r.TargetHostnames(ctx, targetNetworkObject)
	}

	uniqueHostnamesMap := make(map[string]interface{})
	for idx := range ap.Spec.AuthRules {
		if len(ap.Spec.AuthRules[idx].Hosts) == 0 {
			// When one of the rules does not have hosts, just return target hostnames
			return r.TargetHostnames(ctx, targetNetworkObject)
		}

		for _, hostname := range ap.Spec.AuthRules[idx].Hosts {
			uniqueHostnamesMap[hostname] = nil
		}
	}

	hostnames := make([]string, 0, len(uniqueHostnamesMap))
	for k := range uniqueHostnamesMap {
		hostnames = append(hostnames, k)
	}

	return hostnames, nil
}

// authConfigName returns the name of Authorino AuthConfig CR.
func authConfigName(apKey client.ObjectKey) string {
	return fmt.Sprintf("ap-%s-%s", apKey.Namespace, apKey.Name)
}

func alwaysUpdateAuthConfig(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*authorinoapi.AuthConfig)
	if !ok {
		return false, fmt.Errorf("%T is not an *authorinoapi.AuthConfig", existingObj)
	}
	desired, ok := desiredObj.(*authorinoapi.AuthConfig)
	if !ok {
		return false, fmt.Errorf("%T is not an *authorinoapi.AuthConfig", desiredObj)
	}

	if reflect.DeepEqual(existing.Spec, desired.Spec) && reflect.DeepEqual(existing.Annotations, desired.Annotations) {
		return false, nil
	}

	existing.Spec = desired.Spec
	existing.Annotations = desired.Annotations
	return true, nil
}
