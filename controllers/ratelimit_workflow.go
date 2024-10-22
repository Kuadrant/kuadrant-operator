package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"unicode"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/env"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	kuadrantistio "github.com/kuadrant/kuadrant-operator/pkg/istio"
	"github.com/kuadrant/kuadrant-operator/pkg/wasm"
)

const (
	rateLimitClusterLabelKey = "kuadrant.io/rate-limit-cluster"

	// make these configurable?
	istioGatewayControllerName        = "istio.io/gateway-controller"
	envoyGatewayGatewayControllerName = "gateway.envoyproxy.io/gatewayclass-controller"
)

var (
	WASMFilterImageURL = env.GetString("RELATED_IMAGE_WASMSHIM", "oci://quay.io/kuadrant/wasm-shim:latest")

	StateRateLimitPolicyValid                  = "RateLimitPolicyValid"
	StateEffectiveRateLimitPolicies            = "EffectiveRateLimitPolicies"
	StateLimitadorLimitsModified               = "LimitadorLimitsModified"
	StateIstioRateLimitClustersModified        = "IstioRateLimitClustersModified"
	StateIstioExtensionsModified               = "IstioExtensionsModified"
	StateEnvoyGatewayRateLimitClustersModified = "EnvoyGatewayRateLimitClustersModified"
	StateEnvoyGatewayExtensionsModified        = "EnvoyGatewayExtensionsModified"

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

func NewRateLimitWorkflow(client *dynamic.DynamicClient, isIstioInstalled, isEnvoyGatewayInstalled bool) *controller.Workflow {
	effectiveRateLimitPoliciesWorkflow := &controller.Workflow{
		Precondition: (&effectiveRateLimitPolicyReconciler{client: client}).Subscription().Reconcile,
		Tasks: []controller.ReconcileFunc{
			(&limitadorLimitsReconciler{client: client}).Subscription().Reconcile,
		},
	}

	if isIstioInstalled {
		effectiveRateLimitPoliciesWorkflow.Tasks = append(effectiveRateLimitPoliciesWorkflow.Tasks, (&istioRateLimitClusterReconciler{client: client}).Subscription().Reconcile)
		effectiveRateLimitPoliciesWorkflow.Tasks = append(effectiveRateLimitPoliciesWorkflow.Tasks, (&istioExtensionReconciler{client: client}).Subscription().Reconcile)
	}

	if isEnvoyGatewayInstalled {
		effectiveRateLimitPoliciesWorkflow.Tasks = append(effectiveRateLimitPoliciesWorkflow.Tasks, (&envoyGatewayRateLimitClusterReconciler{client: client}).Subscription().Reconcile)
		effectiveRateLimitPoliciesWorkflow.Tasks = append(effectiveRateLimitPoliciesWorkflow.Tasks, (&envoyGatewayExtensionReconciler{client: client}).Subscription().Reconcile)
	}

	return &controller.Workflow{
		Precondition:  (&rateLimitPolicyValidator{}).Subscription().Reconcile,
		Tasks:         []controller.ReconcileFunc{effectiveRateLimitPoliciesWorkflow.Run},
		Postcondition: (&rateLimitPolicyStatusUpdater{client: client}).Subscription().Reconcile,
	}
}

func GetLimitadorFromTopology(topology *machinery.Topology) (*limitadorv1alpha1.Limitador, error) {
	kuadrant, err := GetKuadrantFromTopology(topology)
	if err != nil {
		return nil, err
	}

	limitadorObj, found := lo.Find(topology.Objects().Children(kuadrant), func(child machinery.Object) bool {
		return child.GroupVersionKind().GroupKind() == kuadrantv1beta1.LimitadorGroupKind
	})
	if !found {
		return nil, ErrMissingLimitador
	}

	limitador := limitadorObj.(*controller.RuntimeObject).Object.(*limitadorv1alpha1.Limitador)
	return limitador, nil
}

func LimitsNamespaceFromRoute(route *gatewayapiv1.HTTPRoute) string {
	return k8stypes.NamespacedName{Name: route.GetName(), Namespace: route.GetNamespace()}.String()
}

func LimitNameToLimitadorIdentifier(rlpKey k8stypes.NamespacedName, uniqueLimitName string) string {
	identifier := "limit."

	// sanitize chars that are not allowed in limitador identifiers
	for _, c := range uniqueLimitName {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' {
			identifier += string(c)
		} else {
			identifier += "_"
		}
	}

	// to avoid breaking the uniqueness of the limit name after sanitization, we add a hash of the original name
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s", rlpKey.String(), uniqueLimitName)))
	identifier += "__" + hex.EncodeToString(hash[:4])

	return identifier
}

func RateLimitObjectLabels() labels.Set {
	m := KuadrantManagedObjectLabels()
	m[rateLimitClusterLabelKey] = "true"
	return m
}

func RateLimitClusterName(gatewayName string) string {
	return fmt.Sprintf("kuadrant-ratelimiting-%s", gatewayName)
}

func rateLimitClusterPatch(host string, port int) map[string]any {
	return map[string]any{
		"name":                   common.KuadrantRateLimitClusterName,
		"type":                   "STRICT_DNS",
		"connect_timeout":        "1s",
		"lb_policy":              "ROUND_ROBIN",
		"http2_protocol_options": map[string]any{},
		"load_assignment": map[string]any{
			"cluster_name": common.KuadrantRateLimitClusterName,
			"endpoints": []map[string]any{
				{
					"lb_endpoints": []map[string]any{
						{
							"endpoint": map[string]any{
								"address": map[string]any{
									"socket_address": map[string]any{
										"address":    host,
										"port_value": port,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func rateLimitWasmActionBuilder(pathID string, effectivePolicy EffectiveRateLimitPolicy, state *sync.Map) wasm.ActionBuilderFunc {
	policiesInPath := kuadrantv1.PoliciesInPath(effectivePolicy.Path, isRateLimitPolicyAcceptedAndNotDeletedFunc(state))
	_, _, _, httpRoute, _, _ := common.ObjectsInRequestPath(effectivePolicy.Path)
	limitsNamespace := LimitsNamespaceFromRoute(httpRoute.HTTPRoute)
	return func(uniquePolicyRuleKey string, policyRule kuadrantv1.MergeableRule) (wasm.Action, error) {
		source, found := lo.Find(policiesInPath, func(p machinery.Policy) bool {
			return p.GetLocator() == policyRule.Source
		})
		if !found { // should never happen
			return wasm.Action{}, fmt.Errorf("could not find source policy %s in path %s", policyRule.Source, pathID)
		}
		limitIdentifier := LimitNameToLimitadorIdentifier(k8stypes.NamespacedName{Name: source.GetName(), Namespace: source.GetNamespace()}, uniquePolicyRuleKey)
		limit := policyRule.Spec.(kuadrantv1beta3.Limit)
		return wasmActionFromLimit(limit, limitIdentifier, limitsNamespace), nil
	}
}

// wasmActionFromLimit builds a wasm rate-limit action for a given limit.
// Conditions are built from the limit top-level conditions.
//
// The only action of the rule is the ratelimit service, whose data includes the activation of the limit
// and any counter qualifier of the limit.
func wasmActionFromLimit(limit kuadrantv1beta3.Limit, limitIdentifier, scope string) wasm.Action {
	action := wasm.Action{
		ServiceName: wasm.RateLimitServiceName,
		Scope:       scope,
	}
	if conditions := wasm.PredicatesFromWhenConditions(limit.When...); len(conditions) > 0 {
		action.Conditions = conditions
	}
	if data := wasmDataFromLimit(limitIdentifier, limit); len(data) > 0 {
		action.Data = data
	}
	return action
}

func wasmDataFromLimit(limitIdentifier string, limit kuadrantv1beta3.Limit) (data []wasm.DataType) {
	// static key representing the limit
	data = append(data,
		wasm.DataType{
			Value: &wasm.Static{
				Static: wasm.StaticSpec{Key: limitIdentifier, Value: "1"},
			},
		},
	)

	for _, counter := range limit.Counters {
		data = append(data,
			wasm.DataType{
				Value: &wasm.Selector{
					Selector: wasm.SelectorSpec{Selector: counter},
				},
			},
		)
	}

	return data
}

func isRateLimitPolicyAcceptedAndNotDeletedFunc(state *sync.Map) func(machinery.Policy) bool {
	f := isRateLimitPolicyAcceptedFunc(state)
	return func(policy machinery.Policy) bool {
		p, object := policy.(metav1.Object)
		return object && f(policy) && p.GetDeletionTimestamp() == nil
	}
}

func isRateLimitPolicyAcceptedFunc(state *sync.Map) func(machinery.Policy) bool {
	f := rateLimitPolicyAcceptedStatusFunc(state)
	return func(policy machinery.Policy) bool {
		accepted, _ := f(policy)
		return accepted
	}
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
