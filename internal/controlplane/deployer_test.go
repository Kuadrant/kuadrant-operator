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
		wantRelatedImageEnvVars []string
	}{
		{
			name:       "dns-operator is registered",
			wantName:   "dns-operator",
			wantChart:  chartsBasePath + "/dns-operator",
			wantEnvVar: "RELATED_IMAGE_DNS_OPERATOR",
		},
		{
			name:                    "mcp-gateway is registered",
			wantName:                "mcp-gateway",
			wantChart:               chartsBasePath + "/mcp-gateway",
			wantChartValueOverrides: 2,
		},
		{
			name:                    "authorino-operator is registered",
			wantName:                "authorino-operator",
			wantChart:               chartsBasePath + "/authorino-operator",
			wantEnvVar:              "RELATED_IMAGE_AUTHORINO_OPERATOR",
			wantRelatedImageEnvVars: []string{"RELATED_IMAGE_AUTHORINO"},
		},
		{
			name:                    "limitador-operator is registered",
			wantName:                "limitador-operator",
			wantChart:               chartsBasePath + "/limitador-operator",
			wantEnvVar:              "RELATED_IMAGE_LIMITADOR_OPERATOR",
			wantRelatedImageEnvVars: []string{"RELATED_IMAGE_LIMITADOR"},
		},
		{
			name:                    "developer-portal-controller is registered",
			wantName:                "developer-portal-controller",
			wantChart:               chartsBasePath + "/developer-portal-controller",
			wantChartValueOverrides: 1,
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
			if len(tt.wantRelatedImageEnvVars) > 0 {
				if len(c.RelatedImageEnvVars) != len(tt.wantRelatedImageEnvVars) {
					t.Fatalf("RelatedImageEnvVars = %v, want %v", c.RelatedImageEnvVars, tt.wantRelatedImageEnvVars)
				}
				for i, name := range tt.wantRelatedImageEnvVars {
					if c.RelatedImageEnvVars[i] != name {
						t.Errorf("RelatedImageEnvVars[%d] = %q, want %q", i, c.RelatedImageEnvVars[i], name)
					}
				}
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
	return findComponentChartForDeployer(t, "dns-operator")
}

func findComponentChartForDeployer(t *testing.T, name string) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "component-charts", name),
		filepath.Join("component-charts", name),
	}
	for _, p := range candidates {
		if _, err := os.Stat(filepath.Join(p, "Chart.yaml")); err == nil {
			return p
		}
	}
	t.Skipf("%s chart not found, skipping deployer tests against real chart", name)
	return ""
}

// TestRenderDeveloperPortalComponent renders the vendored
// developer-portal-controller chart exactly as the deployer does at runtime:
// registered component values plus the RELATED_IMAGE_DEVELOPERPORTAL override.
// The chart reads its ClusterRole rules via .Files.Get, so this also proves
// the Helm SDK loader path used by the operator picks those files up.
func TestRenderDeveloperPortalComponent(t *testing.T) {
	registry := &Deployer{components: allComponents()}
	component, ok := registry.ComponentByName("developer-portal-controller")
	if !ok {
		t.Fatal("developer-portal-controller not registered")
	}
	component.ChartPath = findComponentChartForDeployer(t, "developer-portal-controller")

	d := &Deployer{namespace: "kuadrant-system"}

	tests := []struct {
		name  string
		image string
	}{
		{name: "tag reference", image: "quay.io/kuadrant/developer-portal-controller:v1.2.3"},
		{name: "digest reference", image: "quay.io/kuadrant/developer-portal-controller@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("RELATED_IMAGE_DEVELOPERPORTAL", tt.image)

			rendered, err := d.renderComponent(component)
			if err != nil {
				t.Fatalf("renderComponent() error = %v", err)
			}

			wantCRDs := map[string]bool{}
			for _, name := range component.CRDNames {
				wantCRDs[name] = true
			}
			for _, crd := range rendered.CRDs {
				delete(wantCRDs, crd.GetName())
			}
			if len(rendered.CRDs) != len(component.CRDNames) || len(wantCRDs) != 0 {
				t.Errorf("rendered CRDs = %v, want exactly %v", CRDNames(rendered.CRDs), component.CRDNames)
			}

			var deployment, clusterRole *unstructured.Unstructured
			for _, obj := range rendered.Resources {
				switch {
				case obj.GetKind() == "Deployment" && obj.GetName() == component.DeploymentName:
					deployment = obj
				case obj.GetKind() == "ClusterRole" && obj.GetName() == "developer-portal-controller-manager-role":
					clusterRole = obj
				}
			}
			if deployment == nil {
				t.Fatalf("Deployment %q not rendered", component.DeploymentName)
			}
			if clusterRole == nil {
				t.Fatal("ClusterRole developer-portal-controller-manager-role not rendered")
			}

			containers, _, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
			if err != nil || len(containers) != 1 {
				t.Fatalf("containers = %v (err %v), want exactly one", containers, err)
			}
			container, _ := containers[0].(map[string]interface{})
			if got := container["image"]; got != tt.image {
				t.Errorf("container image = %v, want %q", got, tt.image)
			}

			rules, found, err := unstructured.NestedSlice(clusterRole.Object, "rules")
			if err != nil || !found || len(rules) == 0 {
				t.Errorf("ClusterRole rules = %v (found %v, err %v), want rules from rbac/role.yaml", rules, found, err)
			}

			images := extractDeploymentImages(rendered.Resources)
			if len(images) != 1 || images[0].Container != "manager" || images[0].Image != tt.image {
				t.Errorf("extractDeploymentImages() = %v, want manager=%s", images, tt.image)
			}
		})
	}
}
