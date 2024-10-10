package controllers

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/env"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	kuadrantistio "github.com/kuadrant/kuadrant-operator/pkg/istio"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

const (
	rateLimitClusterLabelKey = "kuadrant.io/rate-limit-cluster"

	istioGatewayControllerName = "istio.io/gateway-controller" // make this configurable?
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

func NewRateLimitWorkflow(manager ctrlruntime.Manager, client *dynamic.DynamicClient) *controller.Workflow {
	baseReconciler := reconcilers.NewBaseReconciler(manager.GetClient(), manager.GetScheme(), manager.GetAPIReader(), log.Log.WithName("ratelimit"))

	return &controller.Workflow{
		Precondition: (&rateLimitPolicyValidator{}).Subscription().Reconcile,
		Tasks: []controller.ReconcileFunc{(&controller.Workflow{
			Precondition: (&effectiveRateLimitPolicyReconciler{client: client}).Subscription().Reconcile,
			Tasks: []controller.ReconcileFunc{
				(&limitadorLimitsReconciler{client: client}).Subscription().Reconcile,
				(&istioRateLimitClusterReconciler{BaseReconciler: baseReconciler, client: client}).Subscription().Reconcile,
				(&istioExtensionReconciler{client: client}).Subscription().Reconcile,
				// TODO: reconcile envoy cluster (EnvoyPatchPolicy)
				// TODO: reconcile envoy extension (EnvoyExtensionPolicy)
			},
		}).Run},
		Postcondition: (&rateLimitPolicyStatusUpdater{client: client}).Subscription().Reconcile,
	}
}

func rateLimitPolicyAcceptedStatus(policy machinery.Policy) (accepted bool, err error) {
	p, ok := policy.(*kuadrantv1beta3.RateLimitPolicy)
	if !ok {
		return
	}
	if condition := meta.FindStatusCondition(p.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted)); condition != nil {
		accepted = condition.Status == metav1.ConditionTrue
		if !accepted {
			err = fmt.Errorf(condition.Message)
		}
		return
	}
	return
}

func rateLimitPolicyAcceptedStatusFunc(state *sync.Map) func(policy machinery.Policy) (bool, error) {
	validatedPolicies, validated := state.Load(StateRateLimitPolicyValid)
	if !validated {
		return rateLimitPolicyAcceptedStatus
	}
	validatedPoliciesMap := validatedPolicies.(map[string]error)
	return func(policy machinery.Policy) (bool, error) {
		err, validated := validatedPoliciesMap[policy.GetLocator()]
		if validated {
			return err == nil, err
		}
		return rateLimitPolicyAcceptedStatus(policy)
	}
}

func isRateLimitPolicyAcceptedFunc(state *sync.Map) func(machinery.Policy) bool {
	f := rateLimitPolicyAcceptedStatusFunc(state)
	return func(policy machinery.Policy) bool {
		accepted, _ := f(policy)
		return accepted
	}
}

func isRateLimitPolicyAcceptedAndNotDeletedFunc(state *sync.Map) func(machinery.Policy) bool {
	f := isRateLimitPolicyAcceptedFunc(state)
	return func(policy machinery.Policy) bool {
		p, object := policy.(metav1.Object)
		return object && f(policy) && p.GetDeletionTimestamp() == nil
	}
}

func wasmExtensionName(gatewayName string) string {
	return fmt.Sprintf("kuadrant-%s", gatewayName)
}

func rateLimitClusterName(gatewayName string) string {
	return fmt.Sprintf("kuadrant-ratelimiting-%s", gatewayName)
}

// Used in the tests

func WASMPluginName(gw *gatewayapiv1.Gateway) string {
	return wasmExtensionName(gw.Name)
}
func EnvoyExtensionPolicyName(targetName string) string {
	return fmt.Sprintf("kuadrant-wasm-for-%s", targetName)
}
