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
	objects, err := renderer.Render("dns-operator", kuadrantObj.Namespace, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to render dns-operator chart")
		logger.Error(err, "failed to render dns-operator chart")
		return err
	}

	logger.Info("rendered dns-operator chart", "resourceCount", len(objects))

	// Apply each rendered resource using Server-Side Apply
	for _, obj := range objects {
		if shouldSkipResource(obj.GetKind()) {
			logger.Info("skipping cluster-scoped resource managed by installer",
				"kind", obj.GetKind(), "name", obj.GetName())
			continue
		}

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

		_, err := resourceClient.Apply(
			ctx,
			obj.GetName(),
			obj,
			metav1.ApplyOptions{
				FieldManager: FieldManagerName,
				Force:        false, // Only own fields we explicitly set
			},
		)

		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, fmt.Sprintf("failed to apply %s/%s", obj.GetKind(), obj.GetName()))

			// Handle conflicts specially - these indicate user customization
			if apierrors.IsConflict(err) {
				logger.Info("field ownership conflict detected - preserving user customization",
					"kind", obj.GetKind(),
					"name", obj.GetName(),
					"message", "This resource has fields owned by another manager (likely user customization). "+
						"User's values will be preserved. Kuadrant only manages: image, args, serviceAccountName. "+
						"See docs/helm-minimal-ownership.md for details.",
				)
			} else {
				logger.Error(err, "failed to apply resource",
					"kind", obj.GetKind(),
					"name", obj.GetName(),
				)
			}
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
