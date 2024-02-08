package common

import (
	"errors"
	"testing"

	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestIsTargetNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "err is NewErrTargetNotFound",
			err:  NewErrTargetNotFound("foo", gatewayapiv1alpha2.PolicyTargetReference{}, errors.New("bar")),
			want: true,
		},
		{
			name: "err is NewErrInvalid",
			err:  NewErrInvalid("foo", errors.New("bar")),
			want: false,
		},
		{
			name: "err is standard error",
			err:  errors.New("bar"),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTargetNotFound(tt.err); got != tt.want {
				t.Errorf("IsTargetNotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}
