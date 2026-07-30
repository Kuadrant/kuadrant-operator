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

// HelmDNSOperatorReconciler reconciles DNS Operator deployment using Helm charts
type HelmDNSOperatorReconciler struct {
	Client    *dynamic.DynamicClient
	ChartPath string
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services;serviceaccounts;configmaps;secrets;pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=bind;escalate,resourceNames=dns-operator-manager-role;dns-operator-remote-cluster-role
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete

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

	// Render chart
	renderer := helm.NewRenderer(r.ChartPath)
	objects, err := renderer.Render("dns-operator", operatorNamespace, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to render dns-operator chart")
		logger.Error(err, "failed to render dns-operator chart")
		return err
	}

	logger.Info("rendered dns-operator chart", "resourceCount", len(objects))

	// Log all rendered resource types for debugging
	for _, obj := range objects {
		logger.V(1).Info("rendered resource", "kind", obj.GetKind(), "name", obj.GetName())
	}

	// Patch images on rendered objects
	for _, obj := range objects {
		patchDeploymentImage(obj, DNSOperatorImage, nil)
	}

	// Child operators are deployed at startup by DeployChildOperators.
	// This reconciler only needs to ensure the deployment is up to date
	// when the Kuadrant CR changes (e.g. image updates).
	if err := applyResources(ctx, r.Client, objects, operatorNamespace, logger); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to apply dns-operator resources")
		return err
	}

	span.SetStatus(codes.Ok, "")
	logger.Info("dns-operator reconciled successfully")

	return nil
}
