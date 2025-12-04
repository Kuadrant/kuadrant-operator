package controllers

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	controllerruntime "sigs.k8s.io/controller-runtime"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/env"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantauthorino "github.com/kuadrant/kuadrant-operator/internal/authorino"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/internal/envoygateway"
	kuadrantistio "github.com/kuadrant/kuadrant-operator/internal/istio"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

const (
	defaultIstioGatewayControllerName        = "istio.io/gateway-controller"
	defaultEnvoyGatewayGatewayControllerName = "gateway.envoyproxy.io/gatewayclass-controller"
)

var (
	WASMFilterImageURL = env.GetString("RELATED_IMAGE_WASMSHIM", "oci://quay.io/kuadrant/wasm-shim:latest")
	// protectedRegistry this defines a default protected registry. If this is in the wasm image URL we add a pull secret name to the WASMPLugin resource
	ProtectedRegistry = env.GetString("PROTECTED_REGISTRY", "registry.redhat.io")

	// registryPullSecretName this is the pull secret name we will add to the WASMPlugin if the URL for he image is from the defined PROTECTED_REGISTRY
	RegistryPullSecretName = "wasm-plugin-pull-secret"

	StateIstioExtensionsModified        = "IstioExtensionsModified"
	StateEnvoyGatewayExtensionsModified = "EnvoyGatewayExtensionsModified"

	// Event matchers to match events with potential impact on effective data plane policies (auth or rate limit)
	dataPlaneEffectivePoliciesEventMatchers = []controller.ResourceEventMatcher{
		{Kind: &kuadrantv1beta1.KuadrantGroupKind},
		{Kind: &machinery.GatewayClassGroupKind},
		{Kind: &machinery.GatewayGroupKind},
		{Kind: &machinery.HTTPRouteGroupKind},
		{Kind: &kuadrantv1.RateLimitPolicyGroupKind},
		{Kind: &kuadrantv1alpha1.TokenRateLimitPolicyGroupKind},
		{Kind: &kuadrantv1beta1.LimitadorGroupKind},
		{Kind: &kuadrantv1.AuthPolicyGroupKind},
		{Kind: &kuadrantauthorino.AuthConfigGroupKind},
		{Kind: &kuadrantistio.EnvoyFilterGroupKind},
		{Kind: &kuadrantistio.WasmPluginGroupKind},
		{Kind: &kuadrantenvoygateway.EnvoyPatchPolicyGroupKind},
		{Kind: &kuadrantenvoygateway.EnvoyExtensionPolicyGroupKind},
	}

	istioGatewayControllerNames        = getGatewayControllerNames("ISTIO_GATEWAY_CONTROLLER_NAMES", defaultIstioGatewayControllerName)
	envoyGatewayGatewayControllerNames = getGatewayControllerNames("ENVOY_GATEWAY_GATEWAY_CONTROLLER_NAMES", defaultEnvoyGatewayGatewayControllerName)
)

//+kubebuilder:rbac:groups=kuadrant.io,resources=authpolicies,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=authpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=authpolicies/finalizers,verbs=update

//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies/finalizers,verbs=update

//+kubebuilder:rbac:groups=kuadrant.io,resources=tokenratelimitpolicies,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=tokenratelimitpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=tokenratelimitpolicies/finalizers,verbs=update

func NewDataPlanePoliciesWorkflow(mgr controllerruntime.Manager, client *dynamic.DynamicClient, isGatewayAPInstalled, isIstioInstalled, isEnvoyGatewayInstalled, isLimitadorOperatorInstalled, isAuthorinoOperatorInstalled bool) *controller.Workflow {
	isGatewayProviderInstalled := isIstioInstalled || isEnvoyGatewayInstalled
	dataPlanePoliciesValidation := &controller.Workflow{
		Tasks: []controller.ReconcileFunc{
			traceReconcileFunc("validator.auth_policy", (&AuthPolicyValidator{isGatewayAPIInstalled: isGatewayAPInstalled, isAuthorinoOperatorInstalled: isAuthorinoOperatorInstalled, isGatewayProviderInstalled: isGatewayProviderInstalled}).Subscription().Reconcile),
			traceReconcileFunc("validator.ratelimit_policy", (&RateLimitPolicyValidator{isGatewayAPIInstalled: isGatewayAPInstalled, isLimitadorOperatorInstalled: isLimitadorOperatorInstalled, isGatewayProviderInstalled: isGatewayProviderInstalled}).Subscription().Reconcile),
			traceReconcileFunc("validator.token_ratelimit_policy", (&TokenRateLimitPolicyValidator{isGatewayAPIInstalled: isGatewayAPInstalled, isLimitadorOperatorInstalled: isLimitadorOperatorInstalled, isGatewayProviderInstalled: isGatewayProviderInstalled}).Subscription().Reconcile),
		},
	}

	effectiveDataPlanePoliciesWorkflow := &controller.Workflow{
		Precondition: traceReconcileFunc("effective_policies.compute", (&controller.Workflow{
			Tasks: []controller.ReconcileFunc{
				traceReconcileFunc("effective_policies.auth", (&EffectiveAuthPolicyReconciler{client: client}).Subscription().Reconcile),
				traceReconcileFunc("effective_policies.ratelimit", (&EffectiveRateLimitPolicyReconciler{client: client}).Subscription().Reconcile),
				traceReconcileFunc("effective_policies.token_ratelimit", (&EffectiveTokenRateLimitPolicyReconciler{client: client}).Subscription().Reconcile),
			},
		}).Run),
		Tasks: []controller.ReconcileFunc{
			traceReconcileFunc("reconciler.auth_configs", (&AuthConfigsReconciler{client: client}).Subscription().Reconcile),
			traceReconcileFunc("reconciler.limitador_limits", (&LimitadorLimitsReconciler{client: client}).Subscription().Reconcile),
		},
	}

	if isIstioInstalled {
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks,
			traceReconcileFunc("reconciler.istio_auth_cluster", (&IstioAuthClusterReconciler{client: client}).Subscription().Reconcile))
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks,
			traceReconcileFunc("reconciler.istio_ratelimit_cluster", (&IstioRateLimitClusterReconciler{client: client}).Subscription().Reconcile))
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks,
			traceReconcileFunc("reconciler.istio_tracing_cluster", (&IstioTracingClusterReconciler{client: client}).Subscription().Reconcile))
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks,
			traceReconcileFunc("reconciler.istio_extension", (&IstioExtensionReconciler{client: client}).Subscription().Reconcile))
	}

	if isEnvoyGatewayInstalled {
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks,
			traceReconcileFunc("reconciler.envoy_gateway_auth_cluster", (&EnvoyGatewayAuthClusterReconciler{client: client}).Subscription().Reconcile))
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks,
			traceReconcileFunc("reconciler.envoy_gateway_ratelimit_cluster", (&EnvoyGatewayRateLimitClusterReconciler{client: client}).Subscription().Reconcile))
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks,
			traceReconcileFunc("reconciler.envoy_gateway_tracing_cluster", (&EnvoyGatewayTracingClusterReconciler{client: client}).Subscription().Reconcile))
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks,
			traceReconcileFunc("reconciler.envoy_gateway_extension", (&EnvoyGatewayExtensionReconciler{client: client}).Subscription().Reconcile))
	}

	if isIstioInstalled && isAuthorinoOperatorInstalled && isLimitadorOperatorInstalled {
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks,
			traceReconcileFunc("reconciler.peer_authentication", NewPeerAuthenticationReconciler(mgr, client).Subscription().Reconcile),
			traceReconcileFunc("reconciler.limitador_istio_integration", NewLimitadorIstioIntegrationReconciler(mgr, client).Subscription().Reconcile),
			traceReconcileFunc("reconciler.authorino_istio_integration", NewAuthorinoIstioIntegrationReconciler(mgr, client).Subscription().Reconcile),
		)
	}

	dataPlanePoliciesStatus := &controller.Workflow{
		Tasks: []controller.ReconcileFunc{
			traceReconcileFunc("status.auth_policy", (&AuthPolicyStatusUpdater{client: client}).Subscription().Reconcile),
			traceReconcileFunc("status.ratelimit_policy", (&RateLimitPolicyStatusUpdater{client: client}).Subscription().Reconcile),
			traceReconcileFunc("status.token_ratelimit_policy", (&TokenRateLimitPolicyStatusUpdater{client: client}).Subscription().Reconcile),
		},
	}

	return &controller.Workflow{
		Precondition:  traceReconcileFunc("validation", dataPlanePoliciesValidation.Run),
		Tasks:         []controller.ReconcileFunc{traceReconcileFunc("effective_policies", effectiveDataPlanePoliciesWorkflow.Run)},
		Postcondition: traceReconcileFunc("status_update", dataPlanePoliciesStatus.Run),
	}
}

func gatewayComponentsToSync(gateway *machinery.Gateway, componentGroupKind schema.GroupKind, modifiedGatewayLocators any, topology *machinery.Topology, requiredCondition func(machinery.Object) bool) []string {
	missingConditionInTopologyFunc := func() bool {
		obj, found := lo.Find(topology.Objects().Children(gateway), func(child machinery.Object) bool {
			return child.GroupVersionKind().GroupKind() == componentGroupKind
		})
		return !found || !requiredCondition(obj)
	}
	if (modifiedGatewayLocators != nil && lo.Contains(modifiedGatewayLocators.([]string), gateway.GetLocator())) || missingConditionInTopologyFunc() {
		return []string{fmt.Sprintf("%s (%s/%s)", componentGroupKind.Kind, gateway.GetNamespace(), gateway.GetName())}
	}
	return nil
}

func getGatewayControllerNames(envName string, defaultGatewayControllerName string) []gatewayapiv1.GatewayController {
	envValue := env.GetString(envName, defaultGatewayControllerName)
	gatewayControllers := lo.Map(strings.Split(envValue, ","), func(c string, _ int) gatewayapiv1.GatewayController {
		return gatewayapiv1.GatewayController(strings.TrimSpace(c))
	})

	if envValue == defaultGatewayControllerName {
		return gatewayControllers
	}
	return append(gatewayControllers, gatewayapiv1.GatewayController(defaultGatewayControllerName))
}

func defaultGatewayControllerName(controllerName gatewayapiv1.GatewayController) gatewayapiv1.GatewayController {
	if lo.Contains(istioGatewayControllerNames, controllerName) {
		return defaultIstioGatewayControllerName
	} else if lo.Contains(envoyGatewayGatewayControllerNames, controllerName) {
		return defaultEnvoyGatewayGatewayControllerName
	}
	return "Unknown"
}

func mergeAndVerify(ctx context.Context, actions []wasm.Action) ([]wasm.Action, error) {
	tracer := controller.TracerFromContext(ctx)
	_, span := tracer.Start(ctx, "wasm.MergeAndVerifyActions")
	defer span.End()

	span.SetAttributes(
		attribute.Int("actions.input", len(actions)),
	)

	if len(actions) == 0 {
		span.SetAttributes(attribute.Int("actions.output", 0))
		span.SetStatus(codes.Ok, "")
		return nil, nil
	}

	result := []wasm.Action{actions[0]}
	for _, currentAction := range actions[1:] {
		lastAction := &result[len(result)-1]

		if lastAction.Scope == currentAction.Scope &&
			lastAction.ServiceName == currentAction.ServiceName && lastAction.ServiceName != wasm.AuthServiceName {
			lastAction.ConditionalData = append(lastAction.ConditionalData, currentAction.ConditionalData...)
			// Merge source policy locators - deduplicate them
			lastAction.SourcePolicyLocators = lo.Uniq(append(lastAction.SourcePolicyLocators, currentAction.SourcePolicyLocators...))
			slices.Sort(lastAction.SourcePolicyLocators)
		} else {
			result = append(result, currentAction)
		}
	}

	for i := range result {
		keyValueMap := make(map[string]string)
		for _, conditionalData := range result[i].ConditionalData {
			for _, data := range conditionalData.Data {
				var key, value string

				switch val := data.Value.(type) {
				case *wasm.Static:
					key = val.Static.Key
					value = val.Static.Value
				case *wasm.Expression:
					key = val.ExpressionItem.Key
					value = val.ExpressionItem.Value
				}

				if existingValue, exists := keyValueMap[key]; exists {
					if existingValue != value {
						span.RecordError(fmt.Errorf("duplicate key '%s' with different values", key))
						span.SetStatus(codes.Error, "duplicate key conflict")
						return nil, fmt.Errorf("duplicate key '%s' with different values found in action", key)
					}
				} else {
					keyValueMap[key] = value
				}
			}
		}
	}

	span.SetAttributes(
		attribute.Int("actions.output", len(result)),
		attribute.Int("actions.merged", len(actions)-len(result)),
	)
	span.SetStatus(codes.Ok, "")

	return result, nil
}
