package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
	exttypes "github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

func convertDomainToProtobuf(domain exttypes.Domain) extpb.Domain {
	switch domain {
	case exttypes.DomainAuth:
		return extpb.Domain_DOMAIN_AUTH
	default:
		return extpb.Domain_DOMAIN_UNSPECIFIED
	}
}

func convertPolicyToProtobuf(policy exttypes.Policy) *extpb.Policy {
	pbPolicy := &extpb.Policy{
		Metadata: &extpb.Metadata{
			Group:     policy.GetObjectKind().GroupVersionKind().Group,
			Kind:      policy.GetObjectKind().GroupVersionKind().Kind,
			Namespace: policy.GetNamespace(),
			Name:      policy.GetName(),
		},
		TargetRefs: make([]*extpb.TargetRef, len(policy.GetTargetRefs())),
	}

	for i, ref := range policy.GetTargetRefs() {
		pbPolicy.TargetRefs[i] = &extpb.TargetRef{
			Group:     string(ref.Group),
			Kind:      string(ref.Kind),
			Name:      string(ref.Name),
			Namespace: policy.GetNamespace(), // Use policy namespace for target refs
		}
		if ref.SectionName != nil {
			pbPolicy.TargetRefs[i].SectionName = string(*ref.SectionName)
		}
	}

	return pbPolicy
}

func Resolve[T any](ctx context.Context, kuadrantCtx exttypes.KuadrantCtx, policy exttypes.Policy, expression string, subscribe bool) (T, error) {
	var zero T

	celValue, err := kuadrantCtx.Resolve(ctx, policy, expression, subscribe)
	if err != nil {
		return zero, err
	}

	nativeValue, err := celValue.ConvertToNative(reflect.TypeOf(zero))
	if err != nil {
		return zero, err
	}

	result, ok := nativeValue.(T)
	if !ok {
		return zero, fmt.Errorf("value is not type: %T", zero)
	}
	return result, nil
}

func AcceptedCondition(p exttypes.Policy, err error) *metav1.Condition {
	policyKind := p.GetObjectKind().GroupVersionKind().Kind
	cond := &metav1.Condition{
		Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayapiv1alpha2.PolicyReasonAccepted),
		Message: fmt.Sprintf("%s has been accepted", policyKind),
	}
	if err == nil {
		return cond
	}

	policyErr := intoPolicyError(err, policyKind)

	cond.Status = metav1.ConditionFalse
	cond.Message = policyErr.Error()
	cond.Reason = string(policyErr.Reason())

	return cond
}

func EnforcedCondition(p exttypes.Policy, err error, fully bool) *metav1.Condition {
	policyKind := p.GetObjectKind().GroupVersionKind().Kind
	message := fmt.Sprintf("%s has been successfully enforced", policyKind)
	if !fully {
		message = fmt.Sprintf("%s has been partially enforced", policyKind)
	}
	cond := &metav1.Condition{
		Type:    string(kuadrant.PolicyConditionEnforced),
		Status:  metav1.ConditionTrue,
		Reason:  string(kuadrant.PolicyReasonEnforced),
		Message: message,
	}
	if err == nil {
		return cond
	}
	policyErr := intoPolicyError(err, policyKind)
	cond.Status = metav1.ConditionFalse
	cond.Message = err.Error()
	cond.Reason = string(policyErr.Reason())

	return cond
}

func intoPolicyError(err error, policyKind string) kuadrant.PolicyError {
	var policyErr kuadrant.PolicyError
	if !errors.As(err, &policyErr) {
		policyErr = kuadrant.NewErrUnknown(policyKind, err)
	}
	return policyErr
}
