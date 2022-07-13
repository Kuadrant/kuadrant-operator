package apim

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	authorinov1beta1 "github.com/kuadrant/authorino/api/v1beta1"
	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const APAvailableConditionType string = "Available"

// reconcileStatus makes sure status block of AuthPolicy is up-to-date.
func (r *AuthPolicyReconciler) reconcileStatus(ctx context.Context, ap *apimv1alpha1.AuthPolicy, specErr error) (ctrl.Result, error) {
	logger, _ := logr.FromContext(ctx)
	logger.V(1).Info("Reconciling AuthPolicy status", "spec error", specErr)

	// fetch the AuthConfig and check if it's ready.
	isAuthConfigReady := true
	if specErr == nil { // skip fetching authconfig if we already have a reconciliation error.
		apKey := client.ObjectKeyFromObject(ap)
		authConfigKey := client.ObjectKey{
			Namespace: common.KuadrantNamespace,
			Name:      authConfigName(apKey),
		}
		authConfig := &authorinov1beta1.AuthConfig{}
		if err := r.GetResource(ctx, authConfigKey, authConfig); err != nil {
			return ctrl.Result{}, err
		}

		isAuthConfigReady = authConfig.Status.Ready
	}

	newStatus := r.calculateStatus(ap, specErr, isAuthConfigReady)

	equalStatus := apimv1alpha1.StatusEquals(&ap.Status, newStatus, logger)
	logger.V(1).Info("Status", "status is different", !equalStatus)
	logger.V(1).Info("Status", "generation is different", ap.Generation != ap.Status.ObservedGeneration)
	logger.V(1).Info("Status", "AuthConfig is ready", isAuthConfigReady)
	if equalStatus && ap.Generation == ap.Status.ObservedGeneration {
		logger.V(1).Info("Status up-to-date. No changes required.")
		return ctrl.Result{}, nil
	}

	logger.V(1).Info("Updating Status", "sequence change:", fmt.Sprintf("%v->%v", ap.Status.ObservedGeneration, newStatus.ObservedGeneration))
	ap.Status = *newStatus
	updateErr := r.Client().Status().Update(ctx, ap)
	if updateErr != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(updateErr) {
			logger.Info("Failed to update status: resource might just be outdated")
			return ctrl.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
	}
	return ctrl.Result{}, nil
}

func (r *AuthPolicyReconciler) calculateStatus(ap *apimv1alpha1.AuthPolicy, specErr error, authConfigReady bool) *apimv1alpha1.AuthPolicyStatus {
	newStatus := &apimv1alpha1.AuthPolicyStatus{
		Conditions:         common.CopyConditions(ap.Status.Conditions),
		ObservedGeneration: ap.Generation,
	}

	targetObjectKind := string(ap.Spec.TargetRef.Kind)
	availableCond := r.availableCondition(targetObjectKind, specErr, authConfigReady)

	meta.SetStatusCondition(&newStatus.Conditions, *availableCond)

	return newStatus
}

func (r *AuthPolicyReconciler) availableCondition(targetObjectKind string, specErr error, authConfigReady bool) *metav1.Condition {
	// Condition if there is not issue
	cond := &metav1.Condition{
		Type:    APAvailableConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  fmt.Sprintf("%sProtected", targetObjectKind),
		Message: fmt.Sprintf("%s is protected", targetObjectKind),
	}

	if specErr != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "ReconciliationError"
		cond.Message = specErr.Error()
	} else if !authConfigReady {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "AuthSchemeNotReady"
		cond.Message = "AuthScheme is not ready yet" // TODO(rahul): need to take care if status change is delayed.
	}

	return cond
}
