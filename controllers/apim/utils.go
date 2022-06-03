package apim

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kuadrant/limitador-operator/api/v1alpha1"
	istiosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	meta "k8s.io/apimachinery/pkg/api/meta"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// limitadorRatelimitsName returns the name of Limitador RateLimit CR.
func limitadorRatelimitsName(objKey client.ObjectKey, idx int) string {
	return fmt.Sprintf("rlp-%s-%s-%d", objKey.Namespace, objKey.Name, idx)
}

// getAuthPolicyName generates the name of an AuthorizationPolicy using VirtualService info.
func getAuthPolicyName(gwName, networkingName, action string) string {
	return fmt.Sprintf("on-%s-using-%s-%s", gwName, networkingName, strings.ToLower(action))
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

func alwaysUpdateRateLimit(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*v1alpha1.RateLimit)
	if !ok {
		return false, fmt.Errorf("%T is not a *v1alpha1.RateLimit", existingObj)
	}
	desired, ok := desiredObj.(*v1alpha1.RateLimit)
	if !ok {
		return false, fmt.Errorf("%T is not a *v1alpha1.RateLimit", desiredObj)
	}

	existing.Spec = desired.Spec
	existing.Annotations = desired.Annotations
	return true, nil
}

func TargetableRoute(httpRoute *gatewayapiv1alpha2.HTTPRoute) error {
	for _, parent := range httpRoute.Status.Parents { // no parent mean policies will affect nothing.
		if len(parent.Conditions) == 0 {
			return errors.New("unable to verify targetability: no condition found on status")
		}
		if meta.IsStatusConditionFalse(parent.Conditions, "Accepted") {
			return errors.New("httproute rejected")
		}
	}
	return nil
}
