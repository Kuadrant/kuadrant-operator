//go:build unit

package controlplane

import (
	"testing"
)

func TestEnabledComponents_ReturnsAllComponents(t *testing.T) {
	d := &Deployer{components: allComponents()}
	enabled := d.EnabledComponents()

	if len(enabled) == 0 {
		t.Fatal("expected at least one enabled component")
	}
	if enabled[0].Name != "dns-operator" {
		t.Errorf("first component = %q, want %q", enabled[0].Name, "dns-operator")
	}
}

func TestComponentByName(t *testing.T) {
	d := &Deployer{components: allComponents()}

	tests := []struct {
		name      string
		lookup    string
		wantFound bool
	}{
		{name: "existing component", lookup: "dns-operator", wantFound: true},
		{name: "nonexistent component", lookup: "nonexistent", wantFound: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, found := d.ComponentByName(tt.lookup)
			if found != tt.wantFound {
				t.Errorf("ComponentByName(%q) found = %v, want %v", tt.lookup, found, tt.wantFound)
			}
		})
	}
}

func TestComponentStatus_CRDsFromRegistry(t *testing.T) {
	d := &Deployer{components: allComponents()}
	crdNames := d.CRDNames()

	if len(crdNames) != 2 {
		t.Fatalf("expected 2 CRD names, got %d", len(crdNames))
	}

	want := map[string]bool{
		"dnsrecords.kuadrant.io":           false,
		"dnshealthcheckprobes.kuadrant.io": false,
	}
	for _, name := range crdNames {
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected CRD name %q", name)
		}
		want[name] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing expected CRD name %q", name)
		}
	}
}

func TestGetImageStatuses(t *testing.T) {
	r := &Reconciler{}

	tests := []struct {
		name      string
		component Component
		envVars   map[string]string
		wantCount int
		wantName  string
		wantImage string
	}{
		{
			name: "returns image from ImageEnvVar",
			component: Component{
				Name:        "dns-operator",
				ImageEnvVar: "TEST_RELATED_IMAGE",
			},
			envVars:   map[string]string{"TEST_RELATED_IMAGE": "quay.io/kuadrant/dns-operator:v1.0.0"},
			wantCount: 1,
			wantName:  "controller",
			wantImage: "quay.io/kuadrant/dns-operator:v1.0.0",
		},
		{
			name: "empty env var returns no images",
			component: Component{
				Name:        "dns-operator",
				ImageEnvVar: "TEST_RELATED_IMAGE",
			},
			envVars:   map[string]string{"TEST_RELATED_IMAGE": ""},
			wantCount: 0,
		},
		{
			name: "no ImageEnvVar returns no images",
			component: Component{
				Name: "dns-operator",
			},
			wantCount: 0,
		},
		{
			name: "includes images from ChartValueOverrides implementing ImageReporter",
			component: Component{
				Name: "test",
				ChartValueOverrides: []ChartValueOverride{
					&ImageValue{EnvVar: "TEST_OVERRIDE_IMG", ValueKey: "image", Description: "sidecar"},
				},
			},
			envVars:   map[string]string{"TEST_OVERRIDE_IMG": "ghcr.io/test/sidecar:v2"},
			wantCount: 1,
			wantName:  "sidecar",
			wantImage: "ghcr.io/test/sidecar:v2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}
			images := r.getImageStatuses(tt.component)
			if len(images) != tt.wantCount {
				t.Fatalf("expected %d images, got %d", tt.wantCount, len(images))
			}
			if tt.wantCount > 0 {
				if images[0].Name != tt.wantName {
					t.Errorf("Images[0].Name = %q, want %q", images[0].Name, tt.wantName)
				}
				if images[0].Image != tt.wantImage {
					t.Errorf("Images[0].Image = %q, want %q", images[0].Image, tt.wantImage)
				}
			}
		})
	}
}
