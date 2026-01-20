package controllers

import (
	"context"
	"sync"

	authorinoopapi "github.com/kuadrant/authorino-operator/api/v1beta1"
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
			{Kind: ptr.To(v1beta1.KuadrantGroupKind), EventType: ptr.To(controller.UpdateEvent)},
			{Kind: ptr.To(v1beta1.AuthorinoGroupKind)},
		},
	}
}

func (r *AuthorinoReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	span := trace.SpanFromContext(ctx)
	logger := controller.LoggerFromContext(ctx).WithName("AuthorinoReconciler").WithValues("context", ctx)
	logger.V(1).Info("reconciling authorino resource", "status", "started")
	defer logger.V(1).Info("reconciling authorino resource", "status", "completed")

	kobj := GetKuadrantFromTopology(topology)
	if kobj == nil {
		span.AddEvent("no kuadrant object found")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	// Add Kuadrant attributes to span
	span.SetAttributes(
		attribute.String("kuadrant.name", kobj.Name),
		attribute.String("kuadrant.namespace", kobj.Namespace),
	)

	clusterAuthorino := GetAuthorinoFromTopology(topology)

	if clusterAuthorino != nil {
		span.AddEvent("Authorino resource already exists")

		patch := &authorinoopapi.Authorino{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Authorino",
				APIVersion: "operator.authorino.kuadrant.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "authorino",
				Namespace: kobj.Namespace,
			},
			Spec: authorinoopapi.AuthorinoSpec{
				// Required fields for SSA to succeed as required fields cannot be omitted
				OIDCServer: authorinoopapi.OIDCServer{
					Tls: clusterAuthorino.Spec.OIDCServer.Tls,
				},
				Listener: authorinoopapi.Listener{
					Tls: clusterAuthorino.Spec.Listener.Tls,
				},
			},
		}

		if kobj.Spec.Observability.Tracing != nil {
			patch.Spec.Tracing = authorinoopapi.Tracing{
				Endpoint: kobj.Spec.Observability.Tracing.DefaultEndpoint,
				Insecure: kobj.Spec.Observability.Tracing.Insecure,
			}
		}

		unstructuredAuthorino, err := controller.Destruct(patch)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to destruct authorino")
			logger.Error(err, "failed to destruct authorino", "status", "error")
			return err
		}

		// This is required since Destruct resulted in the spec.tracing.insecure field being omitted
		if kobj.Spec.Observability.Tracing != nil {
			if err := unstructured.SetNestedField(unstructuredAuthorino.Object, kobj.Spec.Observability.Tracing.Insecure, "spec", "tracing", "insecure"); err != nil {
				return err
			}
		}

		force := kobj.Spec.Observability.Tracing != nil && (kobj.Spec.Observability.Tracing.DefaultEndpoint != "" || kobj.Spec.Observability.Tracing.Insecure)

		_, err = r.Client.Resource(v1beta1.AuthorinosResource).Namespace(kobj.Namespace).Apply(
			ctx,
			unstructuredAuthorino.GetName(),
			unstructuredAuthorino,
			metav1.ApplyOptions{
				FieldManager: FieldManagerName,
				Force:        force,
			},
		)

		if err != nil {
			// Force was false initially (i.e., kuadrant tracing was nil or empty)
			// User has set tracing endpoint on authorino
			if apiErrors.IsConflict(err) {
				logger.Info("Ceding ownership of conflicting of tracing fields")
				return nil
			}
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to apply authorino")
			logger.Error(err, "failed to apply authorino", "status", "error")
			return err
		}

		span.SetStatus(codes.Ok, "")
		logger.Info("authorino resource applied successfully")

		return nil
	}

	span.AddEvent("Creating new Authorino resource")

	authorino := &authorinoopapi.Authorino{
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
		Spec: authorinoopapi.AuthorinoSpec{
			ClusterWide:            true,
			SupersedingHostSubsets: true,
			Listener: authorinoopapi.Listener{
				Tls: authorinoopapi.Tls{
					Enabled: ptr.To(false),
				},
			},
			OIDCServer: authorinoopapi.OIDCServer{
				Tls: authorinoopapi.Tls{
					Enabled: ptr.To(false),
				},
			},
		},
	}

	if kobj.Spec.Observability.Tracing != nil {
		authorino.Spec.Tracing = authorinoopapi.Tracing{
			Endpoint: kobj.Spec.Observability.Tracing.DefaultEndpoint,
			Insecure: kobj.Spec.Observability.Tracing.Insecure,
		}
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
			span.SetStatus(codes.Ok, "")
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
		span.SetStatus(codes.Ok, "")
		logger.Info("created authorino resource", "status", "acceptable")
	}

	return nil
}
