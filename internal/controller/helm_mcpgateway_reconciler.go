package controllers

import (
	"context"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	objects, err := renderer.Render("mcp-gateway", operatorNamespace, values)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to render mcp-gateway chart")
		logger.Error(err, "failed to render mcp-gateway chart")
		return err
	}

	logger.Info("rendered mcp-gateway chart", "resourceCount", len(objects))

	// Log all rendered resource types for debugging
	for _, obj := range objects {
		logger.V(1).Info("rendered resource", "kind", obj.GetKind(), "name", obj.GetName())
	}

	// Child operators are deployed at startup by DeployChildOperators.
	// This reconciler only needs to ensure the deployment is up to date
	// when the Kuadrant CR changes (e.g. image updates).
	if err := applyResources(ctx, r.Client, objects, operatorNamespace, logger); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to apply mcp-gateway resources")
		return err
	}

	span.SetStatus(codes.Ok, "")
	logger.Info("mcp-gateway reconciled successfully")

	return nil
}
