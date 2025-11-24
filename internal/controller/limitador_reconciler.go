package controllers

import (
	"context"
	"sync"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	// Add Kuadrant attributes to span
	span.SetAttributes(
		attribute.String("kuadrant.name", kobj.Name),
		attribute.String("kuadrant.namespace", kobj.Namespace),
	)

	span.AddEvent("Creating/applying Limitador resource")

	limitador := &limitadorv1alpha1.Limitador{
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
		},
	}

	unstructuredLimitador, err := controller.Destruct(limitador)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to destruct limitador")
		logger.Error(err, "failed to destruct limitador", "status", "error")
		return err
	}
	logger.Info("applying limitador resource", "status", "processing")
	_, err = r.Client.Resource(v1beta1.LimitadorsResource).Namespace(limitador.Namespace).Apply(ctx, unstructuredLimitador.GetName(), unstructuredLimitador, metav1.ApplyOptions{Force: true, FieldManager: FieldManagerName})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to apply limitador")
		logger.Error(err, "failed to apply limitador resource", "status", "error")
		return err
	}

	span.SetAttributes(
		attribute.String("limitador.name", limitador.Name),
		attribute.String("limitador.namespace", limitador.Namespace),
	)
	span.SetStatus(codes.Ok, "")
	logger.Info("limitador resource applied successfully")

	return nil
}
