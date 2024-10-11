package controllers

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	consolev1 "github.com/openshift/api/console/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/env"
	"k8s.io/utils/ptr"
	ctrlruntime "sigs.k8s.io/controller-runtime"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
	"github.com/kuadrant/kuadrant-operator/pkg/openshift"
	"github.com/kuadrant/kuadrant-operator/pkg/openshift/consoleplugin"
)

//+kubebuilder:rbac:groups=console.openshift.io,resources=consoleplugins,verbs=get;list;watch;create;update;patch;delete

var (
	ConsolePluginImageURL = env.GetString("RELATED_IMAGE_CONSOLEPLUGIN", "quay.io/kuadrant/console-plugin:latest")
)

type ConsolePluginReconciler struct {
	*reconcilers.BaseReconciler

	namespace string
}

func NewConsolePluginReconciler(mgr ctrlruntime.Manager, namespace string) *ConsolePluginReconciler {
	return &ConsolePluginReconciler{
		BaseReconciler: reconcilers.NewBaseReconciler(
			mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
			log.Log.WithName("consoleplugin"),
			mgr.GetEventRecorderFor("ConsolePlugin"),
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
	logger := r.Logger()
	logger.V(1).Info("task started")
	ctx := logr.NewContext(eventCtx, logger)

	existingTopologyConfigMaps := topology.Objects().Items(func(object machinery.Object) bool {
		return object.GetName() == TopologyConfigMapName && object.GetNamespace() == r.namespace && object.GroupVersionKind().Kind == ConfigMapGroupKind.Kind
	})

	topologyExists := len(existingTopologyConfigMaps) > 0

	// Service
	service := consoleplugin.Service(r.namespace)
	if !topologyExists {
		utils.TagObjectToDelete(service)
	}
	err := r.ReconcileResource(ctx, &corev1.Service{}, service, reconcilers.CreateOnlyMutator)
	if err != nil {
		logger.Error(err, "reconciling service")
		return err
	}

	// Deployment
	deployment := consoleplugin.Deployment(r.namespace, ConsolePluginImageURL, TopologyConfigMapName)
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
	err = r.ReconcileResource(ctx, &consolev1.ConsolePlugin{}, consolePlugin, consoleplugin.Mutator(consoleplugin.ServiceMutator))
	if err != nil {
		logger.Error(err, "reconciling consoleplugin")
		return err
	}

	logger.V(1).Info("task ended")
	return nil
}
