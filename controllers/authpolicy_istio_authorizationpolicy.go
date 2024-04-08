package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	istiosecurity "istio.io/api/security/v1beta1"
	istio "istio.io/client-go/pkg/apis/security/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/env"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantistioutils "github.com/kuadrant/kuadrant-operator/pkg/istio"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var KuadrantExtAuthProviderName = env.GetString("AUTH_PROVIDER", "kuadrant-authorization")

// reconcileIstioAuthorizationPolicies translates and reconciles `AuthRules` into an Istio AuthorizationPoilcy containing them.
func (r *AuthPolicyReconciler) reconcileIstioAuthorizationPolicies(ctx context.Context, ap *api.AuthPolicy, targetNetworkObject client.Object, gwDiffObj *reconcilers.GatewayDiffs) error {
	if err := r.deleteIstioAuthorizationPolicies(ctx, ap, gwDiffObj); err != nil {
		return err
	}

	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	// Create IstioAuthorizationPolicy for each gateway directly or indirectly referred by the policy (existing and new)
	for _, gw := range append(gwDiffObj.GatewaysWithValidPolicyRef, gwDiffObj.GatewaysMissingPolicyRef...) {
		iap, err := r.istioAuthorizationPolicy(ctx, ap, targetNetworkObject, gw)
		if err != nil {
			return err
		}
		if err := r.ReconcileResource(ctx, &istio.AuthorizationPolicy{}, iap, alwaysUpdateAuthPolicy); err != nil && !apierrors.IsAlreadyExists(err) {
			logger.Error(err, "failed to reconcile IstioAuthorizationPolicy resource")
			return err
		}
	}

	return nil
}

// deleteIstioAuthorizationPolicies deletes IstioAuthorizationPolicies previously created for gateways no longer targeted by the policy (directly or indirectly)
func (r *AuthPolicyReconciler) deleteIstioAuthorizationPolicies(ctx context.Context, ap *api.AuthPolicy, gwDiffObj *reconcilers.GatewayDiffs) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	for _, gw := range gwDiffObj.GatewaysWithInvalidPolicyRef {
		listOptions := &client.ListOptions{LabelSelector: labels.SelectorFromSet(istioAuthorizationPolicyLabels(client.ObjectKeyFromObject(gw.Gateway), client.ObjectKeyFromObject(ap)))}
		iapList := &istio.AuthorizationPolicyList{}
		if err := r.Client().List(ctx, iapList, listOptions); err != nil {
			return err
		}

		for _, iap := range iapList.Items {
			// it's OK to just go ahead and delete because we only create one IAP per target network object,
			// and a network object can be targeted by no more than one AuthPolicy
			if err := r.DeleteResource(ctx, iap); err != nil && !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to delete IstioAuthorizationPolicy")
				return err
			}
		}
	}

	return nil
}

func (r *AuthPolicyReconciler) istioAuthorizationPolicy(ctx context.Context, ap *api.AuthPolicy, targetNetworkObject client.Object, gw kuadrant.GatewayWrapper) (*istio.AuthorizationPolicy, error) {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("istioAuthorizationPolicy")

	gateway := gw.Gateway

	iap := &istio.AuthorizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      istioAuthorizationPolicyName(gateway.Name, ap.GetTargetRef()),
			Namespace: gateway.Namespace,
			Labels:    istioAuthorizationPolicyLabels(client.ObjectKeyFromObject(gateway), client.ObjectKeyFromObject(ap)),
		},
		Spec: istiosecurity.AuthorizationPolicy{
			Action:   istiosecurity.AuthorizationPolicy_CUSTOM,
			Selector: kuadrantistioutils.WorkloadSelectorFromGateway(ctx, r.Client(), gateway),
			ActionDetail: &istiosecurity.AuthorizationPolicy_Provider{
				Provider: &istiosecurity.AuthorizationPolicy_ExtensionProvider{
					Name: KuadrantExtAuthProviderName,
				},
			},
		},
	}

	var route *gatewayapiv1.HTTPRoute

	gwHostnames := gw.Hostnames()
	if len(gwHostnames) == 0 {
		gwHostnames = []gatewayapiv1.Hostname{"*"}
	}
	var routeHostnames []gatewayapiv1.Hostname

	switch obj := targetNetworkObject.(type) {
	case *gatewayapiv1.HTTPRoute:
		route = obj
		if len(route.Spec.Hostnames) > 0 {
			routeHostnames = kuadrantgatewayapi.FilterValidSubdomains(gwHostnames, route.Spec.Hostnames)
		} else {
			routeHostnames = gwHostnames
		}
	case *gatewayapiv1.Gateway:
		// fake a single httproute with all rules from all httproutes accepted by the gateway,
		// that do not have an authpolicy of its own, so we can generate wasm rules for those cases
		rules := make([]gatewayapiv1.HTTPRouteRule, 0)
		routes := r.TargetRefReconciler.FetchAcceptedGatewayHTTPRoutes(ctx, ap.TargetKey())
		for idx := range routes {
			route := routes[idx]
			// skip routes that have an authpolicy of its own
			if route.GetAnnotations()[common.AuthPolicyBackRefAnnotation] != "" {
				continue
			}
			rules = append(rules, route.Spec.Rules...)
		}
		if len(rules) == 0 {
			logger.V(1).Info("no httproutes attached to the targeted gateway, skipping istio authorizationpolicy for the gateway authpolicy")
			utils.TagObjectToDelete(iap)
			return iap, nil
		}
		route = &gatewayapiv1.HTTPRoute{
			Spec: gatewayapiv1.HTTPRouteSpec{
				Hostnames: gwHostnames,
				Rules:     rules,
			},
		}
		routeHostnames = gwHostnames
	}

	rules, err := istioAuthorizationPolicyRules(ap, route)
	if err != nil {
		return nil, err
	}

	if len(rules) > 0 {
		// make sure all istio authorizationpolicy rules include the hosts so we don't send a request to authorino for hosts that are not in the scope of the policy
		hosts := utils.HostnamesToStrings(routeHostnames)
		for i := range rules {
			for j := range rules[i].To {
				if len(rules[i].To[j].Operation.Hosts) > 0 {
					continue
				}
				rules[i].To[j].Operation.Hosts = hosts
			}
		}
		iap.Spec.Rules = rules
	}

	return iap, nil
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

// istioAuthorizationPolicyRules builds the list of Istio AuthorizationPolicy rules from an AuthPolicy and a HTTPRoute.
// These rules are the conditions that, when matched, will make the gateway to call external authorization.
// If no rules are specified, the gateway will call external authorization for all requests.
// If the route selectors specified in the policy do not match any route rules, an error is returned.
func istioAuthorizationPolicyRules(ap *api.AuthPolicy, route *gatewayapiv1.HTTPRoute) ([]*istiosecurity.Rule, error) {
	commonSpec := ap.Spec.CommonSpec()
	// use only the top level route selectors if defined
	if topLevelRouteSelectors := commonSpec.RouteSelectors; len(topLevelRouteSelectors) > 0 {
		return istioAuthorizationPolicyRulesFromRouteSelectors(route, topLevelRouteSelectors)
	}
	return istioAuthorizationPolicyRulesFromHTTPRoute(route), nil
}

// istioAuthorizationPolicyRulesFromRouteSelectors builds a list of Istio AuthorizationPolicy rules from an HTTPRoute,
// filtered to the HTTPRouteRules and hostnames selected by the route selectors.
func istioAuthorizationPolicyRulesFromRouteSelectors(route *gatewayapiv1.HTTPRoute, routeSelectors []api.RouteSelector) ([]*istiosecurity.Rule, error) {
	istioRules := []*istiosecurity.Rule{}

	if len(routeSelectors) > 0 {
		// build conditions from the rules selected by the route selectors
		for idx := range routeSelectors {
			routeSelector := routeSelectors[idx]
			hostnamesForConditions := routeSelector.HostnamesForConditions(route)
			// TODO(@guicassolato): report about route selectors that match no HTTPRouteRule
			for _, rule := range routeSelector.SelectRules(route) {
				istioRules = append(istioRules, istioAuthorizationPolicyRulesFromHTTPRouteRule(rule, hostnamesForConditions)...)
			}
		}
		if len(istioRules) == 0 {
			return nil, errors.New("cannot match any route rules, check for invalid route selectors in the policy")
		}
	}

	return istioRules, nil
}

// istioAuthorizationPolicyRulesFromHTTPRoute builds a list of Istio AuthorizationPolicy rules from an HTTPRoute,
// without using route selectors.
func istioAuthorizationPolicyRulesFromHTTPRoute(route *gatewayapiv1.HTTPRoute) []*istiosecurity.Rule {
	istioRules := []*istiosecurity.Rule{}

	hostnamesForConditions := (&api.RouteSelector{}).HostnamesForConditions(route)
	for _, rule := range route.Spec.Rules {
		istioRules = append(istioRules, istioAuthorizationPolicyRulesFromHTTPRouteRule(rule, hostnamesForConditions)...)
	}

	return istioRules
}

// istioAuthorizationPolicyRulesFromHTTPRouteRule builds a list of Istio AuthorizationPolicy rules from a HTTPRouteRule
// and a list of hostnames.
// * Each combination of HTTPRouteMatch and hostname yields one condition.
// * Rules that specify no explicit HTTPRouteMatch are assumed to match all requests (i.e. implicit catch-all rule.)
// * Empty list of hostnames yields a condition without a hostname pattern expression.
func istioAuthorizationPolicyRulesFromHTTPRouteRule(rule gatewayapiv1.HTTPRouteRule, hostnames []gatewayapiv1.Hostname) (istioRules []*istiosecurity.Rule) {
	hosts := []string{}
	for _, hostname := range hostnames {
		if hostname == "*" {
			continue
		}
		hosts = append(hosts, string(hostname))
	}

	// no http route matches → we only need one simple istio rule or even no rule at all
	if len(rule.Matches) == 0 {
		if len(hosts) == 0 {
			return
		}
		istioRule := &istiosecurity.Rule{
			To: []*istiosecurity.Rule_To{
				{
					Operation: &istiosecurity.Operation{
						Hosts: hosts,
					},
				},
			},
		}
		istioRules = append(istioRules, istioRule)
		return
	}

	// http route matches and possibly hostnames → we need one istio rule per http route match
	for _, match := range rule.Matches {
		istioRule := &istiosecurity.Rule{}

		var operation *istiosecurity.Operation
		method := match.Method
		path := match.Path

		if len(hosts) > 0 || method != nil || path != nil {
			operation = &istiosecurity.Operation{}
		}

		// hosts
		if len(hosts) > 0 {
			operation.Hosts = hosts
		}

		// method
		if method != nil {
			operation.Methods = []string{string(*method)}
		}

		// path
		if path != nil {
			operator := "*" // gateway api defaults to PathMatchPathPrefix
			skip := false
			if path.Type != nil {
				switch *path.Type {
				case gatewayapiv1.PathMatchExact:
					operator = ""
				case gatewayapiv1.PathMatchRegularExpression:
					// ignore this rule as it is not supported by Istio - Authorino will check it anyway
					skip = true
				}
			}
			if !skip {
				value := "/"
				if path.Value != nil {
					value = *path.Value
				}
				operation.Paths = []string{fmt.Sprintf("%s%s", value, operator)}
			}
		}

		if operation != nil {
			istioRule.To = []*istiosecurity.Rule_To{
				{Operation: operation},
			}
		}

		// headers
		if len(match.Headers) > 0 {
			istioRule.When = []*istiosecurity.Condition{}

			for idx := range match.Headers {
				header := match.Headers[idx]
				if header.Type != nil && *header.Type == gatewayapiv1.HeaderMatchRegularExpression {
					// skip this rule as it is not supported by Istio - Authorino will check it anyway
					continue
				}
				headerCondition := &istiosecurity.Condition{
					Key:    fmt.Sprintf("request.headers[%s]", header.Name),
					Values: []string{header.Value},
				}
				istioRule.When = append(istioRule.When, headerCondition)
			}
		}

		// query params: istio does not support query params in authorization policies, so we build them in the authconfig instead

		istioRules = append(istioRules, istioRule)
	}
	return
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

	var update bool

	if !reflect.DeepEqual(existing.Spec.Action, desired.Spec.Action) {
		update = true
		existing.Spec.Action = desired.Spec.Action
	}

	if !reflect.DeepEqual(existing.Spec.ActionDetail, desired.Spec.ActionDetail) {
		update = true
		existing.Spec.ActionDetail = desired.Spec.ActionDetail
	}

	if !reflect.DeepEqual(existing.Spec.Rules, desired.Spec.Rules) {
		update = true
		existing.Spec.Rules = desired.Spec.Rules
	}

	if !reflect.DeepEqual(existing.Spec.Selector, desired.Spec.Selector) {
		update = true
		existing.Spec.Selector = desired.Spec.Selector
	}

	if !reflect.DeepEqual(existing.Annotations, desired.Annotations) {
		update = true
		existing.Annotations = desired.Annotations
	}

	return update, nil
}
