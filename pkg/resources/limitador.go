package resources

import (
	"context"
	"fmt"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

type LimitadorCrReconciler struct {
	Client *dynamic.DynamicClient
}

func NewLimitadorCrReconciler(client *dynamic.DynamicClient) *LimitadorCrReconciler {
	return &LimitadorCrReconciler{Client: client}
}

func (r *LimitadorCrReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(v1beta1.KuadrantKind), EventType: ptr.To(controller.CreateEvent)},
			{Kind: ptr.To(v1beta1.LimitadorKind), EventType: ptr.To(controller.DeleteEvent)},
		},
	}
}

func (r *LimitadorCrReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error) {
	logger := controller.LoggerFromContext(ctx).WithName("LimitadorResourceReconciler")
	logger.Info("reconciling limtador resource", "status", "started")
	defer logger.Info("reconciling limitador resource", "status", "completed")

	kobjs := lo.FilterMap(topology.Objects().Roots(), func(item machinery.Object, _ int) (*v1beta1.Kuadrant, bool) {
		if item.GroupVersionKind().Kind == v1beta1.KuadrantKind.Kind {
			return item.(*v1beta1.Kuadrant), true
		}
		return nil, false
	})
	if len(kobjs) == 0 {
		logger.Info("no kuadrant resources found", "status", "skipping")
		return
	}
	if len(kobjs) > 1 {
		logger.Error(fmt.Errorf("multiple Kuadrant resources found"), "cannot select root Kuadrant resource", "status", "error")
	}
	kobj := kobjs[0]

	if kobj.GetDeletionTimestamp() != nil {
		logger.Info("root kuadrant marked for deletion", "status", "skipping")
		return
	}

	aobjs := lo.FilterMap(topology.Objects().Objects().Items(), func(item machinery.Object, _ int) (machinery.Object, bool) {
		if item.GroupVersionKind().Kind == v1beta1.LimitadorKind.Kind {
			return item, true
		}
		return nil, false
	})

	if len(aobjs) > 0 {
		logger.Info("limitador resource already exists, no need to create", "status", "skipping")
		return
	}

	limitador := &limitadorv1alpha1.Limitador{
		TypeMeta: v1.TypeMeta{
			Kind:       "Limitador",
			APIVersion: "limitador.kuadrant.io/v1alpha1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      common.LimitadorName,
			Namespace: kobj.Namespace,
			OwnerReferences: []v1.OwnerReference{
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
	}
	logger.Info("creating limitador resource", "status", "processing")
	_, err = r.Client.Resource(v1beta1.LimitadorResource).Namespace(limitador.Namespace).Create(ctx, unstructuredLimitador, v1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("already created limitador resource", "status", "acceptable")
		} else {
			logger.Error(err, "failed to create limitador resource", "status", "error")
		}
	}
}
