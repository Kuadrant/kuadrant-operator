package controllers

import (
	"context"
	"fmt"
	"sync"

	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

const (
	KuadrantAppName                = "kuadrant"
	PolicyAffectedConditionPattern = "kuadrant.io/%sAffected" // Policy kinds are expected to be named XPolicy
)

var (
	AppLabelKey   = "app"
	AppLabelValue = KuadrantAppName
)

func CommonLabels() map[string]string {
	return map[string]string{
		AppLabelKey:                    AppLabelValue,
		"app.kubernetes.io/component":  KuadrantAppName,
		"app.kubernetes.io/managed-by": kuadrant.OperatorDeploymentName,
		"app.kubernetes.io/instance":   KuadrantAppName,
		"app.kubernetes.io/name":       KuadrantAppName,
		"app.kubernetes.io/part-of":    KuadrantAppName,
	}
}

func PolicyAffectedCondition(policyKind string, policies []machinery.Policy) metav1.Condition {
	condition := metav1.Condition{
		Type:   PolicyAffectedConditionType(policyKind),
		Status: metav1.ConditionTrue,
		Reason: string(gatewayapiv1alpha2.PolicyReasonAccepted),
		Message: fmt.Sprintf("Object affected by %s %s", policyKind, lo.Map(policies, func(item machinery.Policy, _ int) client.ObjectKey {
			return client.ObjectKey{Name: item.GetName(), Namespace: item.GetNamespace()}
		})),
	}

	return condition
}

func PolicyAffectedConditionType(policyKind string) string {
	return fmt.Sprintf(PolicyAffectedConditionPattern, policyKind)
}

func IsPolicyAccepted(ctx context.Context, p machinery.Policy, s *sync.Map) bool {
	switch t := p.(type) {
	case *kuadrantv1.AuthPolicy:
		return isAuthPolicyAcceptedFunc(s)(p)
	case *kuadrantv1.RateLimitPolicy:
		return isRateLimitPolicyAcceptedFunc(s)(p)
	case *kuadrantv1.TLSPolicy:
		isValid, _ := IsTLSPolicyValid(ctx, s, t)
		return isValid
	case *kuadrantv1.DNSPolicy:
		isValid, _ := dnsPolicyAcceptedStatusFunc(s)(p)
		return isValid
	default:
		return false
	}
}

func policyGroupKinds() []*schema.GroupKind {
	return []*schema.GroupKind{
		&kuadrantv1.AuthPolicyGroupKind,
		&kuadrantv1.RateLimitPolicyGroupKind,
		&kuadrantv1.TLSPolicyGroupKind,
		&kuadrantv1.DNSPolicyGroupKind,
	}
}
