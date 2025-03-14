package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	kuadrantpolicymachinery "github.com/kuadrant/kuadrant-operator/internal/policymachinery"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

const rateLimitObjectLabelKey = "kuadrant.io/ratelimit"

var (
	StateRateLimitPolicyValid                  = "RateLimitPolicyValid"
	StateEffectiveRateLimitPolicies            = "EffectiveRateLimitPolicies"
	StateLimitadorLimitsModified               = "LimitadorLimitsModified"
	StateIstioRateLimitClustersModified        = "IstioRateLimitClustersModified"
	StateEnvoyGatewayRateLimitClustersModified = "EnvoyGatewayRateLimitClustersModified"

	ErrMissingLimitador                       = fmt.Errorf("missing limitador object in the topology")
	ErrMissingLimitadorServiceInfo            = fmt.Errorf("missing limitador service info in the limitador object")
	ErrMissingStateEffectiveRateLimitPolicies = fmt.Errorf("missing rate limit effective policies stored in the reconciliation state")
)

func GetLimitadorFromTopology(topology *machinery.Topology) *limitadorv1alpha1.Limitador {
	kuadrant := GetKuadrantFromTopology(topology)
	if kuadrant == nil {
		return nil
	}

	limitadorObj, found := lo.Find(topology.Objects().Children(kuadrant), func(child machinery.Object) bool {
		return child.GroupVersionKind().GroupKind() == kuadrantv1beta1.LimitadorGroupKind
	})
	if !found {
		return nil
	}

	limitador := limitadorObj.(*controller.RuntimeObject).Object.(*limitadorv1alpha1.Limitador)
	return limitador
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
	m[rateLimitObjectLabelKey] = "true"
	return m
}

func RateLimitClusterName(gatewayName string) string {
	return fmt.Sprintf("kuadrant-ratelimiting-%s", gatewayName)
}

func rateLimitClusterPatch(host string, port int) map[string]any {
	return map[string]any{
		"name":                   kuadrant.KuadrantRateLimitClusterName,
		"type":                   "STRICT_DNS",
		"connect_timeout":        "1s",
		"lb_policy":              "ROUND_ROBIN",
		"http2_protocol_options": map[string]any{},
		"load_assignment": map[string]any{
			"cluster_name": kuadrant.KuadrantRateLimitClusterName,
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

func buildWasmActionsForRateLimit(effectivePolicy EffectiveRateLimitPolicy, state *sync.Map) []wasm.Action {
	policiesInPath := kuadrantv1.PoliciesInPath(effectivePolicy.Path, isRateLimitPolicyAcceptedAndNotDeletedFunc(state))

	_, _, _, httpRoute, _, _ := kuadrantpolicymachinery.ObjectsInRequestPath(effectivePolicy.Path)
	limitsNamespace := LimitsNamespaceFromRoute(httpRoute.HTTPRoute)

	topLevelRules, limitRules := lo.FilterReject(lo.Entries(effectivePolicy.Spec.Rules()),
		func(r lo.Entry[string, kuadrantv1.MergeableRule], _ int) bool {
			return r.Key == kuadrantv1.RulesKeyTopLevelPredicates
		},
	)

	var topLevelWhenPredicates kuadrantv1.WhenPredicates
	if len(topLevelRules) > 0 {
		if len(topLevelRules) > 1 {
			panic("rate limit policy with multiple top level 'when' predicate lists")
		}
		topLevelWhenPredicates = topLevelRules[0].Value.GetSpec().(kuadrantv1.WhenPredicates)
	}

	return lo.FilterMap(limitRules, func(r lo.Entry[string, kuadrantv1.MergeableRule], _ int) (wasm.Action, bool) {
		uniquePolicyRuleKey := r.Key
		policyRule := r.Value
		source, found := lo.Find(policiesInPath, func(p machinery.Policy) bool {
			return p.GetLocator() == policyRule.GetSource()
		})
		if !found { // should never happen
			return wasm.Action{}, false
		}
		limitIdentifier := LimitNameToLimitadorIdentifier(k8stypes.NamespacedName{Name: source.GetName(), Namespace: source.GetNamespace()}, uniquePolicyRuleKey)
		limit := policyRule.GetSpec().(*kuadrantv1.Limit)
		return wasmActionFromLimit(limit, limitIdentifier, limitsNamespace, topLevelWhenPredicates), true
	})
}

// wasmActionFromLimit builds a wasm rate-limit action for a given limit.
// Conditions are built from the limit top-level conditions.
//
// The only action of the rule is the ratelimit service, whose data includes the activation of the limit
// and any counter qualifier of the limit.
func wasmActionFromLimit(limit *kuadrantv1.Limit, limitIdentifier, scope string, topLevelPredicates kuadrantv1.WhenPredicates) wasm.Action {
	return wasm.Action{
		ServiceName: wasm.RateLimitServiceName,
		Scope:       scope,
		Predicates:  topLevelPredicates.Extend(limit.When).Into(),
		Data:        wasmDataFromLimit(limitIdentifier, limit),
	}
}

func wasmDataFromLimit(limitIdentifier string, limit *kuadrantv1.Limit) []wasm.DataType {
	data := make([]wasm.DataType, 0)

	// static key representing the limit
	data = append(data,
		wasm.DataType{
			Value: &wasm.Expression{
				ExpressionItem: wasm.ExpressionItem{Key: limitIdentifier, Value: "1"},
			},
		},
	)

	for _, counter := range limit.Counters {
		data = append(data,
			wasm.DataType{
				Value: &wasm.Expression{
					ExpressionItem: wasm.ExpressionItem{
						Key:   string(counter.Expression),
						Value: string(counter.Expression),
					},
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
	p, ok := policy.(*kuadrantv1.RateLimitPolicy)
	if !ok {
		return
	}
	if condition := meta.FindStatusCondition(p.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted)); condition != nil {
		accepted = condition.Status == metav1.ConditionTrue
		if !accepted {
			err = errors.New(condition.Message)
		}
		return
	}
	return
}
