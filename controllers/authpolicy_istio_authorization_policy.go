package controllers

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	istiosecurity "istio.io/api/security/v1beta1"
	istio "istio.io/client-go/pkg/apis/security/v1beta1"

	api "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
)

var KuadrantExtAuthProviderName = common.FetchEnv("AUTH_PROVIDER", "kuadrant-authorization")

// reconcileIstioAuthorizationPolicies translates and reconciles `AuthRules` into an Istio AuthorizationPoilcy containing them.
func (r *AuthPolicyReconciler) reconcileIstioAuthorizationPolicies(ctx context.Context, ap *api.AuthPolicy, targetNetworkObject client.Object, gwDiffObj *reconcilers.GatewayDiff) error {
	if err := r.deleteIstioAuthorizationPolicies(ctx, ap, gwDiffObj); err != nil {
		return err
	}

	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	targetHostnames, err := common.TargetHostnames(targetNetworkObject)
	if err != nil {
		return err
	}

	// TODO(guicassolato): should the rules filter only the hostnames valid for each gateway?
	toRules := istioAuthorizationPolicyRules(ap.Spec.AuthRules, targetHostnames, targetNetworkObject)

	// Create IstioAuthorizationPolicy for each gateway directly or indirectly referred by the policy (existing and new)
	for _, gw := range append(gwDiffObj.GatewaysWithValidPolicyRef, gwDiffObj.GatewaysMissingPolicyRef...) {
		iap := r.istioAuthorizationPolicy(ctx, gw.Gateway, ap, toRules)
		err := r.ReconcileResource(ctx, &istio.AuthorizationPolicy{}, iap, alwaysUpdateAuthPolicy)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			logger.Error(err, "failed to reconcile IstioAuthorizationPolicy resource")
			return err
		}
	}

	return nil
}

// deleteIstioAuthorizationPolicies deletes IstioAuthorizationPolicies previously created for gateways no longer targeted by the policy (directly or indirectly)
func (r *AuthPolicyReconciler) deleteIstioAuthorizationPolicies(ctx context.Context, ap *api.AuthPolicy, gwDiffObj *reconcilers.GatewayDiff) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	for _, gw := range gwDiffObj.GatewaysWithInvalidPolicyRef {
		listOptions := &client.ListOptions{LabelSelector: labels.SelectorFromSet(labels.Set(istioAuthorizationPolicyLabels(client.ObjectKeyFromObject(gw.Gateway), client.ObjectKeyFromObject(ap))))}
		iapList := &istio.AuthorizationPolicyList{}
		if err := r.Client().List(ctx, iapList, listOptions); err != nil {
			return err
		}

		for _, iap := range iapList.Items {
			// it's OK to just go ahead and delete because we only create one IAP per target network object,
			// and a network object can be target by no more than one AuthPolicy
			if err := r.DeleteResource(ctx, iap); err != nil && !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to delete IstioAuthorizationPolicy")
				return err
			}
		}
	}

	return nil
}

func (r *AuthPolicyReconciler) istioAuthorizationPolicy(ctx context.Context, gateway *gatewayapiv1beta1.Gateway, ap *api.AuthPolicy, toRules []*istiosecurity.Rule_To) *istio.AuthorizationPolicy {
	return &istio.AuthorizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      istioAuthorizationPolicyName(gateway.Name, ap.GetTargetRef()),
			Namespace: gateway.Namespace,
			Labels:    istioAuthorizationPolicyLabels(client.ObjectKeyFromObject(gateway), client.ObjectKeyFromObject(ap)),
		},
		Spec: istiosecurity.AuthorizationPolicy{
			Action: istiosecurity.AuthorizationPolicy_CUSTOM,
			Rules: []*istiosecurity.Rule{
				{
					To: toRules,
				},
			},
			Selector: common.IstioWorkloadSelectorFromGateway(ctx, r.Client(), gateway),
			ActionDetail: &istiosecurity.AuthorizationPolicy_Provider{
				Provider: &istiosecurity.AuthorizationPolicy_ExtensionProvider{
					Name: KuadrantExtAuthProviderName,
				},
			},
		},
	}
}

// istioAuthorizationPolicyName generates the name of an AuthorizationPolicy.
func istioAuthorizationPolicyName(gwName string, targetRef gatewayapiv1alpha2.PolicyTargetReference) string {
	switch targetRef.Kind {
	case "Gateway":
		return fmt.Sprintf("on-%s", gwName) // Without this, IAP will be named: on-<gw.Name>-using-<gw.Name>;
	case "HTTPRoute":
		return fmt.Sprintf("on-%s-using-%s", gwName, targetRef.Name)
	}
	return ""
}

func istioAuthorizationPolicyLabels(gwKey, apKey client.ObjectKey) map[string]string {
	return map[string]string{
		common.AuthPolicyBackRefAnnotation:                              apKey.Name,
		fmt.Sprintf("%s-namespace", common.AuthPolicyBackRefAnnotation): apKey.Namespace,
		"gateway-namespace":                                             gwKey.Namespace,
		"gateway":                                                       gwKey.Name,
	}
}

func istioAuthorizationPolicyRules(authRules []api.AuthRule, targetHostnames []string, targetNetworkObject client.Object) []*istiosecurity.Rule_To {
	toRules := []*istiosecurity.Rule_To{}

	// Rules set in the AuthPolicy
	for _, rule := range authRules {
		hosts := rule.Hosts
		if len(rule.Hosts) == 0 {
			hosts = targetHostnames
		}
		toRules = append(toRules, &istiosecurity.Rule_To{
			Operation: &istiosecurity.Operation{
				Hosts:   hosts,
				Methods: rule.Methods,
				Paths:   rule.Paths,
			},
		})
	}

	// TODO(guicassolato): always inherit the rules from the target network object and remove AuthRules from the AuthPolicy API

	if len(toRules) == 0 {
		// Rules not set in the AuthPolicy - inherit the rules from the target network object
		switch obj := targetNetworkObject.(type) {
		case *gatewayapiv1beta1.HTTPRoute:
			// Rules not set and targeting a HTTPRoute - inherit the rules (hostnames, methods and paths) from the HTTPRoute
			httpRouterules := common.RulesFromHTTPRoute(obj)
			for idx := range httpRouterules {
				toRules = append(toRules, &istiosecurity.Rule_To{
					Operation: &istiosecurity.Operation{
						Hosts:   common.SliceCopy(httpRouterules[idx].Hosts),
						Methods: common.SliceCopy(httpRouterules[idx].Methods),
						Paths:   common.SliceCopy(httpRouterules[idx].Paths),
					},
				})
			}
		case *gatewayapiv1beta1.Gateway:
			// Rules not set and targeting a Gateway - inherit the rules (hostnames) from the Gateway
			toRules = append(toRules, &istiosecurity.Rule_To{
				Operation: &istiosecurity.Operation{
					Hosts: targetHostnames,
				},
			})
		}
	}

	return toRules
}

func alwaysUpdateAuthPolicy(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*istio.AuthorizationPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not an *istio.AuthorizationPolicy", existingObj)
	}
	desired, ok := desiredObj.(*istio.AuthorizationPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not an *istio.AuthorizationPolicy", desiredObj)
	}

	if reflect.DeepEqual(existing.Spec.Action, desired.Spec.Action) {
		return false, nil
	}
	existing.Spec.Action = desired.Spec.Action

	if reflect.DeepEqual(existing.Spec.ActionDetail, desired.Spec.ActionDetail) {
		return false, nil
	}
	existing.Spec.ActionDetail = desired.Spec.ActionDetail

	if reflect.DeepEqual(existing.Spec.Rules, desired.Spec.Rules) {
		return false, nil
	}
	existing.Spec.Rules = desired.Spec.Rules

	if reflect.DeepEqual(existing.Spec.Selector, desired.Spec.Selector) {
		return false, nil
	}
	existing.Spec.Selector = desired.Spec.Selector

	if reflect.DeepEqual(existing.Annotations, desired.Annotations) {
		return false, nil
	}
	existing.Annotations = desired.Annotations

	return true, nil
}
