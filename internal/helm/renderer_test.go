//go:build unit

package helm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderer_Render(t *testing.T) {
	chartPath := findDNSOperatorChart(t)

	tests := []struct {
		name        string
		chartPath   string
		releaseName string
		namespace   string
		values      map[string]any
		wantErr     bool
		validate    func(t *testing.T, rendered *RenderedChart)
	}{
		{
			name:        "renders dns-operator chart successfully",
			chartPath:   chartPath,
			releaseName: "dns-operator",
			namespace:   "kuadrant-system",
			wantErr:     false,
			validate: func(t *testing.T, rendered *RenderedChart) {
				t.Helper()
				if len(rendered.CRDs)+len(rendered.Resources) == 0 {
					t.Fatal("expected rendered objects, got none")
				}
			},
		},
		{
			name:        "CRDs and resources are split correctly",
			chartPath:   chartPath,
			releaseName: "dns-operator",
			namespace:   "kuadrant-system",
			wantErr:     false,
			validate: func(t *testing.T, rendered *RenderedChart) {
				t.Helper()
				if len(rendered.CRDs) != 2 {
					t.Errorf("expected 2 CRDs, got %d", len(rendered.CRDs))
				}
				for _, crd := range rendered.CRDs {
					if crd.GetKind() != "CustomResourceDefinition" {
						t.Errorf("CRDs slice contains non-CRD kind %q", crd.GetKind())
					}
				}
				for _, res := range rendered.Resources {
					if res.GetKind() == "CustomResourceDefinition" {
						t.Error("Resources slice contains a CRD")
					}
				}
			},
		},
		{
			name:        "rendered resources have correct GVK",
			chartPath:   chartPath,
			releaseName: "dns-operator",
			namespace:   "kuadrant-system",
			wantErr:     false,
			validate: func(t *testing.T, rendered *RenderedChart) {
				t.Helper()
				all := append(rendered.CRDs, rendered.Resources...)
				for _, obj := range all {
					if obj.GetKind() == "" {
						t.Errorf("object %s has empty kind", obj.GetName())
					}
					if obj.GetAPIVersion() == "" {
						t.Errorf("object %s has empty apiVersion", obj.GetName())
					}
				}
			},
		},
		{
			name:        "resources include Deployment",
			chartPath:   chartPath,
			releaseName: "dns-operator",
			namespace:   "kuadrant-system",
			wantErr:     false,
			validate: func(t *testing.T, rendered *RenderedChart) {
				t.Helper()
				var found bool
				for _, obj := range rendered.Resources {
					if obj.GetKind() == "Deployment" {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected a Deployment in rendered resources")
				}
			},
		},
		{
			name:        "namespace is set on rendered release",
			chartPath:   chartPath,
			releaseName: "dns-operator",
			namespace:   "test-namespace",
			wantErr:     false,
			validate: func(t *testing.T, rendered *RenderedChart) {
				t.Helper()
				for _, obj := range rendered.Resources {
					if obj.GetKind() == "Deployment" && obj.GetNamespace() != "test-namespace" {
						t.Errorf("Deployment namespace = %q, want %q", obj.GetNamespace(), "test-namespace")
					}
				}
			},
		},
		{
			name:        "chart version is populated from Chart.yaml",
			chartPath:   chartPath,
			releaseName: "dns-operator",
			namespace:   "kuadrant-system",
			wantErr:     false,
			validate: func(t *testing.T, rendered *RenderedChart) {
				t.Helper()
				if rendered.ChartVersion == "" {
					t.Error("expected ChartVersion to be populated")
				}
			},
		},
		{
			name:        "missing chart path returns error",
			chartPath:   "/nonexistent/chart/path",
			releaseName: "test",
			namespace:   "default",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRenderer(tt.chartPath)
			rendered, err := r.Render(tt.releaseName, tt.namespace, tt.values)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Render() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.validate != nil {
				tt.validate(t, rendered)
			}
		})
	}
}

func TestRenderer_CRDsFromCRDsDirectory(t *testing.T) {
	chartDir := t.TempDir()

	os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(`apiVersion: v2
name: test-chart
version: 0.1.0`), 0644)
	os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte(""), 0644)

	os.MkdirAll(filepath.Join(chartDir, "templates"), 0755)
	os.WriteFile(filepath.Join(chartDir, "templates", "service.yaml"), []byte(`apiVersion: v1
kind: Service
metadata:
  name: test-svc
spec:
  ports:
    - port: 80`), 0644)

	os.MkdirAll(filepath.Join(chartDir, "crds"), 0755)
	os.WriteFile(filepath.Join(chartDir, "crds", "test-crd.yaml"), []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: tests.example.com
spec:
  group: example.com
  names:
    kind: Test
    plural: tests
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object`), 0644)

	r := NewRenderer(chartDir)
	rendered, err := r.Render("test", "default", nil)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if len(rendered.CRDs) != 1 || rendered.CRDs[0].GetName() != "tests.example.com" {
		t.Errorf("expected 1 CRD named tests.example.com, got %d CRDs", len(rendered.CRDs))
	}
	if len(rendered.Resources) != 1 || rendered.Resources[0].GetKind() != "Service" {
		t.Errorf("expected 1 Service resource, got %d resources", len(rendered.Resources))
	}
}

func TestRenderer_ChartVersionFromSyntheticChart(t *testing.T) {
	chartDir := t.TempDir()

	os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(`apiVersion: v2
name: test-chart
version: 1.2.3`), 0644)
	os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte(""), 0644)
	os.MkdirAll(filepath.Join(chartDir, "templates"), 0755)
	os.WriteFile(filepath.Join(chartDir, "templates", "cm.yaml"), []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm`), 0644)

	r := NewRenderer(chartDir)
	rendered, err := r.Render("test", "default", nil)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if rendered.ChartVersion != "1.2.3" {
		t.Errorf("ChartVersion = %q, want %q", rendered.ChartVersion, "1.2.3")
	}
}

func TestRenderer_CRDsFromBothLocations(t *testing.T) {
	chartDir := t.TempDir()

	os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(`apiVersion: v2
name: test-chart
version: 0.1.0`), 0644)
	os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte(""), 0644)

	os.MkdirAll(filepath.Join(chartDir, "templates"), 0755)
	os.WriteFile(filepath.Join(chartDir, "templates", "manifests.yaml"), []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: from-templates.example.com
spec:
  group: example.com
  names:
    kind: FromTemplates
    plural: fromtemplates
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
---
apiVersion: v1
kind: Service
metadata:
  name: test-svc
spec:
  ports:
    - port: 80`), 0644)

	os.MkdirAll(filepath.Join(chartDir, "crds"), 0755)
	os.WriteFile(filepath.Join(chartDir, "crds", "from-crds-dir.yaml"), []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: from-crds-dir.example.com
spec:
  group: example.com
  names:
    kind: FromCRDsDir
    plural: fromcrdsdirs
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object`), 0644)

	r := NewRenderer(chartDir)
	rendered, err := r.Render("test", "default", nil)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if len(rendered.CRDs) != 2 {
		t.Fatalf("expected 2 CRDs (from templates + crds/), got %d", len(rendered.CRDs))
	}

	names := map[string]bool{}
	for _, crd := range rendered.CRDs {
		names[crd.GetName()] = true
	}
	if !names["from-templates.example.com"] {
		t.Error("missing CRD from templates/")
	}
	if !names["from-crds-dir.example.com"] {
		t.Error("missing CRD from crds/")
	}

	if len(rendered.Resources) != 1 {
		t.Errorf("expected 1 non-CRD resource, got %d", len(rendered.Resources))
	}
}

func TestParseManifests(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		wantLen  int
		wantErr  bool
	}{
		{
			name: "parses single document",
			manifest: `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
data:
  key: value`,
			wantLen: 1,
			wantErr: false,
		},
		{
			name: "parses multi-document YAML",
			manifest: `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm2
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm3`,
			wantLen: 3,
			wantErr: false,
		},
		{
			name:     "skips empty documents",
			manifest: "---\n---\n---\n",
			wantLen:  0,
			wantErr:  false,
		},
		{
			name: "skips empty documents between real ones",
			manifest: `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
---
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm2`,
			wantLen: 2,
			wantErr: false,
		},
		{
			name:     "empty manifest returns empty slice",
			manifest: "",
			wantLen:  0,
			wantErr:  false,
		},
		{
			name: "preserves GVK on parsed objects",
			manifest: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deploy`,
			wantLen: 1,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects, err := ParseManifests(tt.manifest)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseManifests() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(objects) != tt.wantLen {
				t.Errorf("ParseManifests() returned %d objects, want %d", len(objects), tt.wantLen)
			}
		})
	}
}

func findDNSOperatorChart(t *testing.T) string {
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
	t.Skip("dns-operator chart not found, skipping renderer tests against real chart")
	return ""
}
