//go:build unit

package controlplane

import (
	"testing"
)

func TestImageValue_Apply(t *testing.T) {
	tests := []struct {
		name     string
		mapping  *ImageValue
		envValue string
		wantKey  string
		wantVal  string
		wantSet  bool
	}{
		{
			name:     "sets value from env var",
			mapping:  &ImageValue{EnvVar: "TEST_IMG", ValueKey: "image", Description: "controller"},
			envValue: "quay.io/test:v1",
			wantKey:  "image",
			wantVal:  "quay.io/test:v1",
			wantSet:  true,
		},
		{
			name:    "empty env var sets nothing",
			mapping: &ImageValue{EnvVar: "TEST_IMG", ValueKey: "image"},
			wantSet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.mapping.EnvVar, tt.envValue)
			}
			values := make(map[string]any)
			tt.mapping.Apply(values)

			val, ok := values[tt.wantKey]
			if tt.wantSet {
				if !ok {
					t.Fatalf("expected key %q in values", tt.wantKey)
				}
				if val != tt.wantVal {
					t.Errorf("values[%q] = %v, want %v", tt.wantKey, val, tt.wantVal)
				}
			} else if ok {
				t.Errorf("expected key %q not to be set, got %v", tt.wantKey, val)
			}
		})
	}
}

func TestImageValue_ImageInfo(t *testing.T) {
	t.Setenv("TEST_IMG", "quay.io/test:v1")
	m := &ImageValue{EnvVar: "TEST_IMG", Description: "controller"}

	desc, image := m.ImageInfo()
	if desc != "controller" {
		t.Errorf("description = %q, want %q", desc, "controller")
	}
	if image != "quay.io/test:v1" {
		t.Errorf("image = %q, want %q", image, "quay.io/test:v1")
	}
}

func TestImageSplitValue_Apply(t *testing.T) {
	tests := []struct {
		name     string
		mapping  *ImageSplitValue
		envValue string
		wantKey  string
		wantRepo string
		wantTag  string
		wantSet  bool
	}{
		{
			name: "splits repo:tag with defaults",
			mapping: &ImageSplitValue{
				ImageValue: ImageValue{EnvVar: "TEST_IMG", ValueKey: "imageController"},
			},
			envValue: "ghcr.io/kuadrant/mcp-controller:v0.8.0",
			wantKey:  "imageController",
			wantRepo: "ghcr.io/kuadrant/mcp-controller",
			wantTag:  "v0.8.0",
			wantSet:  true,
		},
		{
			name: "splits repo without tag",
			mapping: &ImageSplitValue{
				ImageValue: ImageValue{EnvVar: "TEST_IMG", ValueKey: "imageController"},
			},
			envValue: "ghcr.io/kuadrant/mcp-controller",
			wantKey:  "imageController",
			wantRepo: "ghcr.io/kuadrant/mcp-controller",
			wantTag:  "",
			wantSet:  true,
		},
		{
			name: "splits digest reference",
			mapping: &ImageSplitValue{
				ImageValue: ImageValue{EnvVar: "TEST_IMG", ValueKey: "imageController"},
			},
			envValue: "ghcr.io/kuadrant/mcp-controller@sha256:abc123",
			wantKey:  "imageController",
			wantRepo: "ghcr.io/kuadrant/mcp-controller",
			wantTag:  "@sha256:abc123",
			wantSet:  true,
		},
		{
			name: "handles localhost:port registry",
			mapping: &ImageSplitValue{
				ImageValue: ImageValue{EnvVar: "TEST_IMG", ValueKey: "imageController"},
			},
			envValue: "localhost:5000/myimage:dev",
			wantKey:  "imageController",
			wantRepo: "localhost:5000/myimage",
			wantTag:  "dev",
			wantSet:  true,
		},
		{
			name: "custom field names",
			mapping: &ImageSplitValue{
				ImageValue: ImageValue{EnvVar: "TEST_IMG", ValueKey: "ctrl"},
				RepoField:  "image",
				TagField:   "version",
			},
			envValue: "quay.io/test:v2",
			wantKey:  "ctrl",
			wantRepo: "quay.io/test",
			wantTag:  "v2",
			wantSet:  true,
		},
		{
			name: "empty env var sets nothing",
			mapping: &ImageSplitValue{
				ImageValue: ImageValue{EnvVar: "TEST_IMG", ValueKey: "imageController"},
			},
			wantSet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.mapping.EnvVar, tt.envValue)
			}
			values := make(map[string]any)
			tt.mapping.Apply(values)

			val, ok := values[tt.wantKey]
			if !tt.wantSet {
				if ok {
					t.Errorf("expected key %q not to be set", tt.wantKey)
				}
				return
			}
			if !ok {
				t.Fatalf("expected key %q in values", tt.wantKey)
			}

			m, ok := val.(map[string]any)
			if !ok {
				t.Fatalf("expected map at key %q, got %T", tt.wantKey, val)
			}

			repoField := tt.mapping.RepoField
			if repoField == "" {
				repoField = "repository"
			}
			tagField := tt.mapping.TagField
			if tagField == "" {
				tagField = "tag"
			}

			if m[repoField] != tt.wantRepo {
				t.Errorf("%s = %v, want %v", repoField, m[repoField], tt.wantRepo)
			}
			if tt.wantTag != "" {
				if m[tagField] != tt.wantTag {
					t.Errorf("%s = %v, want %v", tagField, m[tagField], tt.wantTag)
				}
			}
		})
	}
}

func TestImageSplitValue_InheritsImageInfo(t *testing.T) {
	t.Setenv("TEST_IMG", "ghcr.io/kuadrant/mcp-controller:v0.8.0")
	m := &ImageSplitValue{
		ImageValue: ImageValue{EnvVar: "TEST_IMG", Description: "controller"},
	}

	desc, image := m.ImageInfo()
	if desc != "controller" {
		t.Errorf("description = %q, want %q", desc, "controller")
	}
	if image != "ghcr.io/kuadrant/mcp-controller:v0.8.0" {
		t.Errorf("image = %q, want %q", image, "ghcr.io/kuadrant/mcp-controller:v0.8.0")
	}
}

func TestNestedValueKey(t *testing.T) {
	tests := []struct {
		name     string
		mapping  ChartValueOverride
		envValue string
		validate func(t *testing.T, values map[string]any)
	}{
		{
			name:     "ImageValue with dotted key",
			mapping:  &ImageValue{EnvVar: "TEST_IMG", ValueKey: "controller.image"},
			envValue: "quay.io/test:v1",
			validate: func(t *testing.T, values map[string]any) {
				ctrl, ok := values["controller"].(map[string]any)
				if !ok {
					t.Fatal("expected controller map")
				}
				if ctrl["image"] != "quay.io/test:v1" {
					t.Errorf("controller.image = %v, want quay.io/test:v1", ctrl["image"])
				}
			},
		},
		{
			name:     "ImageSplitValue with dotted key",
			mapping:  &ImageSplitValue{ImageValue: ImageValue{EnvVar: "TEST_IMG", ValueKey: "spec.controller"}},
			envValue: "quay.io/test:v1",
			validate: func(t *testing.T, values map[string]any) {
				spec, ok := values["spec"].(map[string]any)
				if !ok {
					t.Fatal("expected spec map")
				}
				ctrl, ok := spec["controller"].(map[string]any)
				if !ok {
					t.Fatal("expected spec.controller map")
				}
				if ctrl["repository"] != "quay.io/test" {
					t.Errorf("repository = %v, want quay.io/test", ctrl["repository"])
				}
				if ctrl["tag"] != "v1" {
					t.Errorf("tag = %v, want v1", ctrl["tag"])
				}
			},
		},
		{
			name:     "deeply nested key",
			mapping:  &ImageValue{EnvVar: "TEST_IMG", ValueKey: "a.b.c.image"},
			envValue: "quay.io/deep:v2",
			validate: func(t *testing.T, values map[string]any) {
				a := values["a"].(map[string]any)
				b := a["b"].(map[string]any)
				c := b["c"].(map[string]any)
				if c["image"] != "quay.io/deep:v2" {
					t.Errorf("a.b.c.image = %v, want quay.io/deep:v2", c["image"])
				}
			},
		},
		{
			name:     "merges with existing nested map",
			mapping:  &ImageValue{EnvVar: "TEST_IMG", ValueKey: "controller.image"},
			envValue: "quay.io/test:v1",
			validate: func(t *testing.T, values map[string]any) {
				// Pre-populate existing nested value
				values["controller"] = map[string]any{"replicas": 3}
				// Re-apply
				(&ImageValue{EnvVar: "TEST_IMG", ValueKey: "controller.image"}).Apply(values)

				ctrl := values["controller"].(map[string]any)
				if ctrl["replicas"] != 3 {
					t.Error("existing value should be preserved")
				}
				if ctrl["image"] != "quay.io/test:v1" {
					t.Errorf("controller.image = %v, want quay.io/test:v1", ctrl["image"])
				}
			},
		},
		{
			name:     "flat key still works",
			mapping:  &ImageValue{EnvVar: "TEST_IMG", ValueKey: "image"},
			envValue: "quay.io/flat:v1",
			validate: func(t *testing.T, values map[string]any) {
				if values["image"] != "quay.io/flat:v1" {
					t.Errorf("image = %v, want quay.io/flat:v1", values["image"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_IMG", tt.envValue)
			values := make(map[string]any)
			tt.mapping.Apply(values)
			tt.validate(t, values)
		})
	}
}

func TestImageReporterInterface(t *testing.T) {
	tests := []struct {
		name       string
		mapping    ChartValueOverride
		wantReport bool
	}{
		{
			name:       "ImageValue implements ImageReporter",
			mapping:    &ImageValue{EnvVar: "X", Description: "test"},
			wantReport: true,
		},
		{
			name:       "ImageSplitValue implements ImageReporter",
			mapping:    &ImageSplitValue{ImageValue: ImageValue{EnvVar: "X", Description: "test"}},
			wantReport: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := tt.mapping.(ImageReporter)
			if ok != tt.wantReport {
				t.Errorf("implements ImageReporter = %v, want %v", ok, tt.wantReport)
			}
		})
	}
}
