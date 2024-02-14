package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	v1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

// reconcileStatus makes sure status block of AuthPolicy is up-to-date.
func (r *AuthPolicyReconciler) reconcileStatus(ctx context.Context, ap *api.AuthPolicy, targetNetworkObject client.Object, specErr error) (ctrl.Result, error) {
	logger, _ := logr.FromContext(ctx)
	logger.V(1).Info("Reconciling AuthPolicy status", "spec error", specErr)

	newStatus := r.calculateStatus(ctx, ap, targetNetworkObject, specErr)

	equalStatus := ap.Status.Equals(newStatus, logger)
	logger.V(1).Info("Status", "status is different", !equalStatus)
	logger.V(1).Info("Status", "generation is different", ap.Generation != ap.Status.ObservedGeneration)
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

func (r *AuthPolicyReconciler) calculateStatus(ctx context.Context, ap *api.AuthPolicy, targetNetworkObject client.Object, specErr error) *api.AuthPolicyStatus {
	newStatus := &api.AuthPolicyStatus{
		Conditions:         slices.Clone(ap.Status.Conditions),
		ObservedGeneration: ap.Status.ObservedGeneration,
	}

	acceptedCond := r.acceptedCondition(ap, specErr)
	meta.SetStatusCondition(&newStatus.Conditions, *acceptedCond)

	// Do not set enforced condition if Accepted condition is false
	if meta.IsStatusConditionFalse(newStatus.Conditions, string(gatewayapiv1alpha2.PolicyReasonAccepted)) {
		return newStatus
	}

	enforcedCond := r.enforcedCondition(ctx, ap, targetNetworkObject)
	meta.SetStatusCondition(&newStatus.Conditions, *enforcedCond)

	return newStatus
}

func (r *AuthPolicyReconciler) acceptedCondition(policy common.KuadrantPolicy, specErr error) *metav1.Condition {
	return common.AcceptedCondition(policy, specErr)
}

// enforcedCondition checks if the provided AuthPolicy is enforced, ensuring it is properly configured and applied based
// on the status of the associated AuthConfig and Gateway.
func (r *AuthPolicyReconciler) enforcedCondition(ctx context.Context, policy *api.AuthPolicy, targetNetworkObject client.Object) *metav1.Condition {
	logger, _ := logr.FromContext(ctx)

	// Check if the policy is overridden
	if r.isPolicyOverridden(policy) {
		logger.V(1).Info("Gateway Policy is overridden")
		return r.handleGatewayPolicyOverride(logger, policy, targetNetworkObject)
	}

	// Check if the AuthConfig is ready
	authConfigReady, err := r.isAuthConfigReady(ctx, policy)
	if err != nil {
		logger.Error(err, "Failed to check AuthConfig and Gateway")
		return common.EnforcedCondition(policy, common.NewErrUnknown(policy.Kind(), err))
	}

	if !authConfigReady {
		logger.V(1).Info("AuthConfig is not ready")
		return common.EnforcedCondition(policy, common.NewErrUnknown(policy.Kind(), errors.New("AuthScheme is not ready yet")))
	}

	logger.V(1).Info("AuthPolicy is enforced")
	return common.EnforcedCondition(policy, nil)
}

// setOverriddenPolicy sets the overridden policy in the reconciler's tracking map.
func (r *AuthPolicyReconciler) setOverriddenPolicy(ap *api.AuthPolicy) {
	if r.OverriddenPolicies == nil {
		r.OverriddenPolicies = make(map[types.UID]bool)
	}
	r.OverriddenPolicies[ap.GetUID()] = true
}

// removeOverriddenPolicy removes the overridden policy from the reconciler's tracking map.
func (r *AuthPolicyReconciler) removeOverriddenPolicy(ap *api.AuthPolicy) {
	delete(r.OverriddenPolicies, ap.GetUID())
}

// isPolicyOverridden checks if the provided AuthPolicy is overridden based on the tracking map maintained by the reconciler.
func (r *AuthPolicyReconciler) isPolicyOverridden(ap *api.AuthPolicy) bool {
	return r.OverriddenPolicies[ap.GetUID()] && common.IsTargetRefGateway(ap.GetTargetRef())
}

// isAuthConfigReady checks if the AuthConfig is ready.
func (r *AuthPolicyReconciler) isAuthConfigReady(ctx context.Context, policy *api.AuthPolicy) (bool, error) {
	apKey := client.ObjectKeyFromObject(policy)
	authConfigKey := client.ObjectKey{
		Namespace: policy.Namespace,
		Name:      authConfigName(apKey),
	}
	authConfig := &authorinoapi.AuthConfig{}
	err := r.GetResource(ctx, authConfigKey, authConfig)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("failed to get AuthConfig: %w", err)
		}
	}
	return authConfig.Status.Ready(), nil
}

// handleGatewayPolicyOverride handles the case where the Gateway Policy is overridden by filtering policy references
// and creating a corresponding error condition.
func (r *AuthPolicyReconciler) handleGatewayPolicyOverride(logger logr.Logger, policy *api.AuthPolicy, targetNetworkObject client.Object) *metav1.Condition {
	obj := targetNetworkObject.(*v1.Gateway)
	gatewayWrapper := common.GatewayWrapper{Gateway: obj, PolicyRefsConfig: &common.KuadrantAuthPolicyRefsConfig{}}
	refs := gatewayWrapper.PolicyRefs()
	filteredRef := common.Filter(refs, func(key client.ObjectKey) bool {
		return key != client.ObjectKeyFromObject(policy)
	})
	jsonData, err := json.Marshal(filteredRef)
	if err != nil {
		logger.Error(err, "Failed to marshal filtered references")
		return common.EnforcedCondition(policy, common.NewErrUnknown(policy.Kind(), err))
	}
	return common.EnforcedCondition(policy, common.NewErrOverridden(policy.Kind(), string(jsonData)))
}
