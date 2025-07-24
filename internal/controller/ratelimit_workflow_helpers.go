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
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	kuadrantpolicymachinery "github.com/kuadrant/kuadrant-operator/internal/policymachinery"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

const (
	rateLimitObjectLabelKey      = "kuadrant.io/ratelimit"
	tokenRateLimitObjectLabelKey = "kuadrant.io/tokenratelimit" //nolint:gosec
)

var (
	StateRateLimitPolicyValid                  = "RateLimitPolicyValid"
	StateEffectiveRateLimitPolicies            = "EffectiveRateLimitPolicies"
	StateLimitadorLimitsModified               = "LimitadorLimitsModified"
	StateIstioRateLimitClustersModified        = "IstioRateLimitClustersModified"
	StateEnvoyGatewayRateLimitClustersModified = "EnvoyGatewayRateLimitClustersModified"

	ErrMissingLimitador                            = fmt.Errorf("missing limitador object in the topology")
	ErrMissingLimitadorServiceInfo                 = fmt.Errorf("missing limitador service info in the limitador object")
	ErrMissingStateEffectiveRateLimitPolicies      = fmt.Errorf("missing rate limit effective policies stored in the reconciliation state")
	ErrMissingStateEffectiveTokenRateLimitPolicies = fmt.Errorf("missing token rate limit effective policies stored in the reconciliation state")
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
	return limitador.DeepCopy()
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

func TokenRateLimitObjectLabels() labels.Set {
	m := KuadrantManagedObjectLabels()
	m[tokenRateLimitObjectLabelKey] = "true"
	return m
}

func RateLimitClusterName(gatewayName string) string {
	return fmt.Sprintf("kuadrant-ratelimiting-%s", gatewayName)
}

func rateLimitClusterPatch(host string, port int, mTLS bool) map[string]any {
	base := map[string]any{
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
	if mTLS {
		base["transport_socket"] = map[string]interface{}{
			"name": "envoy.transport_sockets.tls",
			"typed_config": map[string]interface{}{
				"@type": "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext",
				"common_tls_context": map[string]interface{}{
					"tls_certificate_sds_secret_configs": []interface{}{
						map[string]interface{}{
							"name": "default",
							"sds_config": map[string]interface{}{
								"api_config_source": map[string]interface{}{
									"api_type": "GRPC",
									"grpc_services": []interface{}{
										map[string]interface{}{
											"envoy_grpc": map[string]interface{}{
												"cluster_name": "sds-grpc",
											},
										},
									},
								},
							},
						},
					},
					"validation_context_sds_secret_config": map[string]interface{}{
						"name": "ROOTCA",
						"sds_config": map[string]interface{}{
							"api_config_source": map[string]interface{}{
								"api_type": "GRPC",
								"grpc_services": []interface{}{
									map[string]interface{}{
										"envoy_grpc": map[string]interface{}{
											"cluster_name": "sds-grpc",
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
	return base
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

// TokenLimitNameToLimitadorIdentifier converts a token rate limit policy and limit name to a unique Limitador ident
func TokenLimitNameToLimitadorIdentifier(trlpKey k8stypes.NamespacedName, uniqueLimitName string) string {
	identifier := "tokenlimit."

	for _, c := range uniqueLimitName {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' {
			identifier += string(c)
		} else {
			identifier += "_"
		}
	}

	hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s", trlpKey.String(), uniqueLimitName)))
	identifier += "__" + hex.EncodeToString(hash[:4])

	return identifier
}

func wasmActionsFromTokenLimit(tokenLimit *kuadrantv1alpha1.TokenLimit, limitIdentifier, scope string, topLevelPredicates kuadrantv1.WhenPredicates) []wasm.Action {
	predicates := make([]string, 0, len(topLevelPredicates)+1)
	for _, pred := range topLevelPredicates {
		predicates = append(predicates, pred.Predicate)
	}
	for _, pred := range tokenLimit.When {
		predicates = append(predicates, pred.Predicate)
	}

	// common both actions
	commonData := []wasm.DataType{
		{
			Value: &wasm.Expression{
				ExpressionItem: wasm.ExpressionItem{
					Key:   limitIdentifier,
					Value: "1",
				},
			},
		},
	}

	// add counters if specified
	for _, counter := range tokenLimit.Counters {
		counterExpr := string(counter.Expression)
		commonData = append(commonData, wasm.DataType{
			Value: &wasm.Expression{
				ExpressionItem: wasm.ExpressionItem{
					Key:   counterExpr,
					Value: counterExpr,
				},
			},
		})
	}

	// Create separate data slices for request and response phases
	// We need independent copies because each phase has different hits_addend values

	// Request phase - check limit without consuming tokens
	requestPhaseData := make([]wasm.DataType, 0, len(commonData)+1)
	requestPhaseData = append(requestPhaseData, commonData...)
	requestPhaseData = append(requestPhaseData, wasm.DataType{
		Value: &wasm.Expression{
			ExpressionItem: wasm.ExpressionItem{
				Key:   "ratelimit.hits_addend",
				Value: "0",
			},
		},
	})

	requestAction := wasm.Action{
		ServiceName: wasm.RateLimitCheckServiceName,
		Scope:       scope,
		Predicates:  predicates,
		Data:        requestPhaseData,
	}

	// Response phase - increment counter with actual token usage
	responsePhaseData := make([]wasm.DataType, 0, len(commonData)+1)
	responsePhaseData = append(responsePhaseData, commonData...)
	responsePhaseData = append(responsePhaseData, wasm.DataType{
		Value: &wasm.Expression{
			ExpressionItem: wasm.ExpressionItem{
				Key:   "ratelimit.hits_addend",
				Value: "responseBodyJSON(\"usage.total_tokens\")",
			},
		},
	})

	responseAction := wasm.Action{
		ServiceName: wasm.RateLimitServiceName,
		Scope:       scope,
		Predicates:  predicates,
		Data:        responsePhaseData,
	}

	return []wasm.Action{requestAction, responseAction}
}

func buildWasmActionsForRateLimit(effectivePolicy EffectiveRateLimitPolicy, policyPredicate func(machinery.Policy) bool) []wasm.Action {
	return buildWasmActionsForAnyRateLimit(
		effectivePolicy.Path,
		effectivePolicy.Spec.Rules(),
		kuadrantv1.RulesKeyTopLevelPredicates,
		policyPredicate,
		func(key k8stypes.NamespacedName, limitName string) string {
			return LimitNameToLimitadorIdentifier(key, limitName)
		},
		func(spec interface{}, limitIdentifier, scope string, predicates kuadrantv1.WhenPredicates) wasm.Action {
			limit := spec.(*kuadrantv1.Limit)
			return wasmActionFromLimit(limit, limitIdentifier, scope, predicates)
		},
	)
}

func buildWasmActionsForTokenRateLimit(effectivePolicy EffectiveTokenRateLimitPolicy, policyPredicate func(machinery.Policy) bool) []wasm.Action {
	path := effectivePolicy.Path
	rules := effectivePolicy.Spec.Rules()
	policiesInPath := kuadrantv1.PoliciesInPath(path, policyPredicate)

	_, _, _, httpRoute, _, _ := kuadrantpolicymachinery.ObjectsInRequestPath(path)
	limitsNamespace := LimitsNamespaceFromRoute(httpRoute.HTTPRoute)

	topLevelRules, limitRules := lo.FilterReject(lo.Entries(rules),
		func(r lo.Entry[string, kuadrantv1.MergeableRule], _ int) bool {
			return r.Key == kuadrantv1.RulesKeyTopLevelPredicates
		},
	)

	var topLevelWhenPredicates kuadrantv1.WhenPredicates
	if len(topLevelRules) > 0 {
		if len(topLevelRules) > 1 {
			panic("token rate limit policy with multiple top level 'when' predicate lists")
		}
		topLevelWhenPredicates = topLevelRules[0].Value.GetSpec().(kuadrantv1.WhenPredicates)
	}

	var allActions []wasm.Action
	for _, r := range limitRules {
		uniquePolicyRuleKey := r.Key
		policyRule := r.Value
		source, found := lo.Find(policiesInPath, func(p machinery.Policy) bool {
			return p.GetLocator() == policyRule.GetSource()
		})
		if !found { // should never happen
			continue
		}
		limitIdentifier := TokenLimitNameToLimitadorIdentifier(k8stypes.NamespacedName{Name: source.GetName(), Namespace: source.GetNamespace()}, uniquePolicyRuleKey)
		limitSpec := policyRule.GetSpec().(*kuadrantv1alpha1.TokenLimit)
		scope := limitsNamespace

		// TokenRateLimitPolicy generates multiple actions per limit (request + response phase)
		tokenActions := wasmActionsFromTokenLimit(limitSpec, limitIdentifier, scope, topLevelWhenPredicates)
		allActions = append(allActions, tokenActions...)
	}

	return allActions
}

// buildWasmActionsForAnyRateLimit is the generic implementation used by both rate limit policy types
func buildWasmActionsForAnyRateLimit(
	path []machinery.Targetable,
	rules map[string]kuadrantv1.MergeableRule,
	topLevelPredicatesKey string,
	policyPredicate func(machinery.Policy) bool,
	identifierFunc func(k8stypes.NamespacedName, string) string,
	actionFunc func(interface{}, string, string, kuadrantv1.WhenPredicates) wasm.Action,
) []wasm.Action {
	policiesInPath := kuadrantv1.PoliciesInPath(path, policyPredicate)

	_, _, _, httpRoute, _, _ := kuadrantpolicymachinery.ObjectsInRequestPath(path)
	limitsNamespace := LimitsNamespaceFromRoute(httpRoute.HTTPRoute)

	topLevelRules, limitRules := lo.FilterReject(lo.Entries(rules),
		func(r lo.Entry[string, kuadrantv1.MergeableRule], _ int) bool {
			return r.Key == topLevelPredicatesKey
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
		limitIdentifier := identifierFunc(k8stypes.NamespacedName{Name: source.GetName(), Namespace: source.GetNamespace()}, uniquePolicyRuleKey)
		limitSpec := policyRule.GetSpec()
		scope := limitsNamespace

		return actionFunc(limitSpec, limitIdentifier, scope, topLevelWhenPredicates), true
	})
}
