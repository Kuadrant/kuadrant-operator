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
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/utils/ptr"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/openshift"
	"github.com/kuadrant/kuadrant-operator/internal/openshift/consoleplugin"
	"github.com/kuadrant/kuadrant-operator/internal/reconcilers"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

//+kubebuilder:rbac:groups=console.openshift.io,resources=consoleplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

type ConsolePluginReconciler struct {
	*reconcilers.BaseReconciler

	namespace     string
	imageOverride consoleplugin.Image
}

func NewConsolePluginReconciler(mgr ctrlruntime.Manager, namespace string, imageOverride consoleplugin.Image) *ConsolePluginReconciler {
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
			{Kind: ptr.To(openshift.ClusterVersionGroupKind.GroupKind()), ObjectName: "version"},
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
			{
				Kind:            ptr.To(v1beta1.NetworkPolicyGroupKind),
				ObjectNamespace: r.namespace,
				ObjectName:      consoleplugin.NetworkPolicyName(),
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
	consolePluginSupported := clusterVersionExists || r.imageOverride.URL != ""

	// Resolve the image and serving configuration before changing resources.
	var image consoleplugin.Image
	var err error
	if topologyExists && r.imageOverride.URL != "" {
		image = r.imageOverride
	} else if topologyExists && clusterVersionExists {
		clusterVersion := clusterVersions[0].(*controller.RuntimeObject).Object.(*configv1.ClusterVersion)
		image, err = openshift.GetConsolePluginImageForVersion(clusterVersion)
		if err != nil {
			return err
		}
	}

	// Apply ingress protection before starting the backend. The topology
	// ConfigMap anchors the plugin's lifecycle, including garbage collection.
	networkPolicy := consoleplugin.NetworkPolicy(r.namespace)
	if !topologyExists || !consolePluginSupported {
		utils.TagObjectToDelete(networkPolicy)
	} else {
		owner := existingTopologyConfigMaps[0].(*controller.RuntimeObject).Object
		if err := controllerutil.SetOwnerReference(owner, networkPolicy, r.Scheme()); err != nil {
			return err
		}
	}
	_, err = r.ReconcileResource(ctx, &networkingv1.NetworkPolicy{}, networkPolicy,
		reconcilers.Mutator[*networkingv1.NetworkPolicy](consoleplugin.NetworkPolicyMutator))
	if err != nil {
		logger.Error(err, "reconciling network policy")
		return err
	}

	// Service
	service := consoleplugin.Service(r.namespace)
	if !topologyExists || !consolePluginSupported {
		utils.TagObjectToDelete(service)
	}
	_, err = r.ReconcileResource(ctx, &corev1.Service{}, service, reconcilers.CreateOnlyMutator)
	if err != nil {
		logger.Error(err, "reconciling service")
		return err
	}

	// Create nginx configuration before any pod can reference it. Retain an
	// existing ConfigMap for Go deployments: old pods may still need it during
	// a rolling update. It is removed when the plugin is removed.
	if !topologyExists || !consolePluginSupported || image.Runtime == consoleplugin.RuntimeNginx {
		configMap := consoleplugin.LegacyNginxConfigMap(r.namespace)
		if !topologyExists || !consolePluginSupported {
			utils.TagObjectToDelete(configMap)
		}
		_, err = r.ReconcileResource(ctx, &corev1.ConfigMap{}, configMap, reconcilers.CreateOnlyMutator)
		if err != nil {
			return err
		}
	}

	// Deployment
	deployment := consoleplugin.Deployment(r.namespace, image, TopologyConfigMapName)
	if r.imageOverride.URL != "" {
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

	// ConsolePlugin
	consolePlugin := consoleplugin.ConsolePlugin(r.namespace, image.Runtime)
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
