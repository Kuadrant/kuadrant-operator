package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
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

func GetAuthorinoFromTopology(topology *machinery.Topology, state *sync.Map) *authorinooperatorv1beta1.Authorino {
	kuadrant := GetKuadrantFromTopology(topology, state)
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
	return buildClusterPatch(kuadrant.KuadrantAuthClusterName, host, port, mTLS)
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

const authResponseVar = "auth_response"

func buildWasmTypedActionsForAuth(pathID string, effectivePolicy EffectiveAuthPolicy) []wasm.TypedAction {
	spec := effectivePolicy.Spec.Spec.Proper()
	scope := AuthConfigNameForPath(pathID)
	predicate := joinPredicates(spec.Predicates.Into())
	if predicate == "" {
		predicate = "true"
	}

	action := wasm.TypedAction{
		Type:                 "grpc",
		Predicate:            predicate,
		Var:                  authResponseVar,
		Service:              wasm.AuthServiceName,
		MessageBuilder:       buildAuthMessageBuilder(scope),
		OnReply:              buildAuthOnReply(authResponseVar),
		SourcePolicyLocators: effectivePolicy.SourcePolicies,
	}

	return []wasm.TypedAction{action}
}

func joinPredicates(predicates []string) string {
	if len(predicates) == 0 {
		return ""
	}
	if len(predicates) == 1 {
		return predicates[0]
	}
	return strings.Join(predicates, " && ")
}

func buildAuthMessageBuilder(scope string) string {
	return fmt.Sprintf(`envoy.service.auth.v3.CheckRequest {
  attributes: envoy.service.auth.v3.AttributeContext {
    request: envoy.service.auth.v3.AttributeContext.Request {
      time: request.time,
      http: envoy.service.auth.v3.AttributeContext.HttpRequest {
        host: request.host,
        method: request.method,
        scheme: request.scheme,
        path: request.path,
        protocol: request.protocol,
        headers: request.headers
      }
    },
    destination: envoy.service.auth.v3.AttributeContext.Peer {
      address: envoy.config.core.v3.Address {
        socket_address: envoy.config.core.v3.SocketAddress {
          address: destination.address,
          port_value: uint(destination.port)
        }
      }
    },
    source: envoy.service.auth.v3.AttributeContext.Peer {
      address: envoy.config.core.v3.Address {
        socket_address: envoy.config.core.v3.SocketAddress {
          address: source.address,
          port_value: uint(source.port)
        }
      }
    },
    context_extensions: {"host": "%s"},
    metadata_context: envoy.config.core.v3.Metadata{}
  }
}`, scope)
}

func buildAuthOnReply(varName string) []wasm.TypedAction {
	return []wasm.TypedAction{
		{
			Type:      "deny",
			Predicate: fmt.Sprintf("has(%s.denied_response)", varName),
			Terminal:  true,
			DenyWith: fmt.Sprintf(
				`DenyResponse{status: (%s.denied_response.status.code != 0) ? uint(%s.denied_response.status.code) : 403u, headers: %s.denied_response.headers, body: %s.denied_response.body}`,
				varName, varName, varName, varName,
			),
		},
		{
			Type: "fail",
			Predicate: fmt.Sprintf(
				"has(%s.ok_response) && (%s.ok_response.response_headers_to_add.size() > 0 || %s.ok_response.headers_to_remove.size() > 0 || %s.ok_response.query_parameters_to_set.size() > 0 || %s.ok_response.query_parameters_to_remove.size() > 0)",
				varName, varName, varName, varName, varName,
			),
			Terminal:   true,
			LogMessage: "Unsupported field in OkHttpResponse",
		},
		{
			Type:      "store",
			Predicate: fmt.Sprintf("has(%s.ok_response) && has(%s.dynamic_metadata)", varName, varName),
			Path:      "auth",
			Value:     fmt.Sprintf("%s.dynamic_metadata", varName),
		},
		{
			Type:      "headers",
			Predicate: fmt.Sprintf("has(%s.ok_response)", varName),
			Target:    "request",
			Headers:   fmt.Sprintf("%s.ok_response.headers", varName),
		},
		{
			Type:       "fail",
			Predicate:  fmt.Sprintf("!has(%s.denied_response) && !has(%s.ok_response)", varName, varName),
			Terminal:   true,
			LogMessage: fmt.Sprintf("Auth response contained no http_response from %s", varName),
		},
	}
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
