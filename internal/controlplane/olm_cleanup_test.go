//go:build unit

// OLM migration cleanup tests — remove with olm_cleanup.go after 2-3 releases.
package controlplane

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestIsCSVForPkg(t *testing.T) {
	tests := []struct {
		name    string
		csvName string
		pkg     string
		want    bool
	}{
		{name: "CSV with version", csvName: "dns-operator.v0.8.0", pkg: "dns-operator", want: true},
		{name: "CSV exact match", csvName: "dns-operator", pkg: "dns-operator", want: true},
		{name: "different package", csvName: "kuadrant-operator.v1.0.0", pkg: "dns-operator", want: false},
		{name: "empty CSV name", csvName: "", pkg: "dns-operator", want: false},
		{name: "partial match not at prefix", csvName: "my-dns-operator.v1.0", pkg: "dns-operator", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCSVForPackage(tt.csvName, tt.pkg); got != tt.want {
				t.Errorf("isCSVForPackage(%q, %q) = %v, want %v", tt.csvName, tt.pkg, got, tt.want)
			}
		})
	}
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

func TestNewOLMCleaner(t *testing.T) {
	cleaner := NewOLMCleaner(nil, nil, "kuadrant-system", logr.Discard())
	if cleaner.namespace != "kuadrant-system" {
		t.Errorf("namespace = %q, want %q", cleaner.namespace, "kuadrant-system")
	}
}

// TestMigrateDNSOperator_EndToEnd exercises the full sequence documented on
// migrateDNSOperator against a fake cluster carrying a realistic
// pre-consolidation OLM install: an owned Deployment, owned CRDs, a
// catalog-named Subscription, and a versioned CSV.
func TestMigrateDNSOperator_EndToEnd(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = appsv1.AddToScheme(scheme)

	olmLabels := map[string]string{
		"control-plane":  "dns-operator-controller-manager",
		"olm.owner":      "dns-operator.v0.8.0",
		"olm.owner.kind": "ClusterServiceVersion",
	}
	olmOwnerRefs := []interface{}{
		map[string]interface{}{
			"apiVersion": "operators.coreos.com/v1alpha1",
			"kind":       "ClusterServiceVersion",
			"name":       "dns-operator.v0.8.0",
		},
	}

	deployment := newTestDeployment(olmLabels, olmOwnerRefs)

	newOwnedObj := func(gvr schema.GroupVersionResource, kind, name, namespace string) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": gvr.GroupVersion().String(),
				"kind":       kind,
				"metadata": map[string]interface{}{
					"name": name,
				},
			},
		}
		if namespace != "" {
			obj.SetNamespace(namespace)
		}
		obj.SetLabels(olmLabels)
		_ = unstructured.SetNestedSlice(obj.Object, olmOwnerRefs, "metadata", "ownerReferences")
		return obj
	}

	dnsrecordsCRD := newOwnedObj(crdGVR, "CustomResourceDefinition", "dnsrecords.kuadrant.io", "")
	probesCRD := newOwnedObj(crdGVR, "CustomResourceDefinition", "dnshealthcheckprobes.kuadrant.io", "")

	// User-added data must survive the strip untouched -- only OLM ownership
	// metadata should be removed, never the ConfigMap's own content.
	controllerEnv := newOwnedObj(configMapGVR, "ConfigMap", "dns-operator-controller-env", "kuadrant-system")
	_ = unstructured.SetNestedField(controllerEnv.Object, "info", "data", "LOG_LEVEL")

	subscription := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operators.coreos.com/v1alpha1",
			"kind":       "Subscription",
			"metadata": map[string]interface{}{
				"name":      "dns-operator-preview-kuadrant-operator-catalog-kuadrant-system",
				"namespace": "kuadrant-system",
			},
			"spec": map[string]interface{}{
				"name": "dns-operator",
			},
		},
	}

	csv := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operators.coreos.com/v1alpha1",
			"kind":       "ClusterServiceVersion",
			"metadata": map[string]interface{}{
				"name":      "dns-operator.v0.8.0",
				"namespace": "kuadrant-system",
			},
		},
	}

	gvrToListKind := map[schema.GroupVersionResource]string{
		deploymentGVR:   "DeploymentList",
		crdGVR:          "CustomResourceDefinitionList",
		configMapGVR:    "ConfigMapList",
		subscriptionGVR: "SubscriptionList",
		csvGVR:          "ClusterServiceVersionList",
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind,
		deployment, dnsrecordsCRD, probesCRD, controllerEnv, subscription, csv)

	cleaner := &OLMCleaner{client: client, namespace: "kuadrant-system", logger: logr.Discard()}

	result, err := cleaner.migrateDNSOperator(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Package != "dns-operator" {
		t.Errorf("Package = %q, want %q", result.Package, "dns-operator")
	}
	if result.SubscriptionName != subscription.GetName() {
		t.Errorf("SubscriptionName = %q, want %q", result.SubscriptionName, subscription.GetName())
	}
	if result.CSVName != "dns-operator.v0.8.0" {
		t.Errorf("CSVName = %q, want %q", result.CSVName, "dns-operator.v0.8.0")
	}

	// Deployment, CRDs, and the controller-env ConfigMap must survive with
	// OLM metadata stripped.
	for _, tc := range []struct {
		gvr       schema.GroupVersionResource
		namespace string
		name      string
	}{
		{deploymentGVR, "kuadrant-system", "dns-operator-controller-manager"},
		{crdGVR, "", "dnsrecords.kuadrant.io"},
		{crdGVR, "", "dnshealthcheckprobes.kuadrant.io"},
		{configMapGVR, "kuadrant-system", "dns-operator-controller-env"},
	} {
		res := client.Resource(tc.gvr)
		var r dynamic.ResourceInterface = res
		if tc.namespace != "" {
			r = res.Namespace(tc.namespace)
		}
		obj, err := r.Get(context.Background(), tc.name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected %s/%s to survive, got error: %v", tc.gvr.Resource, tc.name, err)
		}
		if _, ok := obj.GetLabels()["olm.owner.kind"]; ok {
			t.Errorf("%s/%s: OLM labels should have been stripped", tc.gvr.Resource, tc.name)
		}
		if refs, _, _ := unstructured.NestedSlice(obj.Object, "metadata", "ownerReferences"); len(refs) != 0 {
			t.Errorf("%s/%s: CSV ownerReference should have been stripped", tc.gvr.Resource, tc.name)
		}
	}

	envObj, err := client.Resource(configMapGVR).Namespace("kuadrant-system").Get(context.Background(), "dns-operator-controller-env", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error re-fetching controller-env ConfigMap: %v", err)
	}
	if logLevel, _, _ := unstructured.NestedString(envObj.Object, "data", "LOG_LEVEL"); logLevel != "info" {
		t.Errorf("user-added data.LOG_LEVEL = %q, want %q (must survive the strip untouched)", logLevel, "info")
	}

	// Subscription and CSV must be gone.
	if _, err := client.Resource(subscriptionGVR).Namespace("kuadrant-system").Get(context.Background(), subscription.GetName(), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("expected Subscription to be deleted, got err: %v", err)
	}
	if _, err := client.Resource(csvGVR).Namespace("kuadrant-system").Get(context.Background(), "dns-operator.v0.8.0", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("expected CSV to be deleted, got err: %v", err)
	}
}

func newTestDeployment(labels map[string]string, ownerRefs []interface{}) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "dns-operator-controller-manager",
				"namespace": "kuadrant-system",
			},
		},
	}
	obj.SetLabels(labels)
	if ownerRefs != nil {
		_ = unstructured.SetNestedSlice(obj.Object, ownerRefs, "metadata", "ownerReferences")
	}
	return obj
}

func TestStripResource(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = appsv1.AddToScheme(scheme)
	depGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	t.Run("strips metadata from the real target Deployment despite olm.owner.kind", func(t *testing.T) {
		// Mirrors the actual label set OLM stamps on the operator's own
		// Deployment (see TestStripOLMLabels's fixture): olm.owner.kind is
		// present on every CSV-owned resource, not just OLM's own internal
		// auxiliary objects, so stripResource must not skip resources
		// carrying it -- it would otherwise always skip the one resource
		// this cleanup exists to protect from cascade-delete.
		obj := newTestDeployment(map[string]string{
			"control-plane":  "dns-operator-controller-manager",
			"olm.owner":      "dns-operator.v0.0.0",
			"olm.owner.kind": "ClusterServiceVersion",
		}, []interface{}{
			map[string]interface{}{
				"apiVersion": "operators.coreos.com/v1alpha1",
				"kind":       "ClusterServiceVersion",
				"name":       "dns-operator.v0.0.0",
			},
		})

		client := dynamicfake.NewSimpleDynamicClient(scheme, obj)
		cleaner := &OLMCleaner{client: client, logger: logr.Discard()}

		if err := cleaner.stripResource(context.Background(), depGVR, "kuadrant-system", "dns-operator-controller-manager"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got, err := client.Resource(depGVR).Namespace("kuadrant-system").Get(context.Background(), "dns-operator-controller-manager", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("unexpected error re-fetching: %v", err)
		}
		if _, ok := got.GetLabels()["olm.owner.kind"]; ok {
			t.Error("olm.owner.kind label should have been stripped")
		}
		if refs, _, _ := unstructured.NestedSlice(got.Object, "metadata", "ownerReferences"); len(refs) != 0 {
			t.Error("CSV ownerReference should have been stripped")
		}
	})

	t.Run("no-op when the resource does not exist", func(t *testing.T) {
		client := dynamicfake.NewSimpleDynamicClient(scheme)
		cleaner := &OLMCleaner{client: client, logger: logr.Discard()}

		if err := cleaner.stripResource(context.Background(), depGVR, "kuadrant-system", "missing"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("no-op when the resource carries no OLM metadata", func(t *testing.T) {
		wantLabels := map[string]string{"control-plane": "dns-operator-controller-manager"}
		obj := newTestDeployment(wantLabels, nil)
		client := dynamicfake.NewSimpleDynamicClient(scheme, obj)
		cleaner := &OLMCleaner{client: client, logger: logr.Discard()}

		if err := cleaner.stripResource(context.Background(), depGVR, "kuadrant-system", "dns-operator-controller-manager"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got, err := client.Resource(depGVR).Namespace("kuadrant-system").Get(context.Background(), "dns-operator-controller-manager", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("unexpected error re-fetching: %v", err)
		}
		if len(got.GetLabels()) != len(wantLabels) || got.GetLabels()["control-plane"] != wantLabels["control-plane"] {
			t.Errorf("labels = %v, want unchanged %v", got.GetLabels(), wantLabels)
		}
	})
}

func TestIsOLMInstalled_PartialDiscoveryFailure(t *testing.T) {
	// This test verifies the fix for partial discovery failures where
	// ServerGroupsAndResources() returns both an error AND partial results.
	// Before the fix, we would incorrectly skip OLM cleanup even when OLM
	// was installed but some unrelated API group failed discovery.

	olmGroup := &metav1.APIResourceList{GroupVersion: "operators.coreos.com/v1"}
	otherGroup := &metav1.APIResourceList{GroupVersion: "apps/v1"}

	tests := []struct {
		name          string
		resources     []*metav1.APIResourceList
		discoveryErr  error
		wantInstalled bool
	}{
		{
			name:          "OLM installed with partial discovery failure",
			resources:     []*metav1.APIResourceList{olmGroup, otherGroup},
			discoveryErr:  &fakePartialDiscoveryError{},
			wantInstalled: true,
		},
		{
			name:          "OLM not installed with partial discovery failure",
			resources:     []*metav1.APIResourceList{otherGroup},
			discoveryErr:  &fakePartialDiscoveryError{},
			wantInstalled: false,
		},
		{
			name:          "complete discovery failure",
			resources:     nil,
			discoveryErr:  &fakeDiscoveryError{},
			wantInstalled: false,
		},
		{
			name:          "OLM installed no error",
			resources:     []*metav1.APIResourceList{olmGroup},
			discoveryErr:  nil,
			wantInstalled: true,
		},
		{
			name:          "OLM not installed no error",
			resources:     []*metav1.APIResourceList{otherGroup},
			discoveryErr:  nil,
			wantInstalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleaner := &OLMCleaner{
				discovery: &fakeOLMDiscovery{
					FakeDiscovery: &fake.FakeDiscovery{},
					resources:     tt.resources,
					err:           tt.discoveryErr,
				},
			}

			got := cleaner.isOLMInstalled()
			if got != tt.wantInstalled {
				t.Errorf("isOLMInstalled() = %v, want %v", got, tt.wantInstalled)
			}
		})
	}
}

type fakeOLMDiscovery struct {
	*fake.FakeDiscovery
	resources []*metav1.APIResourceList
	err       error
}

func (f *fakeOLMDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return nil, f.resources, f.err
}

type fakePartialDiscoveryError struct{}

func (e *fakePartialDiscoveryError) Error() string {
	return "partial discovery failure"
}

type fakeDiscoveryError struct{}

func (e *fakeDiscoveryError) Error() string {
	return "complete discovery failure"
}
