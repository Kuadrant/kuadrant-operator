package controllers

import (
	"fmt"
	"strings"

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
			(&AuthPolicyValidator{isGatewayAPIInstalled: isGatewayAPInstalled, isAuthorinoOperatorInstalled: isAuthorinoOperatorInstalled, isGatewayProviderInstalled: isGatewayProviderInstalled}).Subscription().Reconcile,
			(&RateLimitPolicyValidator{isGatewayAPIInstalled: isGatewayAPInstalled, isLimitadorOperatorInstalled: isLimitadorOperatorInstalled, isGatewayProviderInstalled: isGatewayProviderInstalled}).Subscription().Reconcile,
			(&TokenRateLimitPolicyValidator{isGatewayAPIInstalled: isGatewayAPInstalled, isLimitadorOperatorInstalled: isLimitadorOperatorInstalled, isGatewayProviderInstalled: isGatewayProviderInstalled}).Subscription().Reconcile,
		},
	}

	effectiveDataPlanePoliciesWorkflow := &controller.Workflow{
		Precondition: (&controller.Workflow{
			Tasks: []controller.ReconcileFunc{
				(&EffectiveAuthPolicyReconciler{client: client}).Subscription().Reconcile,
				(&EffectiveRateLimitPolicyReconciler{client: client}).Subscription().Reconcile,
				(&EffectiveTokenRateLimitPolicyReconciler{client: client}).Subscription().Reconcile,
			},
		}).Run,
		Tasks: []controller.ReconcileFunc{
			(&AuthConfigsReconciler{client: client}).Subscription().Reconcile,
			(&LimitadorLimitsReconciler{client: client}).Subscription().Reconcile,
		},
	}

	if isIstioInstalled {
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks, (&IstioRateLimitClusterReconciler{client: client}).Subscription().Reconcile)
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks, (&IstioExtensionReconciler{client: client}).Subscription().Reconcile)
	}

	if isEnvoyGatewayInstalled {
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks, (&EnvoyGatewayAuthClusterReconciler{client: client}).Subscription().Reconcile)
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks, (&EnvoyGatewayRateLimitClusterReconciler{client: client}).Subscription().Reconcile)
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks, (&EnvoyGatewayExtensionReconciler{client: client}).Subscription().Reconcile)
	}

	if isIstioInstalled && isAuthorinoOperatorInstalled && isLimitadorOperatorInstalled {
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks,
			NewPeerAuthenticationReconciler(mgr, client).Subscription().Reconcile,
			NewLimitadorIstioIntegrationReconciler(mgr, client).Subscription().Reconcile,
			NewAuthorinoIstioIntegrationReconciler(mgr, client).Subscription().Reconcile,
		)
	}

	dataPlanePoliciesStatus := &controller.Workflow{
		Tasks: []controller.ReconcileFunc{
			(&AuthPolicyStatusUpdater{client: client}).Subscription().Reconcile,
			(&RateLimitPolicyStatusUpdater{client: client}).Subscription().Reconcile,
			(&TokenRateLimitPolicyStatusUpdater{client: client}).Subscription().Reconcile,
		},
	}

	return &controller.Workflow{
		Precondition:  dataPlanePoliciesValidation.Run,
		Tasks:         []controller.ReconcileFunc{effectiveDataPlanePoliciesWorkflow.Run},
		Postcondition: dataPlanePoliciesStatus.Run,
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
