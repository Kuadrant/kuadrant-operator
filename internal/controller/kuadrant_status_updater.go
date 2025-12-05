package controllers

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/go-logr/logr"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/authorino"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	operatormetrics "github.com/kuadrant/kuadrant-operator/internal/metrics"
)

const (
	ReadyConditionType string = "Ready"
)

type KuadrantStatusUpdater struct {
	Client                       *dynamic.DynamicClient
	isGatewayAPIInstalled        bool
	isGatewayProviderInstalled   bool
	isLimitadorOperatorInstalled bool
	isAuthorinoOperatorInstalled bool
}

func NewKuadrantStatusUpdater(client *dynamic.DynamicClient, isGatewayAPIInstalled, isGatewayProviderInstalled, isLimitadorOperatorInstalled, isAuthorinoOperatorInstalled bool) *KuadrantStatusUpdater {
	return &KuadrantStatusUpdater{
		Client:                       client,
		isGatewayAPIInstalled:        isGatewayAPIInstalled,
		isGatewayProviderInstalled:   isGatewayProviderInstalled,
		isLimitadorOperatorInstalled: isLimitadorOperatorInstalled,
		isAuthorinoOperatorInstalled: isAuthorinoOperatorInstalled,
	}
}

func (r *KuadrantStatusUpdater) Subscription() *controller.Subscription {
	return &controller.Subscription{ReconcileFunc: r.Reconcile, Events: []controller.ResourceEventMatcher{
		{Kind: ptr.To(kuadrantv1beta1.AuthorinoGroupKind), EventType: ptr.To(controller.CreateEvent)},
		{Kind: ptr.To(kuadrantv1beta1.AuthorinoGroupKind), EventType: ptr.To(controller.UpdateEvent)},
		{Kind: ptr.To(kuadrantv1beta1.KuadrantGroupKind), EventType: ptr.To(controller.CreateEvent)},
		{Kind: ptr.To(kuadrantv1beta1.KuadrantGroupKind), EventType: ptr.To(controller.UpdateEvent)},
		{Kind: ptr.To(kuadrantv1beta1.LimitadorGroupKind), EventType: ptr.To(controller.CreateEvent)},
		{Kind: ptr.To(kuadrantv1beta1.LimitadorGroupKind), EventType: ptr.To(controller.UpdateEvent)},
		// required to compute mTLS status fields
		{Kind: &machinery.GatewayClassGroupKind},
		{Kind: &machinery.GatewayGroupKind},
		{Kind: &machinery.HTTPRouteGroupKind},
	},
	}
}

func (r *KuadrantStatusUpdater) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("KuadrantStatusUpdater").WithValues("context", ctx)
	logger.Info("reconciling kuadrant status", "status", "started")
	defer logger.Info("reconciling kuadrant status", "status", "completed")

	kObj := GetKuadrantFromTopology(topology)
	if kObj == nil {
		operatormetrics.SetKuadrantExists(false)
		operatormetrics.ResetKuadrantMetrics()
		return nil
	}

	// Kuadrant CR exists in the cluster
	operatormetrics.SetKuadrantExists(true)

	newStatus := r.calculateStatus(topology, logger, kObj, state)

	// Emit Kuadrant readiness metric
	isReady := meta.IsStatusConditionTrue(newStatus.Conditions, ReadyConditionType)
	operatormetrics.SetKuadrantReady(kObj.Namespace, kObj.Name, isReady)

	// Emit component readiness metrics
	r.emitComponentMetrics(topology, kObj.Namespace, logger)

	equalStatus := kObj.Status.Equals(newStatus, logger)
	logger.V(1).Info("Status", "status is different", !equalStatus)
	logger.V(1).Info("Status", "generation is different", kObj.Generation != kObj.Status.ObservedGeneration)
	if equalStatus && kObj.Generation == kObj.Status.ObservedGeneration {
		// Steady state
		logger.V(1).Info("Status was not updated", "status", "stale")
		return nil
	}

	// Save the generation number we acted on, otherwise we might wrongfully indicate
	// that we've seen a spec update when we retry.
	newStatus.ObservedGeneration = kObj.Generation

	logger.V(1).Info("Updating Status", "sequence no:", fmt.Sprintf("sequence No: %v->%v", kObj.Status.ObservedGeneration, newStatus.ObservedGeneration), "status", "updated")

	kObj.Status = *newStatus
	updateErr := r.updateKuadrantStatus(ctx, kObj)
	if updateErr != nil {
		logger.V(1).Error(updateErr, "updateKuadrantStatusError", "status", "error")
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(updateErr) {
			logger.Info("Failed to update status: resource might just be outdated", "status", "error")
			return nil
		}

		return nil
	}

	return nil
}
func (r *KuadrantStatusUpdater) updateKuadrantStatus(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	obj, err := controller.Destruct(kObj)
	if err != nil {
		return err
	}
	_, err = r.Client.Resource(kuadrantv1beta1.KuadrantsResource).Namespace(kObj.GetNamespace()).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
	return err
}

func (r *KuadrantStatusUpdater) calculateStatus(topology *machinery.Topology, logger logr.Logger, kObj *kuadrantv1beta1.Kuadrant, state *sync.Map) *kuadrantv1beta1.KuadrantStatus {
	newStatus := &kuadrantv1beta1.KuadrantStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions:         slices.Clone(kObj.Status.Conditions),
		ObservedGeneration: kObj.Status.ObservedGeneration,
		MtlsAuthorino:      mtlsAuthorino(kObj, state),
		MtlsLimitador:      mtlsLimitador(kObj, state),
	}

	availableCond := r.readyCondition(topology, logger)

	meta.SetStatusCondition(&newStatus.Conditions, *availableCond)

	return newStatus
}

func mtlsAuthorino(kObj *kuadrantv1beta1.Kuadrant, state *sync.Map) *bool {
	effectiveAuthPolicies, ok := state.Load(StateEffectiveAuthPolicies)
	if !ok {
		return ptr.To(false)
	}
	effectiveAuthPoliciesMap := effectiveAuthPolicies.(EffectiveAuthPolicies)
	return ptr.To(kObj.IsMTLSAuthorinoEnabled() && len(effectiveAuthPoliciesMap) > 0)
}

func mtlsLimitador(kObj *kuadrantv1beta1.Kuadrant, state *sync.Map) *bool {
	effectiveRateLimitPolicies, ok := state.Load(StateEffectiveRateLimitPolicies)
	if !ok {
		return ptr.To(false)
	}
	effectiveRateLimitPoliciesMap := effectiveRateLimitPolicies.(EffectiveRateLimitPolicies)
	return ptr.To(kObj.IsMTLSLimitadorEnabled() && len(effectiveRateLimitPoliciesMap) > 0)
}

func (r *KuadrantStatusUpdater) readyCondition(topology *machinery.Topology, logger logr.Logger) *metav1.Condition {
	cond := &metav1.Condition{
		Type:    ReadyConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  "Ready",
		Message: "Kuadrant is ready",
	}

	if err := r.isMissingDependency(); err != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = string(err.Reason())
		cond.Message = err.Error()
		return cond
	}

	if reason := checkLimitadorReady(topology, logger); reason != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "LimitadorNotReady"
		cond.Message = *reason
		return cond
	}

	if reason := checkAuthorinoAvailable(topology, logger); reason != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "AuthorinoNotReady"
		cond.Message = *reason
		return cond
	}

	return cond
}

func (r *KuadrantStatusUpdater) isMissingDependency() kuadrant.PolicyError {
	isMissingDependency := false
	var missingDependencies []string

	if !r.isGatewayAPIInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "Gateway API")
	}
	if !r.isGatewayProviderInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "Gateway API provider (istio / envoy gateway)")
	}
	if !r.isAuthorinoOperatorInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "Authorino Operator")
	}
	if !r.isLimitadorOperatorInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "Limitador Operator")
	}

	if isMissingDependency {
		return kuadrant.NewErrDependencyNotInstalled(missingDependencies...)
	}

	return nil
}

func checkLimitadorReady(topology *machinery.Topology, logger logr.Logger) *string {
	limitadorObj := GetLimitadorFromTopology(topology)
	if limitadorObj == nil {
		logger.V(1).Info("failed getting Limitador resource from topology", "status", "error")
		return ptr.To("limitador resource not in topology")
	}

	statusConditionReady := meta.FindStatusCondition(limitadorObj.Status.Conditions, limitadorv1alpha1.StatusConditionReady)
	if statusConditionReady == nil {
		return ptr.To("Ready condition not found")
	}
	if statusConditionReady.Status != metav1.ConditionTrue {
		return &statusConditionReady.Message
	}

	return nil
}

func checkAuthorinoAvailable(topology *machinery.Topology, logger logr.Logger) *string {
	authorinoObj := GetAuthorinoFromTopology(topology)
	if authorinoObj == nil {
		logger.V(1).Info("failed getting Authorino resource from topology", "status", "error")
		return ptr.To("authorino resource not in topology")
	}

	readyCondition := authorino.FindAuthorinoStatusCondition(authorinoObj.Status.Conditions, "Ready")
	if readyCondition == nil {
		return ptr.To("Ready condition not found")
	}

	if readyCondition.Status != corev1.ConditionTrue {
		return &readyCondition.Message
	}

	return nil
}

// emitComponentMetrics emits readiness metrics for Kuadrant-managed components (Authorino and Limitador).
// This is called during reconciliation when a Kuadrant CR exists to provide real-time visibility into component health.
// When no CR exists, component metrics are cleared via ResetKuadrantMetrics() instead.
func (r *KuadrantStatusUpdater) emitComponentMetrics(topology *machinery.Topology, namespace string, logger logr.Logger) {
	// Check Authorino readiness
	authorinoReady := checkAuthorinoAvailable(topology, logger) == nil
	operatormetrics.SetComponentReady("authorino", namespace, authorinoReady)

	// Check Limitador readiness
	limitadorReady := checkLimitadorReady(topology, logger) == nil
	operatormetrics.SetComponentReady("limitador", namespace, limitadorReady)
}
