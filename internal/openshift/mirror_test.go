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
		// --- Basic prefix matching ---
		{
			name:     "basic mirror resolution with digest ref and IDMS rule",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "basic mirror resolution with tag ref and ITMS rule",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim:latest",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeTag},
			},
			expected: "mirror.local/kuadrant/wasm-shim:latest",
		},
		{
			name:     "most specific prefix wins",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
				{source: "registry.redhat.io/kuadrant", mirrors: []string{"mirror.local/kuadrant-mirror"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/kuadrant-mirror/wasm-shim@sha256:abc123",
		},
		{
			name:     "no match returns original URL",
			imageURL: "quay.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "quay.io/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "empty mirrors list returns original URL",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{}, pullType: pullTypeDigest},
			},
			expected: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
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
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror1.local", "mirror2.local", "mirror3.local"}, pullType: pullTypeDigest},
			},
			expected: "mirror1.local/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "tag-based image reference with port",
			imageURL: "registry.redhat.io:5000/kuadrant/wasm-shim:v1.0",
			rules: []mirrorRule{
				{source: "registry.redhat.io:5000", mirrors: []string{"mirror.local:8080"}, pullType: pullTypeTag},
			},
			expected: "mirror.local:8080/kuadrant/wasm-shim:v1.0",
		},

		// --- Boundary checking (security) ---
		{
			name:     "source must match on boundary - partial host match rejected",
			imageURL: "registry.redhat.io.evil.com/kuadrant/wasm-shim@sha256:abc",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "registry.redhat.io.evil.com/kuadrant/wasm-shim@sha256:abc",
		},
		{
			name:     "exact source match with digest separator",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:def456",
			rules: []mirrorRule{
				{source: "registry.redhat.io/kuadrant/wasm-shim", mirrors: []string{"mirror.local/wasm"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/wasm@sha256:def456",
		},

		// --- IDMS vs ITMS semantic differentiation ---
		{
			name:     "IDMS rule skipped for tag reference",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim:latest",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "registry.redhat.io/kuadrant/wasm-shim:latest",
		},
		{
			name:     "ITMS rule skipped for digest reference",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeTag},
			},
			expected: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "IDMS and ITMS coexist - digest ref uses IDMS",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"itms-mirror.local"}, pullType: pullTypeTag},
				{source: "registry.redhat.io", mirrors: []string{"idms-mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "idms-mirror.local/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "IDMS and ITMS coexist - tag ref uses ITMS",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim:latest",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"idms-mirror.local"}, pullType: pullTypeDigest},
				{source: "registry.redhat.io", mirrors: []string{"itms-mirror.local"}, pullType: pullTypeTag},
			},
			expected: "itms-mirror.local/kuadrant/wasm-shim:latest",
		},

		// --- Wildcard source support ---
		{
			name:     "wildcard source matches subdomain",
			imageURL: "sub.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "*.redhat.io", mirrors: []string{"mirror.local/redhat"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/redhat/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "wildcard source does not match bare domain",
			imageURL: "redhat.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "*.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "redhat.io/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "wildcard source does not match multi-level subdomain",
			imageURL: "sub.sub.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "*.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "sub.sub.redhat.io/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "exact prefix wins over wildcard of same domain",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "*.redhat.io", mirrors: []string{"wildcard-mirror.local"}, pullType: pullTypeDigest},
				{source: "registry.redhat.io", mirrors: []string{"exact-mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "exact-mirror.local/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "wildcard with port in image URL",
			imageURL: "registry.redhat.io:5000/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "*.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/kuadrant/wasm-shim@sha256:abc123",
		},

		// --- Trailing slash handling ---
		{
			name:     "source with trailing slash is normalized",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io/", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "mirror with trailing slash is normalized",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local/"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/kuadrant/wasm-shim@sha256:abc123",
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

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		name           string
		imageURL       string
		wildcardSource string
		expectedLen    int
	}{
		{
			name:           "single-level subdomain matches",
			imageURL:       "registry.redhat.io/kuadrant/wasm-shim:latest",
			wildcardSource: "*.redhat.io",
			expectedLen:    len("registry.redhat.io"),
		},
		{
			name:           "bare domain does not match",
			imageURL:       "redhat.io/kuadrant/wasm-shim:latest",
			wildcardSource: "*.redhat.io",
			expectedLen:    0,
		},
		{
			name:           "multi-level subdomain does not match",
			imageURL:       "a.b.redhat.io/kuadrant/wasm-shim:latest",
			wildcardSource: "*.redhat.io",
			expectedLen:    0,
		},
		{
			name:           "hostname with port matches",
			imageURL:       "registry.redhat.io:5000/kuadrant/wasm-shim:latest",
			wildcardSource: "*.redhat.io",
			expectedLen:    len("registry.redhat.io:5000"),
		},
		{
			name:           "unrelated domain does not match",
			imageURL:       "quay.io/kuadrant/wasm-shim:latest",
			wildcardSource: "*.redhat.io",
			expectedLen:    0,
		},
		{
			name:           "suffix-only match rejected (evil.redhat.io.attacker.com)",
			imageURL:       "evil.redhat.io.attacker.com/foo:latest",
			wildcardSource: "*.redhat.io",
			expectedLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchWildcard(tt.imageURL, tt.wildcardSource)
			if result != tt.expectedLen {
				t.Errorf("expected match length %d, got %d", tt.expectedLen, result)
			}
		})
	}
}

func TestMatchPrefix(t *testing.T) {
	tests := []struct {
		name        string
		imageURL    string
		source      string
		expectedLen int
	}{
		{
			name:        "matches with path separator",
			imageURL:    "registry.redhat.io/kuadrant/wasm-shim:latest",
			source:      "registry.redhat.io",
			expectedLen: len("registry.redhat.io"),
		},
		{
			name:        "matches with tag separator",
			imageURL:    "registry.redhat.io:latest",
			source:      "registry.redhat.io",
			expectedLen: len("registry.redhat.io"),
		},
		{
			name:        "matches with digest separator",
			imageURL:    "registry.redhat.io@sha256:abc",
			source:      "registry.redhat.io",
			expectedLen: len("registry.redhat.io"),
		},
		{
			name:        "exact match",
			imageURL:    "registry.redhat.io",
			source:      "registry.redhat.io",
			expectedLen: len("registry.redhat.io"),
		},
		{
			name:        "partial hostname rejected",
			imageURL:    "registry.redhat.io.evil.com/foo:latest",
			source:      "registry.redhat.io",
			expectedLen: 0,
		},
		{
			name:        "no prefix match",
			imageURL:    "quay.io/kuadrant/wasm-shim:latest",
			source:      "registry.redhat.io",
			expectedLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchPrefix(tt.imageURL, tt.source)
			if result != tt.expectedLen {
				t.Errorf("expected match length %d, got %d", tt.expectedLen, result)
			}
		})
	}
}
