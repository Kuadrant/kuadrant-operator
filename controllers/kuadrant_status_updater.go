package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/go-logr/logr"
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/authorino"
)

const (
	ReadyConditionType string = "Ready"
)

type KuadrantStatusUpdater struct {
	Client     *dynamic.DynamicClient
	HasGateway bool
}

func NewKuadrantStatusUpdater(client *dynamic.DynamicClient, isIstioInstalled, isEnvoyGatewayInstalled bool) *KuadrantStatusUpdater {
	return &KuadrantStatusUpdater{Client: client, HasGateway: isIstioInstalled || isEnvoyGatewayInstalled}
}

func (r *KuadrantStatusUpdater) Subscription() *controller.Subscription {
	return &controller.Subscription{ReconcileFunc: r.Reconcile, Events: []controller.ResourceEventMatcher{
		{Kind: ptr.To(kuadrantv1beta1.AuthorinoGroupKind), EventType: ptr.To(controller.CreateEvent)},
		{Kind: ptr.To(kuadrantv1beta1.AuthorinoGroupKind), EventType: ptr.To(controller.UpdateEvent)},
		{Kind: ptr.To(kuadrantv1beta1.KuadrantGroupKind), EventType: ptr.To(controller.CreateEvent)},
		{Kind: ptr.To(kuadrantv1beta1.LimitadorGroupKind), EventType: ptr.To(controller.CreateEvent)},
		{Kind: ptr.To(kuadrantv1beta1.LimitadorGroupKind), EventType: ptr.To(controller.UpdateEvent)},
	},
	}
}

func (r *KuadrantStatusUpdater) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("KuadrantStatusUpdater")
	logger.Info("reconciling kuadrant status", "status", "started")
	defer logger.Info("reconciling kuadrant status", "status", "completed")

	kObj, err := GetKuadrantFromTopology(topology)
	if err != nil {
		logger.V(1).Error(err, "error getting kuadrant from topology", "status", "error")
		return nil
	}

	newStatus := r.calculateStatus(topology, logger, kObj)

	equalStatus := kObj.Status.Equals(newStatus, logger)
	logger.V(1).Info("Status", "status is different", !equalStatus)
	logger.V(1).Info("Status", "generation is different", kObj.Generation != kObj.Status.ObservedGeneration)
	if equalStatus && kObj.Generation == kObj.Status.ObservedGeneration {
		// Steady state
		logger.V(1).Info("Status was not updated", "status", "stale")
		return nil
	}

	// Save the generation number we acted on, otherwise we might wrongfully indicate
	// that we've seen a spec update when we retry.
	newStatus.ObservedGeneration = kObj.Generation

	logger.V(1).Info("Updating Status", "sequence no:", fmt.Sprintf("sequence No: %v->%v", kObj.Status.ObservedGeneration, newStatus.ObservedGeneration), "status", "updated")

	kObj.Status = *newStatus
	updateErr := r.updateKuadrantStatus(ctx, kObj)
	if updateErr != nil {
		logger.V(1).Error(updateErr, "updateKuadrantStatusError", "status", "error")
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(updateErr) {
			logger.Info("Failed to update status: resource might just be outdated", "status", "error")
			return nil
		}

		return nil
	}

	return nil
}
func (r *KuadrantStatusUpdater) updateKuadrantStatus(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	obj, err := controller.Destruct(kObj)
	if err != nil {
		return err
	}
	_, err = r.Client.Resource(kuadrantv1beta1.KuadrantsResource).Namespace(kObj.GetNamespace()).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
	return err
}

func (r *KuadrantStatusUpdater) calculateStatus(topology *machinery.Topology, logger logr.Logger, kObj *kuadrantv1beta1.Kuadrant) *kuadrantv1beta1.KuadrantStatus {
	newStatus := &kuadrantv1beta1.KuadrantStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions:         slices.Clone(kObj.Status.Conditions),
		ObservedGeneration: kObj.Status.ObservedGeneration,
	}

	availableCond := r.readyCondition(topology, logger)

	meta.SetStatusCondition(&newStatus.Conditions, *availableCond)

	return newStatus
}

func (r *KuadrantStatusUpdater) readyCondition(topology *machinery.Topology, logger logr.Logger) *metav1.Condition {
	cond := &metav1.Condition{
		Type:    ReadyConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  "Ready",
		Message: "Kuadrant is ready",
	}

	if !r.HasGateway {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "GatewayAPIProviderNotFound"
		cond.Message = "GatewayAPI provider not found"
		return cond
	}

	limitadorObj, err := GetLimitadorFromTopology(topology)
	if err != nil && !errors.Is(err, ErrMissingLimitador) {
		logger.V(1).Error(err, "failed getting Limitador resource from topology", "status", "error")
	}

	if limitadorObj != nil {
		reason := checkLimitadorReady(limitadorObj)
		if reason != nil {
			cond.Status = metav1.ConditionFalse
			cond.Reason = "limitadornotready"
			cond.Message = *reason
			return cond
		}
	}

	authorinoObj, err := GetAuthorinoFromTopology(topology)
	if err != nil && !errors.Is(err, ErrMissingAuthorino) {
		logger.V(1).Error(err, "failed getting Authorino resource from topology", "status", "error")
	}

	if authorinoObj != nil {
		reason := checkAuthorinoAvailable(authorinoObj)
		if reason != nil {
			cond.Status = metav1.ConditionFalse
			cond.Reason = "AuthorinoNotReady"
			cond.Message = *reason
			return cond
		}
	}
	return cond
}

func checkLimitadorReady(limitadorObj *limitadorv1alpha1.Limitador) *string {
	statusConditionReady := meta.FindStatusCondition(limitadorObj.Status.Conditions, "Ready")
	if statusConditionReady == nil {
		reason := "Ready condition not found"
		return &reason
	}
	if statusConditionReady.Status != metav1.ConditionTrue {
		return &statusConditionReady.Message
	}

	return nil
}

func checkAuthorinoAvailable(authorinoObj *authorinov1beta1.Authorino) *string {
	readyCondition := authorino.FindAuthorinoStatusCondition(authorinoObj.Status.Conditions, "Ready")
	if readyCondition == nil {
		tmp := "Ready condition not found"
		return &tmp
	}

	if readyCondition.Status != corev1.ConditionTrue {
		return &readyCondition.Message
	}

	return nil
}
