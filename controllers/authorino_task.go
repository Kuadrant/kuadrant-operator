package controllers

import (
	"context"
	v1beta2 "github.com/kuadrant/authorino-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
)

type AuthorinoCrReconciler struct {
	Client *dynamic.DynamicClient
}

func NewAuthorinoCrReconciler(client *dynamic.DynamicClient) *AuthorinoCrReconciler {
	return &AuthorinoCrReconciler{Client: client}
}

func (r *AuthorinoCrReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(v1beta1.KuadrantKind), EventType: ptr.To(controller.CreateEvent)},
			{Kind: ptr.To(v1beta1.AuthorinoKind), EventType: ptr.To(controller.DeleteEvent)},
		},
	}
}

func (r *AuthorinoCrReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error) {
	logger := controller.LoggerFromContext(ctx).WithName("AuthorinoCrReconciler")
	logger.Info("reconciling authorino resource", "status", "started")
	defer logger.Info("reconciling authorino resource", "status", "completed")

	kobjs := lo.FilterMap(topology.Objects().Roots(), func(item machinery.Object, _ int) (*v1beta1.Kuadrant, bool) {
		if item.GroupVersionKind().Kind == v1beta1.KuadrantKind.Kind {
			return item.(*v1beta1.Kuadrant), true
		}
		return nil, false
	})

	kobj, err := GetOldestKuadrant(kobjs)
	if err != nil {
		logger.Error(err, "cannot find Kuadrant resource", "status", "error")
	}

	if kobj.GetDeletionTimestamp() != nil {
		logger.Info("root kuadrant marked for deletion", "status", "skipping")
		return
	}

	aobjs := lo.FilterMap(topology.Objects().Objects().Items(), func(item machinery.Object, _ int) (machinery.Object, bool) {
		if item.GroupVersionKind().Kind == v1beta1.AuthorinoKind.Kind {
			return item, true
		}
		return nil, false
	})

	if len(aobjs) > 0 {
		logger.Info("authorino resource already exists, no need to create", "status", "skipping")
		return
	}

	authorino := &v1beta2.Authorino{
		TypeMeta: v1.TypeMeta{
			Kind:       "Authorino",
			APIVersion: "operator.authorino.kuadrant.io/v1beta1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      "authorino",
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
		logger.Error(err, "failed to destruct authorino", "status", "error")
	}
	logger.Info("creating authorino resource", "status", "processing")
	_, err = r.Client.Resource(v1beta1.AuthorinoResource).Namespace(authorino.Namespace).Create(ctx, unstructuredAuthorino, v1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("already created authorino resource", "status", "acceptable")
		} else {
			logger.Error(err, "failed to create authorino resource", "status", "error")
		}
	}
}
