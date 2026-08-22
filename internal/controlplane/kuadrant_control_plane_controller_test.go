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
	tests := []struct {
		name           string
		componentName  string
		deployedImages map[string][]DeployedImage
		wantCount      int
		wantName       string
		wantImage      string
	}{
		{
			name:          "returns images from deployed containers",
			componentName: "dns-operator",
			deployedImages: map[string][]DeployedImage{
				"dns-operator": {{Container: "manager", Image: "quay.io/kuadrant/dns-operator:v1.0.0"}},
			},
			wantCount: 1,
			wantName:  "manager",
			wantImage: "quay.io/kuadrant/dns-operator:v1.0.0",
		},
		{
			name:           "no deployed images returns empty",
			componentName:  "dns-operator",
			deployedImages: map[string][]DeployedImage{},
			wantCount:      0,
		},
		{
			name:          "multiple containers reported",
			componentName: "mcp-gateway",
			deployedImages: map[string][]DeployedImage{
				"mcp-gateway": {
					{Container: "controller", Image: "ghcr.io/kuadrant/mcp-controller:v0.8.0"},
					{Container: "broker", Image: "ghcr.io/kuadrant/mcp-gateway:v0.8.0"},
				},
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deployer{deployedImages: tt.deployedImages}
			r := &Reconciler{deployer: d}
			images := r.getImageStatuses(Component{Name: tt.componentName})
			if len(images) != tt.wantCount {
				t.Fatalf("expected %d images, got %d", tt.wantCount, len(images))
			}
			if tt.wantName != "" {
				if images[0].Name != tt.wantName {
					t.Errorf("Images[0].Name = %q, want %q", images[0].Name, tt.wantName)
				}
			}
			if tt.wantImage != "" {
				if images[0].Image != tt.wantImage {
					t.Errorf("Images[0].Image = %q, want %q", images[0].Image, tt.wantImage)
				}
			}
		})
	}
}
