package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"k8s.io/utils/ptr"

	"github.com/go-logr/logr"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
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

func (r *AuthPolicyReconciler) acceptedCondition(policy kuadrant.Policy, specErr error) *metav1.Condition {
	return kuadrant.AcceptedCondition(policy, specErr)
}

// enforcedCondition checks if the provided AuthPolicy is enforced, ensuring it is properly configured and applied based
// on the status of the associated AuthConfig and Gateway.
func (r *AuthPolicyReconciler) enforcedCondition(ctx context.Context, policy *api.AuthPolicy, targetNetworkObject client.Object) *metav1.Condition {
	logger, _ := logr.FromContext(ctx)

	t, err := r.generateTopology(ctx)
	if err != nil {
		logger.V(1).Info("Failed to generate topology", "error", err)
		return nil
	}
	// Check if the policy is overridden
	// Note: This logic assumes synchronous processing, where computing the desired AuthConfig, marking the AuthPolicy
	// as overridden, and calculating the Enforced condition happen sequentially.
	// Introducing a goroutine in this flow could break this assumption and lead to unexpected behavior.
	if r.OverriddenPolicyMap.IsPolicyOverridden(policy) {
		logger.V(1).Info("Gateway Policy is overridden")
		return r.handlePolicyOverride(logger, policy, targetNetworkObject, t)
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

func (r *AuthPolicyReconciler) handlePolicyOverride(logger logr.Logger, policy *api.AuthPolicy, targetNetworkObject client.Object, t *kuadrantgatewayapi.Topology) *metav1.Condition {
	switch targetNetworkObject.(type) {
	case *gatewayapiv1.Gateway:
		return r.handleGatewayPolicyOverride(logger, policy, targetNetworkObject)
	case *gatewayapiv1.HTTPRoute:
		return r.handleHTTPRoutePolicyOverride(logger, policy, targetNetworkObject, t)
	default:
		logger.Error(errors.New("this point should never be reached"), "failed to match target network object", targetNetworkObject)
		return nil
	}
}

// handleGatewayPolicyOverride handles the case where the Gateway Policy is overridden by filtering policy references
// and creating a corresponding error condition.
func (r *AuthPolicyReconciler) handleGatewayPolicyOverride(logger logr.Logger, policy *api.AuthPolicy, targetNetworkObject client.Object) *metav1.Condition {
	obj := targetNetworkObject.(*gatewayapiv1.Gateway)
	gatewayWrapper := kuadrant.GatewayWrapper{Gateway: obj, Referrer: policy}
	refs := gatewayWrapper.PolicyRefs()
	filteredRef := utils.Filter(refs, func(key client.ObjectKey) bool {
		return key != client.ObjectKeyFromObject(policy)
	})

	return kuadrant.EnforcedCondition(policy, kuadrant.NewErrOverridden(policy.Kind(), filteredRef), false)
}

// handleHTTPRoutePolicyOverride handles the case where the HTTPRoute Policy is overridden by filtering policy references
// and creating a corresponding error condition.
func (r *AuthPolicyReconciler) handleHTTPRoutePolicyOverride(logger logr.Logger, policy *api.AuthPolicy, targetNetworkObject client.Object, t *kuadrantgatewayapi.Topology) *metav1.Condition {
	obj := targetNetworkObject.(*gatewayapiv1.HTTPRoute)
	httpRouteWrapper := kuadrant.HTTPRouteWrapper{HTTPRoute: obj, Referrer: policy}
	refs := httpRouteWrapper.PolicyRefs(t)
	jsonData, err := json.Marshal(refs)
	if err != nil {
		logger.Error(err, "Failed to marshal filtered references")
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrUnknown(policy.Kind(), err), false)
	}
	return kuadrant.EnforcedCondition(policy, kuadrant.NewErrOverridden(policy.Kind(), string(jsonData)), false)
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
