//go:build unit

package controlplane

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDefaultComponents(t *testing.T) {
	components := allComponents()

	tests := []struct {
		name                    string
		wantName                string
		wantChart               string
		wantEnvVar              string
		wantChartValueOverrides int
	}{
		{
			name:       "dns-operator is registered",
			wantName:   "dns-operator",
			wantChart:  chartsBasePath + "/dns-operator",
			wantEnvVar: "RELATED_IMAGE_DNS_OPERATOR",
		},
	}

	if len(components) != len(tests) {
		t.Fatalf("expected %d components, got %d", len(tests), len(components))
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := components[i]
			if c.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", c.Name, tt.wantName)
			}
			if c.ChartPath != tt.wantChart {
				t.Errorf("ChartPath = %q, want %q", c.ChartPath, tt.wantChart)
			}
			if tt.wantEnvVar != "" && c.ImageEnvVar != tt.wantEnvVar {
				t.Errorf("ImageEnvVar = %q, want %q", c.ImageEnvVar, tt.wantEnvVar)
			}
			if tt.wantChartValueOverrides > 0 && len(c.ChartValueOverrides) != tt.wantChartValueOverrides {
				t.Errorf("ChartValueOverrides count = %d, want %d", len(c.ChartValueOverrides), tt.wantChartValueOverrides)
			}
		})
	}
}

func TestRenderComponent(t *testing.T) {
	chartPath := findDNSOperatorChartForDeployer(t)

	d := &Deployer{
		namespace: "kuadrant-system",
	}

	component := Component{
		Name:      "dns-operator",
		ChartPath: chartPath,
	}

	tests := []struct {
		name     string
		validate func(t *testing.T)
	}{
		{
			name: "renders successfully",
			validate: func(t *testing.T) {
				t.Helper()
				rendered, err := d.renderComponent(component)
				if err != nil {
					t.Fatalf("renderComponent() error = %v", err)
				}
				if len(rendered.CRDs)+len(rendered.Resources) == 0 {
					t.Fatal("expected rendered objects, got none")
				}
			},
		},
		{
			name: "includes CRDs and non-CRDs",
			validate: func(t *testing.T) {
				t.Helper()
				rendered, err := d.renderComponent(component)
				if err != nil {
					t.Fatalf("renderComponent() error = %v", err)
				}
				if len(rendered.CRDs) == 0 {
					t.Error("expected CRDs in rendered output")
				}
				if len(rendered.Resources) == 0 {
					t.Error("expected non-CRD resources in rendered output")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.validate(t)
		})
	}
}

func TestChartVersionCachedAfterRender(t *testing.T) {
	chartPath := findDNSOperatorChartForDeployer(t)

	d := &Deployer{
		namespace:     "kuadrant-system",
		chartVersions: make(map[string]string),
	}

	component := Component{
		Name:      "dns-operator",
		ChartPath: chartPath,
	}

	tests := []struct {
		name     string
		validate func(t *testing.T)
	}{
		{
			name: "empty before render",
			validate: func(t *testing.T) {
				t.Helper()
				if v := d.ChartVersion("dns-operator"); v != "" {
					t.Errorf("expected empty chart version before render, got %q", v)
				}
			},
		},
		{
			name: "populated after render",
			validate: func(t *testing.T) {
				t.Helper()
				rendered, err := d.renderComponent(component)
				if err != nil {
					t.Fatalf("renderComponent() error = %v", err)
				}
				d.chartVersions[component.Name] = rendered.ChartVersion
				if v := d.ChartVersion("dns-operator"); v == "" {
					t.Error("expected chart version after render, got empty")
				}
			},
		},
		{
			name: "unknown component returns empty",
			validate: func(t *testing.T) {
				t.Helper()
				if v := d.ChartVersion("nonexistent"); v != "" {
					t.Errorf("expected empty for unknown component, got %q", v)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.validate(t)
		})
	}
}

func TestRenderComponent_InvalidChart(t *testing.T) {
	d := &Deployer{
		namespace: "kuadrant-system",
	}

	component := Component{
		Name:      "nonexistent",
		ChartPath: "/nonexistent/chart",
	}

	_, err := d.renderComponent(component)
	if err == nil {
		t.Fatal("expected error for invalid chart path, got nil")
	}
}

func TestDeployerImagePatching(t *testing.T) {
	chartPath := findDNSOperatorChartForDeployer(t)

	d := &Deployer{
		namespace: "kuadrant-system",
	}

	component := Component{
		Name:        "dns-operator",
		ChartPath:   chartPath,
		ImageEnvVar: "RELATED_IMAGE_DNS_OPERATOR",
	}

	tests := []struct {
		name      string
		envValue  string
		wantImage string
	}{
		{
			name:      "env var overrides image",
			envValue:  "quay.io/kuadrant/dns-operator:v1.0.0",
			wantImage: "quay.io/kuadrant/dns-operator:v1.0.0",
		},
		{
			name:      "empty env var preserves chart default",
			envValue:  "",
			wantImage: "quay.io/kuadrant/dns-operator:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv("RELATED_IMAGE_DNS_OPERATOR", tt.envValue)
			} else {
				t.Setenv("RELATED_IMAGE_DNS_OPERATOR", "")
			}

			rendered, err := d.renderComponent(component)
			if err != nil {
				t.Fatalf("renderComponent() error = %v", err)
			}

			image := os.Getenv(component.ImageEnvVar)
			if err := PatchDeploymentImage(rendered.Resources, image); err != nil {
				t.Fatalf("PatchDeploymentImage() error = %v", err)
			}

			foundDeployment := false
			for _, obj := range rendered.Resources {
				if obj.GetKind() != "Deployment" {
					continue
				}
				foundDeployment = true
				containers, _, _ := unstructured.NestedSlice(obj.Object,
					"spec", "template", "spec", "containers")
				if len(containers) == 0 {
					t.Fatal("no containers in Deployment")
				}
				container := containers[0].(map[string]interface{})
				got := container["image"].(string)
				if got != tt.wantImage {
					t.Errorf("image = %q, want %q", got, tt.wantImage)
				}
			}
			if !foundDeployment {
				t.Fatal("no Deployment found in rendered resources")
			}
		})
	}
}

func TestEffectiveValues(t *testing.T) {
	tests := []struct {
		name      string
		component Component
		envVars   map[string]string
		wantNil   bool
		validate  func(t *testing.T, values map[string]any)
	}{
		{
			name:      "no chart values or mappings",
			component: Component{Name: "test"},
			wantNil:   true,
		},
		{
			name:      "chart values only",
			component: Component{Name: "test", ChartValues: map[string]any{"key": "val"}},
			validate: func(t *testing.T, values map[string]any) {
				if values["key"] != "val" {
					t.Errorf("key = %v, want val", values["key"])
				}
			},
		},
		{
			name: "ChartValueOverrides apply env vars to values",
			component: Component{
				Name: "test",
				ChartValueOverrides: []ChartValueOverride{
					&ImageSplitValue{ImageValue: ImageValue{EnvVar: "TEST_IMG", ValueKey: "imageController"}},
				},
			},
			envVars: map[string]string{"TEST_IMG": "ghcr.io/test:v1.0"},
			validate: func(t *testing.T, values map[string]any) {
				ic, ok := values["imageController"].(map[string]any)
				if !ok {
					t.Fatal("expected imageController map in values")
				}
				if ic["repository"] != "ghcr.io/test" {
					t.Errorf("repository = %v, want ghcr.io/test", ic["repository"])
				}
				if ic["tag"] != "v1.0" {
					t.Errorf("tag = %v, want v1.0", ic["tag"])
				}
			},
		},
		{
			name: "ChartValues and ChartValueOverrides merge",
			component: Component{
				Name:        "test",
				ChartValues: map[string]any{"gateway": map[string]any{"create": false}},
				ChartValueOverrides: []ChartValueOverride{
					&ImageSplitValue{ImageValue: ImageValue{EnvVar: "TEST_IMG", ValueKey: "imageController"}},
				},
			},
			envVars: map[string]string{"TEST_IMG": "ghcr.io/test:v1.0"},
			validate: func(t *testing.T, values map[string]any) {
				if values["gateway"] == nil {
					t.Error("expected gateway in values")
				}
				if values["imageController"] == nil {
					t.Error("expected imageController in values")
				}
			},
		},
		{
			name: "empty env var does not set value",
			component: Component{
				Name: "test",
				ChartValueOverrides: []ChartValueOverride{
					&ImageSplitValue{ImageValue: ImageValue{EnvVar: "TEST_IMG", ValueKey: "imageController"}},
				},
			},
			wantNil: true,
		},
		{
			name: "ImageEnvVar does not affect effectiveValues",
			component: Component{
				Name:        "test",
				ImageEnvVar: "TEST_IMG",
			},
			envVars: map[string]string{"TEST_IMG": "override:v1"},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}
			values := tt.component.effectiveValues()
			if tt.wantNil {
				if values != nil {
					t.Errorf("expected nil values, got %v", values)
				}
				return
			}
			if tt.validate != nil {
				tt.validate(t, values)
			}
		})
	}
}

func findDNSOperatorChartForDeployer(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "component-charts", "dns-operator"),
		filepath.Join("component-charts", "dns-operator"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(filepath.Join(p, "Chart.yaml")); err == nil {
			return p
		}
	}
	t.Skip("dns-operator chart not found, skipping deployer tests against real chart")
	return ""
}
