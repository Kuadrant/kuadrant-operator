package controllers

import (
	"context"
	"fmt"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/helm"
)

type HelmMCPGatewayReconciler struct {
	Client    *dynamic.DynamicClient
	ChartPath string
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services;serviceaccounts;configmaps;secrets;pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=bind;escalate,resourceNames=mcp-gateway-controller
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete

func NewHelmMCPGatewayReconciler(client *dynamic.DynamicClient, chartPath string) *HelmMCPGatewayReconciler {
	return &HelmMCPGatewayReconciler{
		Client:    client,
		ChartPath: chartPath,
	}
}

func (r *HelmMCPGatewayReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(v1beta1.KuadrantGroupKind), EventType: ptr.To(controller.CreateEvent)},
			{Kind: ptr.To(v1beta1.KuadrantGroupKind), EventType: ptr.To(controller.UpdateEvent)},
		},
	}
}

func (r *HelmMCPGatewayReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	span := trace.SpanFromContext(ctx)
	logger := controller.LoggerFromContext(ctx).WithName("HelmMCPGatewayReconciler")
	logger.V(1).Info("reconciling mcp-gateway via helm", "status", "started")
	defer logger.V(1).Info("reconciling mcp-gateway via helm", "status", "completed")

	kuadrantObj := GetKuadrantFromTopology(topology, state)
	if kuadrantObj == nil {
		span.AddEvent("no kuadrant object found")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	logger = logger.WithValues("kuadrant", kuadrantObj.Namespace+"/"+kuadrantObj.Name)

	renderer := helm.NewRenderer(r.ChartPath)
	mcpRepo, mcpTag := splitImageRef(MCPGatewayImage)
	values := map[string]interface{}{
		"mcpGatewayExtension": map[string]interface{}{
			"create": false,
		},
		"imageController": map[string]interface{}{
			"repository": mcpRepo,
			"tag":        mcpTag,
		},
	}
	objects, err := renderer.Render("mcp-gateway", kuadrantObj.Namespace, values)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to render mcp-gateway chart")
		logger.Error(err, "failed to render mcp-gateway chart")
		return err
	}

	logger.Info("rendered mcp-gateway chart", "resourceCount", len(objects))

	for _, obj := range objects {
		if shouldSkipResource(obj.GetKind()) {
			logger.Info("skipping cluster-scoped resource managed by installer",
				"kind", obj.GetKind(), "name", obj.GetName())
			continue
		}

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

		var resourceClient dynamic.ResourceInterface
		if obj.GetKind() == "ClusterRoleBinding" {
			resourceClient = r.Client.Resource(gvr)
		} else {
			resourceClient = r.Client.Resource(gvr).Namespace(kuadrantObj.Namespace)
		}

		logger.V(1).Info("applying resource via SSA",
			"kind", obj.GetKind(),
			"name", obj.GetName(),
			"namespace", kuadrantObj.Namespace,
			"gvr", gvr.String(),
			"fieldManager", FieldManagerName,
		)

		force := obj.GetKind() == "ClusterRoleBinding"

		_, err := resourceClient.Apply(
			ctx,
			obj.GetName(),
			obj,
			metav1.ApplyOptions{
				FieldManager: FieldManagerName,
				Force:        force,
			},
		)

		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, fmt.Sprintf("failed to apply %s/%s", obj.GetKind(), obj.GetName()))

			if apierrors.IsConflict(err) {
				logger.Info("field ownership conflict detected - preserving user customization",
					"kind", obj.GetKind(),
					"name", obj.GetName(),
				)
			} else {
				logger.Error(err, "failed to apply resource",
					"kind", obj.GetKind(),
					"name", obj.GetName(),
				)
			}
			continue
		}

		logger.V(1).Info("applied resource",
			"kind", obj.GetKind(),
			"name", obj.GetName(),
		)
	}

	span.SetStatus(codes.Ok, "")
	logger.Info("mcp-gateway helm deployment reconciled successfully")

	return nil
}
