package apim

import (
	"context"
	"fmt"

	"github.com/kuadrant/limitador-operator/api/v1alpha1"
	istio "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// gatewayLabels fetches labels of an Istio gateway identified using the given ObjectKey.
func gatewayLabels(ctx context.Context, client client.Client, gwKey client.ObjectKey) map[string]string {
	gateway := &istio.Gateway{}
	if err := client.Get(ctx, gwKey, gateway); err != nil {
		return map[string]string{}
	}
	return gateway.Spec.Selector
}

// rlFiltersPatchName returns the name of EnvoyFilter adding in rate-limit filters to a gateway.
func rlFiltersPatchName(gatewayName string) string {
	return gatewayName + "-ratelimit-filters"
}

// ratelimitsPatchName returns the name of EnvoyFilter adding in ratelimits to a gateway.
func ratelimitsPatchName(gwName string, networkKey client.ObjectKey) string {
	// TODO(rahulanand16nov): make it unique if VS and HR have same name.
	return fmt.Sprintf("ratelimits-on-%s-using-%s-%s", gwName, networkKey.Namespace, networkKey.Name)
}

// limitadorRatelimitsName returns the name of Limitador RateLimit CR.
func limitadorRatelimitsName(objKey client.ObjectKey, idx int) string {
	return fmt.Sprintf("rlp-%s-%s-%d", objKey.Namespace, objKey.Name, idx)
}

// getAuthPolicyName generates the name of an AuthorizationPolicy using VirtualService info.
func getAuthPolicyName(gwName, vsName string) string {
	return fmt.Sprintf("on-%s-using-%s", gwName, vsName)
}

func alwaysUpdateEnvoyPatches(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*istio.EnvoyFilter)
	if !ok {
		return false, fmt.Errorf("%T is not a *istio.VirtualService", existingObj)
	}
	desired, ok := desiredObj.(*istio.EnvoyFilter)
	if !ok {
		return false, fmt.Errorf("%T is not a *istio.VirtualService", desiredObj)
	}

	existing.Spec = desired.Spec
	existing.Annotations = desired.Annotations
	return true, nil
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

	existing.Spec = desired.Spec
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
