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
			{Kind: ptr.To(v1beta1.LimitadorGroupKind), EventType: ptr.To(controller.CreateEvent)},
			{Kind: ptr.To(v1beta1.LimitadorGroupKind), EventType: ptr.To(controller.UpdateEvent)},
		},
	}
}

func (r *HelmLimitadorReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	span := trace.SpanFromContext(ctx)
	logger := controller.LoggerFromContext(ctx).WithName("HelmLimitadorReconciler")
	logger.V(1).Info("reconciling limitador via helm", "status", "started")
	defer logger.V(1).Info("reconciling limitador via helm", "status", "completed")

	// Get Limitador CR from topology
	limitadorObj := GetLimitadorFromTopology(topology, state)
	if limitadorObj == nil {
		span.AddEvent("no limitador object found")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	logger = logger.WithValues("limitador", limitadorObj.Namespace+"/"+limitadorObj.Name)

	// Build Helm values from Limitador spec
	values := r.buildHelmValues(limitadorObj)

	// Render chart
	renderer := helm.NewRenderer(r.ChartPath)
	objects, err := renderer.Render("limitador", limitadorObj.Namespace, values)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to render limitador chart")
		logger.Error(err, "failed to render limitador chart")
		return err
	}

	logger.Info("rendered limitador chart", "resourceCount", len(objects))

	// Apply each rendered resource using Server-Side Apply
	for _, obj := range objects {
		// Set owner reference to Limitador CR
		obj.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion: limitadorObj.APIVersion,
				Kind:       limitadorObj.Kind,
				Name:       limitadorObj.Name,
				UID:        limitadorObj.UID,
				Controller: ptr.To(true),
			},
		})

		gvr := obj.GroupVersionKind().GroupVersion().WithResource(kindToResource(obj.GetKind()))

		_, err := r.Client.Resource(gvr).Namespace(limitadorObj.Namespace).Apply(
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
			return err
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

	// Use replicas from Limitador CR
	values["replicas"] = limitadorObj.GetReplicas()

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
