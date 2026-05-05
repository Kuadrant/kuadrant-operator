//go:build unit

package openshift

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	dfake "k8s.io/client-go/dynamic/fake"
)

func newPullSecretScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	return s
}

func makeDockerConfigSecret(namespace, name string, auths map[string]string) *corev1.Secret {
	authsMap := make(map[string]json.RawMessage)
	for registry, auth := range auths {
		authsMap[registry] = json.RawMessage(`{"auth":"` + auth + `"}`)
	}
	cfg := dockerConfigJSON{Auths: authsMap}
	data, _ := json.Marshal(cfg)

	return &corev1.Secret{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: data},
	}
}

func makeManagedSecret(namespace, name string, dockerConfigData []byte) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{ManagedLabelKey: ManagedLabelValue},
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{corev1.DockerConfigJsonKey: dockerConfigData},
	}
}

func makeUserSecret(namespace, name string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{"user.registry.io":{"auth":"dXNlcg=="}}}`)},
	}
}

// --- ExtractRegistryHost tests ---

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

// --- parseDockerConfigAuths tests ---

func TestParseDockerConfigAuths(t *testing.T) {
	tests := []struct {
		name          string
		data          string
		expectedAuths []string
		expectError   bool
	}{
		{
			name:          "single registry",
			data:          `{"auths":{"registry.redhat.io":{"auth":"dXNlcjpwYXNz"}}}`,
			expectedAuths: []string{"registry.redhat.io"},
		},
		{
			name:          "multiple registries",
			data:          `{"auths":{"registry.redhat.io":{"auth":"dXNlcjpwYXNz"},"mirror.local:8443":{"auth":"bWlycm9yOnBhc3M="}}}`,
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

// --- buildFilteredDockerConfigJSON tests ---

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

// --- ResolveRegistryCredentials tests ---

func TestResolveRegistryCredentials(t *testing.T) {
	t.Run("returns credentials from pull-secret", func(t *testing.T) {
		s := newPullSecretScheme()
		fakeClient := dfake.NewSimpleDynamicClient(s,
			makeDockerConfigSecret(OpenshiftConfigNamespace, ClusterPullSecretName, map[string]string{
				"registry.redhat.io": "cmVkaGF0OnBhc3M=",
			}),
		)

		creds, err := ResolveRegistryCredentials(context.Background(), fakeClient, "registry.redhat.io", logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds == nil {
			t.Fatal("expected credentials, got nil")
		}

		var cfg dockerConfigJSON
		if err := json.Unmarshal(creds, &cfg); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if _, ok := cfg.Auths["registry.redhat.io"]; !ok {
			t.Error("expected registry.redhat.io in credentials")
		}
	})

	t.Run("additional-pull-secret overrides pull-secret", func(t *testing.T) {
		s := newPullSecretScheme()
		fakeClient := dfake.NewSimpleDynamicClient(s,
			makeDockerConfigSecret(OpenshiftConfigNamespace, ClusterPullSecretName, map[string]string{
				"registry.redhat.io": "b3JpZ2luYWw=",
			}),
			makeDockerConfigSecret(OpenshiftConfigNamespace, AdditionalPullSecretName, map[string]string{
				"registry.redhat.io": "b3ZlcnJpZGRlbg==",
			}),
		)

		creds, err := ResolveRegistryCredentials(context.Background(), fakeClient, "registry.redhat.io", logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds == nil {
			t.Fatal("expected credentials, got nil")
		}

		var cfg dockerConfigJSON
		if err := json.Unmarshal(creds, &cfg); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		entry := cfg.Auths["registry.redhat.io"]
		if string(entry) != `{"auth":"b3ZlcnJpZGRlbg=="}` {
			t.Errorf("expected overridden auth, got %s", string(entry))
		}
	})

	t.Run("additional-pull-secret adds new registries", func(t *testing.T) {
		s := newPullSecretScheme()
		fakeClient := dfake.NewSimpleDynamicClient(s,
			makeDockerConfigSecret(OpenshiftConfigNamespace, ClusterPullSecretName, map[string]string{
				"registry.redhat.io": "cmVkaGF0",
			}),
			makeDockerConfigSecret(OpenshiftConfigNamespace, AdditionalPullSecretName, map[string]string{
				"mirror.local:8443": "bWlycm9y",
			}),
		)

		creds, err := ResolveRegistryCredentials(context.Background(), fakeClient, "mirror.local:8443", logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds == nil {
			t.Fatal("expected credentials for mirror.local:8443, got nil")
		}
	})

	t.Run("returns nil when registry not found in any secret", func(t *testing.T) {
		s := newPullSecretScheme()
		fakeClient := dfake.NewSimpleDynamicClient(s,
			makeDockerConfigSecret(OpenshiftConfigNamespace, ClusterPullSecretName, map[string]string{
				"registry.redhat.io": "cmVkaGF0",
			}),
		)

		creds, err := ResolveRegistryCredentials(context.Background(), fakeClient, "unknown.io", logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds != nil {
			t.Fatalf("expected nil, got %s", string(creds))
		}
	})

	t.Run("returns nil when no secrets exist (non-OpenShift)", func(t *testing.T) {
		s := newPullSecretScheme()
		fakeClient := dfake.NewSimpleDynamicClient(s)

		creds, err := ResolveRegistryCredentials(context.Background(), fakeClient, "registry.redhat.io", logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds != nil {
			t.Fatalf("expected nil, got %s", string(creds))
		}
	})

	t.Run("handles API errors gracefully", func(t *testing.T) {
		s := newPullSecretScheme()
		fakeClient := dfake.NewSimpleDynamicClient(s)
		fakeClient.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, &errFake{msg: "API server error"}
		})

		creds, err := ResolveRegistryCredentials(context.Background(), fakeClient, "registry.redhat.io", logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds != nil {
			t.Fatalf("expected nil on API error, got %s", string(creds))
		}
	})
}

// --- EnsureWasmPluginPullSecret tests ---

func TestEnsureWasmPluginPullSecret(t *testing.T) {
	const (
		namespace  = "gateway-ns"
		secretName = "wasm-plugin-pull-secret"
	)

	sampleCreds := []byte(`{"auths":{"mirror.local:8443":{"auth":"bWlycm9y"}}}`)
	updatedCreds := []byte(`{"auths":{"mirror.local:8443":{"auth":"dXBkYXRlZA=="}}}`)

	t.Run("creates managed secret when none exists and creds available", func(t *testing.T) {
		s := newPullSecretScheme()
		fakeClient := dfake.NewSimpleDynamicClient(s)

		shouldSet, err := EnsureWasmPluginPullSecret(context.Background(), fakeClient, namespace, secretName, sampleCreds, logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !shouldSet {
			t.Error("expected shouldSet=true after creating secret")
		}

		created, err := fakeClient.Resource(secretsResource).Namespace(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("secret was not created: %v", err)
		}
		labels := created.GetLabels()
		if labels[ManagedLabelKey] != ManagedLabelValue {
			t.Error("created secret missing managed label")
		}
	})

	t.Run("returns false when no secret exists and no creds", func(t *testing.T) {
		s := newPullSecretScheme()
		fakeClient := dfake.NewSimpleDynamicClient(s)

		shouldSet, err := EnsureWasmPluginPullSecret(context.Background(), fakeClient, namespace, secretName, nil, logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if shouldSet {
			t.Error("expected shouldSet=false when no secret and no creds")
		}
	})

	t.Run("does not touch user-created secret", func(t *testing.T) {
		s := newPullSecretScheme()
		userSecret := makeUserSecret(namespace, secretName)
		fakeClient := dfake.NewSimpleDynamicClient(s, userSecret)

		shouldSet, err := EnsureWasmPluginPullSecret(context.Background(), fakeClient, namespace, secretName, sampleCreds, logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !shouldSet {
			t.Error("expected shouldSet=true when user-created secret exists")
		}

		existing, _ := fakeClient.Resource(secretsResource).Namespace(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
		labels := existing.GetLabels()
		if labels != nil && labels[ManagedLabelKey] == ManagedLabelValue {
			t.Error("user secret should NOT have managed label")
		}
	})

	t.Run("does not touch user-created secret even with nil creds", func(t *testing.T) {
		s := newPullSecretScheme()
		userSecret := makeUserSecret(namespace, secretName)
		fakeClient := dfake.NewSimpleDynamicClient(s, userSecret)

		shouldSet, err := EnsureWasmPluginPullSecret(context.Background(), fakeClient, namespace, secretName, nil, logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !shouldSet {
			t.Error("expected shouldSet=true when user-created secret exists, even with nil creds")
		}
	})

	t.Run("deletes managed secret when creds are nil", func(t *testing.T) {
		s := newPullSecretScheme()
		managedSecret := makeManagedSecret(namespace, secretName, sampleCreds)
		fakeClient := dfake.NewSimpleDynamicClient(s, managedSecret)

		shouldSet, err := EnsureWasmPluginPullSecret(context.Background(), fakeClient, namespace, secretName, nil, logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if shouldSet {
			t.Error("expected shouldSet=false after deleting managed secret")
		}

		_, err = fakeClient.Resource(secretsResource).Namespace(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
		if err == nil {
			t.Error("expected secret to be deleted")
		}
	})

	t.Run("updates managed secret when data changes", func(t *testing.T) {
		s := newPullSecretScheme()
		managedSecret := makeManagedSecret(namespace, secretName, sampleCreds)
		fakeClient := dfake.NewSimpleDynamicClient(s, managedSecret)

		shouldSet, err := EnsureWasmPluginPullSecret(context.Background(), fakeClient, namespace, secretName, updatedCreds, logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !shouldSet {
			t.Error("expected shouldSet=true after updating secret")
		}
	})

	t.Run("no-op when managed secret data is unchanged", func(t *testing.T) {
		s := newPullSecretScheme()
		managedSecret := makeManagedSecret(namespace, secretName, sampleCreds)
		fakeClient := dfake.NewSimpleDynamicClient(s, managedSecret)

		var updateCalled bool
		fakeClient.PrependReactor("update", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			updateCalled = true
			return false, nil, nil
		})

		shouldSet, err := EnsureWasmPluginPullSecret(context.Background(), fakeClient, namespace, secretName, sampleCreds, logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !shouldSet {
			t.Error("expected shouldSet=true")
		}
		if updateCalled {
			t.Error("expected no update when data is unchanged")
		}
	})

	t.Run("no-op when managed secret has same logical content but different JSON formatting", func(t *testing.T) {
		s := newPullSecretScheme()
		// Existing secret uses formatted JSON with different key order and whitespace
		existingData := []byte(`{ "auths" : { "mirror.local:8443" : { "auth" : "bWlycm9y" } } }`)
		managedSecret := makeManagedSecret(namespace, secretName, existingData)
		fakeClient := dfake.NewSimpleDynamicClient(s, managedSecret)

		var updateCalled bool
		fakeClient.PrependReactor("update", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			updateCalled = true
			return false, nil, nil
		})

		// Desired data is compact JSON — same logical content
		shouldSet, err := EnsureWasmPluginPullSecret(context.Background(), fakeClient, namespace, secretName, sampleCreds, logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !shouldSet {
			t.Error("expected shouldSet=true")
		}
		if updateCalled {
			t.Error("expected no update when logical content is the same")
		}
	})

	t.Run("returns error on get failure", func(t *testing.T) {
		s := newPullSecretScheme()
		fakeClient := dfake.NewSimpleDynamicClient(s)
		fakeClient.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, &errFake{msg: "server error"}
		})

		_, err := EnsureWasmPluginPullSecret(context.Background(), fakeClient, namespace, secretName, sampleCreds, logr.Discard())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("returns error on create failure", func(t *testing.T) {
		s := newPullSecretScheme()
		fakeClient := dfake.NewSimpleDynamicClient(s)
		fakeClient.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, &errFake{msg: "create failed"}
		})

		_, err := EnsureWasmPluginPullSecret(context.Background(), fakeClient, namespace, secretName, sampleCreds, logr.Discard())
		if err == nil {
			t.Fatal("expected error on create failure, got nil")
		}
	})

	t.Run("returns error on delete failure", func(t *testing.T) {
		s := newPullSecretScheme()
		managedSecret := makeManagedSecret(namespace, secretName, sampleCreds)
		fakeClient := dfake.NewSimpleDynamicClient(s, managedSecret)
		fakeClient.PrependReactor("delete", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, &errFake{msg: "delete failed"}
		})

		_, err := EnsureWasmPluginPullSecret(context.Background(), fakeClient, namespace, secretName, nil, logr.Discard())
		if err == nil {
			t.Fatal("expected error on delete failure, got nil")
		}
	})

	t.Run("returns error on update failure", func(t *testing.T) {
		s := newPullSecretScheme()
		managedSecret := makeManagedSecret(namespace, secretName, sampleCreds)
		fakeClient := dfake.NewSimpleDynamicClient(s, managedSecret)
		fakeClient.PrependReactor("update", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, &errFake{msg: "update failed"}
		})

		_, err := EnsureWasmPluginPullSecret(context.Background(), fakeClient, namespace, secretName, updatedCreds, logr.Discard())
		if err == nil {
			t.Fatal("expected error on update failure, got nil")
		}
	})
}

// --- readAndMerge edge case tests (via ResolveRegistryCredentials) ---

func TestResolveRegistryCredentials_EdgeCases(t *testing.T) {
	t.Run("secret without .dockerconfigjson key is skipped", func(t *testing.T) {
		s := newPullSecretScheme()
		secretWithoutKey := &corev1.Secret{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: ClusterPullSecretName, Namespace: OpenshiftConfigNamespace},
			Type:       corev1.SecretTypeOpaque,
			Data:       map[string][]byte{"some-other-key": []byte("irrelevant")},
		}
		fakeClient := dfake.NewSimpleDynamicClient(s, secretWithoutKey)

		creds, err := ResolveRegistryCredentials(context.Background(), fakeClient, "registry.redhat.io", logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds != nil {
			t.Fatalf("expected nil when .dockerconfigjson key missing, got %s", string(creds))
		}
	})

	t.Run("secret with invalid dockerconfigjson data is skipped", func(t *testing.T) {
		s := newPullSecretScheme()
		secretWithBadData := &corev1.Secret{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: ClusterPullSecretName, Namespace: OpenshiftConfigNamespace},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte("not-valid-json")},
		}
		fakeClient := dfake.NewSimpleDynamicClient(s, secretWithBadData)

		creds, err := ResolveRegistryCredentials(context.Background(), fakeClient, "registry.redhat.io", logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds != nil {
			t.Fatalf("expected nil when dockerconfigjson is invalid, got %s", string(creds))
		}
	})

	t.Run("one valid and one invalid secret still returns creds from valid one", func(t *testing.T) {
		s := newPullSecretScheme()
		validSecret := makeDockerConfigSecret(OpenshiftConfigNamespace, ClusterPullSecretName, map[string]string{
			"registry.redhat.io": "dmFsaWQ=",
		})
		invalidSecret := &corev1.Secret{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: AdditionalPullSecretName, Namespace: OpenshiftConfigNamespace},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte("{bad json}")},
		}
		fakeClient := dfake.NewSimpleDynamicClient(s, validSecret, invalidSecret)

		creds, err := ResolveRegistryCredentials(context.Background(), fakeClient, "registry.redhat.io", logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds == nil {
			t.Fatal("expected credentials from valid pull-secret, got nil")
		}
	})
}

// --- dockerConfigEqual tests ---

func TestDockerConfigEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []byte
		expected bool
	}{
		{
			name:     "identical bytes",
			a:        []byte(`{"auths":{"r.io":{"auth":"x"}}}`),
			b:        []byte(`{"auths":{"r.io":{"auth":"x"}}}`),
			expected: true,
		},
		{
			name:     "different whitespace",
			a:        []byte(`{"auths":{"r.io":{"auth":"x"}}}`),
			b:        []byte(`{ "auths" : { "r.io" : { "auth" : "x" } } }`),
			expected: true,
		},
		{
			name:     "different key order in auth entry",
			a:        []byte(`{"auths":{"r.io":{"auth":"x","email":"a"}}}`),
			b:        []byte(`{"auths":{"r.io":{"email":"a","auth":"x"}}}`),
			expected: true,
		},
		{
			name:     "different auth value",
			a:        []byte(`{"auths":{"r.io":{"auth":"x"}}}`),
			b:        []byte(`{"auths":{"r.io":{"auth":"y"}}}`),
			expected: false,
		},
		{
			name:     "different registry key",
			a:        []byte(`{"auths":{"r.io":{"auth":"x"}}}`),
			b:        []byte(`{"auths":{"s.io":{"auth":"x"}}}`),
			expected: false,
		},
		{
			name:     "invalid JSON in a",
			a:        []byte(`not json`),
			b:        []byte(`{"auths":{}}`),
			expected: false,
		},
		{
			name:     "invalid JSON in b",
			a:        []byte(`{"auths":{}}`),
			b:        []byte(`not json`),
			expected: false,
		},
		{
			name:     "both invalid JSON but identical bytes",
			a:        []byte(`nope`),
			b:        []byte(`nope`),
			expected: true,
		},
		{
			name:     "both invalid JSON and different bytes",
			a:        []byte(`nope`),
			b:        []byte(`nah`),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dockerConfigEqual(tt.a, tt.b); got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

// --- buildSecretUnstructured tests ---

func TestBuildSecretUnstructured(t *testing.T) {
	data := []byte(`{"auths":{"mirror.local":{"auth":"dGVzdA=="}}}`)
	obj := buildSecretUnstructured("test-ns", "test-secret", data)

	if obj.GetName() != "test-secret" {
		t.Errorf("expected name test-secret, got %s", obj.GetName())
	}
	if obj.GetNamespace() != "test-ns" {
		t.Errorf("expected namespace test-ns, got %s", obj.GetNamespace())
	}
	labels := obj.GetLabels()
	if labels[ManagedLabelKey] != ManagedLabelValue {
		t.Error("expected managed label")
	}
	if obj.GetKind() != "Secret" {
		t.Errorf("expected kind Secret, got %s", obj.GetKind())
	}
}

// --- readAndMerge FromUnstructured error path ---

func TestResolveRegistryCredentials_UnparseableSecret(t *testing.T) {
	s := newPullSecretScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s)

	// Return an unstructured object that cannot be converted to corev1.Secret
	fakeClient.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		getAction := action.(k8stesting.GetAction)
		if getAction.GetName() == ClusterPullSecretName {
			return true, &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata":   map[string]interface{}{"name": ClusterPullSecretName, "namespace": OpenshiftConfigNamespace},
					"data":       "not-a-map",
				},
			}, nil
		}
		return false, nil, nil
	})

	creds, err := ResolveRegistryCredentials(context.Background(), fakeClient, "registry.redhat.io", logr.Discard())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds != nil {
		t.Fatalf("expected nil when secret is unparseable, got %s", string(creds))
	}
}

// --- EnsureWasmPluginPullSecret FromUnstructured error on existing secret ---

func TestEnsureWasmPluginPullSecret_UnparseableExistingSecret(t *testing.T) {
	s := newPullSecretScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s)

	// Return an unstructured object with the managed label but corrupt data field
	fakeClient.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]interface{}{
					"name":      "wasm-plugin-pull-secret",
					"namespace": "test-ns",
					"labels":    map[string]interface{}{ManagedLabelKey: ManagedLabelValue},
				},
				"data": "not-a-map",
			},
		}, nil
	})

	sampleCreds := []byte(`{"auths":{"mirror.local:8443":{"auth":"bWlycm9y"}}}`)
	_, err := EnsureWasmPluginPullSecret(context.Background(), fakeClient, "test-ns", "wasm-plugin-pull-secret", sampleCreds, logr.Discard())
	if err == nil {
		t.Fatal("expected error when existing secret cannot be parsed, got nil")
	}
}
