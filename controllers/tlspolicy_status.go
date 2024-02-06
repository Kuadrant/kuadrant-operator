package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
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

	return ctrl.Result{}, nil
}

func (r *TLSPolicyReconciler) calculateStatus(tlsPolicy *v1alpha1.TLSPolicy, specErr error) *v1alpha1.TLSPolicyStatus {
	newStatus := &v1alpha1.TLSPolicyStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions:         slices.Clone(tlsPolicy.Status.Conditions),
		ObservedGeneration: tlsPolicy.Status.ObservedGeneration,
	}

	readyCond := r.readyCondition(tlsPolicy.Kind(), specErr)
	meta.SetStatusCondition(&newStatus.Conditions, *readyCond)


	return newStatus
}

func (r *TLSPolicyReconciler) readyCondition(targetNetworkObjectKind string, specErr error) *metav1.Condition {
	cond := &metav1.Condition{
		Type:    string(ReadyConditionType),
		Status:  metav1.ConditionTrue,
		Reason:  fmt.Sprintf("%sTLSEnabled", targetNetworkObjectKind),
		Message: fmt.Sprintf("%s is TLS Enabled", targetNetworkObjectKind),
	}

	if specErr != nil {
		cond.Status = metav1.ConditionFalse
		cond.Message = specErr.Error()
		cond.Reason = "ReconciliationError"

		if errors.Is(specErr, common.ErrTargetNotFound{}) {
			cond.Reason = string(gatewayapiv1alpha2.PolicyReasonTargetNotFound)
		}
	}

	return cond
}
