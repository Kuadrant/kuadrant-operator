package controllers

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
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

	namespace string

	// Simple cache - just the image URL
	mu                  sync.RWMutex
	cachedImageURL      string
	imageURLInitialized bool
}

func NewConsolePluginReconciler(mgr ctrlruntime.Manager, namespace string) *ConsolePluginReconciler {
	return &ConsolePluginReconciler{
		BaseReconciler: reconcilers.NewBaseReconciler(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetAPIReader(),
		),
		namespace: namespace,
	}
}

func (r *ConsolePluginReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Run,
		Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(openshift.ConsolePluginGVK.GroupKind())},
			{
				Kind:       ptr.To(openshift.ClusterVersionGroupKind),
				ObjectName: "version",
			},
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

// getConsolePluginImageURL returns the cached image URL, initializing if necessary
func (r *ConsolePluginReconciler) getConsolePluginImageURL(ctx context.Context) (string, error) {
	r.mu.RLock()
	if r.imageURLInitialized {
		defer r.mu.RUnlock()
		return r.cachedImageURL, nil
	}
	r.mu.RUnlock()

	return r.refreshImageURL(ctx)
}

// refreshImageURL calls the API and updates the cache
func (r *ConsolePluginReconciler) refreshImageURL(ctx context.Context) (string, error) {
	imageURL, err := openshift.GetConsolePluginImageForVersion(ctx, r.Client())
	if err != nil {
		return "", err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.cachedImageURL = imageURL
	r.imageURLInitialized = true

	return imageURL, nil
}

func (r *ConsolePluginReconciler) Run(eventCtx context.Context, events []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(eventCtx).WithName("ConsolePluginReconciler")
	ctx := logr.NewContext(eventCtx, logger)
	logger.V(1).Info("reconciling console plugin", "status", "started")
	defer logger.V(1).Info("reconciling console plugin", "status", "completed")

	for _, event := range events {
		if event.Kind.Kind == "ClusterVersion" {
			logger.V(1).Info("ClusterVersion changed, refreshing console plugin image URL")
			if _, err := r.refreshImageURL(ctx); err != nil {
				logger.Error(err, "failed to refresh console plugin image URL")
				return err
			}
			break
		}
	}

	existingTopologyConfigMaps := topology.Objects().Items(func(object machinery.Object) bool {
		return object.GetName() == TopologyConfigMapName && object.GetNamespace() == r.namespace && object.GroupVersionKind().Kind == ConfigMapGroupKind.Kind
	})

	topologyExists := len(existingTopologyConfigMaps) > 0

	consolePluginImageURL, err := r.getConsolePluginImageURL(ctx)
	if err != nil {
		return err
	}

	// Service
	service := consoleplugin.Service(r.namespace)
	if !topologyExists {
		utils.TagObjectToDelete(service)
	}
	err = r.ReconcileResource(ctx, &corev1.Service{}, service, reconcilers.CreateOnlyMutator)
	if err != nil {
		logger.Error(err, "reconciling service")
		return err
	}

	// Deployment
	deployment := consoleplugin.Deployment(r.namespace, consolePluginImageURL, TopologyConfigMapName)
	deploymentMutators := make([]reconcilers.DeploymentMutateFn, 0)
	deploymentMutators = append(deploymentMutators, reconcilers.DeploymentImageMutator)
	if !topologyExists {
		utils.TagObjectToDelete(deployment)
	}
	err = r.ReconcileResource(ctx, &appsv1.Deployment{}, deployment, reconcilers.DeploymentMutator(deploymentMutators...))
	if err != nil {
		logger.Error(err, "reconciling deployment")
		return err
	}

	// Nginx ConfigMap
	nginxConfigMap := consoleplugin.NginxConfigMap(r.namespace)
	if !topologyExists {
		utils.TagObjectToDelete(nginxConfigMap)
	}
	err = r.ReconcileResource(ctx, &corev1.ConfigMap{}, nginxConfigMap, reconcilers.CreateOnlyMutator)
	if err != nil {
		logger.Error(err, "reconciling nginx configmap")
		return err
	}

	// ConsolePlugin
	consolePlugin := consoleplugin.ConsolePlugin(r.namespace)
	if !topologyExists {
		utils.TagObjectToDelete(consolePlugin)
	}
	consolePluginMutator := reconcilers.Mutator[*consolev1.ConsolePlugin](consoleplugin.ServiceMutator)
	err = r.ReconcileResource(ctx, &consolev1.ConsolePlugin{}, consolePlugin, consolePluginMutator)
	if err != nil {
		logger.Error(err, "reconciling consoleplugin")
		return err
	}

	logger.V(1).Info("task ended")
	return nil
}
