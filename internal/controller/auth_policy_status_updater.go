package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	envoygatewayv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/go-logr/logr"
	authorinooperatorv1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantauthorino "github.com/kuadrant/kuadrant-operator/internal/authorino"
	"github.com/kuadrant/kuadrant-operator/internal/cel"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/internal/envoygateway"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/internal/gatewayapi"
	kuadrantistio "github.com/kuadrant/kuadrant-operator/internal/istio"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	kuadrantpolicymachinery "github.com/kuadrant/kuadrant-operator/internal/policymachinery"
)

type AuthPolicyStatusUpdater struct {
	client *dynamic.DynamicClient
}

// AuthPolicyStatusUpdater reconciles to events with impact to change the status of AuthPolicy resources
func (r *AuthPolicyStatusUpdater) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.UpdateStatus,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1.AuthPolicyGroupKind},
			{Kind: &kuadrantauthorino.AuthConfigGroupKind},
			{Kind: &kuadrantistio.EnvoyFilterGroupKind},
			{Kind: &kuadrantistio.WasmPluginGroupKind},
			{Kind: &kuadrantenvoygateway.EnvoyPatchPolicyGroupKind},
			{Kind: &kuadrantenvoygateway.EnvoyExtensionPolicyGroupKind},
		},
	}
}

func (r *AuthPolicyStatusUpdater) UpdateStatus(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("AuthPolicyStatusUpdater").WithValues("context", ctx)
	tracer := otel.Tracer("kuadrant-operator")

	policies := lo.FilterMap(topology.Policies().Items(), func(item machinery.Policy, _ int) (*kuadrantv1.AuthPolicy, bool) {
		p, ok := item.(*kuadrantv1.AuthPolicy)
		return p, ok
	})

	policyAcceptedFunc := authPolicyAcceptedStatusFunc(state)

	logger.V(1).Info("updating authpolicy statuses", "policies", len(policies))
	defer logger.V(1).Info("finished updating authpolicy statuses")

	for _, policy := range policies {
		policyCtx, span := tracer.Start(ctx, "policy.AuthPolicy")
		span.SetAttributes(
			attribute.String("policy.name", policy.GetName()),
			attribute.String("policy.namespace", policy.GetNamespace()),
			attribute.String("policy.kind", kuadrantv1.AuthPolicyGroupKind.Kind),
			attribute.String("policy.uid", string(policy.GetUID())),
		)

		if policy.GetDeletionTimestamp() != nil {
			logger.V(1).Info("authpolicy is marked for deletion, skipping", "name", policy.Name, "namespace", policy.Namespace)
			span.AddEvent("policy marked for deletion, skipping")
			span.SetStatus(codes.Ok, "")
			span.End()
			continue
		}

		// copy initial conditions, otherwise status will always be updated
		newStatus := &kuadrantv1.AuthPolicyStatus{
			Conditions:         slices.Clone(policy.Status.Conditions),
			ObservedGeneration: policy.Status.ObservedGeneration,
		}

		accepted, err := policyAcceptedFunc(policy)
		meta.SetStatusCondition(&newStatus.Conditions, *kuadrant.AcceptedCondition(policy, err))

		// do not set enforced condition if Accepted condition is false
		if !accepted {
			meta.RemoveStatusCondition(&newStatus.Conditions, string(kuadrant.PolicyConditionEnforced))
		} else {
			enforcedCond := r.enforcedCondition(policy, topology, state, logger)
			meta.SetStatusCondition(&newStatus.Conditions, *enforcedCond)
		}

		equalStatus := equality.Semantic.DeepEqual(newStatus, policy.Status)
		if equalStatus && policy.Generation == policy.Status.ObservedGeneration {
			logger.V(1).Info("policy status unchanged, skipping update")
			span.AddEvent("policy status unchanged, skipping update")
			span.SetStatus(codes.Ok, "")
			span.End()
			continue
		}
		newStatus.ObservedGeneration = policy.Generation
		policy.Status = *newStatus

		obj, err := controller.Destruct(policy)
		if err != nil {
			logger.Error(err, "unable to destruct policy") // should never happen
			span.RecordError(err)
			span.SetStatus(codes.Error, "unable to destruct policy")
			span.End()
			continue
		}

		_, err = r.client.Resource(kuadrantv1.AuthPoliciesResource).Namespace(policy.GetNamespace()).UpdateStatus(policyCtx, obj, metav1.UpdateOptions{})
		if err != nil {
			if strings.Contains(err.Error(), "StorageError: invalid object") {
				logger.Info("possible error updating resource", "err", err, "possible_cause", "resource has being removed from the cluster already")
				span.AddEvent("resource already removed from cluster")
				span.SetStatus(codes.Ok, "")
				span.End()
				continue
			}
			logger.Error(err, "unable to update status for authpolicy", "name", policy.GetName(), "namespace", policy.GetNamespace())
			span.RecordError(err)
			span.SetStatus(codes.Error, "unable to update status")
			span.End()
			// TODO: handle error
			continue
		}

		span.AddEvent("policy status updated successfully")
		span.SetStatus(codes.Ok, "")
		span.End()
	}

	return nil
}

func (r *AuthPolicyStatusUpdater) enforcedCondition(policy *kuadrantv1.AuthPolicy, topology *machinery.Topology, state *sync.Map, logger logr.Logger) *metav1.Condition {
	kObj := GetKuadrantFromTopology(topology)
	if kObj == nil {
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrSystemResource("kuadrant"), false)
	}
	policyKind := kuadrantv1.AuthPolicyGroupKind.Kind

	effectivePolicies, ok := state.Load(StateEffectiveAuthPolicies)
	if !ok {
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrUnknown(policyKind, ErrMissingStateEffectiveAuthPolicies), false)
	}

	type affectedGateway struct {
		gateway      *machinery.Gateway
		gatewayClass *machinery.GatewayClass
	}

	// check the state of the rules of the policy in the effective policies
	policyRuleKeys := lo.Keys(policy.Rules())
	overridingPolicies := map[string][]string{}                     // policyRuleKey → locators of policies overriding the policy rule
	affectedGateways := map[string]affectedGateway{}                // Gateway locator → {GatewayClass, Gateway}
	affectedHTTPRouteRules := map[string]*machinery.HTTPRouteRule{} // pathID → HTTPRouteRule
	setAffectedObjects := func(pathID string, gatewayClass *machinery.GatewayClass, gateway *machinery.Gateway, httpRouteRule *machinery.HTTPRouteRule) {
		affectedGateways[gateway.GetLocator()] = affectedGateway{
			gateway:      gateway,
			gatewayClass: gatewayClass,
		}
		affectedHTTPRouteRules[pathID] = httpRouteRule
	}

	var celValidationErrors []error
	var celIssuesByPathID map[string][]*cel.Issue
	var celIssuesFound bool
	stateCelValErrors, stateCelValErrorsFound := state.Load(cel.StateCELValidationErrors)
	if stateCelValErrorsFound {
		celIssuesCollection := stateCelValErrors.(*cel.IssueCollection)
		celIssuesByPathID, celIssuesFound = celIssuesCollection.GetByPolicyKind(policyKind)
	}

	for pathID, effectivePolicy := range effectivePolicies.(EffectiveAuthPolicies) {
		if len(kuadrantv1.PoliciesInPath(effectivePolicy.Path, func(p machinery.Policy) bool { return p.GetLocator() == policy.GetLocator() })) == 0 {
			continue
		}

		if celIssuesFound {
			storedValidationIssuesForPathID, storedValidationIssuesForPathIDFound := celIssuesByPathID[kuadrantv1.PathID(effectivePolicy.Path)]
			if storedValidationIssuesForPathIDFound {
				celValidationErrors = append(celValidationErrors, lo.Map(storedValidationIssuesForPathID, func(i *cel.Issue, _ int) error { return i.GetError() })...)
			}
		}

		gatewayClass, gateway, listener, httpRoute, httpRouteRule, err := kuadrantpolicymachinery.ObjectsInRequestPath(effectivePolicy.Path)
		if err != nil {
			if errors.As(err, &kuadrantpolicymachinery.ErrInvalidPath{}) {
				logger.V(1).Info("skipping effectivePolicy for invalid path", "path", effectivePolicy.Path)
			} else {
				logger.Error(err, "unable to process effectivePolicy", "path", effectivePolicy.Path)
			}
			continue
		}
		if !kuadrantgatewayapi.IsListenerReady(listener.Listener, gateway.Gateway) || !kuadrantgatewayapi.IsHTTPRouteReady(httpRoute.HTTPRoute, gateway.Gateway, gatewayClass.Spec.ControllerName) {
			continue
		}
		effectivePolicyRules := effectivePolicy.Spec.Rules()
		if len(effectivePolicyRules) > 0 {
			for _, policyRuleKey := range policyRuleKeys {
				if effectivePolicyRule, ok := effectivePolicyRules[policyRuleKey]; !ok || (ok && effectivePolicyRule.GetSource() != policy.GetLocator()) { // policy rule has been overridden by another policy
					var overriddenBy string
					if ok { // TODO(guicassolato): !ok → we cannot tell which policy is overriding the rule, this information is lost when the policy rule is dropped during an atomic override
						overriddenBy = effectivePolicyRule.GetSource()
					}
					overridingPolicies[policyRuleKey] = append(overridingPolicies[policyRuleKey], overriddenBy)
					continue
				}
				// policy rule is in the effective policy, track the Gateway and the HTTPRouteRule affected by the policy
				setAffectedObjects(pathID, gatewayClass, gateway, httpRouteRule)
			}
			continue
		}
		// effective policy has no rules, track the Gateway and the HTTPRouteRule affected by the policy
		setAffectedObjects(pathID, gatewayClass, gateway, httpRouteRule)
	}

	if len(affectedGateways) == 0 { // no rules of the policy found in the effective policies
		if len(overridingPolicies) == 0 { // no rules of the policy have been overridden by any other policy
			return kuadrant.EnforcedCondition(policy, kuadrant.NewErrNoRoutes(policyKind), false)
		}
		// all rules of the policy have been overridden by at least one other policy
		overridingPoliciesKeys := lo.FilterMap(lo.Uniq(lo.Flatten(lo.Values(overridingPolicies))), func(policyLocator string, _ int) (k8stypes.NamespacedName, bool) {
			policyKey, err := kuadrantpolicymachinery.NamespacedNameFromLocator(policyLocator)
			return policyKey, err == nil
		})
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrOverridden(policyKind, overridingPoliciesKeys), false)
	}

	var componentsToSync []string

	// check the status of Authorino
	authorino := GetAuthorinoFromTopology(topology)
	if authorino == nil {
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrSystemResource("authornio"), false)
	}
	if !meta.IsStatusConditionTrue(lo.Map(authorino.Status.Conditions, authorinoOperatorConditionToProperConditionFunc), string(authorinooperatorv1beta1.ConditionReady)) {
		componentsToSync = append(componentsToSync, kuadrantv1beta1.AuthorinoGroupKind.Kind)
	}

	// check status of the authconfigs
	isAuthConfigReady := authConfigReadyStatusFunc(state)
	for pathID, httpRouteRule := range affectedHTTPRouteRules {
		authConfigName := AuthConfigNameForPath(pathID)
		authConfig, found := lo.Find(topology.Objects().Children(httpRouteRule), func(authConfig machinery.Object) bool {
			return authConfig.GroupVersionKind().GroupKind() == kuadrantauthorino.AuthConfigGroupKind && authConfig.GetName() == authConfigName
		})
		if !found || !isAuthConfigReady(authConfig.(*controller.RuntimeObject).Object.(*authorinov1beta3.AuthConfig)) {
			componentsToSync = append(componentsToSync, fmt.Sprintf("%s (%s)", kuadrantauthorino.AuthConfigGroupKind.Kind, authConfigName))
		}
	}

	// check the status of the gateways' configuration resources
	for _, g := range affectedGateways {
		controllerName := g.gatewayClass.Spec.ControllerName
		switch defaultGatewayControllerName(controllerName) {
		case defaultIstioGatewayControllerName:
			// EnvoyFilter
			istioAuthClustersModifiedGateways, _ := state.Load(StateIstioAuthClustersModified)
			componentsToSync = append(componentsToSync, gatewayComponentsToSync(g.gateway, kuadrantistio.EnvoyFilterGroupKind, istioAuthClustersModifiedGateways, topology, func(_ machinery.Object) bool {
				// return meta.IsStatusConditionTrue(lo.Map(obj.(*controller.RuntimeObject).Object.(*istioclientgonetworkingv1alpha3.EnvoyFilter).Status.Conditions, kuadrantistio.ConditionToProperConditionFunc), "Ready")
				return true // Istio won't ever populate the status stanza of EnvoyFilter resources, so we cannot expect to find a given a condition there
			})...)
			// WasmPlugin
			istioExtensionsModifiedGateways, _ := state.Load(StateIstioExtensionsModified)
			componentsToSync = append(componentsToSync, gatewayComponentsToSync(g.gateway, kuadrantistio.WasmPluginGroupKind, istioExtensionsModifiedGateways, topology, func(_ machinery.Object) bool {
				// return meta.IsStatusConditionTrue(lo.Map(obj.(*controller.RuntimeObject).Object.(*istioclientgoextensionv1alpha1.WasmPlugin).Status.Conditions, kuadrantistio.ConditionToProperConditionFunc), "Ready")
				return true // Istio won't ever populate the status stanza of WasmPlugin resources, so we cannot expect to find a given a condition there
			})...)
		case defaultEnvoyGatewayGatewayControllerName:
			gatewayAncestor := gatewayapiv1.ParentReference{Name: gatewayapiv1.ObjectName(g.gateway.GetName()), Namespace: ptr.To(gatewayapiv1.Namespace(g.gateway.GetNamespace()))}
			// EnvoyPatchPolicy
			envoyGatewayAuthClustersModifiedGateways, _ := state.Load(StateEnvoyGatewayAuthClustersModified)
			componentsToSync = append(componentsToSync, gatewayComponentsToSync(g.gateway, kuadrantenvoygateway.EnvoyPatchPolicyGroupKind, envoyGatewayAuthClustersModifiedGateways, topology, func(obj machinery.Object) bool {
				return meta.IsStatusConditionTrue(kuadrantgatewayapi.PolicyStatusConditionsFromAncestor(obj.(*controller.RuntimeObject).Object.(*envoygatewayv1alpha1.EnvoyPatchPolicy).Status, controllerName, gatewayAncestor, gatewayapiv1.Namespace(obj.GetNamespace())), string(envoygatewayv1alpha1.PolicyConditionProgrammed))
			})...)
			// EnvoyExtensionPolicy
			envoyGatewayExtensionsModifiedGateways, _ := state.Load(StateEnvoyGatewayExtensionsModified)
			componentsToSync = append(componentsToSync, gatewayComponentsToSync(g.gateway, kuadrantenvoygateway.EnvoyExtensionPolicyGroupKind, envoyGatewayExtensionsModifiedGateways, topology, func(obj machinery.Object) bool {
				return meta.IsStatusConditionTrue(kuadrantgatewayapi.PolicyStatusConditionsFromAncestor(obj.(*controller.RuntimeObject).Object.(*envoygatewayv1alpha1.EnvoyExtensionPolicy).Status, controllerName, gatewayAncestor, gatewayapiv1.Namespace(obj.GetNamespace())), string(gatewayapiv1alpha2.PolicyConditionAccepted))
			})...)
		default:
			componentsToSync = append(componentsToSync, fmt.Sprintf("%s (%s/%s)", machinery.GatewayGroupKind.Kind, g.gateway.GetNamespace(), g.gateway.GetName()))
		}
	}

	if len(celValidationErrors) > 0 {
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrCelValidation(celValidationErrors), false)
	}

	if len(componentsToSync) > 0 {
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrOutOfSync(policyKind, componentsToSync), false)
	}

	return kuadrant.EnforcedCondition(policy, nil, len(overridingPolicies) == 0)
}

func authorinoOperatorConditionToProperConditionFunc(condition authorinooperatorv1beta1.Condition, _ int) metav1.Condition {
	return metav1.Condition{
		Type:    string(condition.Type),
		Status:  metav1.ConditionStatus(condition.Status),
		Reason:  condition.Reason,
		Message: condition.Message,
	}
}

func authorinoConditionToProperConditionFunc(cond authorinov1beta3.AuthConfigStatusCondition, _ int) metav1.Condition {
	return metav1.Condition{
		Type:    string(cond.Type),
		Status:  metav1.ConditionStatus(cond.Status),
		Reason:  cond.Reason,
		Message: cond.Message,
	}
}

func authConfigReadyStatusFunc(state *sync.Map) func(authConfig *authorinov1beta3.AuthConfig) bool {
	modifiedAuthConfigs, modified := state.Load(StateModifiedAuthConfigs)
	if !modified {
		return authConfigReadyStatus
	}
	modifiedAuthConfigsList := modifiedAuthConfigs.([]string)
	return func(authConfig *authorinov1beta3.AuthConfig) bool {
		if lo.Contains(modifiedAuthConfigsList, authConfig.GetName()) {
			return false
		}
		return authConfigReadyStatus(authConfig)
	}
}

func authConfigReadyStatus(authConfig *authorinov1beta3.AuthConfig) bool {
	if condition := meta.FindStatusCondition(lo.Map(authConfig.Status.Conditions, authorinoConditionToProperConditionFunc), string(authorinov1beta3.StatusConditionReady)); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}
