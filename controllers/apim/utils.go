package apim

import (
	"errors"
	"fmt"

	authorinov1beta1 "github.com/kuadrant/authorino/api/v1beta1"
	istiosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	meta "k8s.io/apimachinery/pkg/api/meta"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

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

func TargetableObject(obj client.Object) error {
	httpRoute, isHTTPRoute := obj.(*gatewayapiv1alpha2.HTTPRoute)
	if isHTTPRoute {
		for _, parent := range httpRoute.Status.Parents { // no parent mean policies will affect nothing.
			if len(parent.Conditions) == 0 {
				return errors.New("unable to verify targetability: no condition found on status")
			}
			if meta.IsStatusConditionFalse(parent.Conditions, "Accepted") {
				return errors.New("httproute rejected")
			}
		}
	} else {
		gateway, _ := obj.(*gatewayapiv1alpha2.Gateway)
		if len(gateway.Status.Conditions) == 0 {
			return errors.New("unable to verify targetability: no condition found on status")
		}
		if meta.IsStatusConditionFalse(gateway.Status.Conditions, "Ready") {
			return errors.New("gateway not ready yet")
		}
	}
	return nil
}

// TargetedGatewayKeys takes either HTTPRoute or Gateway object and return the list of gateways that are being referenced.
func TargetedGatewayKeys(obj client.Object) []client.ObjectKey {
	gwKeys := []client.ObjectKey{}
	httpRoute, isHTTPRoute := obj.(*gatewayapiv1alpha2.HTTPRoute)
	if isHTTPRoute {
		for _, parentRef := range httpRoute.Spec.ParentRefs {
			gwNamespace := httpRoute.Namespace // consider gateway local if namespace is not given
			if parentRef.Namespace != nil {
				gwNamespace = string(*parentRef.Namespace)
			}
			gwKeys = append(gwKeys, client.ObjectKey{Namespace: gwNamespace, Name: string(parentRef.Name)})
		}
	} else {
		gwKeys = append(gwKeys, client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()})
	}
	return gwKeys
}
