package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	authorinooperatorv1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

const authObjectLabelKey = "kuadrant.io/auth"

var (
	StateAuthPolicyValid                  = "AuthPolicyValid"
	StateEffectiveAuthPolicies            = "EffectiveAuthPolicies"
	StateModifiedAuthConfigs              = "ModifiedAuthConfigs"
	StateIstioAuthClustersModified        = "IstioAuthClustersModified"
	StateEnvoyGatewayAuthClustersModified = "EnvoyGatewayAuthClustersModified"

	ErrMissingAuthorino                  = fmt.Errorf("missing authorino object in the topology")
	ErrMissingStateEffectiveAuthPolicies = fmt.Errorf("missing auth effective policies stored in the reconciliation state")
)

func GetAuthorinoFromTopology(topology *machinery.Topology) *authorinooperatorv1beta1.Authorino {
	kuadrant := GetKuadrantFromTopology(topology)
	if kuadrant == nil {
		return nil
	}

	authorinoObj, found := lo.Find(topology.Objects().Children(kuadrant), func(child machinery.Object) bool {
		return child.GroupVersionKind().GroupKind() == kuadrantv1beta1.AuthorinoGroupKind
	})
	if !found {
		return nil
	}

	authorino := authorinoObj.(*controller.RuntimeObject).Object.(*authorinooperatorv1beta1.Authorino)
	return authorino
}

func AuthObjectLabels() labels.Set {
	m := KuadrantManagedObjectLabels()
	m[authObjectLabelKey] = "true"
	return m
}

func AuthClusterName(gatewayName string) string {
	return fmt.Sprintf("kuadrant-auth-%s", gatewayName)
}

func authClusterPatch(host string, port int, mTLS bool) map[string]any {
	patch := map[string]any{
		"name":                   kuadrant.KuadrantAuthClusterName,
		"type":                   "STRICT_DNS",
		"connect_timeout":        "1s",
		"lb_policy":              "ROUND_ROBIN",
		"http2_protocol_options": map[string]any{},
		"load_assignment": map[string]any{
			"cluster_name": kuadrant.KuadrantAuthClusterName,
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
		patch["transport_socket"] = map[string]interface{}{
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
	return patch
}

type authorinoServiceInfo struct {
	Host string
	Port int32
}

func authorinoServiceInfoFromAuthorino(authorino *authorinooperatorv1beta1.Authorino) authorinoServiceInfo {
	info := authorinoServiceInfo{
		Host: fmt.Sprintf("%s-authorino-authorization.%s.svc.cluster.local", authorino.GetName(), authorino.GetNamespace()),
		Port: int32(50051), // default authorino grpc authorization service port
	}
	if p := authorino.Spec.Listener.Ports.GRPC; p != nil {
		info.Port = *p
	} else if p := authorino.Spec.Listener.Port; p != nil {
		info.Port = *p
	}
	return info
}

func AuthConfigNameForPath(pathID string) string {
	hash := sha256.Sum256([]byte(pathID))
	return hex.EncodeToString(hash[:])
}

func buildWasmActionsForAuth(pathID string, effectivePolicy EffectiveAuthPolicy) []wasm.Action {
	action := wasm.Action{
		ServiceName: wasm.AuthServiceName,
		Scope:       AuthConfigNameForPath(pathID),
	}
	spec := effectivePolicy.Spec.Spec.Proper()

	if whenPredicates := spec.Predicates; len(whenPredicates) > 0 {
		action.Predicates = whenPredicates.Into()
	}

	return []wasm.Action{action}
}

func isAuthPolicyAcceptedAndNotDeletedFunc(state *sync.Map) func(machinery.Policy) bool {
	f := isAuthPolicyAcceptedFunc(state)
	return func(policy machinery.Policy) bool {
		p, object := policy.(metav1.Object)
		return object && f(policy) && p.GetDeletionTimestamp() == nil
	}
}

func isAuthPolicyAcceptedFunc(state *sync.Map) func(machinery.Policy) bool {
	f := authPolicyAcceptedStatusFunc(state)
	return func(policy machinery.Policy) bool {
		accepted, _ := f(policy)
		return accepted
	}
}

func authPolicyAcceptedStatusFunc(state *sync.Map) func(policy machinery.Policy) (bool, error) {
	validatedPolicies, validated := state.Load(StateAuthPolicyValid)
	if !validated {
		return authPolicyAcceptedStatus
	}
	validatedPoliciesMap := validatedPolicies.(map[string]error)
	return func(policy machinery.Policy) (bool, error) {
		err, validated := validatedPoliciesMap[policy.GetLocator()]
		if validated {
			return err == nil, err
		}
		return authPolicyAcceptedStatus(policy)
	}
}

func authPolicyAcceptedStatus(policy machinery.Policy) (accepted bool, err error) {
	p, ok := policy.(*kuadrantv1.AuthPolicy)
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
