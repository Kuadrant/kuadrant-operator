package controllers

import (
	"context"
	"sync"

	v1beta2 "github.com/kuadrant/authorino-operator/api/v1beta1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

type AuthorinoReconciler struct {
	Client *dynamic.DynamicClient
}

//+kubebuilder:rbac:groups=operator.authorino.kuadrant.io,resources=authorinos,verbs=get;list;watch;create;update;delete;patch

func NewAuthorinoReconciler(client *dynamic.DynamicClient) *AuthorinoReconciler {
	return &AuthorinoReconciler{Client: client}
}

func (r *AuthorinoReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(v1beta1.KuadrantGroupKind), EventType: ptr.To(controller.CreateEvent)},
			{Kind: ptr.To(v1beta1.AuthorinoGroupKind), EventType: ptr.To(controller.DeleteEvent)},
		},
	}
}

func (r *AuthorinoReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	tracer := otel.Tracer("kuadrant-operator")
	ctx, span := tracer.Start(ctx, "AuthorinoReconciler.Reconcile")
	defer span.End()

	logger := controller.LoggerFromContext(ctx).WithValues("context", ctx).WithName("AuthorinoReconciler")
	logger.V(1).Info("reconciling authorino resource", "status", "started")
	defer logger.V(1).Info("reconciling authorino resource", "status", "completed")

	kobj := GetKuadrantFromTopology(topology)
	if kobj == nil {
		span.SetStatus(codes.Ok, "no Kuadrant object found")
		return nil
	}

	// Add Kuadrant attributes to span
	span.SetAttributes(
		attribute.String("kuadrant.name", kobj.Name),
		attribute.String("kuadrant.namespace", kobj.Namespace),
	)

	aobjs := lo.FilterMap(topology.Objects().Objects().Children(kobj), func(item machinery.Object, _ int) (machinery.Object, bool) {
		if item.GroupVersionKind().Kind == v1beta1.AuthorinoGroupKind.Kind {
			return item, true
		}
		return nil, false
	})

	if len(aobjs) > 0 {
		span.AddEvent("Authorino resource already exists")
		span.SetStatus(codes.Ok, "authorino exists, skipping creation")
		logger.V(1).Info("authorino resource already exists, no need to create", "status", "skipping")
		return nil
	}

	span.AddEvent("Creating new Authorino resource")

	authorino := &v1beta2.Authorino{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Authorino",
			APIVersion: "operator.authorino.kuadrant.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "authorino",
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
		Spec: v1beta2.AuthorinoSpec{
			ClusterWide:            true,
			SupersedingHostSubsets: true,
			Listener: v1beta2.Listener{
				Tls: v1beta2.Tls{
					Enabled: ptr.To(false),
				},
			},
			OIDCServer: v1beta2.OIDCServer{
				Tls: v1beta2.Tls{
					Enabled: ptr.To(false),
				},
			},
		},
	}

	unstructuredAuthorino, err := controller.Destruct(authorino)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to destruct authorino")
		logger.Error(err, "failed to destruct authorino", "status", "error")
		return err
	}

	logger.V(1).Info("creating authorino resource", "status", "processing")
	_, err = r.Client.Resource(v1beta1.AuthorinosResource).Namespace(authorino.Namespace).Create(ctx, unstructuredAuthorino, metav1.CreateOptions{})
	if err != nil {
		if apiErrors.IsAlreadyExists(err) {
			span.SetStatus(codes.Ok, "authorino already exists")
			logger.V(1).Info("already created authorino resource", "status", "acceptable")
		} else {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to create authorino")
			logger.Error(err, "failed to create authorino resource", "status", "error")
			return err
		}
	} else {
		span.AddEvent("Authorino resource created successfully")
		span.SetAttributes(
			attribute.String("authorino.name", authorino.Name),
			attribute.String("authorino.namespace", authorino.Namespace),
		)
		span.SetStatus(codes.Ok, "created authorino resource")
		logger.Info("created authorino resource", "status", "acceptable")
	}

	return nil
}
