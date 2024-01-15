/*
Copyright 2024.

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
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func (r *TLSPolicyReconciler) reconcileStatus(ctx context.Context, tlsPolicy *v1alpha1.TLSPolicy, specErr error) (ctrl.Result, error) {
	newStatus := r.calculateStatus(tlsPolicy, specErr)

	equalStatus := equality.Semantic.DeepEqual(newStatus, tlsPolicy.Status)
	if equalStatus && tlsPolicy.Generation == tlsPolicy.Status.ObservedGeneration {
		return reconcile.Result{}, nil
	}

	newStatus.ObservedGeneration = tlsPolicy.Generation

	tlsPolicy.Status = *newStatus
	updateErr := r.Client().Status().Update(ctx, tlsPolicy)
	if updateErr != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(updateErr) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, updateErr
	}

	if utils.IsTargetNotFound(specErr) {
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (r *TLSPolicyReconciler) calculateStatus(tlsPolicy *v1alpha1.TLSPolicy, specErr error) *v1alpha1.TLSPolicyStatus {
	newStatus := &v1alpha1.TLSPolicyStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions:         slices.Clone(tlsPolicy.Status.Conditions),
		ObservedGeneration: tlsPolicy.Status.ObservedGeneration,
	}

	acceptedCond := utils.AcceptedCondition(tlsPolicy, specErr)
	meta.SetStatusCondition(&newStatus.Conditions, *acceptedCond)

	return newStatus
}
