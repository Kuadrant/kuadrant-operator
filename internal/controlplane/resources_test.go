//go:build unit

package controlplane

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newUnstructured(kind, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       kind,
			"metadata": map[string]interface{}{
				"name": name,
			},
		},
	}
}

func TestSortByInstallOrder(t *testing.T) {
	tests := []struct {
		name      string
		kinds     []string
		wantOrder []string
	}{
		{
			name:      "already sorted",
			kinds:     []string{"ServiceAccount", "ClusterRole", "Deployment"},
			wantOrder: []string{"ServiceAccount", "ClusterRole", "Deployment"},
		},
		{
			name:      "reverse order",
			kinds:     []string{"Deployment", "ClusterRole", "ServiceAccount"},
			wantOrder: []string{"ServiceAccount", "ClusterRole", "Deployment"},
		},
		{
			name:      "CRDs before everything",
			kinds:     []string{"Deployment", "CustomResourceDefinition", "ServiceAccount"},
			wantOrder: []string{"ServiceAccount", "CustomResourceDefinition", "Deployment"},
		},
		{
			name:      "full dns-operator ordering",
			kinds:     []string{"Deployment", "Service", "ConfigMap", "ClusterRoleBinding", "RoleBinding", "Role", "ServiceAccount", "ClusterRole"},
			wantOrder: []string{"ServiceAccount", "ConfigMap", "ClusterRole", "ClusterRoleBinding", "Role", "RoleBinding", "Service", "Deployment"},
		},
		{
			name:      "unknown kinds sorted after known",
			kinds:     []string{"Deployment", "FooBar", "ServiceAccount"},
			wantOrder: []string{"ServiceAccount", "Deployment", "FooBar"},
		},
		{
			name:      "empty input",
			kinds:     nil,
			wantOrder: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []*unstructured.Unstructured
			for _, kind := range tt.kinds {
				objects = append(objects, newUnstructured(kind, "test-"+kind))
			}

			SortByInstallOrder(objects)

			for i, obj := range objects {
				if i >= len(tt.wantOrder) {
					break
				}
				if obj.GetKind() != tt.wantOrder[i] {
					t.Errorf("position %d: got kind %q, want %q", i, obj.GetKind(), tt.wantOrder[i])
				}
			}
		})
	}
}

func TestSortByInstallOrder_Stability(t *testing.T) {
	objects := []*unstructured.Unstructured{
		newUnstructured("Service", "svc-a"),
		newUnstructured("Service", "svc-b"),
		newUnstructured("Service", "svc-c"),
	}

	SortByInstallOrder(objects)

	if objects[0].GetName() != "svc-a" || objects[1].GetName() != "svc-b" || objects[2].GetName() != "svc-c" {
		t.Error("sort is not stable: resources of the same kind changed relative order")
	}
}

func TestCRDNames(t *testing.T) {
	tests := []struct {
		name string
		crds []*unstructured.Unstructured
		want []string
	}{
		{
			name: "returns names",
			crds: []*unstructured.Unstructured{
				newUnstructured("CustomResourceDefinition", "dnsrecords.kuadrant.io"),
				newUnstructured("CustomResourceDefinition", "dnshealthcheckprobes.kuadrant.io"),
			},
			want: []string{"dnsrecords.kuadrant.io", "dnshealthcheckprobes.kuadrant.io"},
		},
		{
			name: "empty input",
			crds: nil,
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CRDNames(tt.crds)
			if len(got) != len(tt.want) {
				t.Fatalf("CRDNames() = %v, want %v", got, tt.want)
			}
			for i, name := range got {
				if name != tt.want[i] {
					t.Errorf("CRDNames()[%d] = %q, want %q", i, name, tt.want[i])
				}
			}
		})
	}
}

func TestPatchDeploymentImage(t *testing.T) {
	tests := []struct {
		name      string
		objects   []*unstructured.Unstructured
		image     string
		wantImage string
	}{
		{
			name: "patches deployment image",
			objects: []*unstructured.Unstructured{
				deploymentWithImage("my-deploy", "original:latest"),
			},
			image:     "override:v1.0",
			wantImage: "override:v1.0",
		},
		{
			name: "empty image preserves original",
			objects: []*unstructured.Unstructured{
				deploymentWithImage("my-deploy", "original:latest"),
			},
			image:     "",
			wantImage: "original:latest",
		},
		{
			name: "only patches Deployments",
			objects: []*unstructured.Unstructured{
				newUnstructured("Service", "my-svc"),
				deploymentWithImage("my-deploy", "original:latest"),
			},
			image:     "override:v1.0",
			wantImage: "override:v1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := PatchDeploymentImage(tt.objects, tt.image); err != nil {
				t.Fatalf("PatchDeploymentImage() error = %v", err)
			}

			for _, obj := range tt.objects {
				if obj.GetKind() != "Deployment" {
					continue
				}
				containers, _, _ := unstructured.NestedSlice(obj.Object,
					"spec", "template", "spec", "containers")
				if len(containers) == 0 {
					t.Fatal("no containers found")
				}
				container := containers[0].(map[string]interface{})
				got := container["image"].(string)
				if got != tt.wantImage {
					t.Errorf("image = %q, want %q", got, tt.wantImage)
				}
			}
		})
	}
}

func TestIsCRDEstablished(t *testing.T) {
	tests := []struct {
		name string
		obj  *unstructured.Unstructured
		want bool
	}{
		{
			name: "established CRD",
			obj:  crdWithCondition("Established", "True"),
			want: true,
		},
		{
			name: "not established",
			obj:  crdWithCondition("Established", "False"),
			want: false,
		},
		{
			name: "no conditions",
			obj:  newUnstructured("CustomResourceDefinition", "test.example.com"),
			want: false,
		},
		{
			name: "wrong condition type",
			obj:  crdWithCondition("NamesAccepted", "True"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCRDEstablished(tt.obj); got != tt.want {
				t.Errorf("isCRDEstablished() = %v, want %v", got, tt.want)
			}
		})
	}
}

func deploymentWithImage(name, image string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "manager",
								"image": image,
							},
						},
					},
				},
			},
		},
	}
}

func crdWithCondition(condType, status string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]interface{}{
				"name": "test.example.com",
			},
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"type":   condType,
						"status": status,
					},
				},
			},
		},
	}
}
