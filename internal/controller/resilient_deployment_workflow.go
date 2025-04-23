package controllers

// TODO: When feature complete, remove all experimental code references.

import (
	"context"
	"sync"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

// WARNING: level varible is only here for the basic dev work and should not end up in the finished feature
// FIXME: don't merge to main with value of zero, set to one.
var level = 1

const (
	ExperimentalResilienceFeature = "ExperimentalResilienceFeature"
	ResilienceFeatureAnnotation   = "kuadrant.io/experimental-dont-use-resilient-data-plane"
)

func NewResilienceDeploymentWorkflow(client *dynamic.DynamicClient) *controller.Workflow {
	return &controller.Workflow{
		Precondition:  NewResilienceDeploymentPrecondition().Subscription().Reconcile,
		Tasks:         NewResilienceDeploymentTasks(client),
		Postcondition: NewResilienceDeploymentPostcondition().Subscription().Reconcile,
	}
}

// INFO: Precontion Section

func NewResilienceDeploymentPrecondition() *ResilienceDeploymentPrecondition {
	return &ResilienceDeploymentPrecondition{}
}

type ResilienceDeploymentPrecondition struct{}

func (r *ResilienceDeploymentPrecondition) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.run,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			// TODO: review the limitador event watch after the feature comes out of experimental
			{Kind: &kuadrantv1beta1.LimitadorGroupKind},
		},
	}
}

func (r *ResilienceDeploymentPrecondition) run(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("ResilienceDeploymentPrecondition")
	logger.V(level).Info("ResilienceDeployment Precondition", "status", "started")
	defer logger.V(level).Info("ResilienceDeployment Precondition", "status", "completed")

	state.Store(ExperimentalResilienceFeature, isExperimentalFeatureEnabled(topology))

	return nil
}

// INFO: Task Section

func NewResilienceDeploymentTasks(client *dynamic.DynamicClient) []controller.ReconcileFunc {
	return []controller.ReconcileFunc{
		NewResilienceAuthorizationReconciler().Subscription().Reconcile,
		NewResilienceCounterStorageReconciler(client).Subscription().Reconcile,
		NewResilienceRateLimitingReconciler().Subscription().Reconcile,
	}
}

func NewResilienceAuthorizationReconciler() *ResilienceAuthorizationReconciler {
	return &ResilienceAuthorizationReconciler{}
}

type ResilienceAuthorizationReconciler struct{}

func (r *ResilienceAuthorizationReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
		},
	}
}

func (r *ResilienceAuthorizationReconciler) reconcile(ctx context.Context, _ []controller.ResourceEvent, _ *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("ResilienceAuthorizationReconciler")

	logger.V(level).Info("ResilienceAuthorizationReconciler Task", "status", "started")
	defer logger.V(level).Info("ResilienceAuthorizationReconciler Task", "status", "completed")
	if !experimentalFeatureEnabledSate(state) {
		logger.V(level).Info("Experimental resilience feature is not enabled, early exit", "status", "exiting")
		return nil
	}
	logger.V(level).Info("Experimental resilience feature is enabled", "status", "processing")

	return nil
}

func NewResilienceRateLimitingReconciler() *ResilienceRateLimitingReconciler {
	return &ResilienceRateLimitingReconciler{}
}

type ResilienceRateLimitingReconciler struct{}

func (r *ResilienceRateLimitingReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
		},
	}
}

func (r *ResilienceRateLimitingReconciler) reconcile(ctx context.Context, _ []controller.ResourceEvent, _ *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("ResilienceRateLimitingReconciler")

	logger.V(level).Info("ResilienceRateLimitingReconciler Task", "status", "started")
	defer logger.V(level).Info("ResilienceRateLimitingReconciler Task", "status", "completed")
	if !experimentalFeatureEnabledSate(state) {
		logger.V(level).Info("Experimental resilience feature is not enabled, early exit", "status", "exiting")
		return nil
	}
	logger.V(level).Info("Experimental resilience feature is enabled", "status", "processing")

	return nil
}

func NewResilienceCounterStorageReconciler(client *dynamic.DynamicClient) *ResilienceCounterStorageReconciler {
	return &ResilienceCounterStorageReconciler{
		Client: client,
	}
}

type ResilienceCounterStorageReconciler struct {
	Client *dynamic.DynamicClient
}

func (r *ResilienceCounterStorageReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &kuadrantv1beta1.LimitadorGroupKind},
		},
	}
}

func (r *ResilienceCounterStorageReconciler) reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("ResilienceCounterStorageReconciler")

	logger.V(level).Info("ResilienceCounterStorageReconciler Task", "status", "started")
	defer logger.V(level).Info("ResilienceCounterStorageReconciler Task", "status", "completed")
	if !experimentalFeatureEnabledSate(state) {
		logger.V(level).Info("Experimental resilience feature is not enabled, early exit", "status", "exiting")
		return nil
	}
	logger.V(level).Info("Experimental resilience feature is enabled", "status", "processing")

	kObj := GetKuadrantFromTopology(topology)
	lObj := GetLimitadorFromTopology(topology)
	if lObj == nil {
		logger.V(level).Info("limitador resource has not being created yet.")
		return nil
	}

	if !r.isConfigured(kObj) {
		logger.V(level).Info("CounterStorage not configured", "status", "exiting")
		return nil
	}
	logger.V(level).Info("CounterStorage configured", "status", "contiune")

	lObj.Spec.Storage = kObj.Spec.Resilience.CounterStorage
	err := r.updateLimitador(ctx, lObj)
	if err != nil {
		logger.V(level).Info("failed to update limitador resource", "status", "error", "error", err)
		return nil
	}

	return nil
}

func (r *ResilienceCounterStorageReconciler) isConfigured(kObj *kuadrantv1beta1.Kuadrant) bool {
	if kObj == nil {
		return false
	}
	if resilience := kObj.Spec.Resilience; resilience == nil {
		return false
	}
	if configuration := kObj.Spec.Resilience.CounterStorage; configuration != nil {
		return true
	}
	return false
}

func (r *ResilienceCounterStorageReconciler) updateLimitador(ctx context.Context, lObj *limitadorv1alpha1.Limitador) error {
	obj, err := controller.Destruct(lObj)
	if err != nil {
		return err
	}
	_, err = r.Client.Resource(kuadrantv1beta1.LimitadorsResource).Namespace(lObj.GetNamespace()).Update(ctx, obj, metav1.UpdateOptions{})
	return err
}

// INFO: Postconditon Section

func NewResilienceDeploymentPostcondition() *ResilienceDeploymentPostcondition {
	return &ResilienceDeploymentPostcondition{}
}

type ResilienceDeploymentPostcondition struct{}

func (r *ResilienceDeploymentPostcondition) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.run,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
		},
	}
}

func (r *ResilienceDeploymentPostcondition) run(ctx context.Context, _ []controller.ResourceEvent, _ *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("ResilienceDeploymentPrecondition")

	logger.V(level).Info("ResilienceDeployment Postcondition", "status", "started")
	defer logger.V(level).Info("ResilienceDeployment Postcondition", "status", "completed")
	return nil
}

// INFO: Local Functions

func isExperimentalFeatureEnabled(topology *machinery.Topology) bool {
	k := GetKuadrantFromTopology(topology)
	if k == nil {
		return false
	}

	if val, exists := k.GetAnnotations()[ResilienceFeatureAnnotation]; exists {
		return val == "true"
	}
	return false
}

func experimentalFeatureEnabledSate(state *sync.Map) bool {
	value, ok := state.Load(ExperimentalResilienceFeature)
	if ok {
		return value.(bool)
	}
	return false
}
