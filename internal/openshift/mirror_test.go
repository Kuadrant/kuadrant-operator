//go:build unit

package openshift

import (
	"testing"
)

const (
	// Realistic production image references used by Red Hat builds of the wasm-shim
	realDigestRef = "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb"
	realTagRef    = "registry.redhat.io/rhcl-1/wasm-shim-rhel9:v0.12.3-4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb"
	altDigestRef  = "registry.access.redhat.com/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb"
	altTagRef     = "registry.access.redhat.com/rhcl-1/wasm-shim-rhel9:v0.12.3-4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb"
)

func TestResolveFromMirrors(t *testing.T) {
	tests := []struct {
		name     string
		imageURL string
		rules    []mirrorRule
		expected string
	}{
		// --- Realistic production image references ---
		{
			name:     "realistic digest ref resolved via IDMS",
			imageURL: realDigestRef,
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.disconnected.local"}, pullType: pullTypeDigest},
			},
			expected: "mirror.disconnected.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "realistic tag ref resolved via ITMS",
			imageURL: realTagRef,
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.disconnected.local"}, pullType: pullTypeTag},
			},
			expected: "mirror.disconnected.local/rhcl-1/wasm-shim-rhel9:v0.12.3-4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "realistic alt registry digest ref resolved via IDMS",
			imageURL: altDigestRef,
			rules: []mirrorRule{
				{source: "registry.access.redhat.com", mirrors: []string{"mirror.disconnected.local"}, pullType: pullTypeDigest},
			},
			expected: "mirror.disconnected.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "realistic alt registry tag ref resolved via ITMS",
			imageURL: altTagRef,
			rules: []mirrorRule{
				{source: "registry.access.redhat.com", mirrors: []string{"mirror.disconnected.local"}, pullType: pullTypeTag},
			},
			expected: "mirror.disconnected.local/rhcl-1/wasm-shim-rhel9:v0.12.3-4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "realistic specific namespace source wins over broad registry",
			imageURL: realDigestRef,
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"broad-mirror.local"}, pullType: pullTypeDigest},
				{source: "registry.redhat.io/rhcl-1", mirrors: []string{"specific-mirror.local/rhcl-1"}, pullType: pullTypeDigest},
			},
			expected: "specific-mirror.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "realistic IDMS rule skipped for tag reference",
			imageURL: realTagRef,
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: realTagRef,
		},
		{
			name:     "realistic ITMS rule skipped for digest reference",
			imageURL: realDigestRef,
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeTag},
			},
			expected: realDigestRef,
		},

		// --- Basic prefix matching ---
		{
			name:     "basic mirror resolution with digest ref and IDMS rule",
			imageURL: "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/rhcl-1/wasm-shim-rhel9@sha256:abc123",
		},
		{
			name:     "basic mirror resolution with tag ref and ITMS rule",
			imageURL: "registry.redhat.io/rhcl-1/wasm-shim-rhel9:latest",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeTag},
			},
			expected: "mirror.local/rhcl-1/wasm-shim-rhel9:latest",
		},
		{
			name:     "most specific prefix wins",
			imageURL: "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
				{source: "registry.redhat.io/rhcl-1", mirrors: []string{"mirror.local/rhcl-1-mirror"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/rhcl-1-mirror/wasm-shim-rhel9@sha256:abc123",
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
			imageURL: realDigestRef,
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{}, pullType: pullTypeDigest},
			},
			expected: realDigestRef,
		},
		{
			name:     "empty rules returns original URL",
			imageURL: realTagRef,
			rules:    []mirrorRule{},
			expected: realTagRef,
		},
		{
			name:     "nil rules returns original URL",
			imageURL: realTagRef,
			rules:    nil,
			expected: realTagRef,
		},
		{
			name:     "first mirror is used when multiple mirrors exist",
			imageURL: realDigestRef,
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror1.local", "mirror2.local", "mirror3.local"}, pullType: pullTypeDigest},
			},
			expected: "mirror1.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "tag-based image reference with port",
			imageURL: "registry.redhat.io:5000/rhcl-1/wasm-shim-rhel9:v1.0",
			rules: []mirrorRule{
				{source: "registry.redhat.io:5000", mirrors: []string{"mirror.local:8080"}, pullType: pullTypeTag},
			},
			expected: "mirror.local:8080/rhcl-1/wasm-shim-rhel9:v1.0",
		},

		// --- Mirror with port and path-based source ---
		{
			name:     "mirror port preserved with path-based source and digest ref",
			imageURL: "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
			rules: []mirrorRule{
				{source: "registry.redhat.io/rhcl-1", mirrors: []string{"bastion.example.com:8443/rhcl-1"}, pullType: pullTypeDigest},
			},
			expected: "bastion.example.com:8443/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "mirror port preserved with path-based source and tag ref",
			imageURL: "registry.redhat.io/rhcl-1/wasm-shim-rhel9:v0.12.3",
			rules: []mirrorRule{
				{source: "registry.redhat.io/rhcl-1", mirrors: []string{"bastion.example.com:8443/rhcl-1"}, pullType: pullTypeTag},
			},
			expected: "bastion.example.com:8443/rhcl-1/wasm-shim-rhel9:v0.12.3",
		},
		{
			name:     "dual source registries with port-bearing mirror",
			imageURL: "registry.access.redhat.com/rhcl-1/wasm-shim-rhel9@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io/rhcl-1", mirrors: []string{"bastion.example.com:8443/rhcl-1"}, pullType: pullTypeDigest},
				{source: "registry.access.redhat.com/rhcl-1", mirrors: []string{"bastion.example.com:8443/rhcl-1"}, pullType: pullTypeDigest},
			},
			expected: "bastion.example.com:8443/rhcl-1/wasm-shim-rhel9@sha256:abc123",
		},

		// --- Boundary checking (security) ---
		{
			name:     "source must match on boundary - partial host match rejected",
			imageURL: "registry.redhat.io.evil.com/rhcl-1/wasm-shim-rhel9@sha256:abc",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "registry.redhat.io.evil.com/rhcl-1/wasm-shim-rhel9@sha256:abc",
		},
		{
			name:     "exact source match with digest separator",
			imageURL: "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:def456",
			rules: []mirrorRule{
				{source: "registry.redhat.io/rhcl-1/wasm-shim-rhel9", mirrors: []string{"mirror.local/wasm"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/wasm@sha256:def456",
		},

		// --- IDMS vs ITMS semantic differentiation ---
		{
			name:     "IDMS rule skipped for tag reference",
			imageURL: "registry.redhat.io/rhcl-1/wasm-shim-rhel9:latest",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "registry.redhat.io/rhcl-1/wasm-shim-rhel9:latest",
		},
		{
			name:     "ITMS rule skipped for digest reference",
			imageURL: "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123",
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeTag},
			},
			expected: "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123",
		},
		{
			name:     "IDMS and ITMS coexist - digest ref uses IDMS",
			imageURL: realDigestRef,
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"itms-mirror.local"}, pullType: pullTypeTag},
				{source: "registry.redhat.io", mirrors: []string{"idms-mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "idms-mirror.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "IDMS and ITMS coexist - tag ref uses ITMS",
			imageURL: realTagRef,
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"idms-mirror.local"}, pullType: pullTypeDigest},
				{source: "registry.redhat.io", mirrors: []string{"itms-mirror.local"}, pullType: pullTypeTag},
			},
			expected: "itms-mirror.local/rhcl-1/wasm-shim-rhel9:v0.12.3-4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},

		// --- Wildcard source support ---
		{
			name:     "wildcard source matches subdomain",
			imageURL: "sub.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123",
			rules: []mirrorRule{
				{source: "*.redhat.io", mirrors: []string{"mirror.local/redhat"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/redhat/rhcl-1/wasm-shim-rhel9@sha256:abc123",
		},
		{
			name:     "wildcard source does not match bare domain",
			imageURL: "redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123",
			rules: []mirrorRule{
				{source: "*.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123",
		},
		{
			name:     "wildcard source does not match multi-level subdomain",
			imageURL: "sub.sub.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123",
			rules: []mirrorRule{
				{source: "*.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "sub.sub.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123",
		},
		{
			name:     "exact prefix wins over wildcard of same domain",
			imageURL: "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123",
			rules: []mirrorRule{
				{source: "*.redhat.io", mirrors: []string{"wildcard-mirror.local"}, pullType: pullTypeDigest},
				{source: "registry.redhat.io", mirrors: []string{"exact-mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "exact-mirror.local/rhcl-1/wasm-shim-rhel9@sha256:abc123",
		},
		{
			name:     "wildcard with port in image URL",
			imageURL: "registry.redhat.io:5000/rhcl-1/wasm-shim-rhel9@sha256:abc123",
			rules: []mirrorRule{
				{source: "*.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/rhcl-1/wasm-shim-rhel9@sha256:abc123",
		},
		{
			name:     "wildcard source with image URL without path does not match",
			imageURL: "registry.redhat.io@sha256:abc123",
			rules: []mirrorRule{
				{source: "*.redhat.io", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "registry.redhat.io@sha256:abc123",
		},

		// --- Trailing slash handling ---
		{
			name:     "source with trailing slash is normalized",
			imageURL: realDigestRef,
			rules: []mirrorRule{
				{source: "registry.redhat.io/", mirrors: []string{"mirror.local"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "mirror with trailing slash is normalized",
			imageURL: realDigestRef,
			rules: []mirrorRule{
				{source: "registry.redhat.io", mirrors: []string{"mirror.local/"}, pullType: pullTypeDigest},
			},
			expected: "mirror.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
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
			imageURL:       "registry.redhat.io/rhcl-1/wasm-shim-rhel9:latest",
			wildcardSource: "*.redhat.io",
			expectedLen:    len("registry.redhat.io"),
		},
		{
			name:           "bare domain does not match",
			imageURL:       "redhat.io/rhcl-1/wasm-shim-rhel9:latest",
			wildcardSource: "*.redhat.io",
			expectedLen:    0,
		},
		{
			name:           "multi-level subdomain does not match",
			imageURL:       "a.b.redhat.io/rhcl-1/wasm-shim-rhel9:latest",
			wildcardSource: "*.redhat.io",
			expectedLen:    0,
		},
		{
			name:           "hostname with port matches",
			imageURL:       "registry.redhat.io:5000/rhcl-1/wasm-shim-rhel9:latest",
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
		{
			name:           "image URL without path",
			imageURL:       "registry.redhat.io",
			wildcardSource: "*.redhat.io",
			expectedLen:    len("registry.redhat.io"),
		},
		{
			name:           "image URL without path and with port",
			imageURL:       "registry.redhat.io:5000",
			wildcardSource: "*.redhat.io",
			expectedLen:    len("registry.redhat.io:5000"),
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
			imageURL:    "registry.redhat.io/rhcl-1/wasm-shim-rhel9:latest",
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
		{
			name:        "realistic registry.access.redhat.com match",
			imageURL:    altDigestRef,
			source:      "registry.access.redhat.com",
			expectedLen: len("registry.access.redhat.com"),
		},
		{
			name:        "realistic nested path source match",
			imageURL:    realDigestRef,
			source:      "registry.redhat.io/rhcl-1",
			expectedLen: len("registry.redhat.io/rhcl-1"),
		},
		{
			name:        "realistic full repo path match",
			imageURL:    realDigestRef,
			source:      "registry.redhat.io/rhcl-1/wasm-shim-rhel9",
			expectedLen: len("registry.redhat.io/rhcl-1/wasm-shim-rhel9"),
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

func TestRewriteWildcard(t *testing.T) {
	tests := []struct {
		name           string
		imageURL       string
		wildcardSource string
		mirror         string
		expected       string
	}{
		{
			name:           "rewrites with path",
			imageURL:       "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123",
			wildcardSource: "*.redhat.io",
			mirror:         "mirror.local/redhat",
			expected:       "mirror.local/redhat/rhcl-1/wasm-shim-rhel9@sha256:abc123",
		},
		{
			name:           "rewrites without path returns mirror only",
			imageURL:       "registry.redhat.io",
			wildcardSource: "*.redhat.io",
			mirror:         "mirror.local",
			expected:       "mirror.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rewriteWildcard(tt.imageURL, tt.wildcardSource, tt.mirror)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
