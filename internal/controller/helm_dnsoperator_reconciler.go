package controllers

import (
	"context"
	"fmt"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/helm"
)

// HelmDNSOperatorReconciler reconciles DNS Operator deployment using Helm charts
type HelmDNSOperatorReconciler struct {
	Client    *dynamic.DynamicClient
	ChartPath string
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services;serviceaccounts;configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete

func NewHelmDNSOperatorReconciler(client *dynamic.DynamicClient, chartPath string) *HelmDNSOperatorReconciler {
	return &HelmDNSOperatorReconciler{
		Client:    client,
		ChartPath: chartPath,
	}
}

func (r *HelmDNSOperatorReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(v1beta1.KuadrantGroupKind), EventType: ptr.To(controller.CreateEvent)},
			{Kind: ptr.To(v1beta1.KuadrantGroupKind), EventType: ptr.To(controller.UpdateEvent)},
		},
	}
}

func (r *HelmDNSOperatorReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	span := trace.SpanFromContext(ctx)
	logger := controller.LoggerFromContext(ctx).WithName("HelmDNSOperatorReconciler")
	logger.V(1).Info("reconciling dns-operator via helm", "status", "started")
	defer logger.V(1).Info("reconciling dns-operator via helm", "status", "completed")

	// Get Kuadrant CR from topology
	kuadrantObj := GetKuadrantFromTopology(topology, state)
	if kuadrantObj == nil {
		span.AddEvent("no kuadrant object found")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	logger = logger.WithValues("kuadrant", kuadrantObj.Namespace+"/"+kuadrantObj.Name)

	// Build Helm values
	values := r.buildHelmValues()

	// Render chart
	renderer := helm.NewRenderer(r.ChartPath)
	objects, err := renderer.Render("dns-operator", kuadrantObj.Namespace, values)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to render dns-operator chart")
		logger.Error(err, "failed to render dns-operator chart")
		return err
	}

	logger.Info("rendered dns-operator chart", "resourceCount", len(objects))

	// Apply each rendered resource using Server-Side Apply
	for _, obj := range objects {
		// Set owner reference to Kuadrant CR
		obj.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion: kuadrantObj.APIVersion,
				Kind:       kuadrantObj.Kind,
				Name:       kuadrantObj.Name,
				UID:        kuadrantObj.UID,
				Controller: ptr.To(true),
			},
		})

		gvr := obj.GroupVersionKind().GroupVersion().WithResource(kindToResource(obj.GetKind()))

		// ClusterRoleBindings are cluster-scoped, don't apply with namespace
		var resourceClient dynamic.ResourceInterface
		if obj.GetKind() == "ClusterRoleBinding" {
			resourceClient = r.Client.Resource(gvr)
		} else {
			resourceClient = r.Client.Resource(gvr).Namespace(kuadrantObj.Namespace)
		}

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
			span.RecordError(err)
			span.SetStatus(codes.Error, fmt.Sprintf("failed to apply %s/%s", obj.GetKind(), obj.GetName()))
			logger.Error(err, "failed to apply resource",
				"kind", obj.GetKind(),
				"name", obj.GetName(),
			)
			// Continue with other resources instead of failing entire reconciliation
			continue
		}

		logger.V(1).Info("applied resource",
			"kind", obj.GetKind(),
			"name", obj.GetName(),
		)
	}

	span.SetStatus(codes.Ok, "")
	logger.Info("dns-operator helm deployment reconciled successfully")

	return nil
}

func (r *HelmDNSOperatorReconciler) buildHelmValues() map[string]interface{} {
	return map[string]interface{}{
		"image": map[string]interface{}{
			"repository": "quay.io/kuadrant/dns-operator",
			"tag":        "latest",
			"pullPolicy": "IfNotPresent",
		},
		"rbac": map[string]interface{}{
			"install":               false,                // OLM installs ClusterRole from bundle
			"create":                true,                 // Chart creates ClusterRoleBinding
			"clusterRoleNamePrefix": "kuadrant-operator-", // Match Kustomize namePrefix
		},
		"serviceAccount": map[string]interface{}{
			"create": true,
			"name":   "",
		},
		"replicas": 1,
	}
}
