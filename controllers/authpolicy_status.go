package controllers

import (
	"context"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// reconcileStatus makes sure status block of AuthPolicy is up-to-date.
func (r *AuthPolicyReconciler) reconcileStatus(ctx context.Context, ap *api.AuthPolicy, specErr error) (ctrl.Result, error) {
	logger, _ := logr.FromContext(ctx)
	logger.V(1).Info("Reconciling AuthPolicy status", "spec error", specErr)

	// fetch the AuthConfig and check if it's ready.
	isAuthConfigReady := true
	if specErr == nil { // skip fetching authconfig if we already have a reconciliation error.
		apKey := client.ObjectKeyFromObject(ap)
		authConfigKey := client.ObjectKey{
			Namespace: ap.Namespace,
			Name:      authConfigName(apKey),
		}
		authConfig := &authorinoapi.AuthConfig{}
		if err := r.GetResource(ctx, authConfigKey, authConfig); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		isAuthConfigReady = authConfig.Status.Ready()
	}

	newStatus := r.calculateStatus(ap, specErr, isAuthConfigReady)

	equalStatus := ap.Status.Equals(newStatus, logger)
	logger.V(1).Info("Status", "status is different", !equalStatus)
	logger.V(1).Info("Status", "generation is different", ap.Generation != ap.Status.ObservedGeneration)
	logger.V(1).Info("Status", "AuthConfig is ready", isAuthConfigReady)
	if equalStatus && ap.Generation == ap.Status.ObservedGeneration {
		logger.V(1).Info("Status up-to-date. No changes required.")
		return ctrl.Result{}, nil
	}

	// Save the generation number we acted on, otherwise we might wrongfully indicate
	// that we've seen a spec update when we retry.
	// TODO: This can clobber an update if we allow multiple agents to write to the
	// same status.
	newStatus.ObservedGeneration = ap.Generation

	logger.V(1).Info("Updating Status", "sequence no:", fmt.Sprintf("sequence No: %v->%v", ap.Status.ObservedGeneration, newStatus.ObservedGeneration))

	ap.Status = *newStatus
	updateErr := r.Client().Status().Update(ctx, ap)
	if updateErr != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(updateErr) {
			logger.Info("Failed to update status: resource might just be outdated")
			return ctrl.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
	}
	return ctrl.Result{}, nil
}

func (r *AuthPolicyReconciler) calculateStatus(ap *api.AuthPolicy, specErr error, authConfigReady bool) *api.AuthPolicyStatus {
	newStatus := &api.AuthPolicyStatus{
		Conditions:         slices.Clone(ap.Status.Conditions),
		ObservedGeneration: ap.Status.ObservedGeneration,
	}

	availableCond := r.acceptedCondition(ap, specErr, authConfigReady)

	meta.SetStatusCondition(&newStatus.Conditions, *availableCond)

	return newStatus
}

func (r *AuthPolicyReconciler) acceptedCondition(policy common.KuadrantPolicy, specErr error, authConfigReady bool) *metav1.Condition {
	cond := common.AcceptedCondition(policy, specErr)

	if !authConfigReady {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "AuthSchemeNotReady"
		cond.Message = "AuthScheme is not ready yet" // TODO(rahul): need to take care if status change is delayed.
	}

	return cond
}
