//go:build unit

package controlplane

import (
	"context"
	"errors"
	"strings"
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

// mockDeployer allows injecting errors for specific components
type mockDeployer struct {
	*Deployer
	deployErrors map[string]error
}

func (m *mockDeployer) DeployComponent(ctx context.Context, component Component) error {
	if err, found := m.deployErrors[component.Name]; found {
		return err
	}
	return nil
}

func TestReconcile_MultipleComponentFailures(t *testing.T) {
	components := []Component{
		{Name: "dns-operator", DeploymentName: "dns-operator-controller-manager"},
		{Name: "mcp-gateway", DeploymentName: "mcp-gateway-controller"},
	}

	tests := []struct {
		name          string
		deployErrors  map[string]error
		wantErrCount  int
		wantErrContns []string
	}{
		{
			name:          "all components succeed",
			deployErrors:  map[string]error{},
			wantErrCount:  0,
			wantErrContns: nil,
		},
		{
			name: "single component fails",
			deployErrors: map[string]error{
				"dns-operator": errors.New("failed to apply manifests"),
			},
			wantErrCount:  1,
			wantErrContns: []string{"dns-operator", "failed to apply manifests"},
		},
		{
			name: "multiple components fail independently",
			deployErrors: map[string]error{
				"dns-operator": errors.New("chart not found"),
				"mcp-gateway":  errors.New("invalid image reference"),
			},
			wantErrCount:  2,
			wantErrContns: []string{"dns-operator", "chart not found", "mcp-gateway", "invalid image reference"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployer := &mockDeployer{
				Deployer:     &Deployer{components: components},
				deployErrors: tt.deployErrors,
			}

			gotErrCount := 0
			err := deployComponents(context.Background(), deployer.EnabledComponents(), deployer.DeployComponent, func(Component, error) {
				gotErrCount++
			})

			if gotErrCount != tt.wantErrCount {
				t.Errorf("expected %d errors, got %d", tt.wantErrCount, gotErrCount)
			}

			if err != nil {
				errStr := err.Error()
				for _, want := range tt.wantErrContns {
					if !strings.Contains(errStr, want) {
						t.Errorf("joined error %q does not contain %q", errStr, want)
					}
				}
			} else if tt.wantErrCount != 0 {
				t.Errorf("expected a joined error, got nil")
			}
		})
	}
}
