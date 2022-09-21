package apim

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

const (
	RLPAvailableConditionType string = "Available"
)

func (r *RateLimitPolicyReconciler) reconcileStatus(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy, specErr error) (ctrl.Result, error) {
	logger, _ := logr.FromContext(ctx)
	newStatus, err := r.calculateStatus(ctx, rlp, specErr)
	if err != nil {
		return reconcile.Result{}, err
	}

	equalStatus := rlp.Status.Equals(newStatus, logger)
	logger.V(1).Info("Status", "status is different", !equalStatus)
	logger.V(1).Info("Status", "generation is different", rlp.Generation != rlp.Status.ObservedGeneration)
	if equalStatus && rlp.Generation == rlp.Status.ObservedGeneration {
		// Steady state
		logger.V(1).Info("Status was not updated")
		return reconcile.Result{}, nil
	}

	// Save the generation number we acted on, otherwise we might wrongfully indicate
	// that we've seen a spec update when we retry.
	// TODO: This can clobber an update if we allow multiple agents to write to the
	// same status.
	newStatus.ObservedGeneration = rlp.Generation

	logger.V(1).Info("Updating Status", "sequence no:", fmt.Sprintf("sequence No: %v->%v", rlp.Status.ObservedGeneration, newStatus.ObservedGeneration))

	rlp.Status = *newStatus
	updateErr := r.Client().Status().Update(ctx, rlp)
	logger.V(1).Info("Updating Status", "err", updateErr)
	if updateErr != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(updateErr) {
			logger.Info("Failed to update status: resource might just be outdated")
			return reconcile.Result{Requeue: true}, nil
		}

		return reconcile.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
	}
	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyReconciler) calculateStatus(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy, specErr error) (*apimv1alpha1.RateLimitPolicyStatus, error) {
	newStatus := &apimv1alpha1.RateLimitPolicyStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions:         common.CopyConditions(rlp.Status.Conditions),
		ObservedGeneration: rlp.Status.ObservedGeneration,
	}

	// Only makes sense for rlp's targeting a route
	if common.IsTargetRefHTTPRoute(rlp.Spec.TargetRef) {
		gwRateLimits, err := r.gatewaysRateLimits(ctx, rlp)
		if err != nil {
			return nil, err
		}
		newStatus.GatewaysRateLimits = gwRateLimits
	}

	availableCond := r.availableCondition(specErr)

	meta.SetStatusCondition(&newStatus.Conditions, *availableCond)

	return newStatus, nil
}

func (r *RateLimitPolicyReconciler) availableCondition(specErr error) *metav1.Condition {
	cond := &metav1.Condition{
		Type:    RLPAvailableConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  "HTTPRouteProtected",
		Message: "HTTPRoute is ratelimited",
	}

	if specErr != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "ReconcilliationError"
		cond.Message = specErr.Error()
	}

	return cond
}

// gatewaysRateLimits returns all gateway level rate limits configuration from all the
// gateways where this rate limit policy adds configuration
func (r *RateLimitPolicyReconciler) gatewaysRateLimits(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) ([]apimv1alpha1.GatewayRateLimits, error) {
	logger, _ := logr.FromContext(ctx)
	gwKeys, err := r.TargetedGatewayKeys(ctx, rlp.Spec.TargetRef, rlp.Namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			gwKeys = make([]client.ObjectKey, 0)
		} else {
			return nil, err
		}
	}

	result := make([]apimv1alpha1.GatewayRateLimits, 0)

	for _, gwKey := range gwKeys {
		gw := &gatewayapiv1alpha2.Gateway{}
		err := r.Client().Get(ctx, gwKey, gw)
		logger.V(1).Info("get gateway", "key", gwKey, "err", err)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}

		if gw.GetAnnotations() == nil {
			continue
		}

		if rlpKeyStr, ok := gw.GetAnnotations()[common.RateLimitPolicyBackRefAnnotation]; ok {
			rlpKey, err := common.UnMarshallObjectKey(rlpKeyStr)
			if err != nil {
				logger.V(1).Info("gatewaysRateLimits", "cannot parse rlp back ref key", rlpKey, "err", err)
				continue
			}
			gwRLP := &apimv1alpha1.RateLimitPolicy{}
			err = r.Client().Get(ctx, rlpKey, gwRLP)
			logger.V(1).Info("gatewaysRateLimits", "get gateway rlp", rlpKey, "err", err)
			if err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				return nil, err
			}

			result = append(result, apimv1alpha1.GatewayRateLimits{
				GatewayName: gwKey.String(),
				RateLimits:  gwRLP.Spec.RateLimits,
			})
		}
	}

	return result, nil
}
