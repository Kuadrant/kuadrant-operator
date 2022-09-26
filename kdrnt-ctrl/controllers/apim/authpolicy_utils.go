package apim

import (
	"fmt"

	authorinov1beta1 "github.com/kuadrant/authorino/api/v1beta1"
	istiosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// authConfigName returns the name of Authorino AuthConfig CR.
func authConfigName(objKey client.ObjectKey) string {
	return fmt.Sprintf("ap-%s-%s", objKey.Namespace, objKey.Name)
}

// getAuthPolicyName generates the name of an AuthorizationPolicy.
func getAuthPolicyName(gwName, networkName, networkKind string) string {
	if networkKind == "Gateway" {
		return fmt.Sprintf("on-%s", gwName) // Without this, IAP will be named: on-<gw.Name>-using-<gw.Name>;
	}
	return fmt.Sprintf("on-%s-using-%s", gwName, networkName)
}

func alwaysUpdateAuthPolicy(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*istiosecurityv1beta1.AuthorizationPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not a *istiosecurityv1beta1.AuthorizationPolicy", existingObj)
	}
	desired, ok := desiredObj.(*istiosecurityv1beta1.AuthorizationPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not a *istiosecurityv1beta1.AuthorizationPolicy", desiredObj)
	}

	existing.Spec.Action = desired.Spec.Action
	existing.Spec.ActionDetail = desired.Spec.ActionDetail
	existing.Spec.Rules = desired.Spec.Rules
	existing.Spec.Selector = desired.Spec.Selector
	existing.Annotations = desired.Annotations
	return true, nil
}

func alwaysUpdateAuthConfig(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*authorinov1beta1.AuthConfig)
	if !ok {
		return false, fmt.Errorf("%T is not a *authorinov1beta1.AuthConfig", existingObj)
	}
	desired, ok := desiredObj.(*authorinov1beta1.AuthConfig)
	if !ok {
		return false, fmt.Errorf("%T is not a *authorinov1beta1.AuthConfig", desiredObj)
	}

	existing.Spec = desired.Spec
	existing.Annotations = desired.Annotations
	return true, nil
}
