//go:build unit

// OLM migration cleanup tests — remove with olm_cleanup.go after 2-3 releases.
package controlplane

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestIsOrphanedCSV(t *testing.T) {
	cleaner := &OLMCleaner{
		packageNames: []string{"dns-operator"},
	}

	tests := []struct {
		name    string
		csvName string
		want    bool
	}{
		{name: "dns-operator CSV with version", csvName: "dns-operator.v0.8.0", want: true},
		{name: "dns-operator CSV exact match", csvName: "dns-operator", want: true},
		{name: "kuadrant-operator CSV", csvName: "kuadrant-operator.v1.0.0", want: false},
		{name: "authorino-operator CSV", csvName: "authorino-operator.v0.15.0", want: false},
		{name: "empty string", csvName: "", want: false},
		{name: "partial match not at prefix", csvName: "my-dns-operator.v1.0", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleaner.isOrphanedCSV(tt.csvName); got != tt.want {
				t.Errorf("isOrphanedCSV(%q) = %v, want %v", tt.csvName, got, tt.want)
			}
		})
	}
}

func TestIsOrphanedCSV_MultiplePackages(t *testing.T) {
	cleaner := &OLMCleaner{
		packageNames: []string{"dns-operator", "mcp-gateway"},
	}

	tests := []struct {
		name    string
		csvName string
		want    bool
	}{
		{name: "dns-operator", csvName: "dns-operator.v0.8.0", want: true},
		{name: "mcp-gateway", csvName: "mcp-gateway.v1.0.0", want: true},
		{name: "authorino-operator", csvName: "authorino-operator.v0.15.0", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleaner.isOrphanedCSV(tt.csvName); got != tt.want {
				t.Errorf("isOrphanedCSV(%q) = %v, want %v", tt.csvName, got, tt.want)
			}
		})
	}
}

func TestEmptyOLMPackageName_Skipped(t *testing.T) {
	d := &Deployer{
		components: []Component{
			{Name: "dns-operator", OLMPackageName: "dns-operator", DeploymentName: "dns-operator-controller-manager"},
			{Name: "new-component", OLMPackageName: "", DeploymentName: "new-component-controller"},
		},
	}

	t.Run("OLMPackageNames excludes empty entries", func(t *testing.T) {
		names := d.OLMPackageNames()
		if len(names) != 1 {
			t.Fatalf("expected 1 OLM package name, got %d: %v", len(names), names)
		}
		if names[0] != "dns-operator" {
			t.Errorf("OLMPackageNames()[0] = %q, want %q", names[0], "dns-operator")
		}
	})

	t.Run("DeploymentNames excludes empty entries", func(t *testing.T) {
		d2 := &Deployer{
			components: []Component{
				{Name: "a", DeploymentName: "a-deploy"},
				{Name: "b", DeploymentName: ""},
			},
		}
		names := d2.DeploymentNames()
		if len(names) != 1 {
			t.Fatalf("expected 1 deployment name, got %d: %v", len(names), names)
		}
	})

	t.Run("isOrphanedCSV ignores component with empty OLM package", func(t *testing.T) {
		cleaner := &OLMCleaner{packageNames: d.OLMPackageNames()}
		if cleaner.isOrphanedCSV("new-component.v1.0") {
			t.Error("should not match component with empty OLMPackageName")
		}
		if !cleaner.isOrphanedCSV("dns-operator.v0.8.0") {
			t.Error("should match dns-operator")
		}
	})
}

func TestStripOLMLabels(t *testing.T) {
	tests := []struct {
		name       string
		labels     map[string]string
		wantLabels map[string]string
		wantMod    bool
	}{
		{
			name: "strips all OLM labels",
			labels: map[string]string{
				"app.kubernetes.io/managed-by": "helm",
				"control-plane":                "dns-operator-controller-manager",
				"olm.managed":                  "true",
				"olm.owner":                    "dns-operator.v0.0.0",
				"olm.owner.kind":               "ClusterServiceVersion",
				"olm.owner.namespace":          "kuadrant-system",
				"olm.deployment-spec-hash":     "abc123",
				"operators.coreos.com/dns-operator.kuadrant-system": "",
			},
			wantLabels: map[string]string{
				"app.kubernetes.io/managed-by": "helm",
				"control-plane":                "dns-operator-controller-manager",
			},
			wantMod: true,
		},
		{
			name: "no OLM labels — no modification",
			labels: map[string]string{
				"app.kubernetes.io/managed-by": "kuadrant-operator",
				"control-plane":                "dns-operator-controller-manager",
			},
			wantLabels: map[string]string{
				"app.kubernetes.io/managed-by": "kuadrant-operator",
				"control-plane":                "dns-operator-controller-manager",
			},
			wantMod: false,
		},
		{
			name:       "nil labels",
			labels:     nil,
			wantLabels: nil,
			wantMod:    false,
		},
		{
			name: "only OLM labels — all removed",
			labels: map[string]string{
				"olm.managed": "true",
				"operators.coreos.com/dns-operator.kuadrant-system": "",
			},
			wantLabels: map[string]string{},
			wantMod:    true,
		},
		{
			name: "preserves non-OLM labels",
			labels: map[string]string{
				"app":          "dns-operator",
				"olm.managed":  "true",
				"custom-label": "value",
			},
			wantLabels: map[string]string{
				"app":          "dns-operator",
				"custom-label": "value",
			},
			wantMod: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name": "test",
					},
				},
			}
			obj.SetLabels(tt.labels)

			cleaner := &OLMCleaner{}
			modified := cleaner.stripOLMLabels(obj)

			if modified != tt.wantMod {
				t.Errorf("stripOLMLabels() modified = %v, want %v", modified, tt.wantMod)
			}

			gotLabels := obj.GetLabels()
			if tt.wantLabels == nil {
				if gotLabels != nil && len(gotLabels) > 0 {
					t.Errorf("expected nil/empty labels, got %v", gotLabels)
				}
				return
			}
			if len(gotLabels) != len(tt.wantLabels) {
				t.Errorf("label count = %d, want %d\ngot:  %v\nwant: %v", len(gotLabels), len(tt.wantLabels), gotLabels, tt.wantLabels)
				return
			}
			for k, v := range tt.wantLabels {
				if gotLabels[k] != v {
					t.Errorf("label %q = %q, want %q", k, gotLabels[k], v)
				}
			}
		})
	}
}

func TestStripCSVOwnerRefs(t *testing.T) {
	tests := []struct {
		name      string
		ownerRefs []interface{}
		wantCount int
		wantMod   bool
	}{
		{
			name: "removes CSV ownerReference",
			ownerRefs: []interface{}{
				map[string]interface{}{
					"kind": "ClusterServiceVersion",
					"name": "dns-operator.v0.0.0",
				},
			},
			wantCount: 0,
			wantMod:   true,
		},
		{
			name: "preserves non-CSV ownerReference",
			ownerRefs: []interface{}{
				map[string]interface{}{
					"kind": "Deployment",
					"name": "some-owner",
				},
			},
			wantCount: 1,
			wantMod:   false,
		},
		{
			name: "removes only CSV, keeps others",
			ownerRefs: []interface{}{
				map[string]interface{}{
					"kind": "ClusterServiceVersion",
					"name": "dns-operator.v0.0.0",
				},
				map[string]interface{}{
					"kind": "Deployment",
					"name": "some-owner",
				},
			},
			wantCount: 1,
			wantMod:   true,
		},
		{
			name:      "no ownerRefs",
			ownerRefs: nil,
			wantCount: 0,
			wantMod:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name": "test",
					},
				},
			}
			if tt.ownerRefs != nil {
				_ = unstructured.SetNestedSlice(obj.Object, tt.ownerRefs, "metadata", "ownerReferences")
			}

			cleaner := &OLMCleaner{}
			modified := cleaner.stripCSVOwnerRefs(obj)

			if modified != tt.wantMod {
				t.Errorf("stripCSVOwnerRefs() modified = %v, want %v", modified, tt.wantMod)
			}

			refs, found, _ := unstructured.NestedSlice(obj.Object, "metadata", "ownerReferences")
			if !found && tt.wantCount == 0 {
				return
			}
			if len(refs) != tt.wantCount {
				t.Errorf("ownerRef count = %d, want %d", len(refs), tt.wantCount)
			}
		})
	}
}

func TestIsChildOperatorResource_ByOwnerLabel(t *testing.T) {
	cleaner := &OLMCleaner{
		packageNames: []string{"dns-operator"},
		namespace:    "kuadrant-system",
	}

	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{
			name:   "matches olm.owner for orphaned CSV",
			labels: map[string]string{"olm.owner": "dns-operator.v0.0.0"},
			want:   true,
		},
		{
			name:   "does not match olm.owner for non-orphaned CSV",
			labels: map[string]string{"olm.owner": "kuadrant-operator.v1.0.0"},
			want:   false,
		},
		{
			name:   "no olm.owner label",
			labels: map[string]string{"app": "test"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name": "test",
					},
				},
			}
			obj.SetLabels(tt.labels)

			// isChildOperatorResource was removed but the logic is in isOrphanedCSV
			owner, ok := tt.labels["olm.owner"]
			got := ok && cleaner.isOrphanedCSV(owner)
			if got != tt.want {
				t.Errorf("isOrphanedCSV via olm.owner = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeployerRegistryDrives_OLMCleanup(t *testing.T) {
	components := allComponents()

	t.Run("deployment names derived from registry", func(t *testing.T) {
		d := &Deployer{components: components}
		names := d.DeploymentNames()
		if len(names) == 0 {
			t.Fatal("expected deployment names from registry")
		}
		if names[0] != "dns-operator-controller-manager" {
			t.Errorf("DeploymentNames()[0] = %q, want %q", names[0], "dns-operator-controller-manager")
		}
	})

	t.Run("OLM package names derived from registry", func(t *testing.T) {
		d := &Deployer{components: components}
		names := d.OLMPackageNames()
		if len(names) == 0 {
			t.Fatal("expected OLM package names from registry")
		}
		if names[0] != "dns-operator" {
			t.Errorf("OLMPackageNames()[0] = %q, want %q", names[0], "dns-operator")
		}
	})
}
