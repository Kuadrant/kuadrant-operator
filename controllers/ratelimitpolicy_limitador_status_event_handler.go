package controllers

import (
	"context"

	"github.com/go-logr/logr"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

var _ handler.EventHandler = &limitadorStatusEventHandler{}

type limitadorStatusEventHandler struct {
	Client client.Client
	Logger logr.Logger
}

func (eh limitadorStatusEventHandler) Create(_ context.Context, _ event.CreateEvent, _ workqueue.RateLimitingInterface) {
}

func (eh limitadorStatusEventHandler) Update(ctx context.Context, e event.UpdateEvent, limitingInterface workqueue.RateLimitingInterface) {
	oldL := e.ObjectOld.(*limitadorv1alpha1.Limitador)
	newL := e.ObjectNew.(*limitadorv1alpha1.Limitador)

	if !eh.IsKuadrantInstalled(ctx, oldL) {
		return
	}

	oldCond := meta.FindStatusCondition(oldL.Status.Conditions, "Ready")
	newCond := meta.FindStatusCondition(newL.Status.Conditions, "Ready")

	if oldCond != nil && newCond != nil && oldCond.Status != newCond.Status && oldL.Name == common.LimitadorName {
		eh.Logger.V(1).Info("Limitador status Ready condition change event detected")
		eh.enqueue(ctx, limitingInterface)
	}
}

func (eh limitadorStatusEventHandler) Delete(ctx context.Context, e event.DeleteEvent, limitingInterface workqueue.RateLimitingInterface) {
	eh.Logger.V(1).Info("Limitador delete event detected")
	if !eh.IsKuadrantInstalled(ctx, e.Object) || e.Object.GetName() == common.LimitadorName {
		eh.enqueue(ctx, limitingInterface)
	}
}

func (eh limitadorStatusEventHandler) Generic(_ context.Context, _ event.GenericEvent, _ workqueue.RateLimitingInterface) {
}

func (eh limitadorStatusEventHandler) IsKuadrantInstalled(ctx context.Context, obj client.Object) bool {
	kuadrantList := &kuadrantv1beta1.KuadrantList{}
	if err := eh.Client.List(ctx, kuadrantList, &client.ListOptions{Namespace: obj.GetNamespace()}); err != nil {
		eh.Logger.V(1).Error(err, "failed to list kuadrant in namespace", "namespace", obj.GetNamespace())
		return false
	}

	// No kuadrant in limitador namespace - skipping as it's not managed by kuadrant
	if len(kuadrantList.Items) == 0 {
		eh.Logger.V(1).Info("no kuadrant resources found in limitador namespace, skipping")
		return false
	}

	return true
}
func (eh limitadorStatusEventHandler) enqueue(ctx context.Context, limitingInterface workqueue.RateLimitingInterface) {
	// List all RLPs as there's been an event from Limitador which may affect RLP status
	rlpList := &kuadrantv1beta2.RateLimitPolicyList{}
	if err := eh.Client.List(ctx, rlpList); err != nil {
		eh.Logger.V(1).Error(err, "failed to list RLPs")
	}
	for idx := range rlpList.Items {
		eh.Logger.V(1).Info("queueing rate limiting policy", "policy", rlpList.Items[idx].Name)
		limitingInterface.Add(ctrl.Request{
			NamespacedName: client.ObjectKeyFromObject(&rlpList.Items[idx]),
		})
	}
}
