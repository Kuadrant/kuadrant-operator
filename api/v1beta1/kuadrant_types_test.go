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

func TestObservabilitySpec(t *testing.T) {
	tests := []struct {
		name          string
		observability Observability
		description   string
	}{
		{
			name: "observability disabled with nil dataPlane",
			observability: Observability{
				Enable:    false,
				DataPlane: nil,
			},
			description: "observability can be disabled",
		},
		{
			name: "observability enabled with empty dataPlane",
			observability: Observability{
				Enable:    true,
				DataPlane: &DataPlane{},
			},
			description: "observability enabled with empty dataPlane config",
		},
		{
			name: "observability with single debug level",
			observability: Observability{
				Enable: true,
				DataPlane: &DataPlane{
					DefaultLevels: []LogLevel{
						{Debug: ptr.To("true")},
					},
				},
			},
			description: "debug level can be configured",
		},
		{
			name: "observability with multiple log levels",
			observability: Observability{
				Enable: true,
				DataPlane: &DataPlane{
					DefaultLevels: []LogLevel{
						{Debug: ptr.To("true")},
						{Info: ptr.To("true")},
						{Warn: ptr.To("true")},
						{Error: ptr.To("true")},
					},
				},
			},
			description: "multiple log levels can be configured",
		},
		{
			name: "observability with CEL predicates (future)",
			observability: Observability{
				Enable: true,
				DataPlane: &DataPlane{
					DefaultLevels: []LogLevel{
						{Debug: ptr.To(`source.ip == "127.0.0.1"`)},
						{Error: ptr.To("true")},
					},
				},
			},
			description: "CEL predicates can be stored for future use",
		},
		{
			name: "observability with max items (10 levels)",
			observability: Observability{
				Enable: true,
				DataPlane: &DataPlane{
					DefaultLevels: []LogLevel{
						{Debug: ptr.To("true")},
						{Info: ptr.To("true")},
						{Warn: ptr.To("true")},
						{Error: ptr.To("true")},
						{Debug: ptr.To("false")},
						{Info: ptr.To("false")},
						{Warn: ptr.To("false")},
						{Error: ptr.To("false")},
						{Debug: ptr.To("maybe")},
						{Info: ptr.To("maybe")},
					},
				},
			},
			description: "up to 10 log level entries are allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(subT *testing.T) {
			kuadrantCR := &Kuadrant{
				Spec: KuadrantSpec{
					Observability: tt.observability,
				},
			}
			// Verify the spec can be set without panic
			assert.Assert(subT, kuadrantCR != nil)
			assert.Equal(subT, kuadrantCR.Spec.Observability.Enable, tt.observability.Enable)

			// Verify DataPlane configuration
			if tt.observability.DataPlane != nil {
				assert.Assert(subT, kuadrantCR.Spec.Observability.DataPlane != nil)
				assert.Equal(subT, len(kuadrantCR.Spec.Observability.DataPlane.DefaultLevels),
					len(tt.observability.DataPlane.DefaultLevels))
			}
		})
	}
}

func TestLogLevelStructure(t *testing.T) {
	tests := []struct {
		name     string
		logLevel LogLevel
		hasDebug bool
		hasInfo  bool
		hasWarn  bool
		hasError bool
	}{
		{
			name:     "debug only",
			logLevel: LogLevel{Debug: ptr.To("true")},
			hasDebug: true,
			hasInfo:  false,
			hasWarn:  false,
			hasError: false,
		},
		{
			name:     "info only",
			logLevel: LogLevel{Info: ptr.To("true")},
			hasDebug: false,
			hasInfo:  true,
			hasWarn:  false,
			hasError: false,
		},
		{
			name:     "warn only",
			logLevel: LogLevel{Warn: ptr.To("true")},
			hasDebug: false,
			hasInfo:  false,
			hasWarn:  true,
			hasError: false,
		},
		{
			name:     "error only",
			logLevel: LogLevel{Error: ptr.To("true")},
			hasDebug: false,
			hasInfo:  false,
			hasWarn:  false,
			hasError: true,
		},
		{
			name:     "all nil - empty LogLevel",
			logLevel: LogLevel{},
			hasDebug: false,
			hasInfo:  false,
			hasWarn:  false,
			hasError: false,
		},
		{
			name: "multiple fields set (not recommended but structurally valid)",
			logLevel: LogLevel{
				Debug: ptr.To("true"),
				Info:  ptr.To("true"),
			},
			hasDebug: true,
			hasInfo:  true,
			hasWarn:  false,
			hasError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(subT *testing.T) {
			assert.Equal(subT, tt.logLevel.Debug != nil, tt.hasDebug)
			assert.Equal(subT, tt.logLevel.Info != nil, tt.hasInfo)
			assert.Equal(subT, tt.logLevel.Warn != nil, tt.hasWarn)
			assert.Equal(subT, tt.logLevel.Error != nil, tt.hasError)
		})
	}
}

func TestIsDeveloperPortalEnabled(t *testing.T) {
	tests := []struct {
		name        string
		components  *Components
		expectedRes bool
	}{
		{
			name:        "components is nil",
			components:  nil,
			expectedRes: false,
		},
		{
			name:        "developer portal not set",
			components:  &Components{},
			expectedRes: false,
		},
		{
			name: "developer portal disabled",
			components: &Components{
				DeveloperPortal: &DeveloperPortal{Enabled: false},
			},
			expectedRes: false,
		},
		{
			name: "developer portal enabled",
			components: &Components{
				DeveloperPortal: &DeveloperPortal{Enabled: true},
			},
			expectedRes: true,
		},
	}
	for _, tt := range tests {
		kuadrantCR := &Kuadrant{
			Spec: KuadrantSpec{
				Components: tt.components,
			},
		}
		t.Run(tt.name, func(subT *testing.T) {
			got := kuadrantCR.IsDeveloperPortalEnabled()
			assert.Equal(subT, got, tt.expectedRes)
		})
	}

	t.Run("kuadrant is nil", func(subT *testing.T) {
		var kuadrantCR *Kuadrant
		got := kuadrantCR.IsDeveloperPortalEnabled()
		assert.Assert(subT, !got)
	})
}
