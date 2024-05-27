package controllers

import (
	"context"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantistioutils "github.com/kuadrant/kuadrant-operator/pkg/istio"
)

const (
	ReadyConditionType string = "Ready"

	PolicyReasonUnknown string = "Unknown"
)

func (r *KuadrantReconciler) reconcileStatus(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant, specErr error) (ctrl.Result, error) {
	logger, _ := logr.FromContext(ctx)
	newStatus, err := r.calculateStatus(ctx, kObj, specErr)
	if err != nil {
		return reconcile.Result{}, err
	}

	equalStatus := kObj.Status.Equals(newStatus, logger)
	logger.V(1).Info("Status", "status is different", !equalStatus)
	logger.V(1).Info("Status", "generation is different", kObj.Generation != kObj.Status.ObservedGeneration)
	if equalStatus && kObj.Generation == kObj.Status.ObservedGeneration {
		// Steady state
		logger.V(1).Info("Status was not updated")
		return reconcile.Result{}, nil
	}

	// Save the generation number we acted on, otherwise we might wrongfully indicate
	// that we've seen a spec update when we retry.
	// TODO: This can clobber an update if we allow multiple agents to write to the
	// same status.
	newStatus.ObservedGeneration = kObj.Generation

	logger.V(1).Info("Updating Status", "sequence no:", fmt.Sprintf("sequence No: %v->%v", kObj.Status.ObservedGeneration, newStatus.ObservedGeneration))

	kObj.Status = *newStatus
	updateErr := r.Client().Status().Update(ctx, kObj)
	if updateErr != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(updateErr) {
			logger.Info("Failed to update status: resource might just be outdated")
			return reconcile.Result{Requeue: true}, nil
		}

		return reconcile.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
	}
	return ctrl.Result{}, nil
}

func (r *KuadrantReconciler) calculateStatus(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant, specErr error) (*kuadrantv1beta1.KuadrantStatus, error) {
	newStatus := &kuadrantv1beta1.KuadrantStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions:         slices.Clone(kObj.Status.Conditions),
		ObservedGeneration: kObj.Status.ObservedGeneration,
	}

	availableCond, err := r.readyCondition(ctx, kObj, specErr)
	if err != nil {
		return nil, err
	}

	meta.SetStatusCondition(&newStatus.Conditions, *availableCond)

	return newStatus, nil
}

func (r *KuadrantReconciler) readyCondition(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant, specErr error) (*metav1.Condition, error) {
	cond := &metav1.Condition{
		Type:    ReadyConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  "Ready",
		Message: "Kuadrant is ready",
	}

	if specErr != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "ReconciliationError"
		cond.Message = specErr.Error()
		return cond, nil
	}

	reason, err := r.checkGatewayProviders()
	if err != nil {
		return nil, err
	}
	if reason != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "GatewayAPIPRoviderNotFound"
		cond.Message = *reason
		return cond, nil
	}

	reason, err = r.checkLimitadorReady(ctx, kObj)
	if err != nil {
		return nil, err
	}
	if reason != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "LimitadorNotReady"
		cond.Message = *reason
		return cond, nil
	}

	reason, err = r.checkAuthorinoAvailable(ctx, kObj)
	if err != nil {
		return nil, err
	}
	if reason != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "AuthorinoNotReady"
		cond.Message = *reason
		return cond, nil
	}

	return cond, nil
}

func (r *KuadrantReconciler) checkLimitadorReady(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) (*string, error) {
	limitadorObj := &limitadorv1alpha1.Limitador{}
	limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kObj.Namespace}

	err := r.Client().Get(ctx, limitadorKey, limitadorObj)
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}

	if errors.IsNotFound(err) {
		reason := "Limitador not found"
		return &reason, nil
	}

	statusConditionReady := meta.FindStatusCondition(limitadorObj.Status.Conditions, "Ready")
	if statusConditionReady == nil {
		reason := "Ready condition not found"
		return &reason, nil
	}
	if statusConditionReady.Status != metav1.ConditionTrue {
		return &statusConditionReady.Message, nil
	}

	return nil, nil
}

func (r *KuadrantReconciler) checkAuthorinoAvailable(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) (*string, error) {
	authorino := &authorinov1beta1.Authorino{}
	dKey := client.ObjectKey{Name: "authorino", Namespace: kObj.Namespace}
	err := r.Client().Get(ctx, dKey, authorino)
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}

	if err != nil && errors.IsNotFound(err) {
		tmp := err.Error()
		return &tmp, nil
	}

	readyCondition := common.FindAuthorinoStatusCondition(authorino.Status.Conditions, "Ready")
	if readyCondition == nil {
		tmp := "Ready condition not found"
		return &tmp, nil
	}

	if readyCondition.Status != corev1.ConditionTrue {
		return &readyCondition.Message, nil
	}

	return nil, nil
}

func (r *KuadrantReconciler) checkGatewayProviders() (*string, error) {
	anyProviderFunc := func(checks []func(restMapper meta.RESTMapper) (bool, error)) (bool, error) {
		for _, check := range checks {
			ok, err := check(r.RestMapper)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}

	anyProvider, err := anyProviderFunc([]func(restMapper meta.RESTMapper) (bool, error){
		kuadrantistioutils.IsIstioInstalled,
	})

	if err != nil {
		return nil, err
	}

	if anyProvider {
		return nil, nil
	}

	return ptr.To("GatewayAPI provider not found"), nil
}
