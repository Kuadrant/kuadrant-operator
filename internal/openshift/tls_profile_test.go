package openshift

import (
	"os"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestResolveTLSProfile(t *testing.T) {
	t.Run("nil profile defaults to Intermediate", func(t *testing.T) {
		minVer, ciphers := ResolveTLSProfile(nil)
		if minVer != "1.2" {
			t.Errorf("expected VersionTLS12, got %s", minVer)
		}
		if len(ciphers) == 0 {
			t.Fatal("expected ciphers, got none")
		}
		for _, c := range ciphers {
			if c == "" {
				t.Error("empty cipher in result")
			}
		}
	})

	t.Run("empty type defaults to Intermediate", func(t *testing.T) {
		minVer, _ := ResolveTLSProfile(&configv1.TLSSecurityProfile{Type: ""})
		if minVer != "1.2" {
			t.Errorf("expected VersionTLS12, got %s", minVer)
		}
	})

	t.Run("Old profile", func(t *testing.T) {
		minVer, ciphers := ResolveTLSProfile(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileOldType})
		if minVer != "1.0" {
			t.Errorf("expected VersionTLS10, got %s", minVer)
		}
		if len(ciphers) < 10 {
			t.Errorf("expected many ciphers for Old profile, got %d", len(ciphers))
		}
	})

	t.Run("Modern profile", func(t *testing.T) {
		minVer, ciphers := ResolveTLSProfile(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType})
		if minVer != "1.3" {
			t.Errorf("expected VersionTLS13, got %s", minVer)
		}
		// TLS 1.3 ciphers have the same name in OpenSSL and IANA
		if len(ciphers) != 3 {
			t.Errorf("expected 3 ciphers for Modern profile, got %d", len(ciphers))
		}
	})

	t.Run("Custom profile", func(t *testing.T) {
		profile := &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{
				TLSProfileSpec: configv1.TLSProfileSpec{
					Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256", "ECDHE-RSA-CHACHA20-POLY1305"},
					MinTLSVersion: configv1.VersionTLS12,
				},
			},
		}
		minVer, ciphers := ResolveTLSProfile(profile)
		if minVer != "1.2" {
			t.Errorf("expected VersionTLS12, got %s", minVer)
		}
		if len(ciphers) != 2 {
			t.Fatalf("expected 2 ciphers, got %d", len(ciphers))
		}
		if ciphers[0] != "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256" {
			t.Errorf("unexpected cipher: %s", ciphers[0])
		}
		if ciphers[1] != "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256" {
			t.Errorf("unexpected cipher: %s", ciphers[1])
		}
	})

	t.Run("Custom profile with unsupported DHE cipher skips it", func(t *testing.T) {
		profile := &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{
				TLSProfileSpec: configv1.TLSProfileSpec{
					Ciphers:       []string{"DHE-RSA-AES128-GCM-SHA256", "ECDHE-RSA-AES128-GCM-SHA256"},
					MinTLSVersion: configv1.VersionTLS12,
				},
			},
		}
		_, ciphers := ResolveTLSProfile(profile)
		if len(ciphers) != 1 {
			t.Fatalf("expected 1 cipher (DHE skipped), got %d: %v", len(ciphers), ciphers)
		}
	})

	t.Run("Custom type with nil Custom falls back to Intermediate", func(t *testing.T) {
		profile := &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
		}
		minVer, ciphers := ResolveTLSProfile(profile)
		if minVer != "1.2" {
			t.Errorf("expected VersionTLS12 fallback, got %s", minVer)
		}
		if len(ciphers) != 9 {
			t.Errorf("expected 9 ciphers (Intermediate fallback), got %d", len(ciphers))
		}
	})

	t.Run("Intermediate profile excludes DHE ciphers", func(t *testing.T) {
		_, ciphers := ResolveTLSProfile(nil)
		for _, c := range ciphers {
			if strings.HasPrefix(c, "TLS_DHE_") {
				t.Errorf("DHE cipher should not appear in translated output: %s", c)
			}
		}
		if len(ciphers) != 9 {
			t.Errorf("expected 9 ciphers for Intermediate profile (11 minus 2 DHE), got %d: %v", len(ciphers), ciphers)
		}
	})
}

func TestAPIServerCRName(t *testing.T) {
	t.Run("defaults to cluster", func(t *testing.T) {
		os.Unsetenv("APISERVER_CR_NAME")
		if name := APIServerCRName(); name != "cluster" {
			t.Errorf("expected 'cluster', got %q", name)
		}
	})

	t.Run("respects env override", func(t *testing.T) {
		t.Setenv("APISERVER_CR_NAME", "custom-server")
		if name := APIServerCRName(); name != "custom-server" {
			t.Errorf("expected 'custom-server', got %q", name)
		}
	})
}
