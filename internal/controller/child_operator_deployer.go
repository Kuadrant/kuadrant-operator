package controllers

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-logr/logr"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"

	"github.com/kuadrant/kuadrant-operator/pkg/helm"
)

//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;escalate
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services;serviceaccounts;configmaps,verbs=get;list;watch;create;update;patch;delete

type childOperator struct {
	name          string
	chartPath     string
	operatorImage string
	relatedImages map[string]string
	chartValues   map[string]interface{}
}

// DeployChildOperators renders and applies all child operator charts at startup.
// CRDs are applied first and waited on, then all other resources.
// All resources are deployed into the operator's namespace.
func DeployChildOperators(ctx context.Context, client *dynamic.DynamicClient, namespace string, chartsBasePath string, logger logr.Logger) error {
	logger.Info("deploying child operators", "namespace", namespace, "chartsBasePath", chartsBasePath)

	children := []childOperator{
		{
			name:          "authorino-operator",
			chartPath:     chartsBasePath + "/authorino-operator",
			operatorImage: AuthorinoOperatorImage,
			relatedImages: map[string]string{
				"RELATED_IMAGE_AUTHORINO": AuthorinoImage,
			},
		},
		{
			name:          "limitador-operator",
			chartPath:     chartsBasePath + "/limitador-operator",
			operatorImage: LimitadorOperatorImage,
			relatedImages: map[string]string{
				"RELATED_IMAGE_LIMITADOR": LimitadorImage,
			},
		},
		{
			name:          "dns-operator",
			chartPath:     chartsBasePath + "/dns-operator",
			operatorImage: DNSOperatorImage,
		},
		{
			name:          "mcp-gateway",
			chartPath:     chartsBasePath + "/mcp-gateway",
			operatorImage: MCPGatewayImage,
			chartValues: map[string]interface{}{
				"mcpGatewayExtension": map[string]interface{}{"create": false},
			},
		},
	}

	var allCRDs []*unstructured.Unstructured
	var allOther []*unstructured.Unstructured

	for _, child := range children {
		renderer := helm.NewRenderer(child.chartPath)
		objects, err := renderer.Render(child.name, namespace, child.chartValues)
		if err != nil {
			return fmt.Errorf("failed to render chart %s: %w", child.name, err)
		}

		logger.Info("rendered child operator chart", "name", child.name, "resourceCount", len(objects))

		for _, obj := range objects {
			patchDeploymentImage(obj, child.operatorImage, child.relatedImages)

			if isCRD(obj.GetKind()) {
				allCRDs = append(allCRDs, obj)
			} else {
				allOther = append(allOther, obj)
			}
		}
	}

	// Phase 1: Apply CRDs first
	if len(allCRDs) > 0 {
		logger.Info("applying child operator CRDs", "count", len(allCRDs))
		if err := applyResources(ctx, client, allCRDs, namespace, logger); err != nil {
			return fmt.Errorf("failed to apply CRDs: %w", err)
		}

		// Wait for CRDs to be established
		if err := waitForCRDs(ctx, client, allCRDs, logger); err != nil {
			return fmt.Errorf("CRDs not established: %w", err)
		}
	}

	// Phase 2: Apply all other resources (sorted: ClusterRoles before bindings before deployments)
	if len(allOther) > 0 {
		sortByApplyOrder(allOther)
		logger.Info("applying child operator resources", "count", len(allOther))
		if err := applyResources(ctx, client, allOther, namespace, logger); err != nil {
			return fmt.Errorf("failed to apply resources: %w", err)
		}
	}

	logger.Info("child operators deployed successfully")
	return nil
}

func applyResources(ctx context.Context, client *dynamic.DynamicClient, objects []*unstructured.Unstructured, namespace string, logger logr.Logger) error {
	for _, obj := range objects {
		gvr := obj.GroupVersionKind().GroupVersion().WithResource(kindToResource(obj.GetKind()))

		var resourceClient dynamic.ResourceInterface
		if isClusterScoped(obj.GetKind()) {
			resourceClient = client.Resource(gvr)
		} else {
			resourceClient = client.Resource(gvr).Namespace(namespace)
		}

		logger.V(1).Info("applying resource", "kind", obj.GetKind(), "name", obj.GetName())

		_, err := resourceClient.Apply(
			ctx,
			obj.GetName(),
			obj,
			metav1.ApplyOptions{
				FieldManager: FieldManagerName,
				Force:        true,
			},
		)
		if err != nil {
			if apierrors.IsConflict(err) {
				logger.Info("field ownership conflict, skipping", "kind", obj.GetKind(), "name", obj.GetName())
				continue
			}
			return fmt.Errorf("failed to apply %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}
	}
	return nil
}

func waitForCRDs(ctx context.Context, client *dynamic.DynamicClient, crds []*unstructured.Unstructured, logger logr.Logger) error {
	crdGVR := schema.GroupVersionResource{
		Group:    apiextv1.SchemeGroupVersion.Group,
		Version:  apiextv1.SchemeGroupVersion.Version,
		Resource: "customresourcedefinitions",
	}

	for _, crd := range crds {
		name := crd.GetName()
		logger.Info("waiting for CRD to be established", "name", name)

		err := wait.PollUntilContextTimeout(ctx, time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			obj, err := client.Resource(crdGVR).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}

			conditions, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
			if !found {
				return false, nil
			}

			for _, c := range conditions {
				cond := c.(map[string]interface{})
				if cond["type"] == "Established" && cond["status"] == "True" {
					return true, nil
				}
			}
			return false, nil
		})

		if err != nil {
			return fmt.Errorf("CRD %s not established: %w", name, err)
		}
		logger.Info("CRD established", "name", name)
	}
	return nil
}

// sortByApplyOrder sorts resources so dependencies are applied first:
// ClusterRoles -> ServiceAccounts -> Roles -> RoleBindings -> ClusterRoleBindings -> ConfigMaps -> Services -> Deployments
func sortByApplyOrder(objects []*unstructured.Unstructured) {
	order := map[string]int{
		"ClusterRole":        0,
		"ServiceAccount":     1,
		"Role":               2,
		"RoleBinding":        3,
		"ClusterRoleBinding": 4,
		"ConfigMap":          5,
		"Service":            6,
		"Deployment":         7,
	}

	sort.Slice(objects, func(i, j int) bool {
		oi, ok := order[objects[i].GetKind()]
		if !ok {
			oi = 99
		}
		oj, ok := order[objects[j].GetKind()]
		if !ok {
			oj = 99
		}
		return oi < oj
	})
}
