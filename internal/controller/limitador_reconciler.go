package controllers

import (
	"context"
	"sync"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrant "github.com/kuadrant/kuadrant-operator/internal/kuadrant"
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
			{Kind: ptr.To(v1beta1.LimitadorGroupKind), EventType: ptr.To(controller.DeleteEvent)},
		},
	}
}

func (r *LimitadorReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("LimitadorResourceReconciler")
	logger.Info("reconciling limitador resource", "status", "started")
	defer logger.Info("reconciling limitador resource", "status", "completed")

	kobj := GetKuadrantFromTopology(topology)
	if kobj == nil {
		return nil
	}

	lobjs := lo.FilterMap(topology.Objects().Objects().Items(), func(item machinery.Object, _ int) (machinery.Object, bool) {
		if item.GroupVersionKind().Kind == v1beta1.LimitadorGroupKind.Kind {
			return item, true
		}
		return nil, false
	})

	if len(lobjs) > 0 {
		logger.Info("limitador resource already exists, no need to create", "status", "skipping")
		return nil
	}

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
		Spec: limitadorv1alpha1.LimitadorSpec{},
	}

	unstructuredLimitador, err := controller.Destruct(limitador)
	if err != nil {
		logger.Error(err, "failed to destruct limitador", "status", "error")
		return err
	}
	logger.Info("creating limitador resource", "status", "processing")
	_, err = r.Client.Resource(v1beta1.LimitadorsResource).Namespace(limitador.Namespace).Create(ctx, unstructuredLimitador, metav1.CreateOptions{})
	if err != nil {
		if apiErrors.IsAlreadyExists(err) {
			logger.Info("already created limitador resource", "status", "acceptable")
		} else {
			logger.Error(err, "failed to create limitador resource", "status", "error")
			return err
		}
	}
	return nil
}
