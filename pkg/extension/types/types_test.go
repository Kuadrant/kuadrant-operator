//go:build unit

package types

import "testing"

func TestKuadrantLoggingBinding(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"client_identity", "logging.fields.client_identity"},
		{"model", "logging.fields.model"},
		{"request_path", "logging.fields.request_path"},
	}
	for _, tc := range tests {
		if got := KuadrantLoggingBinding(tc.input); got != tc.want {
			t.Errorf("KuadrantLoggingBinding(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestKuadrantMetricBinding(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"model", "metrics.labels.model"},
		{"user", "metrics.labels.user"},
	}
	for _, tc := range tests {
		if got := KuadrantMetricBinding(tc.input); got != tc.want {
			t.Errorf("KuadrantMetricBinding(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
