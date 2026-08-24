package controlplane

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-logr/logr"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
)

const (
	fieldManager    = "kuadrant-operator"
	crdWaitTimeout  = 30 * time.Second
	crdWaitInterval = 1 * time.Second
)

var installOrder = map[string]int{
	"Namespace":                0,
	"ResourceQuota":            1,
	"LimitRange":               2,
	"ServiceAccount":           3,
	"Secret":                   4,
	"ConfigMap":                5,
	"StorageClass":             6,
	"PersistentVolume":         7,
	"PersistentVolumeClaim":    8,
	"CustomResourceDefinition": 9,
	"ClusterRole":              10,
	"ClusterRoleBinding":       11,
	"Role":                     12,
	"RoleBinding":              13,
	"Service":                  14,
	"DaemonSet":                15,
	"Pod":                      16,
	"Deployment":               17,
	"StatefulSet":              18,
	"Job":                      19,
	"CronJob":                  20,
}

type ResourceApplier struct {
	client    dynamic.Interface
	mapper    meta.RESTMapper
	logger    logr.Logger
	namespace string
}

func NewResourceApplier(client dynamic.Interface, discoveryClient discovery.DiscoveryInterface, namespace string, logger logr.Logger) *ResourceApplier {
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(
		memory.NewMemCacheClient(discoveryClient),
	)
	return &ResourceApplier{
		client:    client,
		mapper:    mapper,
		logger:    logger,
		namespace: namespace,
	}
}

func SortByInstallOrder(objects []*unstructured.Unstructured) {
	sort.SliceStable(objects, func(i, j int) bool {
		iOrder := kindOrder(objects[i].GetKind())
		jOrder := kindOrder(objects[j].GetKind())
		return iOrder < jOrder
	})
}

func kindOrder(kind string) int {
	if order, ok := installOrder[kind]; ok {
		return order
	}
	return len(installOrder)
}

func (a *ResourceApplier) ApplyResources(ctx context.Context, objects []*unstructured.Unstructured) error {
	for _, obj := range objects {
		if err := a.applyResource(ctx, obj); err != nil {
			return err
		}
	}
	return nil
}

func (a *ResourceApplier) applyResource(ctx context.Context, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	mapping, err := a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("mapping GVK %s: %w", gvk, err)
	}

	var rc dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		rc = a.client.Resource(mapping.Resource)
	} else {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = a.namespace
		}
		obj.SetNamespace(ns)
		rc = a.client.Resource(mapping.Resource).Namespace(ns)
	}

	a.logger.V(1).Info("applying resource",
		"kind", obj.GetKind(),
		"name", obj.GetName(),
		"namespace", obj.GetNamespace(),
	)

	_, err = rc.Apply(ctx, obj.GetName(), obj, metav1.ApplyOptions{
		FieldManager: fieldManager,
		Force:        true,
	})
	if err != nil {
		return fmt.Errorf("applying %s %s: %w", obj.GetKind(), obj.GetName(), err)
	}

	return nil
}

func (a *ResourceApplier) WaitForCRDs(ctx context.Context, crdNames []string) error {
	crdGVR := apiextv1.SchemeGroupVersion.WithResource("customresourcedefinitions")

	for _, name := range crdNames {
		a.logger.Info("waiting for CRD to be established", "crd", name)
		err := wait.PollUntilContextTimeout(ctx, crdWaitInterval, crdWaitTimeout, true,
			func(ctx context.Context) (bool, error) {
				obj, getErr := a.client.Resource(crdGVR).Get(ctx, name, metav1.GetOptions{})
				if getErr != nil {
					return false, nil
				}
				return isCRDEstablished(obj), nil
			},
		)
		if err != nil {
			return fmt.Errorf("CRD %s not established within %s: %w", name, crdWaitTimeout, err)
		}
		a.logger.Info("CRD established", "crd", name)
	}
	return nil
}

func isCRDEstablished(obj *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	for _, c := range conditions {
		condition, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if condition["type"] == string(apiextv1.Established) && condition["status"] == string(metav1.ConditionTrue) {
			return true
		}
	}
	return false
}

func CRDNames(crds []*unstructured.Unstructured) []string {
	names := make([]string, 0, len(crds))
	for _, crd := range crds {
		names = append(names, crd.GetName())
	}
	return names
}

func extractDeploymentImages(objects []*unstructured.Unstructured) []DeployedImage {
	var images []DeployedImage
	for _, obj := range objects {
		if obj.GetKind() != "Deployment" {
			continue
		}
		containers, found, err := unstructured.NestedSlice(obj.Object,
			"spec", "template", "spec", "containers")
		if err != nil || !found {
			continue
		}
		for _, c := range containers {
			container, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := container["name"].(string)
			image, _ := container["image"].(string)
			if image != "" {
				images = append(images, DeployedImage{Container: name, Image: image})
			}
		}
	}
	return images
}

// PatchDeploymentImage overrides the first container's image on all Deployment
// objects in the slice. This assumes every child operator chart uses a single
// Deployment with the operator binary as containers[0] — which holds for all
// current Kuadrant component charts.
//
// This is a stopgap for charts that don't support values-based image overrides.
// When upstream charts add image configurability, prefer passing the image via
// Component.ChartValues instead of post-render patching.
func PatchDeploymentImage(objects []*unstructured.Unstructured, image string) error {
	if image == "" {
		return nil
	}
	for _, obj := range objects {
		if obj.GetKind() != "Deployment" {
			continue
		}
		containers, found, err := unstructured.NestedSlice(obj.Object,
			"spec", "template", "spec", "containers")
		if err != nil || !found || len(containers) == 0 {
			continue
		}
		container, ok := containers[0].(map[string]interface{})
		if !ok {
			continue
		}
		container["image"] = image
		containers[0] = container
		if err := unstructured.SetNestedSlice(obj.Object,
			containers, "spec", "template", "spec", "containers"); err != nil {
			return fmt.Errorf("patching image on Deployment %s: %w", obj.GetName(), err)
		}
	}
	return nil
}
