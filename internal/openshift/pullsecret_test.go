//go:build unit

package openshift

import (
	"encoding/json"
	"testing"
)

func TestExtractRegistryHost(t *testing.T) {
	tests := []struct {
		name     string
		imageURL string
		expected string
	}{
		{
			name:     "standard registry with path and digest",
			imageURL: "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123",
			expected: "registry.redhat.io",
		},
		{
			name:     "registry with port and path",
			imageURL: "mirror.local:8443/rhcl-1/wasm-shim-rhel9@sha256:abc123",
			expected: "mirror.local:8443",
		},
		{
			name:     "registry with tag",
			imageURL: "quay.io/kuadrant/wasm-shim:latest",
			expected: "quay.io",
		},
		{
			name:     "registry with port and tag",
			imageURL: "bastion.example.com:8443/rhcl-1/wasm-shim-rhel9:v0.12.3",
			expected: "bastion.example.com:8443",
		},
		{
			name:     "registry without path with digest",
			imageURL: "registry.redhat.io@sha256:abc123",
			expected: "registry.redhat.io",
		},
		{
			name:     "bare registry",
			imageURL: "registry.redhat.io",
			expected: "registry.redhat.io",
		},
		{
			name:     "default quay image",
			imageURL: "quay.io/kuadrant/wasm-shim:latest",
			expected: "quay.io",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractRegistryHost(tt.imageURL)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestParseDockerConfigAuths(t *testing.T) {
	tests := []struct {
		name           string
		data           string
		expectedAuths  []string
		expectError    bool
	}{
		{
			name: "single registry",
			data: `{"auths":{"registry.redhat.io":{"auth":"dXNlcjpwYXNz"}}}`,
			expectedAuths: []string{"registry.redhat.io"},
		},
		{
			name: "multiple registries",
			data: `{"auths":{"registry.redhat.io":{"auth":"dXNlcjpwYXNz"},"mirror.local:8443":{"auth":"bWlycm9yOnBhc3M="}}}`,
			expectedAuths: []string{"registry.redhat.io", "mirror.local:8443"},
		},
		{
			name:          "empty auths",
			data:          `{"auths":{}}`,
			expectedAuths: []string{},
		},
		{
			name:        "invalid json",
			data:        `not json`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auths, err := parseDockerConfigAuths([]byte(tt.data))
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(auths) != len(tt.expectedAuths) {
				t.Fatalf("expected %d auths, got %d", len(tt.expectedAuths), len(auths))
			}
			for _, key := range tt.expectedAuths {
				if _, ok := auths[key]; !ok {
					t.Errorf("expected key %q in auths", key)
				}
			}
		})
	}
}

func TestBuildFilteredDockerConfigJSON(t *testing.T) {
	auths := map[string]json.RawMessage{
		"registry.redhat.io": json.RawMessage(`{"auth":"dXNlcjpwYXNz"}`),
		"mirror.local:8443":  json.RawMessage(`{"auth":"bWlycm9yOnBhc3M="}`),
		"quay.io":            json.RawMessage(`{"auth":"cXVheTpwYXNz"}`),
	}

	tests := []struct {
		name         string
		registryHost string
		expectNil    bool
		expectKey    string
	}{
		{
			name:         "matching registry returns filtered config",
			registryHost: "registry.redhat.io",
			expectKey:    "registry.redhat.io",
		},
		{
			name:         "matching mirror returns filtered config",
			registryHost: "mirror.local:8443",
			expectKey:    "mirror.local:8443",
		},
		{
			name:         "non-matching registry returns nil",
			registryHost: "nonexistent.io",
			expectNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildFilteredDockerConfigJSON(auths, tt.registryHost)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.expectNil {
				if result != nil {
					t.Fatalf("expected nil, got %s", string(result))
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}

			var cfg dockerConfigJSON
			if err := json.Unmarshal(result, &cfg); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}
			if len(cfg.Auths) != 1 {
				t.Fatalf("expected 1 auth entry, got %d", len(cfg.Auths))
			}
			if _, ok := cfg.Auths[tt.expectKey]; !ok {
				t.Errorf("expected key %q in filtered auths", tt.expectKey)
			}
		})
	}
}
