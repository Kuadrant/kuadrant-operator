//go:build unit

package openshift

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetConsolePluginImageForVersion(t *testing.T) {
	tests := []struct {
		name              string
		version           string
		latestEnvVar      string
		pf5EnvVar         string
		expectedImage     string
		expectedErrSubstr string
	}{
		{
			name:          "OpenShift 4.16 uses PF5 env var",
			version:       "4.16.0",
			latestEnvVar:  "quay.io/kuadrant/console-plugin:latest",
			pf5EnvVar:     "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage: "quay.io/kuadrant/console-plugin:v0.1.5",
		},
		{
			name:          "OpenShift 4.19 uses PF5 env var",
			version:       "4.19.0",
			latestEnvVar:  "quay.io/kuadrant/console-plugin:latest",
			pf5EnvVar:     "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage: "quay.io/kuadrant/console-plugin:v0.1.5",
		},
		{
			name:          "OpenShift 4.20 uses LATEST env var",
			version:       "4.20.0",
			latestEnvVar:  "quay.io/kuadrant/console-plugin:latest",
			pf5EnvVar:     "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage: "quay.io/kuadrant/console-plugin:latest",
		},
		{
			name:          "OpenShift 4.21 uses LATEST env var",
			version:       "4.21.0",
			latestEnvVar:  "quay.io/kuadrant/console-plugin:latest",
			pf5EnvVar:     "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage: "quay.io/kuadrant/console-plugin:latest",
		},
		{
			name:          "OpenShift 5.0 uses LATEST env var",
			version:       "5.0.0",
			latestEnvVar:  "quay.io/kuadrant/console-plugin:latest",
			pf5EnvVar:     "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage: "quay.io/kuadrant/console-plugin:latest",
		},
		{
			name:          "OpenShift 4.20.0-rc.1 pre-release uses LATEST env var",
			version:       "4.20.0-rc.1",
			latestEnvVar:  "quay.io/kuadrant/console-plugin:latest",
			pf5EnvVar:     "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage: "quay.io/kuadrant/console-plugin:latest",
		},
		{
			name:          "OpenShift 4.20.0-alpha.1 pre-release uses LATEST env var",
			version:       "4.20.0-alpha.1",
			latestEnvVar:  "quay.io/kuadrant/console-plugin:latest",
			pf5EnvVar:     "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage: "quay.io/kuadrant/console-plugin:latest",
		},
		{
			name:          "OpenShift 4.19.0-rc.1 pre-release uses PF5 env var",
			version:       "4.19.0-rc.1",
			latestEnvVar:  "quay.io/kuadrant/console-plugin:latest",
			pf5EnvVar:     "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage: "quay.io/kuadrant/console-plugin:v0.1.5",
		},
		{
			name:              "Empty version returns error",
			version:           "",
			latestEnvVar:      "quay.io/kuadrant/console-plugin:latest",
			pf5EnvVar:         "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedErrSubstr: "OpenShift version is empty",
		},
		{
			name:              "Invalid version returns error",
			version:           "invalid",
			latestEnvVar:      "quay.io/kuadrant/console-plugin:latest",
			pf5EnvVar:         "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedErrSubstr: "failed to parse OpenShift version",
		},
		{
			name:              "Missing PF5 env var for old version returns error",
			version:           "4.18.0",
			latestEnvVar:      "quay.io/kuadrant/console-plugin:latest",
			pf5EnvVar:         "",
			expectedErrSubstr: "environment variable RELATED_IMAGE_CONSOLE_PLUGIN_PF5 is not set",
		},
		{
			name:              "Missing LATEST env var for new version returns error",
			version:           "4.20.0",
			latestEnvVar:      "",
			pf5EnvVar:         "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedErrSubstr: "environment variable RELATED_IMAGE_CONSOLE_PLUGIN_LATEST is not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			if tt.latestEnvVar != "" {
				t.Setenv(RelatedImageConsolePluginLatestEnvVar, tt.latestEnvVar)
			}
			if tt.pf5EnvVar != "" {
				t.Setenv(RelatedImageConsolePluginPF5EnvVar, tt.pf5EnvVar)
			}

			clusterVersion := &configv1.ClusterVersion{
				ObjectMeta: metav1.ObjectMeta{
					Name: "version",
				},
				Status: configv1.ClusterVersionStatus{
					Desired: configv1.Release{
						Version: tt.version,
					},
				},
			}

			image, err := GetConsolePluginImageForVersion(clusterVersion)

			if tt.expectedErrSubstr != "" {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.expectedErrSubstr)
					return
				}
				if !contains(err.Error(), tt.expectedErrSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.expectedErrSubstr, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if image != tt.expectedImage {
				t.Errorf("expected image %q, got %q", tt.expectedImage, image)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
