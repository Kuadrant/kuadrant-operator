package controllers

import (
	"context"
	"fmt"
	"sync"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
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

// HelmLimitadorReconciler reconciles Limitador deployment using Helm charts
// instead of creating a Limitador CR
type HelmLimitadorReconciler struct {
	Client    *dynamic.DynamicClient
	ChartPath string
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services;serviceaccounts;configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=limitador.kuadrant.io,resources=limitadors,verbs=get;list;watch

func NewHelmLimitadorReconciler(client *dynamic.DynamicClient, chartPath string) *HelmLimitadorReconciler {
	return &HelmLimitadorReconciler{
		Client:    client,
		ChartPath: chartPath,
	}
}

func (r *HelmLimitadorReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			// Watch Kuadrant CR (primary trigger)
			{Kind: ptr.To(v1beta1.KuadrantGroupKind), EventType: ptr.To(controller.CreateEvent)},
			{Kind: ptr.To(v1beta1.KuadrantGroupKind), EventType: ptr.To(controller.UpdateEvent)},
			// Also watch Limitador CR for migration detection
			{Kind: ptr.To(v1beta1.LimitadorGroupKind), EventType: ptr.To(controller.CreateEvent)},
			{Kind: ptr.To(v1beta1.LimitadorGroupKind), EventType: ptr.To(controller.UpdateEvent)},
			{Kind: ptr.To(v1beta1.LimitadorGroupKind), EventType: ptr.To(controller.DeleteEvent)},
		},
	}
}

func (r *HelmLimitadorReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	span := trace.SpanFromContext(ctx)
	logger := controller.LoggerFromContext(ctx).WithName("HelmLimitadorReconciler")
	logger.V(1).Info("reconciling limitador via helm", "status", "started")
	defer logger.V(1).Info("reconciling limitador via helm", "status", "completed")

	// Get Kuadrant CR (primary resource we reconcile from)
	kuadrantObj := GetKuadrantFromTopology(topology, state)
	if kuadrantObj == nil {
		span.AddEvent("no kuadrant object found")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	logger = logger.WithValues("kuadrant", kuadrantObj.Namespace+"/"+kuadrantObj.Name)

	// Check for Limitador wrapper CR
	// Keep wrapper CR in topology for migration detection in future task
	limitadorWrapperObj := GetLimitadorFromTopology(topology, state)
	if limitadorWrapperObj == nil {
		logger.V(1).Info("no limitador wrapper CR found, using defaults")
	}

	// Build Helm values from wrapper CR if it exists, otherwise use defaults
	var values map[string]interface{}
	if limitadorWrapperObj != nil {
		logger.V(1).Info("building values from Limitador wrapper CR", "wrapper", limitadorWrapperObj.Namespace+"/"+limitadorWrapperObj.Name)
		values = r.buildHelmValues(limitadorWrapperObj)
	} else {
		logger.V(1).Info("building default values (no wrapper CR)")
		values = r.buildDefaultHelmValues()
	}

	// Render chart
	renderer := helm.NewRenderer(r.ChartPath)
	objects, err := renderer.Render("limitador", kuadrantObj.Namespace, values)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to render limitador chart")
		logger.Error(err, "failed to render limitador chart")
		return err
	}

	logger.Info("rendered limitador chart", "resourceCount", len(objects))

	// Apply each rendered resource using Server-Side Apply
	for _, obj := range objects {
		// Set owner reference to Kuadrant CR (not wrapper CR)
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

		_, err := r.Client.Resource(gvr).Namespace(kuadrantObj.Namespace).Apply(
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
	logger.Info("limitador helm deployment reconciled successfully")

	return nil
}

func (r *HelmLimitadorReconciler) buildHelmValues(limitadorObj *limitadorv1alpha1.Limitador) map[string]interface{} {
	values := map[string]interface{}{
		"image": map[string]interface{}{
			"repository": "quay.io/kuadrant/limitador",
			"tag":        "latest",
			"pullPolicy": "IfNotPresent",
		},
		"storage": map[string]interface{}{
			"type": "memory",
		},
	}

	// Only set replicas if explicitly specified in CR
	// GetReplicas() returns 1 if nil, but we want to distinguish between
	// "user set to 1" vs "user didn't set (allow free scaling)"
	if limitadorObj.Spec.Replicas != nil {
		values["replicas"] = *limitadorObj.Spec.Replicas
	}

	// Use custom image if specified
	if limitadorObj.Spec.Image != nil {
		imageParts := splitImageString(*limitadorObj.Spec.Image)
		values["image"] = imageParts
	}

	// Use storage configuration if specified
	if limitadorObj.Spec.Storage != nil {
		storageType := "memory" // default
		if limitadorObj.Spec.Storage.Redis != nil {
			if limitadorObj.Spec.Storage.Redis.ConfigSecretRef != nil {
				storageType = "redis"
			}
		} else if limitadorObj.Spec.Storage.RedisCached != nil {
			if limitadorObj.Spec.Storage.RedisCached.ConfigSecretRef != nil {
				storageType = "redis-cached"
			}
		} else if limitadorObj.Spec.Storage.Disk != nil {
			storageType = "disk"
		}
		values["storage"] = map[string]interface{}{
			"type": storageType,
		}
	}

	return values
}

func (r *HelmLimitadorReconciler) buildDefaultHelmValues() map[string]interface{} {
	// Minimal values when no wrapper CR exists
	// Let Helm chart's values.yaml provide the defaults for everything else
	return map[string]interface{}{
		// Everything (image, storage, etc.) comes from chart's values.yaml
		// Empty map means "use chart defaults"
	}
}
