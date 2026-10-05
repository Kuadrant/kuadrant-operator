//go:build unit

package controlplane

import (
	"context"
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
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

func TestKCPOwnerReference(t *testing.T) {
	cp := &kuadrantv1alpha1.KuadrantControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName,
			UID:  "test-uid",
		},
	}

	ref := kcpOwnerReference(cp)

	if ref.APIVersion != kuadrantv1alpha1.GroupVersion.String() {
		t.Errorf("APIVersion = %q, want %q", ref.APIVersion, kuadrantv1alpha1.GroupVersion.String())
	}
	if ref.Kind != "KuadrantControlPlane" {
		t.Errorf("Kind = %q, want %q", ref.Kind, "KuadrantControlPlane")
	}
	if ref.Name != cp.Name {
		t.Errorf("Name = %q, want %q", ref.Name, cp.Name)
	}
	if ref.UID != cp.UID {
		t.Errorf("UID = %q, want %q", ref.UID, cp.UID)
	}
	if ref.Controller == nil || !*ref.Controller {
		t.Error("Controller = false or nil, want true")
	}
	if ref.BlockOwnerDeletion != nil {
		t.Errorf("BlockOwnerDeletion = %v, want nil (requires finalizers RBAC on the owner we don't have or need)", *ref.BlockOwnerDeletion)
	}
}

func TestReconcile_MissingKCP_DoesNotRecreate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kuadrantv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &Reconciler{
		Client:   fakeClient,
		deployer: &Deployer{components: nil},
	}

	res, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName},
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0 (no self-heal requeue)", res.RequeueAfter)
	}

	cp := &kuadrantv1alpha1.KuadrantControlPlane{}
	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}, cp)
	if !apierrors.IsNotFound(err) {
		t.Errorf("expected KuadrantControlPlane to remain absent, got error = %v", err)
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
