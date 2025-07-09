//go:build unit

package controllers

import (
	"context"
	"testing"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	controllersfake "github.com/kuadrant/kuadrant-operator/internal/controller/fake"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/internal/openshift"
	"github.com/kuadrant/kuadrant-operator/internal/openshift/consoleplugin"
)

var (
	TestNamespace         = "test-namespace"
	ConsolePluginImageURL = "quay.io/kuadrant/console-plugin:latest"
)

func buildTopologyWithConfigMaps(t *testing.T) *machinery.Topology {
	topologyConfigMap := &controller.RuntimeObject{
		Object: &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       ConfigMapGroupKind.Kind,
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      TopologyConfigMapName,
				Namespace: TestNamespace,
				Labels:    map[string]string{kuadrant.TopologyLabel: "true"},
			},
			Data: map[string]string{},
		},
	}

	consolePluginConfigMap := &controller.RuntimeObject{
		Object: &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       ConfigMapGroupKind.Kind,
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      ConsolePluginImagesConfigMapName,
				Namespace: TestNamespace,
			},
			Data: map[string]string{
				"4.16":    "quay.io/kuadrant/console-plugin:v0.1.5",
				"4.17":    "quay.io/kuadrant/console-plugin:v0.1.5",
				"4.18":    "quay.io/kuadrant/console-plugin:v0.1.5",
				"4.19":    "quay.io/kuadrant/console-plugin:v0.1.5",
				"4.20":    "quay.io/kuadrant/console-plugin:latest",
				"default": "quay.io/kuadrant/console-plugin:latest",
			},
		},
	}

	clusterVersion := &controller.RuntimeObject{
		Object: &configv1.ClusterVersion{
			TypeMeta: metav1.TypeMeta{
				Kind:       openshift.ClusterVersionGroupKind.Kind,
				APIVersion: openshift.ClusterVersionGroupKind.GroupVersion().String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "version",
			},
			Status: configv1.ClusterVersionStatus{
				Desired: configv1.Release{
					Version: "4.20.0",
				},
			},
		},
	}

	topology, err := machinery.NewTopology(machinery.WithObjects(topologyConfigMap, consolePluginConfigMap, clusterVersion))
	if err != nil {
		t.Fatalf("failed to create topology: %v", err)
	}
	return topology
}

// Since this reconciler only runs on Openshift,
// this unit test will add some coverage
func TestConsolePluginReconciler(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = gatewayapiv1.AddToScheme(scheme)
	_ = consolev1.AddToScheme(scheme)
	_ = configv1.AddToScheme(scheme)

	// Create a mock ClusterVersion object for the test
	clusterVersion := &configv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: "version",
		},
		Status: configv1.ClusterVersionStatus{
			Desired: configv1.Release{
				Version: "4.20.0",
			},
		},
	}

	manager := controllersfake.
		NewManagerBuilder().
		WithClient(fake.NewClientBuilder().WithScheme(scheme).WithObjects(clusterVersion).Build()).
		WithScheme(scheme).
		Build()

	reconciler := NewConsolePluginReconciler(manager, TestNamespace)
	assert.Assert(t, reconciler != nil)

	t.Run("Subscription", func(subT *testing.T) {
		subscription := reconciler.Subscription()
		assert.Assert(subT, subscription != nil)
		events := subscription.Events
		assert.Assert(subT, is.Len(events, 4))
		assert.DeepEqual(subT, events[0].Kind, ptr.To(openshift.ConsolePluginGVK.GroupKind()))
		assert.DeepEqual(subT, events[1].Kind, ptr.To(ConfigMapGroupKind))
		assert.DeepEqual(subT, events[1].ObjectName, TopologyConfigMapName)
		assert.DeepEqual(subT, events[1].ObjectNamespace, TestNamespace)
		assert.DeepEqual(subT, events[1].EventType, ptr.To(controller.CreateEvent))
		assert.DeepEqual(subT, events[2].Kind, ptr.To(ConfigMapGroupKind))
		assert.DeepEqual(subT, events[2].ObjectName, TopologyConfigMapName)
		assert.DeepEqual(subT, events[2].ObjectNamespace, TestNamespace)
		assert.DeepEqual(subT, events[2].EventType, ptr.To(controller.DeleteEvent))
		assert.DeepEqual(subT, events[3].Kind, ptr.To(ConfigMapGroupKind))
		assert.DeepEqual(subT, events[3].ObjectName, ConsolePluginImagesConfigMapName)
		assert.DeepEqual(subT, events[3].ObjectNamespace, TestNamespace)
	})

	t.Run("Create service", func(subT *testing.T) {
		topology := buildTopologyWithConfigMaps(subT)
		assert.NilError(subT, reconciler.Run(context.TODO(), nil, topology, nil, nil))
		service := &corev1.Service{}
		serviceKey := client.ObjectKey{Name: consoleplugin.ServiceName(), Namespace: TestNamespace}
		assert.NilError(subT, manager.GetClient().Get(context.TODO(), serviceKey, service))
		assert.DeepEqual(subT, service.GetLabels(), consoleplugin.CommonLabels())
		assert.DeepEqual(subT, service.GetAnnotations(), consoleplugin.ServiceAnnotations())
		assert.DeepEqual(subT, service.Spec.Selector, consoleplugin.ServiceSelector())
		assert.DeepEqual(subT, service.Spec.Ports, []corev1.ServicePort{
			{
				Name: "9443-tcp", Protocol: corev1.ProtocolTCP,
				Port: 9443, TargetPort: intstr.FromInt32(9443),
			},
		})
	})

	t.Run("Delete service", func(subT *testing.T) {
		topology, err := machinery.NewTopology()
		assert.Assert(subT, err == nil)
		assert.NilError(subT, reconciler.Run(context.TODO(), nil, topology, nil, nil))
		service := &corev1.Service{}
		serviceKey := client.ObjectKey{Name: consoleplugin.ServiceName(), Namespace: TestNamespace}
		err = manager.GetClient().Get(context.TODO(), serviceKey, service)
		assert.Assert(subT, apierrors.IsNotFound(err))
	})

	t.Run("Create deployment", func(subT *testing.T) {
		topology := buildTopologyWithConfigMaps(subT)
		assert.NilError(subT, reconciler.Run(context.TODO(), nil, topology, nil, nil))
		deployment := &appsv1.Deployment{}
		deploymentKey := client.ObjectKey{Name: consoleplugin.DeploymentName(), Namespace: TestNamespace}
		assert.NilError(subT, manager.GetClient().Get(context.TODO(), deploymentKey, deployment))
		assert.DeepEqual(subT, deployment.GetLabels(), consoleplugin.DeploymentLabels(TestNamespace))
		assert.DeepEqual(subT, deployment.Spec.Selector, consoleplugin.DeploymentSelector())
		assert.DeepEqual(subT, deployment.Spec.Strategy, consoleplugin.DeploymentStrategy())
		assert.Assert(subT, is.Len(deployment.Spec.Template.Spec.Containers, 1))
		assert.Assert(subT, deployment.Spec.Template.Spec.Containers[0].Image == ConsolePluginImageURL)
	})

	t.Run("Delete deployment", func(subT *testing.T) {
		topology, err := machinery.NewTopology()
		assert.Assert(subT, err == nil)
		assert.NilError(subT, reconciler.Run(context.TODO(), nil, topology, nil, nil))
		deployment := &appsv1.Deployment{}
		deploymentKey := client.ObjectKey{Name: consoleplugin.DeploymentName(), Namespace: TestNamespace}
		err = manager.GetClient().Get(context.TODO(), deploymentKey, deployment)
		assert.Assert(subT, apierrors.IsNotFound(err))
	})

	t.Run("Create nginx configmap", func(subT *testing.T) {
		topology := buildTopologyWithConfigMaps(subT)
		assert.NilError(subT, reconciler.Run(context.TODO(), nil, topology, nil, nil))
		configMap := &corev1.ConfigMap{}
		cmKey := client.ObjectKey{Name: consoleplugin.NginxConfigMapName(), Namespace: TestNamespace}
		assert.NilError(subT, manager.GetClient().Get(context.TODO(), cmKey, configMap))
		assert.DeepEqual(subT, configMap.GetLabels(), consoleplugin.CommonLabels())
		_, ok := configMap.Data["nginx.conf"]
		assert.Assert(subT, ok)
	})

	t.Run("Delete nginx configmap", func(subT *testing.T) {
		topology, err := machinery.NewTopology()
		assert.Assert(subT, err == nil)
		assert.NilError(subT, reconciler.Run(context.TODO(), nil, topology, nil, nil))
		configMap := &corev1.ConfigMap{}
		cmKey := client.ObjectKey{Name: consoleplugin.NginxConfigMapName(), Namespace: TestNamespace}
		err = manager.GetClient().Get(context.TODO(), cmKey, configMap)
		assert.Assert(subT, apierrors.IsNotFound(err))
	})

	t.Run("Create consoleplugin", func(subT *testing.T) {
		topology := buildTopologyWithConfigMaps(subT)
		assert.NilError(subT, reconciler.Run(context.TODO(), nil, topology, nil, nil))
		consolePlugin := &consolev1.ConsolePlugin{}
		consolePluginKey := client.ObjectKey{Name: consoleplugin.Name()}
		assert.NilError(subT, manager.GetClient().Get(context.TODO(), consolePluginKey, consolePlugin))
		assert.DeepEqual(subT, consolePlugin.GetLabels(), consoleplugin.CommonLabels())
		assert.Assert(subT, consolePlugin.Spec.Backend.Service != nil)
		assert.Assert(subT, consolePlugin.Spec.Backend.Service.Name == consoleplugin.ServiceName())
		assert.Assert(subT, consolePlugin.Spec.Backend.Service.Namespace == TestNamespace)
	})

	t.Run("Delete consoleplugin", func(subT *testing.T) {
		topology, err := machinery.NewTopology()
		assert.Assert(subT, err == nil)
		assert.NilError(subT, reconciler.Run(context.TODO(), nil, topology, nil, nil))
		consolePlugin := &consolev1.ConsolePlugin{}
		consolePluginKey := client.ObjectKey{Name: consoleplugin.Name()}
		err = manager.GetClient().Get(context.TODO(), consolePluginKey, consolePlugin)
		assert.Assert(subT, apierrors.IsNotFound(err))
	})
}
