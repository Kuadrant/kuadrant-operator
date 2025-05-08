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
)

const (
	ReadyConditionType               string = "Ready"
	ResilienceInfoRRConditionType    string = "Resilience.Info.RateLimiting.Replicas"
	ResilienceWarningRRConditionType string = "Resilience.Warning.RateLimiting.Replicas"
	ResilienceInfoPDBConditionType   string = "Resilience.Info.RateLimiting.PBD"
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
	logger := controller.LoggerFromContext(ctx).WithName("KuadrantStatusUpdater")
	logger.Info("reconciling kuadrant status", "status", "started")
	defer logger.Info("reconciling kuadrant status", "status", "completed")

	kObj := GetKuadrantFromTopology(topology)
	if kObj == nil {
		return nil
	}

	newStatus := r.calculateStatus(topology, logger, kObj, state)

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
		Resilience:         ptr.To(kObj.IsResilienceEnabled()),
	}

	availableCond := r.readyCondition(topology, logger)

	meta.SetStatusCondition(&newStatus.Conditions, *availableCond)

	setResilienceCond, removeResilienceCond := r.resilienceCondition(topology)

	for _, condition := range setResilienceCond {
		meta.SetStatusCondition(&newStatus.Conditions, *condition)
	}

	for _, condition := range removeResilienceCond {
		meta.RemoveStatusCondition(&newStatus.Conditions, condition)
	}

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

func (r *KuadrantStatusUpdater) resilienceCondition(topology *machinery.Topology) ([]*metav1.Condition, []string) {
	create := make([]*metav1.Condition, 0)
	remove := make([]string, 0)

	kObj := GetKuadrantFromTopology(topology)
	isConfigured := kObj.Spec.Resilience.IsRateLimitingConfigured()
	if !isConfigured {
		remove = append(remove, ResilienceInfoRRConditionType, ResilienceWarningRRConditionType, ResilienceInfoPDBConditionType)
		return nil, remove
	}

	lObj := GetLimitadorFromTopology(topology)
	if lObj == nil {
		remove = append(remove, ResilienceInfoRRConditionType, ResilienceWarningRRConditionType, ResilienceInfoPDBConditionType)
		return nil, remove
	}

	if lObj.Spec.Replicas != nil && *lObj.Spec.Replicas < LimitadorReplicas {
		cond := &metav1.Condition{
			Type:    ResilienceWarningRRConditionType,
			Message: fmt.Sprintf("Number of Limitador replicas (%v) below minimum default", *lObj.Spec.Replicas),
			Reason:  "UserModifiedLimitadorReplicas",
			Status:  metav1.ConditionUnknown,
		}
		create = append(create, cond)
	} else {
		remove = append(remove, ResilienceWarningRRConditionType)
	}

	if lObj.Spec.Replicas != nil && *lObj.Spec.Replicas > LimitadorReplicas {
		cond := &metav1.Condition{
			Type:    ResilienceInfoRRConditionType,
			Message: fmt.Sprintf("Number of Limitador replicas (%v) greater than minimum default", *lObj.Spec.Replicas),
			Reason:  "UserModifiedLimitadorReplicas",
			Status:  metav1.ConditionUnknown,
		}
		create = append(create, cond)
	} else {
		remove = append(remove, ResilienceInfoRRConditionType)
	}

	if lObj.Spec.PodDisruptionBudget != nil && lObj.Spec.PodDisruptionBudget.MaxUnavailable != nil && *&lObj.Spec.PodDisruptionBudget.MaxUnavailable.IntVal != LimitadorPDB {
		cond := &metav1.Condition{
			Type:    ResilienceInfoPDBConditionType,
			Message: "Limitador recource Pod Disruption Budget differs from default configuration",
			Reason:  "UserModifiedLimitadorPodDisruptionBudget",
			Status:  metav1.ConditionUnknown,
		}
		create = append(create, cond)
	} else if lObj.Spec.PodDisruptionBudget != nil && lObj.Spec.PodDisruptionBudget.MinAvailable != nil {
		cond := &metav1.Condition{
			Type:    ResilienceInfoPDBConditionType,
			Message: "Limitador recource Pod Disruption Budget differs from default configuration",
			Reason:  "UserModifiedLimitadorPodDisruptionBudget",
			Status:  metav1.ConditionUnknown,
		}
		create = append(create, cond)
	} else {
		remove = append(remove, ResilienceInfoPDBConditionType)
	}

	return create, remove
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
		return ptr.To("limitador resoure not in topology")
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
		return ptr.To("authorino resoure not in topology")
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
