package controllers

// TODO: When feature complete, remove all experimental code references.

import (
	"context"
	"sync"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

// WARNING: level varible is only here for the basic dev work and should not end up in the finished feature
// FIXME: don't merge to main with value of zero, set to one.
var level = 1

const (
	ExperimentalResilienceFeature = "ExperimentalResilienceFeature"
	ResilienceFeatureAnnotation   = "kuadrant.io/experimental-dont-use-resilient-data-plane"
	LimitadorReplicas             = 2
	LimitadorPDB                  = 1

	Resource_10Mi = "10Mi"
	Resource_10m  = "10m"
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
		NewResilienceRateLimitingReconciler(client).Subscription().Reconcile,
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

func NewResilienceRateLimitingReconciler(client *dynamic.DynamicClient) *ResilienceRateLimitingReconciler {
	return &ResilienceRateLimitingReconciler{Client: client}
}

type ResilienceRateLimitingReconciler struct {
	Client *dynamic.DynamicClient
}

func (r *ResilienceRateLimitingReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &kuadrantv1beta1.LimitadorGroupKind},
		},
	}
}

func (r *ResilienceRateLimitingReconciler) reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("ResilienceRateLimitingReconciler")

	logger.V(level).Info("ResilienceRateLimitingReconciler Task", "status", "started")
	defer logger.V(level).Info("ResilienceRateLimitingReconciler Task", "status", "completed")
	if !experimentalFeatureEnabledSate(state) {
		logger.V(level).Info("Experimental resilience feature is not enabled, early exit", "status", "exiting")
		return nil
	}
	logger.V(level).Info("Experimental resilience feature is enabled", "status", "processing")

	history := GetHistory()

	kObj := GetKuadrantFromTopology(topology)
	if kObj == nil {
		logger.V(level).Info("kuadrant resource has not being created yet.")
		return nil
	}
	lObj := GetLimitadorFromTopology(topology)
	if lObj == nil {
		logger.V(level).Info("limitador resource has not being created yet.")
		return nil
	}

	wasConfigured := false
	if history.kuadrant != nil {
		wasConfigured = history.kuadrant.Spec.Resilience.IsRateLimitingConfigured()
	}
	nowConfigured := kObj.Spec.Resilience.IsRateLimitingConfigured()

	if wasConfigured && !nowConfigured {
		deployment := GetDeploymentForParent(topology, kuadrantv1beta1.LimitadorGroupKind)
		if deployment != nil {
			constraints := []corev1.TopologySpreadConstraint{}
			for _, item := range deployment.Spec.Template.Spec.TopologySpreadConstraints {
				if item.TopologyKey == "kubernetes.io/hostname" || item.TopologyKey == "kubernetes.io/zone" {
					logger.V(level).Info("skipping item", "item", item)
					continue
				}
				logger.V(level).Info("adding item", "item", item)
				constraints = append(constraints, item)
			}

			deployment.Spec.Template.Spec.TopologySpreadConstraints = constraints
			err := r.updateDeployment(ctx, deployment)
			if err != nil {
				logger.V(level).Info("failed to update limitador deployment", "status", "error", "error", err)
				return nil
			}
		}

		lObj.Spec.Replicas = ptr.To(1)
		lObj.Spec.PodDisruptionBudget = nil
		lObj.Spec.ResourceRequirements = nil
		err := r.updateLimitador(ctx, lObj)
		if err != nil {
			logger.V(level).Info("failed to update limitador resource", "status", "error", "error", err)
			return nil
		}

	}

	if !nowConfigured {
		logger.V(level).Info("RateLimiting not configured", "status", "exiting")
		return nil
	}
	logger.V(level).Info("RateLimiting configured", "status", "contiune")

	writeLimitador := false
	if lObj.Spec.Replicas == nil || !wasConfigured {
		lObj.Spec.Replicas = ptr.To(LimitadorReplicas)
		writeLimitador = true
	}

	if !limitadorPDBIsConfigured(lObj) || !wasConfigured {
		if lObj.Spec.PodDisruptionBudget == nil {
			logger.Info("setting the pdb", "status", "working")
			lObj.Spec.PodDisruptionBudget = &limitadorv1alpha1.PodDisruptionBudgetType{
				MaxUnavailable: &intstr.IntOrString{IntVal: LimitadorPDB},
			}
			writeLimitador = true
		}
	}

	if !limitadorResourceRequestsIsConfigured(lObj) || !wasConfigured {
		logger.Info("setting the Resource Request", "status", "working")
		cpu, err := resource.ParseQuantity(Resource_10m)
		if err != nil {
			logger.Error(err, "failed to parse resurce cpu string", "status", "error")
		}
		memory, err := resource.ParseQuantity(Resource_10Mi)
		if err != nil {
			logger.Error(err, "failed to parse resurce memory string", "status", "error")
		}

		if lObj.Spec.ResourceRequirements == nil {
			lObj.Spec.ResourceRequirements = &corev1.ResourceRequirements{}
		}

		if lObj.Spec.ResourceRequirements.Requests.Cpu().Value() == 0 {
			if lObj.Spec.ResourceRequirements.Requests == nil {
				lObj.Spec.ResourceRequirements.Requests = corev1.ResourceList{corev1.ResourceCPU: cpu}
			} else {
				lObj.Spec.ResourceRequirements.Requests[corev1.ResourceCPU] = cpu
			}
		}

		if lObj.Spec.ResourceRequirements.Requests.Memory().Value() == 0 {
			if lObj.Spec.ResourceRequirements.Requests == nil {
				lObj.Spec.ResourceRequirements.Requests = corev1.ResourceList{corev1.ResourceMemory: memory}
			} else {
				lObj.Spec.ResourceRequirements.Requests[corev1.ResourceMemory] = memory
			}
		}

		writeLimitador = true
	}

	if writeLimitador {
		err := r.updateLimitador(ctx, lObj)
		if err != nil {
			logger.V(level).Info("failed to update limitador resource", "status", "error", "error", err)
			return nil
		}
	}

	writeLimitadorDeployment := false

	deployment := GetDeploymentForParent(topology, kuadrantv1beta1.LimitadorGroupKind)

	if !limitadorTopologySpreadConstranits(deployment) {
		hostname, zone := false, false
		for _, item := range deployment.Spec.Template.Spec.TopologySpreadConstraints {
			logger.V(level).Info("TSC item", "item", item)
			if item.TopologyKey == "kubernetes.io/hostname" {
				logger.V(level).Info("hostname", "value", true)
				hostname = true
			}
			if item.TopologyKey == "kubernetes.io/zone" {
				logger.V(level).Info("zone", "value", true)
				zone = true
			}
		}

		if !hostname {
			hostnameConstraint := corev1.TopologySpreadConstraint{
				MaxSkew:           1,
				TopologyKey:       "kubernetes.io/hostname",
				WhenUnsatisfiable: "ScheduleAnyway",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"limitador-reource": "limitador"},
				},
			}
			deployment.Spec.Template.Spec.TopologySpreadConstraints = append(deployment.Spec.Template.Spec.TopologySpreadConstraints, hostnameConstraint)
			writeLimitadorDeployment = true
		}

		if !zone {
			zoneConstraint := corev1.TopologySpreadConstraint{
				MaxSkew:           1,
				TopologyKey:       "kubernetes.io/zone",
				WhenUnsatisfiable: "ScheduleAnyway",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"limitador-reource": "limitador"},
				},
			}
			deployment.Spec.Template.Spec.TopologySpreadConstraints = append(deployment.Spec.Template.Spec.TopologySpreadConstraints, zoneConstraint)
			writeLimitadorDeployment = true
		}

	}

	if writeLimitadorDeployment {
		err := r.updateDeployment(ctx, deployment)
		if err != nil {
			logger.V(level).Info("failed to update limitador deployment resource", "status", "error", "error", err)
			return nil
		}
	}

	return nil
}

func (r *ResilienceRateLimitingReconciler) updateLimitador(ctx context.Context, lObj *limitadorv1alpha1.Limitador) error {
	obj, err := controller.Destruct(lObj)
	if err != nil {
		return err
	}
	_, err = r.Client.Resource(kuadrantv1beta1.LimitadorsResource).Namespace(lObj.GetNamespace()).Update(ctx, obj, metav1.UpdateOptions{})
	return err
}

func (r *ResilienceRateLimitingReconciler) updateDeployment(ctx context.Context, deployment *appsv1.Deployment) error {
	obj, err := controller.Destruct(deployment)
	if err != nil {
		return err
	}
	_, err = r.Client.Resource(appsv1.SchemeGroupVersion.WithResource("deployments")).Namespace(deployment.GetNamespace()).Update(ctx, obj, metav1.UpdateOptions{})
	return err
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

	history := GetHistory()

	kObj := GetKuadrantFromTopology(topology)
	lObj := GetLimitadorFromTopology(topology)
	if lObj == nil {
		logger.V(level).Info("limitador resource has not being created yet.")
		return nil
	}

	wasConfigured := r.isConfigured(history.kuadrant)
	nowConfigured := r.isConfigured(kObj)

	if wasConfigured && !nowConfigured {
		logger.V(level).Info("spec.storage should be removed from the limitador resource", "status", "cleanup")
		lObj.Spec.Storage = nil
		err := r.updateLimitador(ctx, lObj)
		if err != nil {
			logger.V(level).Info("failed to update limitador resource", "status", "error", "error", err)
			return nil
		}
		return nil
	}

	if !nowConfigured {
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

func limitadorPDBIsConfigured(lObj *limitadorv1alpha1.Limitador) bool {
	if lObj == nil {
		return false
	}

	if lObj.Spec.PodDisruptionBudget == nil {
		return false
	}

	if lObj.Spec.PodDisruptionBudget.MaxUnavailable == nil && lObj.Spec.PodDisruptionBudget.MinAvailable == nil {
		return false
	}

	return true
}

func limitadorResourceRequestsIsConfigured(lObj *limitadorv1alpha1.Limitador) bool {
	if lObj == nil {
		return false
	}

	if lObj.Spec.ResourceRequirements == nil {
		return false
	}

	if lObj.Spec.ResourceRequirements.Requests == nil {
		return false
	}

	if lObj.Spec.ResourceRequirements.Requests.Cpu().Value() == 0 || lObj.Spec.ResourceRequirements.Requests.Memory().Value() == 0 {
		return false
	}

	return true
}

func limitadorTopologySpreadConstranits(deployment *appsv1.Deployment) bool {
	if deployment == nil {
		return false
	}

	if deployment.Spec.Template.Spec.TopologySpreadConstraints == nil {
		return false
	}

	count := 0
	for _, item := range deployment.Spec.Template.Spec.TopologySpreadConstraints {
		if item.TopologyKey == "kubernetes.io/hostname" || item.TopologyKey == "kubernetes.io/zone" {
			count += 1
		}
	}

	// There is only two types of topologh keys that we care about.
	if count < 2 {
		return false
	}

	return true
}

// GetDeploymentForParent returns the deployment for the kind in the topology, if a deployment has being linked.
func GetDeploymentForParent(topology *machinery.Topology, groupKind schema.GroupKind) *appsv1.Deployment {
	// read deployment objects that are children of the groupKind
	deploymentObjs := lo.FilterMap(topology.Objects().Children(GetMachineryObjectFromTopology(topology, groupKind)), func(child machinery.Object, _ int) (*appsv1.Deployment, bool) {
		if child.GroupVersionKind().GroupKind() != kuadrantv1beta1.DeploymentGroupKind {
			return nil, false
		}

		runtimeObj, ok := child.(*controller.RuntimeObject)
		if !ok {
			return nil, false
		}

		// cannot do "return runtimeObj.Object.(*appsv1.Deployment)" as strict mode is used and does not match main method signature.
		deployment, ok := runtimeObj.Object.(*appsv1.Deployment)
		return deployment, ok
	})

	if len(deploymentObjs) == 0 {
		// Nothing to be done.
		return nil
	}

	// Currently only one instance of deployment is supported as a child of limitaodor
	// Needs to be deep copied to avoid race conditions with the object living in the topology
	return deploymentObjs[0].DeepCopy()

}
