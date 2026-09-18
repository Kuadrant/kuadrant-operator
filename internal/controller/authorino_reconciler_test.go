package controllers

import (
	"os"
	"testing"

	authorinoopapi "github.com/kuadrant/authorino-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
)

func TestConfigureAuthorinoLoggingFields(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   *string
		enabled bool
	}{
		{name: "unset"},
		{name: "empty", value: ptr.To("")},
		{name: "false", value: ptr.To("false")},
		{name: "true", value: ptr.To("true"), enabled: true},
		{name: "invalid", value: ptr.To("invalid")},
		{name: "numeric", value: ptr.To("1")},
		{name: "uppercase", value: ptr.To("TRUE")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AUTHORINO_ENABLE_LOGGING_FIELDS", "")
			if tc.value == nil {
				if err := os.Unsetenv("AUTHORINO_ENABLE_LOGGING_FIELDS"); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("AUTHORINO_ENABLE_LOGGING_FIELDS", *tc.value)
			}
			for _, initial := range []map[string]any{
				{},
				{"spec": map[string]any{"enableLoggingFields": true, "loggingFieldsMaxValueBytes": int64(4096)}},
				{"spec": map[string]any{"enableLoggingFields": false}},
			} {
				authorino := &unstructured.Unstructured{Object: initial}
				for range 2 {
					if err := configureAuthorinoLoggingFields(authorino); err != nil {
						t.Fatal(err)
					}
					enabled, found, err := unstructured.NestedBool(authorino.Object, "spec", "enableLoggingFields")
					if err != nil || !found || enabled != tc.enabled {
						t.Fatalf("expected enableLoggingFields=%v, got %v (found=%v, err=%v)", tc.enabled, enabled, found, err)
					}
				}
				if limit, found, err := unstructured.NestedInt64(authorino.Object, "spec", "loggingFieldsMaxValueBytes"); err != nil || (found && limit != 4096) {
					t.Fatalf("unexpected value limit: %d (found=%v, err=%v)", limit, found, err)
				}
			}
		})
	}
}

func TestBuildTLSPatch(t *testing.T) {
	ciphers := []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"}

	t.Run("TLS disabled (nil Enabled) returns empty Tls", func(t *testing.T) {
		result := buildTLSPatch(authorinoopapi.Tls{}, "1.2", ciphers)
		if result.MinVersion != "" {
			t.Errorf("expected empty MinVersion, got %q", result.MinVersion)
		}
		if result.CipherSuites != nil {
			t.Errorf("expected nil CipherSuites, got %v", result.CipherSuites)
		}
		if result.Enabled != nil {
			t.Errorf("expected nil Enabled, got %v", *result.Enabled)
		}
	})

	t.Run("TLS disabled (Enabled=false) returns empty Tls", func(t *testing.T) {
		existing := authorinoopapi.Tls{Enabled: ptr.To(false)}
		result := buildTLSPatch(existing, "1.2", ciphers)
		if result.MinVersion != "" {
			t.Errorf("expected empty MinVersion, got %q", result.MinVersion)
		}
		if result.CipherSuites != nil {
			t.Errorf("expected nil CipherSuites, got %v", result.CipherSuites)
		}
	})

	t.Run("TLS enabled returns only MinVersion and CipherSuites", func(t *testing.T) {
		existing := authorinoopapi.Tls{
			Enabled:    ptr.To(true),
			CertSecret: &corev1.LocalObjectReference{Name: "my-cert"},
		}
		result := buildTLSPatch(existing, "1.2", ciphers)
		if result.MinVersion != "1.2" {
			t.Errorf("expected MinVersion 1.2, got %q", result.MinVersion)
		}
		if len(result.CipherSuites) != len(ciphers) {
			t.Fatalf("expected %d ciphers, got %d", len(ciphers), len(result.CipherSuites))
		}
		for i, c := range result.CipherSuites {
			if c != ciphers[i] {
				t.Errorf("cipher[%d]: expected %q, got %q", i, ciphers[i], c)
			}
		}
		if result.Enabled != nil {
			t.Errorf("Enabled must not be set in SSA patch, got %v", *result.Enabled)
		}
		if result.CertSecret != nil {
			t.Errorf("CertSecret must not be set in SSA patch, got %v", result.CertSecret)
		}
	})

	t.Run("TLS enabled with Modern profile", func(t *testing.T) {
		existing := authorinoopapi.Tls{Enabled: ptr.To(true)}
		modernCiphers := []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384", "TLS_CHACHA20_POLY1305_SHA256"}
		result := buildTLSPatch(existing, "1.3", modernCiphers)
		if result.MinVersion != "1.3" {
			t.Errorf("expected MinVersion 1.3, got %q", result.MinVersion)
		}
		if len(result.CipherSuites) != 3 {
			t.Errorf("expected 3 ciphers, got %d", len(result.CipherSuites))
		}
	})
}
