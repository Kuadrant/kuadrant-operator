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
	return buildClusterPatch(kuadrant.KuadrantAuthClusterName, host, port, mTLS, true)
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
	spec := effectivePolicy.Spec.Spec.Proper()

	action := wasm.Action{
		ServiceName: wasm.AuthServiceName,
		Scope:       AuthConfigNameForPath(pathID),
		Predicates:  spec.Predicates.Into(),
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
