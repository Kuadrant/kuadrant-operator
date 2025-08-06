package controllers

// TODO: When feature complete, remove all experimental code references.

import (
	"context"
	"sync"
	"time"

	"github.com/go-logr/logr"
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

	Resource10Mi = "10Mi"
	Resource10m  = "10m"

	ResilienceError = "resilienceError"
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
	state.Store(ResilienceError, false)

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

	if r.startCleanup(kObj) {
		logger.V(level).Info("Reconciling Rate Limiting resilient configuration", "status", "started")
		defer logger.V(level).Info("Reconciling Rate Limiting resilient configuration cleanup", "status", "completed")

		err := r.cleanupLimitadorDeployment(ctx, topology, logger)
		if err != nil {
			logger.V(level).Info("failed to cleanup limitador deployment", "status", "error", "error", err)
			state.Store(ResilienceError, true)
			return nil
		}

		err = r.cleanupLimitador(ctx, logger, lObj)
		if err != nil {
			logger.V(level).Info("failed to cleanup limitador resource", "status", "error", "error", err)
			state.Store(ResilienceError, true)
			return nil
		}

		return nil
	}

	if r.startReconcile(kObj) {
		logger.V(level).Info("Reconciling Rate Limiting resilient configuration creation", "status", "started")
		defer logger.V(level).Info("Reconciling Rate Limiting resilient configuration creation", "status", "completed")

		err := r.configureLimitador(ctx, logger, lObj)
		if err != nil {
			logger.V(level).Info("failed to configure limitador deployment", "status", "error", "error", err)
			state.Store(ResilienceError, true)
			return nil
		}

		err = r.configureLimitadorDeployment(ctx, topology, logger)
		if err != nil {
			logger.V(level).Info("failed to configure limitador deployment", "status", "error", "error", err)
			state.Store(ResilienceError, true)
			return nil
		}
	}

	return nil
}

func (r *ResilienceRateLimitingReconciler) configureLimitador(ctx context.Context, logger logr.Logger, lObj *limitadorv1alpha1.Limitador) error {
	update := false
	if !limitadorReplicasIsConfigured(lObj) {
		lObj.Spec.Replicas = ptr.To(LimitadorReplicas)
		update = true
	}

	if !limitadorPDBIsConfigured(lObj) {
		if lObj.Spec.PodDisruptionBudget == nil {
			logger.Info("setting the pdb", "status", "working")
			lObj.Spec.PodDisruptionBudget = &limitadorv1alpha1.PodDisruptionBudgetType{
				MaxUnavailable: &intstr.IntOrString{IntVal: LimitadorPDB},
			}
			update = true
		}
	}

	if !limitadorResourceRequestsIsConfigured(lObj) {
		logger.Info("setting the Resource Request", "status", "working")

		if lObj.Spec.ResourceRequirements == nil {
			lObj.Spec.ResourceRequirements = &corev1.ResourceRequirements{}
		}

		if lObj.Spec.ResourceRequirements.Requests == nil {
			lObj.Spec.ResourceRequirements.Requests = make(corev1.ResourceList)
		}

		if lObj.Spec.ResourceRequirements.Requests.Cpu().Value() == 0 {
			cpu, err := resource.ParseQuantity(Resource10m)
			if err != nil {
				logger.Error(err, "failed to parse resurce cpu string", "status", "error")
			}
			lObj.Spec.ResourceRequirements.Requests[corev1.ResourceCPU] = cpu
			update = true
		}

		if lObj.Spec.ResourceRequirements.Requests.Memory().Value() == 0 {
			memory, err := resource.ParseQuantity(Resource10Mi)
			if err != nil {
				logger.Error(err, "failed to parse resurce memory string", "status", "error")
			}
			lObj.Spec.ResourceRequirements.Requests[corev1.ResourceMemory] = memory
			update = true
		}
	}
	if update {
		_, err := r.updateLimitador(ctx, lObj)
		if err != nil {
			logger.V(level).Info("failed to update limitador resource", "status", "error", "error", err)
			return err
		}
	}
	return nil
}

func (r *ResilienceRateLimitingReconciler) configureLimitadorDeployment(ctx context.Context, topology *machinery.Topology, logger logr.Logger) error {
	update := false
	deployment := GetDeploymentForParent(topology, kuadrantv1beta1.LimitadorGroupKind)

	if !limitadorTopologySpreadConstranits(deployment) && deployment != nil {
		if deployment.Spec.Template.Spec.TopologySpreadConstraints == nil {
			deployment.Spec.Template.Spec.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{}
		}

		hostname, zone := false, false
		for _, item := range deployment.Spec.Template.Spec.TopologySpreadConstraints {
			logger.V(level).Info("Topology Spread Constraint item", "item", item)
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
			update = true
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
			update = true
		}
	}

	if update {
		err := r.updateDeployment(ctx, deployment)
		if err != nil {
			logger.V(level).Info("failed to update limitador deployment resource", "status", "error", "error", err)
			return err
		}
	}
	return nil
}

func (r *ResilienceRateLimitingReconciler) cleanupLimitador(ctx context.Context, logger logr.Logger, lObj *limitadorv1alpha1.Limitador) error {
	lObj.Spec.Replicas = ptr.To(1)
	lObj.Spec.PodDisruptionBudget = nil
	lObj.Spec.ResourceRequirements = nil
	newLObj, err := r.updateLimitador(ctx, lObj)
	if err != nil {
		logger.V(level).Info("failed to update limitador resource", "status", "error", "error", err)
		return err
	}

	newLObj.Spec.Replicas = nil
	// Sleep is required to allow limitador-operator to pick up initail changes before apply the second set.
	// Delay of 3 seconds was a random time. Could be short, but seems to work reliably.
	time.Sleep(3 * time.Second)
	_, err = r.updateLimitador(ctx, newLObj)
	if err != nil {
		logger.V(level).Info("failed to update limitador resource", "status", "error", "error", err)
		return err
	}

	return nil
}

func (r *ResilienceRateLimitingReconciler) cleanupLimitadorDeployment(ctx context.Context, topology *machinery.Topology, logger logr.Logger) error {
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
			return err
		}
	}
	return nil
}

func (r *ResilienceRateLimitingReconciler) startCleanup(kObj *kuadrantv1beta1.Kuadrant) bool {
	if kObj == nil {
		return false
	}

	if kObj.Status.Resilience == nil {
		return false
	}

	if kObj.Spec.Resilience == nil && *kObj.Status.Resilience.RateLimiting == kuadrantv1beta1.KuadrantDefined {
		return true
	}

	if kObj.Spec.Resilience == nil {
		return false
	}

	if !kObj.Spec.Resilience.RateLimiting && *kObj.Status.Resilience.RateLimiting == kuadrantv1beta1.KuadrantDefined {
		return true
	}

	return false
}

func (r *ResilienceRateLimitingReconciler) startReconcile(kObj *kuadrantv1beta1.Kuadrant) bool {
	if kObj == nil {
		return false
	}

	if kObj.Spec.Resilience == nil {
		return false
	}

	if kObj.Status.Resilience == nil {
		return false
	}

	if kObj.Spec.Resilience.RateLimiting && *kObj.Status.Resilience.RateLimiting == kuadrantv1beta1.KuadrantDefined {
		return true
	}

	return false
}

func (r *ResilienceRateLimitingReconciler) updateLimitador(ctx context.Context, lObj *limitadorv1alpha1.Limitador) (*limitadorv1alpha1.Limitador, error) {
	obj, err := controller.Destruct(lObj)
	if err != nil {
		return nil, err
	}
	rObj, err := r.Client.Resource(kuadrantv1beta1.LimitadorsResource).Namespace(lObj.GetNamespace()).Update(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	restructObj, err := controller.Restructure[limitadorv1alpha1.Limitador](rObj)
	if err != nil {
		return nil, err
	}
	return ptr.To(restructObj.(limitadorv1alpha1.Limitador)), err
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

	if r.startReconcile(kObj) {
		logger.V(level).Info("CounterStorage configured", "status", "contiune")

		lObj.Spec.Storage = nil
		if kObj.Spec.Resilience != nil {
			lObj.Spec.Storage = kObj.Spec.Resilience.CounterStorage
		}

		err := r.updateLimitador(ctx, lObj)
		if err != nil {
			logger.V(level).Info("failed to update limitador resource", "status", "error", "error", err)
			state.Store(ResilienceError, true)
			return nil
		}
	}

	return nil
}

func (r *ResilienceCounterStorageReconciler) startReconcile(kObj *kuadrantv1beta1.Kuadrant) bool {
	if kObj == nil {
		return false
	}

	if kObj.Status.Resilience == nil {
		return false
	}

	if *kObj.Status.Resilience.CounterStorage == kuadrantv1beta1.KuadrantDefined {
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
	logger := controller.LoggerFromContext(ctx).WithName("ResilienceDeploymentPostcondition")

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

func limitadorReplicasIsConfigured(lObj *limitadorv1alpha1.Limitador) bool {
	if lObj == nil {
		return false
	}

	if lObj.Spec.Replicas == nil {
		return false
	}

	return true
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
			count++
		}
	}

	// There is only two types of topology keys that we care about.
	return count >= 2
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
