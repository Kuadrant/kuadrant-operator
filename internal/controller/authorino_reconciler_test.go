package controllers

import (
	"testing"

	authorinoopapi "github.com/kuadrant/authorino-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
)

func TestEnableAuthorinoLoggingFields(t *testing.T) {
	authorino := &unstructured.Unstructured{Object: map[string]any{}}

	if err := enableAuthorinoLoggingFields(authorino); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	enabled, found, err := unstructured.NestedBool(authorino.Object, "spec", "enableLoggingFields")
	if err != nil {
		t.Fatalf("unexpected error reading enableLoggingFields: %v", err)
	}
	if !found || !enabled {
		t.Fatalf("expected spec.enableLoggingFields to be true, got found=%v value=%v", found, enabled)
	}
	if _, found, err := unstructured.NestedInt64(authorino.Object, "spec", "loggingFieldsMaxValueBytes"); err != nil || found {
		t.Fatalf("expected loggingFieldsMaxValueBytes to remain unset, got found=%v err=%v", found, err)
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
