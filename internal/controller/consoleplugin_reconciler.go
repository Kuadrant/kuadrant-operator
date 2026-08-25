package controllers

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	ctrlruntime "sigs.k8s.io/controller-runtime"

	"github.com/kuadrant/kuadrant-operator/internal/openshift"
	"github.com/kuadrant/kuadrant-operator/internal/openshift/consoleplugin"
	"github.com/kuadrant/kuadrant-operator/internal/reconcilers"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

//+kubebuilder:rbac:groups=console.openshift.io,resources=consoleplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=get;list;watch

type ConsolePluginReconciler struct {
	*reconcilers.BaseReconciler

	namespace     string
	imageOverride string
}

func NewConsolePluginReconciler(mgr ctrlruntime.Manager, namespace, imageOverride string) *ConsolePluginReconciler {
	return &ConsolePluginReconciler{
		BaseReconciler: reconcilers.NewBaseReconciler(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetAPIReader(),
		),
		namespace:     namespace,
		imageOverride: imageOverride,
	}
}

func (r *ConsolePluginReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Run,
		Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(openshift.ConsolePluginGVK.GroupKind())},
			{
				Kind:            ptr.To(ConfigMapGroupKind),
				ObjectNamespace: r.namespace,
				ObjectName:      TopologyConfigMapName,
				EventType:       ptr.To(controller.CreateEvent),
			},
			{
				Kind:            ptr.To(ConfigMapGroupKind),
				ObjectNamespace: r.namespace,
				ObjectName:      TopologyConfigMapName,
				EventType:       ptr.To(controller.DeleteEvent),
			},
		},
	}
}

func (r *ConsolePluginReconciler) Run(eventCtx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(eventCtx).WithName("ConsolePluginReconciler")
	ctx := logr.NewContext(eventCtx, logger)
	logger.V(1).Info("reconciling console plugin", "status", "started")
	defer logger.V(1).Info("reconciling console plugin", "status", "completed")

	existingTopologyConfigMaps := topology.Objects().Items(func(object machinery.Object) bool {
		return object.GetName() == TopologyConfigMapName && object.GetNamespace() == r.namespace && object.GroupVersionKind().Kind == ConfigMapGroupKind.Kind
	})

	topologyExists := len(existingTopologyConfigMaps) > 0

	clusterVersions := topology.Objects().Items(func(object machinery.Object) bool {
		return object.GetName() == "version" && object.GroupVersionKind().GroupKind() == openshift.ClusterVersionGroupKind.GroupKind()
	})

	clusterVersionExists := len(clusterVersions) > 0
	consolePluginSupported := clusterVersionExists || r.imageOverride != ""

	// Service
	service := consoleplugin.Service(r.namespace)
	if !topologyExists || !consolePluginSupported {
		utils.TagObjectToDelete(service)
	}
	_, err := r.ReconcileResource(ctx, &corev1.Service{}, service, reconcilers.CreateOnlyMutator)
	if err != nil {
		logger.Error(err, "reconciling service")
		return err
	}

	// Deployment
	var consolePluginImageURL string
	if topologyExists && r.imageOverride != "" {
		consolePluginImageURL = r.imageOverride
	} else if topologyExists && clusterVersionExists {
		clusterVersion := clusterVersions[0].(*controller.RuntimeObject).Object.(*configv1.ClusterVersion)

		consolePluginImageURL, err = openshift.GetConsolePluginImageForVersion(clusterVersion)
		if err != nil {
			logger.Error(err, "failed to get console plugin image for OpenShift version")
			return err
		}
	}

	deployment := consoleplugin.Deployment(r.namespace, consolePluginImageURL, TopologyConfigMapName)
	if r.imageOverride != "" {
		deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullIfNotPresent
	}
	deploymentMutators := make([]reconcilers.DeploymentMutateFn, 0, 2)
	deploymentMutators = append(deploymentMutators, reconcilers.DeploymentImageMutator)
	deploymentMutators = append(deploymentMutators, consoleplugin.DeploymentConfigMutator)
	if !topologyExists || !consolePluginSupported {
		utils.TagObjectToDelete(deployment)
	}
	_, err = r.ReconcileResource(ctx, &appsv1.Deployment{}, deployment, reconcilers.DeploymentMutator(deploymentMutators...))
	if err != nil {
		logger.Error(err, "reconciling deployment")
		return err
	}

	// Remove the nginx configuration left behind by older Console plugin
	// deployments. The combined asset server/backend no longer mounts it.
	legacyNginxConfigMap := consoleplugin.LegacyNginxConfigMap(r.namespace)
	utils.TagObjectToDelete(legacyNginxConfigMap)
	_, err = r.ReconcileResource(ctx, &corev1.ConfigMap{}, legacyNginxConfigMap, reconcilers.CreateOnlyMutator)
	if err != nil {
		logger.Error(err, "deleting legacy nginx configmap")
		return err
	}

	// ConsolePlugin
	consolePlugin := consoleplugin.ConsolePlugin(r.namespace)
	if !topologyExists || !consolePluginSupported {
		utils.TagObjectToDelete(consolePlugin)
	}
	consolePluginMutator := reconcilers.Mutator[*consolev1.ConsolePlugin](consoleplugin.SpecMutator)
	_, err = r.ReconcileResource(ctx, &consolev1.ConsolePlugin{}, consolePlugin, consolePluginMutator)
	if err != nil {
		logger.Error(err, "reconciling consoleplugin")
		return err
	}

	logger.V(1).Info("task ended")
	return nil
}
