//go:build unit

package controlplane

import (
	"testing"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
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

func TestComponentStatusImages(t *testing.T) {
	cs := kuadrantv1beta1.ComponentStatus{
		Name:  "dns-operator",
		Ready: true,
		Images: []kuadrantv1beta1.ImageStatus{
			{Name: "controller", Image: "quay.io/kuadrant/dns-operator:v1.0.0"},
		},
	}

	if len(cs.Images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(cs.Images))
	}
	if cs.Images[0].Name != "controller" {
		t.Errorf("Images[0].Name = %q, want %q", cs.Images[0].Name, "controller")
	}
	if cs.Images[0].Image != "quay.io/kuadrant/dns-operator:v1.0.0" {
		t.Errorf("Images[0].Image = %q, want %q", cs.Images[0].Image, "quay.io/kuadrant/dns-operator:v1.0.0")
	}
}
