package controllers

import (
	"path/filepath"
	"testing"

	authorinoopapi "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestHelmAuthorinoReconciler_BuildValues(t *testing.T) {
	chartPath := filepath.Join("..", "..", "charts", "authorino")
	reconciler := NewHelmAuthorinoReconciler(nil, chartPath)

	authorino := &authorinoopapi.Authorino{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "authorino",
			Namespace: "kuadrant-system",
		},
		Spec: authorinoopapi.AuthorinoSpec{
			Replicas: ptr.To(int32(2)),
		},
	}

	values := reconciler.buildHelmValues(authorino)

	assert.NotNil(t, values)
	assert.Equal(t, int32(2), values["replicas"])

	imageMap, ok := values["image"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "quay.io/kuadrant/authorino", imageMap["repository"])
	assert.Equal(t, "latest", imageMap["tag"])
}

func TestHelmLimitadorReconciler_BuildValues(t *testing.T) {
	chartPath := filepath.Join("..", "..", "charts", "limitador")
	reconciler := NewHelmLimitadorReconciler(nil, chartPath)

	limitador := &limitadorv1alpha1.Limitador{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "limitador",
			Namespace: "kuadrant-system",
		},
		Spec: limitadorv1alpha1.LimitadorSpec{
			Replicas: ptr.To(3),
		},
	}

	values := reconciler.buildHelmValues(limitador)

	assert.NotNil(t, values)
	assert.Equal(t, int32(3), values["replicas"])

	storageMap, ok := values["storage"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "memory", storageMap["type"])
}

func TestHelmAuthorinoReconciler_Reconcile(t *testing.T) {
	chartPath := filepath.Join("..", "..", "charts", "authorino")
	reconciler := NewHelmAuthorinoReconciler(nil, chartPath)

	// For a full test, we'd need to properly populate the topology
	// This is a simplified test just to verify compilation

	assert.NotNil(t, reconciler)
	assert.Equal(t, chartPath, reconciler.ChartPath)
}

func TestKindToResource(t *testing.T) {
	tests := []struct {
		kind     string
		expected string
	}{
		{"Service", "services"},
		{"ServiceAccount", "serviceaccounts"},
		{"Deployment", "deployments"},
		{"ConfigMap", "configmaps"},
		{"Pod", "Pods"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			result := kindToResource(tt.kind)
			assert.Equal(t, tt.expected, result)
		})
	}
}
