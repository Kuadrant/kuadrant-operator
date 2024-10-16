package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/samber/lo"
	istiosecurity "istio.io/api/security/v1beta1"
	istiov1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/env"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantistioutils "github.com/kuadrant/kuadrant-operator/pkg/istio"
	"github.com/kuadrant/kuadrant-operator/pkg/kuadranttools"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var KuadrantExtAuthProviderName = env.GetString("AUTH_PROVIDER", "kuadrant-authorization")

// AuthPolicyIstioAuthorizationPolicyReconciler reconciles IstioAuthorizationPolicy objects for auth
type AuthPolicyIstioAuthorizationPolicyReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=security.istio.io,resources=authorizationpolicies,verbs=get;list;watch;create;update;patch;delete

func (r *AuthPolicyIstioAuthorizationPolicyReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("Gateway", req.NamespacedName, "request id", uuid.NewString())
	logger.Info("Reconciling istio AuthorizationPolicy")
	ctx := logr.NewContext(eventCtx, logger)

	gw := &gatewayapiv1.Gateway{}
	if err := r.Client().Get(ctx, req.NamespacedName, gw); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no gateway found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get gateway")
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(gw, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	if !kuadrant.IsKuadrantManaged(gw) {
		return ctrl.Result{}, nil
	}

	topology, err := kuadranttools.TopologyFromGateway(ctx, r.Client(), gw, kuadrantv1beta3.NewAuthPolicyType())
	if err != nil {
		return ctrl.Result{}, err
	}
	topologyIndex := kuadrantgatewayapi.NewTopologyIndexes(topology)
	policies := lo.FilterMap(topologyIndex.PoliciesFromGateway(gw), func(policy kuadrantgatewayapi.Policy, _ int) (*kuadrantv1beta3.AuthPolicy, bool) {
		ap, ok := policy.(*kuadrantv1beta3.AuthPolicy)
		if !ok {
			return nil, false
		}
		return ap, true
	})

	for _, policy := range policies {
		iap, err := r.istioAuthorizationPolicy(ctx, gw, policy, topologyIndex, topology)
		if err != nil {
			return ctrl.Result{}, err
		}

		if policy.GetDeletionTimestamp() != nil {
			utils.TagObjectToDelete(iap)
		}

		if err := r.ReconcileResource(ctx, &istiov1beta1.AuthorizationPolicy{}, iap, kuadrantistioutils.AuthorizationPolicyMutator); err != nil && !apierrors.IsAlreadyExists(err) {
			logger.Error(err, "failed to reconcile IstioAuthorizationPolicy resource")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *AuthPolicyIstioAuthorizationPolicyReconciler) istioAuthorizationPolicy(ctx context.Context, gateway *gatewayapiv1.Gateway, ap *kuadrantv1beta3.AuthPolicy, topologyIndex *kuadrantgatewayapi.TopologyIndexes, topology *kuadrantgatewayapi.Topology) (*istiov1beta1.AuthorizationPolicy, error) {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("istioAuthorizationPolicy")

	iap := &istiov1beta1.AuthorizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      IstioAuthorizationPolicyName(gateway.Name, ap.GetTargetRef()),
			Namespace: gateway.Namespace,
			Labels:    istioAuthorizationPolicyLabels(client.ObjectKeyFromObject(gateway), client.ObjectKeyFromObject(ap)),
		},
		Spec: istiosecurity.AuthorizationPolicy{
			Action:    istiosecurity.AuthorizationPolicy_CUSTOM,
			TargetRef: kuadrantistioutils.PolicyTargetRefFromGateway(gateway),
			ActionDetail: &istiosecurity.AuthorizationPolicy_Provider{
				Provider: &istiosecurity.AuthorizationPolicy_ExtensionProvider{
					Name: KuadrantExtAuthProviderName,
				},
			},
		},
	}

	gwHostnames := kuadrantgatewayapi.GatewayHostnames(gateway)
	if len(gwHostnames) == 0 {
		gwHostnames = []gatewayapiv1.Hostname{"*"}
	}

	var route *gatewayapiv1.HTTPRoute
	var routeHostnames []gatewayapiv1.Hostname
	targetNetworkObject := topologyIndex.GetPolicyTargetObject(ap)

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
		routes := topology.Routes()
		for idx := range routes {
			route := routes[idx].Route()
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

	rules := istioAuthorizationPolicyRulesFromHTTPRoute(route)
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

	if err := r.SetOwnerReference(gateway, iap); err != nil {
		return nil, err
	}

	return iap, nil
}

func (r *AuthPolicyIstioAuthorizationPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantistioutils.IsAuthorizationPolicyInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("Istio AuthorizationPolicy controller disabled. Istio was not found")
		return nil
	}

	ok, err = kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("Istio AuthorizationPolicy controller disabled. GatewayAPI was not found")
		return nil
	}

	httpRouteToParentGatewaysEventMapper := mappers.NewHTTPRouteToParentGatewaysEventMapper(
		mappers.WithLogger(r.Logger().WithName("httpRouteToParentGatewaysEventMapper")),
	)

	apToParentGatewaysEventMapper := mappers.NewPolicyToParentGatewaysEventMapper(
		mappers.WithLogger(r.Logger().WithName("authPolicyToParentGatewaysEventMapper")),
		mappers.WithClient(r.Client()),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayapiv1.Gateway{}).
		Owns(&istiov1beta1.AuthorizationPolicy{}).
		Watches(
			&gatewayapiv1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(httpRouteToParentGatewaysEventMapper.Map),
		).
		Watches(
			&kuadrantv1beta3.AuthPolicy{},
			handler.EnqueueRequestsFromMapFunc(apToParentGatewaysEventMapper.Map),
		).
		Complete(r)
}

// IstioAuthorizationPolicyName generates the name of an AuthorizationPolicy.
func IstioAuthorizationPolicyName(gwName string, targetRef gatewayapiv1alpha2.LocalPolicyTargetReference) string {
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

// istioAuthorizationPolicyRulesFromHTTPRoute builds a list of Istio AuthorizationPolicy rules from an HTTPRoute.
// v1beta2 version of this function used RouteSelectors
// v1beta3 should use Section Names, once implemented
func istioAuthorizationPolicyRulesFromHTTPRoute(route *gatewayapiv1.HTTPRoute) []*istiosecurity.Rule {
	istioRules := make([]*istiosecurity.Rule, 0)
	for _, rule := range route.Spec.Rules {
		istioRules = append(istioRules, istioAuthorizationPolicyRulesFromHTTPRouteRule(rule, []gatewayapiv1.Hostname{"*"})...)
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
