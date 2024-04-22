package kuadrant

import (
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type PolicyError interface {
	error
	Reason() gatewayapiv1alpha2.PolicyConditionReason
}

var _ PolicyError = ErrTargetNotFound{}

type ErrTargetNotFound struct {
	Kind      string
	TargetRef gatewayapiv1alpha2.PolicyTargetReference
	Err       error
}

func (e ErrTargetNotFound) Error() string {
	if apierrors.IsNotFound(e.Err) {
		return fmt.Sprintf("%s target %s was not found", e.Kind, e.TargetRef.Name)
	}

	return fmt.Sprintf("%s target %s was not found: %s", e.Kind, e.TargetRef.Name, e.Err.Error())
}

func (e ErrTargetNotFound) Reason() gatewayapiv1alpha2.PolicyConditionReason {
	return gatewayapiv1alpha2.PolicyReasonTargetNotFound
}

func NewErrTargetNotFound(kind string, targetRef gatewayapiv1alpha2.PolicyTargetReference, err error) ErrTargetNotFound {
	return ErrTargetNotFound{
		Kind:      kind,
		TargetRef: targetRef,
		Err:       err,
	}
}

var _ PolicyError = ErrInvalid{}

type ErrInvalid struct {
	Kind string
	Err  error
}

func (e ErrInvalid) Error() string {
	return fmt.Sprintf("%s target is invalid: %s", e.Kind, e.Err.Error())
}

func (e ErrInvalid) Reason() gatewayapiv1alpha2.PolicyConditionReason {
	return gatewayapiv1alpha2.PolicyReasonInvalid
}

func NewErrInvalid(kind string, err error) ErrInvalid {
	return ErrInvalid{
		Kind: kind,
		Err:  err,
	}
}

var _ PolicyError = ErrConflict{}

type ErrConflict struct {
	Kind          string
	NameNamespace string
	Err           error
}

func (e ErrConflict) Error() string {
	return fmt.Sprintf("%s is conflicted by %s: %s", e.Kind, e.NameNamespace, e.Err.Error())
}

func (e ErrConflict) Reason() gatewayapiv1alpha2.PolicyConditionReason {
	return gatewayapiv1alpha2.PolicyReasonConflicted
}

func NewErrConflict(kind string, nameNamespace string, err error) ErrConflict {
	return ErrConflict{
		Kind:          kind,
		NameNamespace: nameNamespace,
		Err:           err,
	}
}

var _ PolicyError = ErrUnknown{}

type ErrUnknown struct {
	Kind string
	Err  error
}

func (e ErrUnknown) Error() string {
	return fmt.Sprintf("%s has encountered some issues: %s", e.Kind, e.Err.Error())
}

func (e ErrUnknown) Reason() gatewayapiv1alpha2.PolicyConditionReason {
	return PolicyReasonUnknown
}

func NewErrUnknown(kind string, err error) ErrUnknown {
	return ErrUnknown{
		Kind: kind,
		Err:  err,
	}
}

var _ PolicyError = ErrOverridden{}

type ErrOverridden struct {
	Kind               string
	OverridingPolicies []client.ObjectKey
}

func (e ErrOverridden) Error() string {
	return fmt.Sprintf("%s is overridden by %s", e.Kind, e.OverridingPolicies)
}

func (e ErrOverridden) Reason() gatewayapiv1alpha2.PolicyConditionReason {
	return PolicyReasonOverridden
}

func NewErrOverridden(kind string, overridingPolicies []client.ObjectKey) ErrOverridden {
	return ErrOverridden{
		Kind:               kind,
		OverridingPolicies: overridingPolicies,
	}
}

// IsTargetNotFound returns true if the specified error was created by NewErrTargetNotFound.
func IsTargetNotFound(err error) bool {
	return reasonForError(err) == gatewayapiv1alpha2.PolicyReasonTargetNotFound
}

func reasonForError(err error) gatewayapiv1alpha2.PolicyConditionReason {
	var policyErr PolicyError
	if errors.As(err, &policyErr) {
		return policyErr.Reason()
	}
	return ""
}
