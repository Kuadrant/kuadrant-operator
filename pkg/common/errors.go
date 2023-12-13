package common

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

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

func NewErrTargetNotFound(kind string, targetRef gatewayapiv1alpha2.PolicyTargetReference, err error) ErrTargetNotFound {
	return ErrTargetNotFound{
		Kind:      kind,
		TargetRef: targetRef,
		Err:       err,
	}
}

type ErrInvalid struct {
	Kind string
	Err  error
}

func (e ErrInvalid) Error() string {
	return fmt.Sprintf("%s target is invalid: %s", e.Kind, e.Err.Error())
}

func NewErrInvalid(kind string, err error) ErrInvalid {
	return ErrInvalid{
		Kind: kind,
		Err:  err,
	}
}

type ErrConflict struct {
	Kind          string
	NameNamespace string
	Err           error
}

func (e ErrConflict) Error() string {
	return fmt.Sprintf("%s is conflicted by %s: %s", e.Kind, e.NameNamespace, e.Err.Error())
}

func NewErrConflict(kind string, nameNamespace string, err error) ErrConflict {
	return ErrConflict{
		Kind:          kind,
		NameNamespace: nameNamespace,
		Err:           err,
	}
}
