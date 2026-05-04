//go:build unit

package openshift

import (
	"testing"
)

func TestResolveFromMirrors(t *testing.T) {
	tests := []struct {
		name     string
		imageURL string
		rules    []mirrorRule
		expected string
	}{
		{
			name:     "basic mirror resolution",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim:latest",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}},
			},
			expected: "mirror.local/kuadrant/wasm-shim:latest",
		},
		{
			name:     "most specific prefix wins",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim:latest",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}},
				{source: "registry.redhat.io/kuadrant", mirrors: []string{"mirror.local/kuadrant-mirror"}},
			},
			expected: "mirror.local/kuadrant-mirror/wasm-shim:latest",
		},
		{
			name:     "no match returns original URL",
			imageURL: "quay.io/kuadrant/wasm-shim:latest",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}},
			},
			expected: "quay.io/kuadrant/wasm-shim:latest",
		},
		{
			name:     "empty mirrors list returns original URL",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim:latest",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{}},
			},
			expected: "registry.redhat.io/kuadrant/wasm-shim:latest",
		},
		{
			name:     "digest-based image reference",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}},
			},
			expected: "mirror.local/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "tag-based image reference with port",
			imageURL: "registry.redhat.io:5000/kuadrant/wasm-shim:v1.0",
			rules: []mirrorRule{
				{source: "registry.redhat.io:5000", mirrors: []string{"mirror.local:8080"}},
			},
			expected: "mirror.local:8080/kuadrant/wasm-shim:v1.0",
		},
		{
			name:     "multiple rules with overlapping sources uses longest match",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim:latest",
			rules: []mirrorRule{
				{source: "registry.redhat.io/kuadrant/wasm-shim", mirrors: []string{"mirror.local/exact-match"}},
				{source: "registry.redhat.io/kuadrant", mirrors: []string{"mirror.local/ns-match"}},
				{source: "registry.redhat.io", mirrors: []string{"mirror.local/host-match"}},
			},
			expected: "mirror.local/exact-match:latest",
		},
		{
			name:     "empty rules returns original URL",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim:latest",
			rules:    []mirrorRule{},
			expected: "registry.redhat.io/kuadrant/wasm-shim:latest",
		},
		{
			name:     "nil rules returns original URL",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim:latest",
			rules:    nil,
			expected: "registry.redhat.io/kuadrant/wasm-shim:latest",
		},
		{
			name:     "first mirror is used when multiple mirrors exist",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim:latest",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror1.local", "mirror2.local", "mirror3.local"}},
			},
			expected: "mirror1.local/kuadrant/wasm-shim:latest",
		},
		{
			name:     "source must match on boundary - partial host match rejected",
			imageURL: "registry.redhat.io.evil.com/kuadrant/wasm-shim:latest",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}},
			},
			expected: "registry.redhat.io.evil.com/kuadrant/wasm-shim:latest",
		},
		{
			name:     "exact source match with digest separator",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:def456",
			rules: []mirrorRule{
				{source: "registry.redhat.io/kuadrant/wasm-shim", mirrors: []string{"mirror.local/wasm"}},
			},
			expected: "mirror.local/wasm@sha256:def456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveFromMirrors(tt.imageURL, tt.rules)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
