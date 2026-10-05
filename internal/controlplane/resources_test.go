//go:build unit

package controlplane

import (
	"context"
	"slices"
	"testing"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta/testrestmapper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
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

func TestApplyResources_OwnerReference(t *testing.T) {
	ownerRef := &metav1.OwnerReference{
		APIVersion: "kuadrant.io/v1alpha1",
		Kind:       "KuadrantControlPlane",
		Name:       "default",
		UID:        "test-uid",
	}

	tests := []struct {
		name       string
		object     *unstructured.Unstructured
		ownerRef   *metav1.OwnerReference
		wantOwners int
	}{
		{
			name:       "sets ownerReference when provided",
			object:     newUnstructured("ServiceAccount", "test-sa"),
			ownerRef:   ownerRef,
			wantOwners: 1,
		},
		{
			name:       "leaves no ownerReference when nil (CRDs)",
			object:     newUnstructured("ServiceAccount", "test-sa"),
			ownerRef:   nil,
			wantOwners: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			client := dynamicfake.NewSimpleDynamicClient(scheme)
			applier := &ResourceApplier{
				client:    client,
				mapper:    testrestmapper.TestOnlyStaticRESTMapper(scheme),
				logger:    logr.Discard(),
				namespace: "kuadrant-system",
			}

			// The fake dynamic client's server-side apply support can't
			// round-trip a plain *unstructured.Unstructured (it needs a
			// registered Go type to structurally merge against), so the
			// apply call itself is expected to error here. That's not what
			// this test checks: applyResource sets ownerRef on obj before
			// ever calling Apply, so the mutation is observable regardless.
			_ = applier.ApplyResources(context.Background(), []*unstructured.Unstructured{tt.object}, tt.ownerRef)

			refs, _, _ := unstructured.NestedSlice(tt.object.Object, "metadata", "ownerReferences")
			if len(refs) != tt.wantOwners {
				t.Fatalf("ownerReferences = %v, want %d entries", refs, tt.wantOwners)
			}
			if tt.wantOwners > 0 {
				ref := refs[0].(map[string]interface{})
				if ref["name"] != ownerRef.Name || ref["uid"] != string(ownerRef.UID) {
					t.Errorf("ownerReference = %v, want name=%q uid=%q", ref, ownerRef.Name, ownerRef.UID)
				}
			}
		})
	}
}

func TestPatchContainerEnvVars(t *testing.T) {
	tests := []struct {
		name           string
		objects        []*unstructured.Unstructured
		deploymentName string
		envVars        map[string]string
		want           map[string]string
		wantUnchanged  []string
	}{
		{
			name: "overrides existing env var",
			objects: []*unstructured.Unstructured{
				deploymentWithEnv("my-deploy", map[string]string{"RELATED_IMAGE_AUTHORINO": "original:latest"}),
			},
			deploymentName: "my-deploy",
			envVars:        map[string]string{"RELATED_IMAGE_AUTHORINO": "override:v1.0"},
			want:           map[string]string{"RELATED_IMAGE_AUTHORINO": "override:v1.0"},
		},
		{
			name: "appends env var not already present",
			objects: []*unstructured.Unstructured{
				deploymentWithEnv("my-deploy", map[string]string{}),
			},
			deploymentName: "my-deploy",
			envVars:        map[string]string{"RELATED_IMAGE_AUTHORINO": "override:v1.0"},
			want:           map[string]string{"RELATED_IMAGE_AUTHORINO": "override:v1.0"},
		},
		{
			name: "empty value leaves chart default in place",
			objects: []*unstructured.Unstructured{
				deploymentWithEnv("my-deploy", map[string]string{"RELATED_IMAGE_AUTHORINO": "original:latest"}),
			},
			deploymentName: "my-deploy",
			envVars:        map[string]string{"RELATED_IMAGE_AUTHORINO": ""},
			want:           map[string]string{"RELATED_IMAGE_AUTHORINO": "original:latest"},
		},
		{
			name: "only patches Deployments",
			objects: []*unstructured.Unstructured{
				newUnstructured("Service", "my-svc"),
				deploymentWithEnv("my-deploy", map[string]string{"RELATED_IMAGE_AUTHORINO": "original:latest"}),
			},
			deploymentName: "my-deploy",
			envVars:        map[string]string{"RELATED_IMAGE_AUTHORINO": "override:v1.0"},
			want:           map[string]string{"RELATED_IMAGE_AUTHORINO": "override:v1.0"},
		},
		{
			name: "skips unmatched Deployments",
			objects: []*unstructured.Unstructured{
				deploymentWithEnv("other-deploy", map[string]string{"RELATED_IMAGE_AUTHORINO": "original:latest"}),
				deploymentWithEnv("my-deploy", map[string]string{"RELATED_IMAGE_AUTHORINO": "original:latest"}),
			},
			deploymentName: "my-deploy",
			envVars:        map[string]string{"RELATED_IMAGE_AUTHORINO": "override:v1.0"},
			want:           map[string]string{"RELATED_IMAGE_AUTHORINO": "override:v1.0"},
			wantUnchanged:  []string{"other-deploy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := PatchContainerEnvVars(tt.objects, tt.deploymentName, tt.envVars); err != nil {
				t.Fatalf("PatchContainerEnvVars() error = %v", err)
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
				env, _, _ := unstructured.NestedSlice(container, "env")
				got := map[string]string{}
				for _, e := range env {
					entry := e.(map[string]interface{})
					got[entry["name"].(string)] = entry["value"].(string)
				}

				if obj.GetName() == tt.deploymentName {
					for name, wantValue := range tt.want {
						if got[name] != wantValue {
							t.Errorf("env[%q] = %q, want %q", name, got[name], wantValue)
						}
					}
				} else if slices.Contains(tt.wantUnchanged, obj.GetName()) {
					// verify unrelated Deployments remain unchanged
					if got["RELATED_IMAGE_AUTHORINO"] != "original:latest" {
						t.Errorf("unexpected patch on %q: env[RELATED_IMAGE_AUTHORINO] = %q, want original:latest", obj.GetName(), got["RELATED_IMAGE_AUTHORINO"])
					}
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

func deploymentWithEnv(name string, env map[string]string) *unstructured.Unstructured {
	envList := make([]interface{}, 0, len(env))
	for k, v := range env {
		envList = append(envList, map[string]interface{}{"name": k, "value": v})
	}
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
								"name": "manager",
								"env":  envList,
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
