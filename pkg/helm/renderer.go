package helm

import (
	"bytes"
	"fmt"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// Renderer renders Helm charts to Kubernetes manifests
type Renderer struct {
	chartPath string
}

// NewRenderer creates a new Helm chart renderer
func NewRenderer(chartPath string) *Renderer {
	return &Renderer{
		chartPath: chartPath,
	}
}

// Render renders a Helm chart with the given values and returns Kubernetes objects
func (r *Renderer) Render(releaseName, namespace string, values map[string]interface{}) ([]*unstructured.Unstructured, error) {
	// Load the chart
	chart, err := loader.Load(r.chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	// Validate chart values against schema
	if err := chartutil.ValidateAgainstSchema(chart, values); err != nil {
		return nil, fmt.Errorf("failed to validate values: %w", err)
	}

	// Configure action
	client := action.NewInstall(&action.Configuration{})
	client.ClientOnly = true  // Don't talk to Kubernetes
	client.DryRun = true       // Don't actually install
	client.ReleaseName = releaseName
	client.Namespace = namespace
	client.DisableHooks = true // Don't run hooks

	// Render the chart
	rel, err := client.Run(chart, values)
	if err != nil {
		return nil, fmt.Errorf("failed to render chart: %w", err)
	}

	// Parse rendered manifests into Unstructured objects
	return parseManifests(rel)
}

// parseManifests parses the rendered manifest string into Unstructured objects
func parseManifests(rel *release.Release) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured

	// Split manifest by YAML document separator
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(rel.Manifest)), 4096)

	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(obj); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("failed to decode manifest: %w", err)
		}

		// Skip empty documents
		if obj.GetKind() == "" {
			continue
		}

		objects = append(objects, obj)
	}

	return objects, nil
}

// RenderToObjects is a convenience wrapper that converts Unstructured to runtime.Objects
func (r *Renderer) RenderToObjects(releaseName, namespace string, values map[string]interface{}) ([]runtime.Object, error) {
	unstructuredObjs, err := r.Render(releaseName, namespace, values)
	if err != nil {
		return nil, err
	}

	objects := make([]runtime.Object, len(unstructuredObjs))
	for i, u := range unstructuredObjs {
		objects[i] = u
	}

	return objects, nil
}
