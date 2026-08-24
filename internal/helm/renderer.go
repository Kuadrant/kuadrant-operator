package helm

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// RenderedChart holds the output of a Helm chart render, pre-split into
// CRDs and other resources. CRDs are sourced from both the standard
// crds/ directory and any CustomResourceDefinition objects in templates/.
type RenderedChart struct {
	CRDs         []*unstructured.Unstructured
	Resources    []*unstructured.Unstructured
	ChartVersion string
}

type Renderer struct {
	chartPath string
}

func NewRenderer(chartPath string) *Renderer {
	return &Renderer{chartPath: chartPath}
}

func (r *Renderer) Render(releaseName, namespace string, values map[string]any) (*RenderedChart, error) {
	chart, err := loader.Load(r.chartPath)
	if err != nil {
		return nil, fmt.Errorf("loading chart from %s: %w", r.chartPath, err)
	}

	install := action.NewInstall(&action.Configuration{})
	install.ClientOnly = true
	install.DryRun = true
	install.ReleaseName = releaseName
	install.Namespace = namespace
	install.DisableHooks = true

	rel, err := install.Run(chart, values)
	if err != nil {
		return nil, fmt.Errorf("rendering chart %s: %w", releaseName, err)
	}

	templateObjects, err := ParseManifests(rel.Manifest)
	if err != nil {
		return nil, err
	}

	result := &RenderedChart{
		ChartVersion: chart.Metadata.Version,
	}

	// Split template-rendered objects by kind.
	for _, obj := range templateObjects {
		if obj.GetKind() == "CustomResourceDefinition" {
			result.CRDs = append(result.CRDs, obj)
		} else {
			result.Resources = append(result.Resources, obj)
		}
	}

	// CRDs in the standard crds/ directory are not included in rel.Manifest.
	// Helm treats them separately — extract and append them.
	for _, crd := range chart.CRDObjects() {
		crdObjects, err := ParseManifests(string(crd.File.Data))
		if err != nil {
			return nil, fmt.Errorf("parsing CRD %s: %w", crd.Name, err)
		}
		result.CRDs = append(result.CRDs, crdObjects...)
	}

	// Override managed-by label on all rendered objects so they are clearly
	// identified as managed by the kuadrant-operator, not Helm.
	for _, obj := range result.CRDs {
		setManagedByLabel(obj)
	}
	for _, obj := range result.Resources {
		setManagedByLabel(obj)
	}

	return result, nil
}

func setManagedByLabel(obj *unstructured.Unstructured) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["app.kubernetes.io/managed-by"] = "kuadrant-operator"
	obj.SetLabels(labels)
}

func ParseManifests(manifest string) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(manifest), 4096)

	for {
		obj := &unstructured.Unstructured{}
		err := decoder.Decode(obj)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decoding manifest: %w", err)
		}
		if len(obj.Object) == 0 {
			continue
		}
		objects = append(objects, obj)
	}

	return objects, nil
}
