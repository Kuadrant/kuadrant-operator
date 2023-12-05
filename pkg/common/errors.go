package common

import (
	"fmt"

	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type ErrTargetNotFound struct {
	Kind      string
	TargetRef gatewayapiv1alpha2.PolicyTargetReference
}

func (e ErrTargetNotFound) Error() string {
	return fmt.Sprintf("%s target %s was not found", e.Kind, e.TargetRef.Name)
}

type ErrInvalid struct {
	Kind string
	Err  error
}

func (e ErrInvalid) Error() string {
	return fmt.Sprintf("%s target is invalid: %s", e.Kind, e.Err.Error())
}

type ErrConflict struct {
	Kind          string
	NameNamespace string
	Err           error
}

func (e ErrConflict) Error() string {
	return fmt.Sprintf("%s is conflicted by %s: %s", e.Kind, e.NameNamespace, e.Err.Error())
}
