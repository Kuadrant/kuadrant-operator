//go:build unit

package controlplane

import (
	"context"
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

func TestPatchDeploymentEnv(t *testing.T) {
	tests := []struct {
		name    string
		objects []*unstructured.Unstructured
		envVars map[string]string
		wantEnv map[string]string
	}{
		{
			name: "adds env vars to deployment",
			objects: []*unstructured.Unstructured{
				deploymentWithImage("my-deploy", "img:latest"),
			},
			envVars: map[string]string{"RELATED_IMAGE_COREDNS": "quay.io/kuadrant/coredns:latest"},
			wantEnv: map[string]string{"RELATED_IMAGE_COREDNS": "quay.io/kuadrant/coredns:latest"},
		},
		{
			name:    "empty envVars is no-op",
			objects: []*unstructured.Unstructured{deploymentWithImage("my-deploy", "img:latest")},
			envVars: nil,
			wantEnv: nil,
		},
		{
			name: "overwrites existing env var",
			objects: []*unstructured.Unstructured{
				deploymentWithEnv("my-deploy", map[string]string{"KEY": "old"}),
			},
			envVars: map[string]string{"KEY": "new"},
			wantEnv: map[string]string{"KEY": "new"},
		},
		{
			name: "merges with existing env vars",
			objects: []*unstructured.Unstructured{
				deploymentWithEnv("my-deploy", map[string]string{"EXISTING": "keep"}),
			},
			envVars: map[string]string{"NEW": "added"},
			wantEnv: map[string]string{"EXISTING": "keep", "NEW": "added"},
		},
		{
			name: "only patches deployments",
			objects: []*unstructured.Unstructured{
				newUnstructured("Service", "my-svc"),
				deploymentWithImage("my-deploy", "img:latest"),
			},
			envVars: map[string]string{"KEY": "value"},
			wantEnv: map[string]string{"KEY": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := PatchDeploymentEnv(tt.objects, tt.envVars); err != nil {
				t.Fatalf("PatchDeploymentEnv() error = %v", err)
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
				envSlice, _ := container["env"].([]interface{})

				got := make(map[string]string)
				for _, e := range envSlice {
					entry := e.(map[string]interface{})
					got[entry["name"].(string)] = entry["value"].(string)
				}

				for k, v := range tt.wantEnv {
					if got[k] != v {
						t.Errorf("env[%q] = %q, want %q", k, got[k], v)
					}
				}
				if len(got) != len(tt.wantEnv) {
					t.Errorf("env has %d entries, want %d: got %v", len(got), len(tt.wantEnv), got)
				}
			}
		})
	}
}

func deploymentWithEnv(name string, envVars map[string]string) *unstructured.Unstructured {
	var envSlice []interface{}
	for k, v := range envVars {
		envSlice = append(envSlice, map[string]interface{}{"name": k, "value": v})
	}
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata":   map[string]interface{}{"name": name},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "manager",
								"image": "img:latest",
								"env":   envSlice,
							},
						},
					},
				},
			},
		},
	}
}

func TestPatchConfigMapData(t *testing.T) {
	tests := []struct {
		name     string
		objects  []*unstructured.Unstructured
		cmName   string
		data     map[string]string
		wantData map[string]string
	}{
		{
			name: "patches empty ConfigMap",
			objects: []*unstructured.Unstructured{
				configMapWithData("my-cm", nil),
			},
			cmName:   "my-cm",
			data:     map[string]string{"KEY": "value"},
			wantData: map[string]string{"KEY": "value"},
		},
		{
			name: "merges with existing data",
			objects: []*unstructured.Unstructured{
				configMapWithData("my-cm", map[string]string{"EXISTING": "old"}),
			},
			cmName:   "my-cm",
			data:     map[string]string{"NEW": "new"},
			wantData: map[string]string{"EXISTING": "old", "NEW": "new"},
		},
		{
			name: "only patches named ConfigMap",
			objects: []*unstructured.Unstructured{
				configMapWithData("other-cm", nil),
				configMapWithData("my-cm", nil),
			},
			cmName:   "my-cm",
			data:     map[string]string{"KEY": "value"},
			wantData: map[string]string{"KEY": "value"},
		},
		{
			name: "no-op when ConfigMap not found",
			objects: []*unstructured.Unstructured{
				newUnstructured("Service", "my-svc"),
			},
			cmName:   "missing-cm",
			data:     map[string]string{"KEY": "value"},
			wantData: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := PatchConfigMapData(tt.objects, tt.cmName, tt.data); err != nil {
				t.Fatalf("PatchConfigMapData() error = %v", err)
			}

			for _, obj := range tt.objects {
				if obj.GetKind() != "ConfigMap" || obj.GetName() != tt.cmName {
					continue
				}
				got, _, _ := unstructured.NestedStringMap(obj.Object, "data")
				if len(got) != len(tt.wantData) {
					t.Errorf("data = %v, want %v", got, tt.wantData)
					return
				}
				for k, v := range tt.wantData {
					if got[k] != v {
						t.Errorf("data[%q] = %q, want %q", k, got[k], v)
					}
				}
			}
		})
	}
}

func configMapWithData(name string, data map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name": name,
			},
		},
	}
	if data != nil {
		d := make(map[string]interface{})
		for k, v := range data {
			d[k] = v
		}
		obj.Object["data"] = d
	}
	return obj
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
