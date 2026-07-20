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

// HelmLimitadorOperatorReconciler reconciles Limitador Operator deployment using Helm charts
type HelmLimitadorOperatorReconciler struct {
	Client    *dynamic.DynamicClient
	ChartPath string
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services;serviceaccounts;configmaps;secrets;pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=bind;escalate,resourceNames=limitador-operator-manager-role
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete

func NewHelmLimitadorOperatorReconciler(client *dynamic.DynamicClient, chartPath string) *HelmLimitadorOperatorReconciler {
	return &HelmLimitadorOperatorReconciler{
		Client:    client,
		ChartPath: chartPath,
	}
}

func (r *HelmLimitadorOperatorReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(v1beta1.KuadrantGroupKind), EventType: ptr.To(controller.CreateEvent)},
			{Kind: ptr.To(v1beta1.KuadrantGroupKind), EventType: ptr.To(controller.UpdateEvent)},
		},
	}
}

func (r *HelmLimitadorOperatorReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	span := trace.SpanFromContext(ctx)
	logger := controller.LoggerFromContext(ctx).WithName("HelmLimitadorOperatorReconciler")
	logger.V(1).Info("reconciling limitador-operator via helm", "status", "started")
	defer logger.V(1).Info("reconciling limitador-operator via helm", "status", "completed")

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
	objects, err := renderer.Render("limitador-operator", kuadrantObj.Namespace, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to render limitador-operator chart")
		logger.Error(err, "failed to render limitador-operator chart")
		return err
	}

	logger.Info("rendered limitador-operator chart", "resourceCount", len(objects))

	// Apply each rendered resource using Server-Side Apply
	for _, obj := range objects {
		if shouldSkipResource(obj.GetKind()) {
			logger.Info("skipping cluster-scoped resource managed by installer",
				"kind", obj.GetKind(), "name", obj.GetName())
			continue
		}

		patchDeploymentImage(obj, LimitadorOperatorImage, map[string]string{
			"RELATED_IMAGE_LIMITADOR": LimitadorImage,
		})

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

		logger.V(1).Info("applying resource via SSA",
			"kind", obj.GetKind(),
			"name", obj.GetName(),
			"namespace", kuadrantObj.Namespace,
			"gvr", gvr.String(),
			"fieldManager", FieldManagerName,
		)

		// Use Force: true for cluster-scoped resources to avoid "not found" errors
		// when the resource doesn't exist yet
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
	logger.Info("limitador-operator helm deployment reconciled successfully")

	return nil
}
