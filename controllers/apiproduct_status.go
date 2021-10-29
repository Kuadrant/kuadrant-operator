/*
Copyright 2021 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

const (
	ServiceNotFoundReason = "ServiceNotFound"
	APINotFoundReason     = "APINotFound"
	InvalidSpecReason     = "InvalidSpec"
	UnknownReason         = "Unknown"
)

func (r *APIProductReconciler) reconcileStatus(ctx context.Context, apip *networkingv1beta1.APIProduct) (ctrl.Result, error) {
	logger := logr.FromContext(ctx)
	logger.V(1).Info("reconcile status START")

	newStatus, err := r.calculateStatus(ctx, apip)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to calculate status: %w", err)
	}

	equalStatus := apip.Status.Equals(newStatus, logger)
	logger.V(1).Info("Status", "status is different", !equalStatus)
	if equalStatus {
		// Steady state
		logger.V(1).Info("Status was not updated")
		return reconcile.Result{}, nil
	}

	apip.Status = *newStatus
	return reconcile.Result{}, r.UpdateResourceStatus(ctx, apip)
}

func (r *APIProductReconciler) calculateStatus(ctx context.Context, apip *networkingv1beta1.APIProduct) (*networkingv1beta1.APIProductStatus, error) {
	newStatus := &networkingv1beta1.APIProductStatus{}

	newStatus.ObservedGen = apip.GetGeneration()

	newStatus.Conditions = []metav1.Condition{}
	// Clone conditions to keep LastTransitionTime and other attributes updated
	for idx := range apip.Status.Conditions {
		newStatus.Conditions = append(newStatus.Conditions, *apip.Status.Conditions[idx].DeepCopy())
	}

	readyCondition, err := r.calculateReadyCondition(ctx, apip)
	if err != nil {
		return nil, err
	}
	meta.SetStatusCondition(&newStatus.Conditions, readyCondition)

	return newStatus, nil
}

func (r *APIProductReconciler) calculateReadyCondition(ctx context.Context, apip *networkingv1beta1.APIProduct) (metav1.Condition, error) {
	readyCondition := &metav1.Condition{
		Type:    common.ReadyStatusConditionType,
		Status:  metav1.ConditionTrue,
		Message: "Ready",
		Reason:  "Ready",
	}

	ingressOK, err := r.IngressProvider.Status(ctx, apip)
	if err != nil {
		return metav1.Condition{}, err
	}

	if !ingressOK {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Message = "Ingress was not reconciled"
		// TODO(eastizle): provide more info from the providers
		readyCondition.Reason = "Unknown"
	}

	authOK, err := r.AuthProvider.Status(ctx, apip)
	if err != nil {
		return metav1.Condition{}, err
	}

	if !authOK {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Message = "Authentication was not reconciled"
		// TODO(eastizle): provide more info from the providers
		readyCondition.Reason = "Unknown"
	}

	rateLimitOK, err := r.RateLimitProvider.Status(ctx, apip)
	if err != nil {
		return metav1.Condition{}, err
	}

	if !rateLimitOK {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Message = "RateLimit was not reconciled"
		// TODO(eastizle): provide more info from the providers
		readyCondition.Reason = "Unknown"
	}

	if err := r.validateSpec(ctx, apip); err != nil {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Message = err.Error()
		readyCondition.Reason = string(apierrors.ReasonForError(err))
	}

	return *readyCondition, nil
}
