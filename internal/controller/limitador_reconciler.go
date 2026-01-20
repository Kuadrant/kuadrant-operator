package controllers

import (
	"context"
	"strings"
	"sync"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

type LimitadorReconciler struct {
	Client *dynamic.DynamicClient
}

//+kubebuilder:rbac:groups=limitador.kuadrant.io,resources=limitadors,verbs=get;list;watch;create;update;patch;delete

func NewLimitadorReconciler(client *dynamic.DynamicClient) *LimitadorReconciler {
	return &LimitadorReconciler{Client: client}
}

func (r *LimitadorReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(v1beta1.KuadrantGroupKind), EventType: ptr.To(controller.CreateEvent)},
			{Kind: ptr.To(v1beta1.KuadrantGroupKind), EventType: ptr.To(controller.UpdateEvent)},
			{Kind: ptr.To(v1beta1.LimitadorGroupKind)},
		},
	}
}

func (r *LimitadorReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	span := trace.SpanFromContext(ctx)
	logger := controller.LoggerFromContext(ctx).WithName("LimitadorResourceReconciler").WithValues("context", ctx)
	logger.Info("reconciling limitador resource", "status", "started")
	defer logger.Info("reconciling limitador resource", "status", "completed")

	kobj := GetKuadrantFromTopology(topology)
	if kobj == nil {
		span.AddEvent("no kuadrant object found")
		span.SetStatus(codes.Ok, "no kuadrant resource")
		return nil
	}

	span.SetAttributes(
		attribute.String("kuadrant.name", kobj.Name),
		attribute.String("kuadrant.namespace", kobj.Namespace),
	)

	// Build the desired Limitador spec
	desiredLimitador := &limitadorv1alpha1.Limitador{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Limitador",
			APIVersion: "limitador.kuadrant.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kuadrant.LimitadorName,
			Namespace: kobj.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         kobj.GroupVersionKind().GroupVersion().String(),
					Kind:               kobj.GroupVersionKind().Kind,
					Name:               kobj.Name,
					UID:                kobj.UID,
					BlockOwnerDeletion: ptr.To(true),
					Controller:         ptr.To(true),
				},
			},
		},
		Spec: limitadorv1alpha1.LimitadorSpec{
			MetricLabelsDefault: ptr.To("descriptors[1]"),
			Tracing: &limitadorv1alpha1.Tracing{
				Endpoint: "",
			},
		},
	}

	if kobj.Spec.Observability.Tracing != nil {
		desiredLimitador.Spec.Tracing.Endpoint = kobj.Spec.Observability.Tracing.DefaultEndpoint
	}

	unstructuredLimitador, err := controller.Destruct(desiredLimitador)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to destruct limitador")
		logger.Error(err, "failed to destruct limitador", "status", "error")
		return err
	}

	span.AddEvent("Applying Limitador resource")
	logger.Info("applying limitador resource")

	force := kobj.Spec.Observability.Tracing != nil && kobj.Spec.Observability.Tracing.DefaultEndpoint != ""

	_, err = r.Client.Resource(v1beta1.LimitadorsResource).Namespace(kobj.Namespace).Apply(
		ctx,
		unstructuredLimitador.GetName(),
		unstructuredLimitador,
		metav1.ApplyOptions{
			FieldManager: FieldManagerName,
			Force:        force,
		},
	)

	if err != nil {
		// Force was false initially (i.e., kuadrant tracing was nil or empty)
		if apiErrors.IsConflict(err) {
			statusErr, _ := err.(apiErrors.APIStatus)
			conflicts := statusErr.Status().Details.Causes

			// User has set tracing endpoint on limitador
			for _, cause := range conflicts {
				if cause.Field == ".spec.tracing.endpoint" {
					path := strings.Split(cause.Field, ".")
					unstructured.RemoveNestedField(unstructuredLimitador.Object, path[1:]...)
					logger.Info("Ceding ownership of conflicting field", "field", cause.Field)
					break
				}
			}

			_, err = r.Client.Resource(v1beta1.LimitadorsResource).Namespace(kobj.Namespace).Apply(
				ctx,
				unstructuredLimitador.GetName(),
				unstructuredLimitador,
				metav1.ApplyOptions{
					FieldManager: FieldManagerName,
					Force:        true,
				},
			)
			return err
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to apply limitador")
		logger.Error(err, "failed to apply limitador", "status", "error")
		return err
	}

	span.SetAttributes(
		attribute.String("limitador.name", desiredLimitador.Name),
		attribute.String("limitador.namespace", desiredLimitador.Namespace),
	)
	span.SetStatus(codes.Ok, "")
	logger.Info("limitador resource applied successfully")

	return nil
}
