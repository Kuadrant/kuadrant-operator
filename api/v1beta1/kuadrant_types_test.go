//go:build unit

package v1beta1

import (
	"testing"

	"gotest.tools/assert"
	"k8s.io/utils/ptr"
)

func TestIsMTLSLimitadorEnabled(t *testing.T) {
	tests := []struct {
		name        string
		mtls        *MTLS
		expectedRes bool
	}{
		{
			name:        "nil mtls",
			mtls:        nil,
			expectedRes: false,
		},
		{
			name:        "limitador not set and global disabled",
			mtls:        &MTLS{Enable: false, Limitador: nil},
			expectedRes: false,
		},
		{
			name:        "limitador enabled and global disabled",
			mtls:        &MTLS{Enable: false, Limitador: ptr.To(true)},
			expectedRes: false,
		},
		{
			name:        "limitador disabled and global disabled",
			mtls:        &MTLS{Enable: false, Limitador: ptr.To(false)},
			expectedRes: false,
		},
		{
			name:        "limitador not set and global enabled",
			mtls:        &MTLS{Enable: true, Limitador: nil},
			expectedRes: true,
		},
		{
			name:        "limitador enabled and global enabled",
			mtls:        &MTLS{Enable: true, Limitador: ptr.To(true)},
			expectedRes: true,
		},
		{
			name:        "limitador disabled and global enabled",
			mtls:        &MTLS{Enable: true, Limitador: ptr.To(false)},
			expectedRes: false,
		},
	}
	for _, tt := range tests {
		kuadrantCR := &Kuadrant{
			Spec: KuadrantSpec{
				MTLS: tt.mtls,
			},
		}
		t.Run(tt.name, func(subT *testing.T) {
			got := kuadrantCR.IsMTLSLimitadorEnabled()
			assert.Equal(subT, got, tt.expectedRes)
		})
	}

	t.Run("kuadrant is nil", func(subT *testing.T) {
		var kuadrantCR *Kuadrant
		got := kuadrantCR.IsMTLSLimitadorEnabled()
		assert.Assert(subT, !got)
	})
}

func TestIsMTLSAuthorinoEnabled(t *testing.T) {
	tests := []struct {
		name        string
		mtls        *MTLS
		expectedRes bool
	}{
		{
			name:        "nil mtls",
			mtls:        nil,
			expectedRes: false,
		},
		{
			name:        "authorino not set and global disabled",
			mtls:        &MTLS{Enable: false, Authorino: nil},
			expectedRes: false,
		},
		{
			name:        "authorino enabled and global disabled",
			mtls:        &MTLS{Enable: false, Authorino: ptr.To(true)},
			expectedRes: false,
		},
		{
			name:        "authorino disabled and global disabled",
			mtls:        &MTLS{Enable: false, Authorino: ptr.To(false)},
			expectedRes: false,
		},
		{
			name:        "authorino not set and global enabled",
			mtls:        &MTLS{Enable: true, Authorino: nil},
			expectedRes: true,
		},
		{
			name:        "authorino enabled and global enabled",
			mtls:        &MTLS{Enable: true, Authorino: ptr.To(true)},
			expectedRes: true,
		},
		{
			name:        "authorino disabled and global enabled",
			mtls:        &MTLS{Enable: true, Authorino: ptr.To(false)},
			expectedRes: false,
		},
	}
	for _, tt := range tests {
		kuadrantCR := &Kuadrant{
			Spec: KuadrantSpec{
				MTLS: tt.mtls,
			},
		}
		t.Run(tt.name, func(subT *testing.T) {
			got := kuadrantCR.IsMTLSAuthorinoEnabled()
			assert.Equal(subT, got, tt.expectedRes)
		})
	}

	t.Run("kuadrant is nil", func(subT *testing.T) {
		var kuadrantCR *Kuadrant
		got := kuadrantCR.IsMTLSAuthorinoEnabled()
		assert.Assert(subT, !got)
	})
}
