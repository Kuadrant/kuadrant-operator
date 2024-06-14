package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

// reconcileStatus makes sure status block of AuthPolicy is up-to-date.
func (r *AuthPolicyReconciler) reconcileStatus(ctx context.Context, ap *api.AuthPolicy, specErr error) (ctrl.Result, error) {
	logger, _ := logr.FromContext(ctx)
	logger.V(1).Info("Reconciling AuthPolicy status", "spec error", specErr)

	newStatus := r.calculateStatus(ctx, ap, specErr)

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

func (r *AuthPolicyReconciler) calculateStatus(ctx context.Context, ap *api.AuthPolicy, specErr error) *api.AuthPolicyStatus {
	newStatus := &api.AuthPolicyStatus{
		Conditions:         slices.Clone(ap.Status.Conditions),
		ObservedGeneration: ap.Status.ObservedGeneration,
	}

	acceptedCond := r.acceptedCondition(ap, specErr)
	meta.SetStatusCondition(&newStatus.Conditions, *acceptedCond)

	// Do not set enforced condition if Accepted condition is false
	if meta.IsStatusConditionFalse(newStatus.Conditions, string(gatewayapiv1alpha2.PolicyReasonAccepted)) {
		meta.RemoveStatusCondition(&newStatus.Conditions, string(kuadrant.PolicyConditionEnforced))
		return newStatus
	}

	enforcedCond := r.enforcedCondition(ctx, ap)
	meta.SetStatusCondition(&newStatus.Conditions, *enforcedCond)

	return newStatus
}

func (r *AuthPolicyReconciler) acceptedCondition(policy kuadrant.Policy, specErr error) *metav1.Condition {
	return kuadrant.AcceptedCondition(policy, specErr)
}

// enforcedCondition checks if the provided AuthPolicy is enforced, ensuring it is properly configured and applied based
// on the status of the associated AuthConfig and Gateway.
func (r *AuthPolicyReconciler) enforcedCondition(ctx context.Context, policy *api.AuthPolicy) *metav1.Condition {
	logger, _ := logr.FromContext(ctx)

	// Check if the policy is Affected
	// Note: This logic assumes synchronous processing, where computing the desired AuthConfig, marking the AuthPolicy
	// as Affected, and calculating the Enforced condition happen sequentially.
	// Introducing a goroutine in this flow could break this assumption and lead to unexpected behavior.
	if r.AffectedPolicyMap.IsPolicyAffected(policy) {
		logger.V(1).Info("Gateway Policy is overridden")
		return r.handlePolicyOverride(policy)
	}

	// Check if the AuthConfig is ready
	authConfigReady, err := r.isAuthConfigReady(ctx, policy)
	if err != nil {
		logger.Error(err, "Failed to check AuthConfig and Gateway")
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrUnknown(policy.Kind(), err), false)
	}

	if !authConfigReady {
		logger.V(1).Info("AuthConfig is not ready")
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrUnknown(policy.Kind(), errors.New("AuthScheme is not ready yet")), false)
	}

	logger.V(1).Info("AuthPolicy is enforced")
	return kuadrant.EnforcedCondition(policy, nil, true)
}

// isAuthConfigReady checks if the AuthConfig is ready.
func (r *AuthPolicyReconciler) isAuthConfigReady(ctx context.Context, policy *api.AuthPolicy) (bool, error) {
	apKey := client.ObjectKeyFromObject(policy)
	authConfigKey := client.ObjectKey{
		Namespace: policy.Namespace,
		Name:      AuthConfigName(apKey),
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

func (r *AuthPolicyReconciler) handlePolicyOverride(policy *api.AuthPolicy) *metav1.Condition {
	if !r.AffectedPolicyMap.IsPolicyOverridden(policy) {
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrUnknown(policy.Kind(), errors.New("no free routes to enforce policy")), false) // Maybe this should be a standard condition rather than an unknown condition
	}

	return kuadrant.EnforcedCondition(policy, kuadrant.NewErrOverridden(policy.Kind(), r.AffectedPolicyMap.PolicyAffectedBy(policy)), false)
}

func (r *AuthPolicyReconciler) generateTopology(ctx context.Context) (*kuadrantgatewayapi.Topology, error) {
	logger, _ := logr.FromContext(ctx)

	gwList := &gatewayapiv1.GatewayList{}
	err := r.Client().List(ctx, gwList)
	logger.V(1).Info("topology: list gateways", "#Gateways", len(gwList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	routeList := &gatewayapiv1.HTTPRouteList{}
	err = r.Client().List(ctx, routeList)
	logger.V(1).Info("topology: list httproutes", "#HTTPRoutes", len(routeList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	aplist := &api.AuthPolicyList{}
	err = r.Client().List(ctx, aplist)
	logger.V(1).Info("topology: list rate limit policies", "#RLPS", len(aplist.Items), "err", err)
	if err != nil {
		return nil, err
	}

	policies := utils.Map(aplist.Items, func(p api.AuthPolicy) kuadrantgatewayapi.Policy {
		return &p
	})

	return kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways(utils.Map(gwList.Items, ptr.To[gatewayapiv1.Gateway])),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
		kuadrantgatewayapi.WithPolicies(policies),
		kuadrantgatewayapi.WithLogger(logger),
	)
}
