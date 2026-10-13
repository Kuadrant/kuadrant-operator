//go:build unit

package openshift

import (
	"testing"

	"github.com/kuadrant/kuadrant-operator/internal/openshift/consoleplugin"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetConsolePluginImageForVersion(t *testing.T) {
	tests := []struct {
		name              string
		version           string
		latestEnvVar      string
		sdk1EnvVar        string
		pf5EnvVar         string
		expectedImage     string
		expectedRuntime   consoleplugin.Runtime
		expectedErrSubstr string
	}{
		{
			name:            "OpenShift 4.16 uses PF5 env var",
			version:         "4.16.0",
			latestEnvVar:    "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:      "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:       "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage:   "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedRuntime: consoleplugin.RuntimeNginx,
		},
		{
			name:            "OpenShift 4.19 uses PF5 env var",
			version:         "4.19.0",
			latestEnvVar:    "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:      "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:       "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage:   "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedRuntime: consoleplugin.RuntimeNginx,
		},
		{
			name:            "OpenShift 4.20 uses SDK1 env var",
			version:         "4.20.0",
			latestEnvVar:    "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:      "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:       "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage:   "quay.io/kuadrant/console-plugin:v0.6.0",
			expectedRuntime: consoleplugin.RuntimeNginx,
		},
		{
			name:            "OpenShift 4.21 uses SDK1 env var",
			version:         "4.21.0",
			latestEnvVar:    "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:      "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:       "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage:   "quay.io/kuadrant/console-plugin:v0.6.0",
			expectedRuntime: consoleplugin.RuntimeNginx,
		},
		{
			name:            "OpenShift 4.22 uses LATEST env var",
			version:         "4.22.0",
			latestEnvVar:    "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:      "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:       "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage:   "quay.io/kuadrant/console-plugin:latest",
			expectedRuntime: consoleplugin.RuntimeGo,
		},
		{
			name:            "OpenShift 5.0 uses LATEST env var",
			version:         "5.0.0",
			latestEnvVar:    "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:      "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:       "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage:   "quay.io/kuadrant/console-plugin:latest",
			expectedRuntime: consoleplugin.RuntimeGo,
		},
		{
			name:            "OpenShift 4.20.0-rc.1 pre-release uses SDK1 env var",
			version:         "4.20.0-rc.1",
			latestEnvVar:    "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:      "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:       "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage:   "quay.io/kuadrant/console-plugin:v0.6.0",
			expectedRuntime: consoleplugin.RuntimeNginx,
		},
		{
			name:            "OpenShift 4.22.0-rc.1 pre-release uses LATEST env var",
			version:         "4.22.0-rc.1",
			latestEnvVar:    "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:      "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:       "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage:   "quay.io/kuadrant/console-plugin:latest",
			expectedRuntime: consoleplugin.RuntimeGo,
		},
		{
			name:            "OpenShift 4.20.0-alpha.1 pre-release uses SDK1 env var",
			version:         "4.20.0-alpha.1",
			latestEnvVar:    "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:      "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:       "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage:   "quay.io/kuadrant/console-plugin:v0.6.0",
			expectedRuntime: consoleplugin.RuntimeNginx,
		},
		{
			name:            "OpenShift 4.19.0-rc.1 pre-release uses PF5 env var",
			version:         "4.19.0-rc.1",
			latestEnvVar:    "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:      "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:       "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedImage:   "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedRuntime: consoleplugin.RuntimeNginx,
		},
		{
			name:              "Empty version returns error",
			version:           "",
			latestEnvVar:      "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:        "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:         "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedErrSubstr: "OpenShift version is empty",
		},
		{
			name:              "Invalid version returns error",
			version:           "invalid",
			latestEnvVar:      "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:        "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:         "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedErrSubstr: "failed to parse OpenShift version",
		},
		{
			name:              "Missing PF5 env var for old version returns error",
			version:           "4.18.0",
			latestEnvVar:      "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:        "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:         "",
			expectedErrSubstr: "environment variable RELATED_IMAGE_CONSOLE_PLUGIN_PF5 is not set",
		},
		{
			name:              "Missing SDK1 env var for 4.20-4.21 returns error",
			version:           "4.20.0",
			latestEnvVar:      "quay.io/kuadrant/console-plugin:latest",
			sdk1EnvVar:        "",
			pf5EnvVar:         "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedErrSubstr: "environment variable RELATED_IMAGE_CONSOLE_PLUGIN_SDK1 is not set",
		},
		{
			name:              "Missing LATEST env var for new version returns error",
			version:           "4.22.0",
			latestEnvVar:      "",
			sdk1EnvVar:        "quay.io/kuadrant/console-plugin:v0.6.0",
			pf5EnvVar:         "quay.io/kuadrant/console-plugin:v0.1.5",
			expectedErrSubstr: "environment variable RELATED_IMAGE_CONSOLE_PLUGIN_LATEST is not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(RelatedImageConsolePluginLatestEnvVar, tt.latestEnvVar)
			t.Setenv(RelatedImageConsolePluginSDK1EnvVar, tt.sdk1EnvVar)
			t.Setenv(RelatedImageConsolePluginPF5EnvVar, tt.pf5EnvVar)

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

			if image.Runtime != tt.expectedRuntime {
				t.Errorf("expected runtime %q, got %q", tt.expectedRuntime, image.Runtime)
			}
			if image.URL != tt.expectedImage {
				t.Errorf("expected image %q, got %q", tt.expectedImage, image.URL)
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

func TestConsolePluginImageOverrideRuntime(t *testing.T) {
	for _, tc := range []struct {
		name, image, runtime string
		want                 consoleplugin.Runtime
		wantError            bool
	}{
		{name: "default preserves Go development images", image: "localhost/kuadrant/console-plugin:dev", want: consoleplugin.RuntimeGo},
		{name: "explicit Go", image: "quay.io/kuadrant/console-plugin:v0.7.0", runtime: "go", want: consoleplugin.RuntimeGo},
		{name: "explicit nginx", image: "quay.io/kuadrant/console-plugin:v0.6.0", runtime: "nginx", want: consoleplugin.RuntimeNginx},
		{name: "invalid runtime", image: "localhost/kuadrant/console-plugin:dev", runtime: "invalid", wantError: true},
		{name: "removed image allows cleanup", runtime: "nginx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			image, err := GetConsolePluginImageOverride(tc.image, tc.runtime)
			if tc.wantError {
				if err == nil {
					t.Fatal("invalid runtime must be rejected")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if image.URL != tc.image || image.Runtime != tc.want {
				t.Fatalf("unexpected image configuration: %+v", image)
			}
		})
	}
}

func TestConsolePluginWaitsForCompletedOpenShiftUpdate(t *testing.T) {
	t.Setenv(RelatedImageConsolePluginSDK1EnvVar, "quay.io/kuadrant/console-plugin:v0.6.0")
	t.Setenv(RelatedImageConsolePluginLatestEnvVar, "quay.io/kuadrant/console-plugin:v0.7.0")
	cv := &configv1.ClusterVersion{Status: configv1.ClusterVersionStatus{
		Desired: configv1.Release{Version: "4.22.0"},
		History: []configv1.UpdateHistory{
			{Version: "4.22.0", State: configv1.PartialUpdate},
			{Version: "4.21.0", State: configv1.CompletedUpdate},
		},
	}}
	image, err := GetConsolePluginImageForVersion(cv)
	if err != nil {
		t.Fatal(err)
	}
	if image.URL != "quay.io/kuadrant/console-plugin:v0.6.0" || image.Runtime != consoleplugin.RuntimeNginx {
		t.Fatalf("must keep the legacy plugin until the OpenShift update completes, got %+v", image)
	}
	cv.Status.History[0].State = configv1.CompletedUpdate
	image, err = GetConsolePluginImageForVersion(cv)
	if err != nil {
		t.Fatal(err)
	}
	if image.URL != "quay.io/kuadrant/console-plugin:v0.7.0" || image.Runtime != consoleplugin.RuntimeGo {
		t.Fatalf("completed OpenShift update must select the Go plugin, got %+v", image)
	}
}
