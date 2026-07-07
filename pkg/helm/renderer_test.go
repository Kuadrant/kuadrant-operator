package helm

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRenderer_RenderAuthorino(t *testing.T) {
	chartPath := filepath.Join("..", "..", "charts", "authorino")
	renderer := NewRenderer(chartPath)

	values := map[string]interface{}{
		"image": map[string]interface{}{
			"repository": "quay.io/kuadrant/authorino",
			"tag":        "v0.19.0",
		},
		"replicas": 2,
	}

	objects, err := renderer.Render("test-authorino", "kuadrant-system", values)
	require.NoError(t, err)
	require.NotEmpty(t, objects)

	// Verify we got the expected resources
	kinds := make(map[string]int)
	for _, obj := range objects {
		kinds[obj.GetKind()]++
	}

	assert.Equal(t, 1, kinds["ServiceAccount"], "should have 1 ServiceAccount")
	assert.Equal(t, 1, kinds["Deployment"], "should have 1 Deployment")
	assert.Equal(t, 2, kinds["Service"], "should have 2 Services (auth + oidc)")

	// Verify Deployment has correct values
	for _, obj := range objects {
		if obj.GetKind() == "Deployment" {
			replicas, found, err := unstructured.NestedInt64(obj.Object, "spec", "replicas")
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, int64(2), replicas, "replicas should be 2")

			containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
			require.NoError(t, err)
			require.True(t, found)
			require.Len(t, containers, 1)

			container := containers[0].(map[string]interface{})
			assert.Equal(t, "quay.io/kuadrant/authorino:v0.19.0", container["image"])
		}
	}
}

func TestRenderer_RenderLimitador(t *testing.T) {
	chartPath := filepath.Join("..", "..", "charts", "limitador")
	renderer := NewRenderer(chartPath)

	values := map[string]interface{}{
		"image": map[string]interface{}{
			"repository": "quay.io/kuadrant/limitador",
			"tag":        "v1.8.0",
		},
		"replicas": 3,
		"storage": map[string]interface{}{
			"type": "memory",
		},
	}

	objects, err := renderer.Render("test-limitador", "kuadrant-system", values)
	require.NoError(t, err)
	require.NotEmpty(t, objects)

	// Verify we got the expected resources
	kinds := make(map[string]int)
	for _, obj := range objects {
		kinds[obj.GetKind()]++
	}

	assert.Equal(t, 1, kinds["ServiceAccount"], "should have 1 ServiceAccount")
	assert.Equal(t, 1, kinds["Deployment"], "should have 1 Deployment")
	assert.Equal(t, 1, kinds["Service"], "should have 1 Service")
	assert.Equal(t, 1, kinds["ConfigMap"], "should have 1 ConfigMap")
}
