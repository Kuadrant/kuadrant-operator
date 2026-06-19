//go:build unit

package controllers

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dfake "k8s.io/client-go/dynamic/fake"

	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

func TestTopologyReconciler_OversizedPlaceholder(t *testing.T) {
	assert.Assert(t, len(oversizedPlaceholder) < maxTopologyBytes,
		"placeholder (%d bytes) must be smaller than the limit (%d bytes)", len(oversizedPlaceholder), maxTopologyBytes)
	assert.Assert(t, strings.Contains(oversizedPlaceholder, "digraph"),
		"placeholder must be valid DOT syntax")
}

func TestTopologyReconciler_Create(t *testing.T) {
	namespace := "test-ns"
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	fakeClient := dfake.NewSimpleDynamicClient(scheme)
	reconciler := NewTopologyReconciler(fakeClient, namespace)

	topology, err := machinery.NewTopology(
		machinery.WithObjects(
			&controller.RuntimeObject{
				Object: &corev1.ConfigMap{
					TypeMeta: metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "some-other-configmap",
						Namespace: namespace,
					},
				},
			},
		),
	)
	assert.NilError(t, err)

	err = reconciler.Reconcile(context.Background(), nil, topology, nil, &sync.Map{})
	assert.NilError(t, err)

	created, err := fakeClient.Resource(controller.ConfigMapsResource).Namespace(namespace).Get(context.Background(), TopologyConfigMapName, metav1.GetOptions{})
	assert.NilError(t, err)
	assert.Assert(t, created != nil)
	assert.Equal(t, created.GetName(), TopologyConfigMapName)
	assert.Equal(t, created.GetLabels()[kuadrant.TopologyLabel], "true")

	data, found, err := unstructuredNestedString(created.Object, "data", "topology")
	assert.NilError(t, err)
	assert.Assert(t, found, "topology data key should exist")
	assert.Assert(t, strings.Contains(data, "digraph"), "topology data should be DOT format")
}

func TestTopologyReconciler_Update(t *testing.T) {
	namespace := "test-ns"
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	existingCM := &controller.RuntimeObject{
		Object: &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      TopologyConfigMapName,
				Namespace: namespace,
				Labels:    map[string]string{kuadrant.TopologyLabel: "true"},
			},
			Data: map[string]string{
				"topology": "old-data",
			},
		},
	}

	topology, err := machinery.NewTopology(
		machinery.WithObjects(existingCM),
	)
	assert.NilError(t, err)

	fakeClient := dfake.NewSimpleDynamicClient(scheme)
	// Pre-create the configmap so the update path is hit
	unstructuredCM, err := controller.Destruct(existingCM.Object.(*corev1.ConfigMap))
	assert.NilError(t, err)
	_, err = fakeClient.Resource(controller.ConfigMapsResource).Namespace(namespace).Create(context.Background(), unstructuredCM, metav1.CreateOptions{})
	assert.NilError(t, err)

	reconciler := NewTopologyReconciler(fakeClient, namespace)
	err = reconciler.Reconcile(context.Background(), nil, topology, nil, &sync.Map{})
	assert.NilError(t, err)

	updated, err := fakeClient.Resource(controller.ConfigMapsResource).Namespace(namespace).Get(context.Background(), TopologyConfigMapName, metav1.GetOptions{})
	assert.NilError(t, err)

	data, found, err := unstructuredNestedString(updated.Object, "data", "topology")
	assert.NilError(t, err)
	assert.Assert(t, found, "topology data key should exist")
	assert.Assert(t, data != "old-data", "topology data should have been updated")
}

func TestTopologyReconciler_NoUpdateWhenUnchanged(t *testing.T) {
	namespace := "test-ns"
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Build a topology first to get the expected DOT output
	tempTopology, err := machinery.NewTopology()
	assert.NilError(t, err)
	expectedDot := tempTopology.ToDot()

	existingCM := &controller.RuntimeObject{
		Object: &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      TopologyConfigMapName,
				Namespace: namespace,
				Labels:    map[string]string{kuadrant.TopologyLabel: "true"},
			},
			Data: map[string]string{
				"topology": expectedDot,
			},
		},
	}

	topology, err := machinery.NewTopology(
		machinery.WithObjects(existingCM),
	)
	assert.NilError(t, err)

	fakeClient := dfake.NewSimpleDynamicClient(scheme)
	unstructuredCM, err := controller.Destruct(existingCM.Object.(*corev1.ConfigMap))
	assert.NilError(t, err)
	_, err = fakeClient.Resource(controller.ConfigMapsResource).Namespace(namespace).Create(context.Background(), unstructuredCM, metav1.CreateOptions{})
	assert.NilError(t, err)

	reconciler := NewTopologyReconciler(fakeClient, namespace)
	err = reconciler.Reconcile(context.Background(), nil, topology, nil, &sync.Map{})
	assert.NilError(t, err)
}

func TestNewTopologyReconciler_PanicsOnEmptyNamespace(t *testing.T) {
	defer func() {
		r := recover()
		assert.Assert(t, r != nil, "expected panic for empty namespace")
	}()
	NewTopologyReconciler(nil, "")
}

func unstructuredNestedString(obj map[string]any, fields ...string) (string, bool, error) {
	current := obj
	for i, field := range fields {
		if i == len(fields)-1 {
			val, ok := current[field]
			if !ok {
				return "", false, nil
			}
			s, ok := val.(string)
			return s, ok, nil
		}
		next, ok := current[field]
		if !ok {
			return "", false, nil
		}
		current, ok = next.(map[string]any)
		if !ok {
			return "", false, nil
		}
	}
	return "", false, nil
}
