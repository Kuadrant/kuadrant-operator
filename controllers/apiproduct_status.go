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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

const (
	ServiceNotFoundReason       = "ServiceNotFound"
	APINotFoundReason           = "APINotFound"
	DuplicatedPrefixFoundReason = "DuplicatedPrefix"
	UnknownReason               = "Unknown"
)

func (r *APIProductReconciler) reconcileStatus(ctx context.Context, logger logr.Logger, apip *networkingv1beta1.APIProduct) (ctrl.Result, error) {
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

	ok, message, reason, err := r.apiReferenceStatus(ctx, apip)
	if err != nil {
		return metav1.Condition{}, err
	}

	if !ok {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Message = message
		readyCondition.Reason = reason
	}

	return *readyCondition, nil
}

func (r *APIProductReconciler) apiReferenceStatus(ctx context.Context, apip *networkingv1beta1.APIProduct) (bool, string, string, error) {
	log := r.Logger().WithValues("apiproduct", client.ObjectKeyFromObject(apip))
	fieldErrors := field.ErrorList{}
	apisFldPath := field.NewPath("spec").Child("APIs")

	mappingPrefix := map[string]interface{}{}
	for idx, apiSel := range apip.Spec.APIs {
		apiField := apisFldPath.Index(idx)

		if _, ok := mappingPrefix[apiSel.Mapping.Prefix]; ok {
			fieldErrors = append(fieldErrors, field.Invalid(apiField, apiSel, "duplicated prefix"))
			return false, fieldErrors.ToAggregate().Error(), DuplicatedPrefixFoundReason, nil
		}
		mappingPrefix[apiSel.Mapping.Prefix] = nil

		api := &networkingv1beta1.API{}
		err := r.Client().Get(ctx, apiSel.APINamespacedName(), api)
		log.V(1).Info("get API", "objectKey", apiSel.APINamespacedName(), "error", err)
		if err != nil && errors.IsNotFound(err) {
			fieldErrors = append(fieldErrors, field.Invalid(apiField, apiSel, "Not found"))
			return false, fieldErrors.ToAggregate().Error(), APINotFoundReason, nil
		}

		if err != nil {
			return false, err.Error(), UnknownReason, err
		}

		// Check destinations
		service := &corev1.Service{}
		err = r.Client().Get(ctx, api.Spec.Destination.NamespacedName(), service)
		log.V(1).Info("get service", "objectKey", api.Spec.Destination.NamespacedName(), "error", err)
		if err != nil && errors.IsNotFound(err) {
			fieldErrors = append(fieldErrors, field.Invalid(apiField, apiSel, "the API resource references a service which has not been found"))
			return false, fieldErrors.ToAggregate().Error(), ServiceNotFoundReason, nil
		}

		if err != nil {
			return false, err.Error(), UnknownReason, err
		}
	}
	return true, "", "", nil
}
