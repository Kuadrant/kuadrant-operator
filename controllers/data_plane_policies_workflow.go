package controllers

import (
	"fmt"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/env"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	kuadrantistio "github.com/kuadrant/kuadrant-operator/pkg/istio"
)

const (
	// make these configurable?
	istioGatewayControllerName        = "istio.io/gateway-controller"
	envoyGatewayGatewayControllerName = "gateway.envoyproxy.io/gatewayclass-controller"
)

var (
	WASMFilterImageURL = env.GetString("RELATED_IMAGE_WASMSHIM", "oci://quay.io/kuadrant/wasm-shim:latest")

	StateIstioExtensionsModified        = "IstioExtensionsModified"
	StateEnvoyGatewayExtensionsModified = "EnvoyGatewayExtensionsModified"

	// Event matchers to match events with potential impact on effective data plane policies (auth or rate limit)
	dataPlaneEffectivePoliciesEventMatchers = []controller.ResourceEventMatcher{
		{Kind: &kuadrantv1beta1.KuadrantGroupKind},
		{Kind: &machinery.GatewayClassGroupKind},
		{Kind: &machinery.GatewayGroupKind},
		{Kind: &machinery.HTTPRouteGroupKind},
		{Kind: &kuadrantv1beta3.RateLimitPolicyGroupKind},
		{Kind: &kuadrantv1beta1.LimitadorGroupKind},
		{Kind: &kuadrantv1beta3.AuthPolicyGroupKind},
		{Kind: &kuadrantv1beta1.AuthConfigGroupKind},
		{Kind: &kuadrantistio.EnvoyFilterGroupKind},
		{Kind: &kuadrantistio.WasmPluginGroupKind},
		{Kind: &kuadrantenvoygateway.EnvoyPatchPolicyGroupKind},
		{Kind: &kuadrantenvoygateway.EnvoyExtensionPolicyGroupKind},
	}
)

func NewDataPlanePoliciesWorkflow(client *dynamic.DynamicClient, isIstioInstalled, isEnvoyGatewayInstalled bool) *controller.Workflow {
	dataPlanePoliciesValidation := &controller.Workflow{
		Tasks: []controller.ReconcileFunc{
			(&AuthPolicyValidator{}).Subscription().Reconcile,
			(&RateLimitPolicyValidator{}).Subscription().Reconcile,
		},
	}

	effectiveDataPlanePoliciesWorkflow := &controller.Workflow{
		Precondition: (&controller.Workflow{
			Tasks: []controller.ReconcileFunc{
				(&EffectiveAuthPolicyReconciler{client: client}).Subscription().Reconcile,
				(&EffectiveRateLimitPolicyReconciler{client: client}).Subscription().Reconcile,
			},
		}).Run,
		Tasks: []controller.ReconcileFunc{
			(&AuthConfigsReconciler{client: client}).Subscription().Reconcile,
			(&LimitadorLimitsReconciler{client: client}).Subscription().Reconcile,
		},
	}

	if isIstioInstalled {
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks, (&IstioAuthClusterReconciler{client: client}).Subscription().Reconcile)
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks, (&IstioRateLimitClusterReconciler{client: client}).Subscription().Reconcile)
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks, (&IstioExtensionReconciler{client: client}).Subscription().Reconcile)
	}

	if isEnvoyGatewayInstalled {
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks, (&EnvoyGatewayAuthClusterReconciler{client: client}).Subscription().Reconcile)
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks, (&EnvoyGatewayRateLimitClusterReconciler{client: client}).Subscription().Reconcile)
		effectiveDataPlanePoliciesWorkflow.Tasks = append(effectiveDataPlanePoliciesWorkflow.Tasks, (&EnvoyGatewayExtensionReconciler{client: client}).Subscription().Reconcile)
	}

	dataPlanePoliciesStatus := &controller.Workflow{
		Tasks: []controller.ReconcileFunc{
			(&AuthPolicyStatusUpdater{client: client}).Subscription().Reconcile,
			(&RateLimitPolicyStatusUpdater{client: client}).Subscription().Reconcile,
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
