package controllers

import (
	"fmt"
	"sync"

	"k8s.io/client-go/dynamic"
	"k8s.io/utils/env"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	kuadrantistio "github.com/kuadrant/kuadrant-operator/pkg/istio"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

var (
	WASMFilterImageURL = env.GetString("RELATED_IMAGE_WASMSHIM", "oci://quay.io/kuadrant/wasm-shim:latest")

	StateRateLimitPolicyValid       = "RateLimitPolicyValid"
	StateEffectiveRateLimitPolicies = "EffectiveRateLimitPolicies"

	ErrMissingTarget                          = fmt.Errorf("target not found")
	ErrMissingLimitador                       = fmt.Errorf("missing limitador object in the topology")
	ErrMissingStateEffectiveRateLimitPolicies = fmt.Errorf("missing rate limit effective policies stored in the reconciliation state")

	rateLimitEventMatchers = []controller.ResourceEventMatcher{ // matches reconciliation events that change the rate limit definitions or status of rate limit policies
		{Kind: &kuadrantv1beta1.KuadrantGroupKind},
		{Kind: &machinery.GatewayClassGroupKind},
		{Kind: &machinery.GatewayGroupKind},
		{Kind: &machinery.HTTPRouteGroupKind},
		{Kind: &kuadrantv1beta3.RateLimitPolicyGroupKind},
		{Kind: &kuadrantv1beta1.LimitadorGroupKind},
		{Kind: &kuadrantistio.EnvoyFilterGroupKind},
		{Kind: &kuadrantistio.WasmPluginGroupKind},
		{Kind: &kuadrantenvoygateway.EnvoyPatchPolicyGroupKind},
		{Kind: &kuadrantenvoygateway.EnvoyExtensionPolicyGroupKind},
	}
)

//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=limitador.kuadrant.io,resources=limitadors,verbs=get;list;watch;create;update;patch;delete

func NewRateLimitWorkflow(client *dynamic.DynamicClient) *controller.Workflow {
	return &controller.Workflow{
		Precondition: (&rateLimitPolicyValidator{}).Subscription().Reconcile,
		Tasks: []controller.ReconcileFunc{(&controller.Workflow{
			Precondition: (&effectiveRateLimitPolicyReconciler{client: client}).Subscription().Reconcile,
			Tasks: []controller.ReconcileFunc{
				(&limitadorLimitsReconciler{client: client}).Subscription().Reconcile,
				// TODO: reconcile istio cluster (EnvoyFilter)
				(&istioExtensionReconciler{client: client}).Subscription().Reconcile,
				// TODO: reconcile envoy cluster (EnvoyPatchPolicy)
				// TODO: reconcile envoy extension (EnvoyExtensionPolicy)
			},
		}).Run},
		Postcondition: (&rateLimitPolicyStatusUpdater{client: client}).Subscription().Reconcile,
	}
}

func isRateLimitPolicyAcepted(policy machinery.Policy) bool {
	p, ok := policy.(*kuadrantv1beta3.RateLimitPolicy)
	return ok && kuadrantgatewayapi.IsPolicyAccepted(p) && p.GetDeletionTimestamp() == nil
}

func acceptedRateLimitPolicyFunc(state *sync.Map) func(machinery.Policy) bool {
	validatedPolicies, validated := state.Load(StateRateLimitPolicyValid)
	if !validated {
		return isRateLimitPolicyAcepted
	}
	return func(policy machinery.Policy) bool {
		err, validated := validatedPolicies.(map[string]error)[policy.GetLocator()]
		return (validated && err == nil) || isRateLimitPolicyAcepted(policy)
	}
}

func wasmPluginName(gatewayName string) string {
	return fmt.Sprintf("kuadrant-%s", gatewayName)
}

// Used in the tests

func WASMPluginName(gw *gatewayapiv1.Gateway) string {
	return wasmPluginName(gw.Name)
}
func EnvoyExtensionPolicyName(targetName string) string {
	return fmt.Sprintf("kuadrant-wasm-for-%s", targetName)
}
