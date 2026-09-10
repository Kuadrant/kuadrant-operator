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
	networkingv1 "k8s.io/api/networking/v1"
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
	ConsolePluginImageURL = "quay.io/kuadrant/console-plugin:v0.6.0"
)

func buildTopologyWithClusterVersion(t *testing.T) *machinery.Topology {
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

	topology, err := machinery.NewTopology(machinery.WithObjects(topologyConfigMap, clusterVersion))
	if err != nil {
		t.Fatalf("failed to create topology: %v", err)
	}
	return topology
}

// Since this reconciler only runs on Openshift,
// this unit test will add some coverage
func TestConsolePluginReconciler(t *testing.T) {
	t.Setenv(openshift.RelatedImageConsolePluginLatestEnvVar, "quay.io/kuadrant/console-plugin:latest")
	t.Setenv(openshift.RelatedImageConsolePluginSDK1EnvVar, ConsolePluginImageURL)
	t.Setenv(openshift.RelatedImageConsolePluginPF5EnvVar, "quay.io/kuadrant/console-plugin:v0.1.5")

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
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

	reconciler := NewConsolePluginReconciler(manager, TestNamespace, "")
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
		assert.DeepEqual(subT, events[3].Kind, ptr.To(networkingv1.SchemeGroupVersion.WithKind("NetworkPolicy").GroupKind()))
		assert.Equal(subT, events[3].ObjectName, consoleplugin.KuadrantConsoleName)
		assert.Equal(subT, events[3].ObjectNamespace, TestNamespace)
		assert.Assert(subT, events[3].EventType == nil)
	})

	t.Run("Create, repair, recreate and delete network policy", func(t *testing.T) {
		topology := buildTopologyWithClusterVersion(t)
		assert.NilError(t, reconciler.Run(context.TODO(), nil, topology, nil, nil))
		policy := &networkingv1.NetworkPolicy{}
		key := client.ObjectKey{Name: consoleplugin.KuadrantConsoleName, Namespace: TestNamespace}
		assert.NilError(t, manager.GetClient().Get(context.TODO(), key, policy))
		expected := policy.DeepCopy()
		assert.DeepEqual(t, policy.Spec, consoleplugin.NetworkPolicy(TestNamespace).Spec)
		assert.Assert(t, is.Len(policy.OwnerReferences, 1))
		assert.Equal(t, policy.OwnerReferences[0].Name, TopologyConfigMapName)
		policy.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{}}
		policy.Labels = nil
		policy.OwnerReferences = nil
		assert.NilError(t, manager.GetClient().Update(context.TODO(), policy))
		assert.NilError(t, reconciler.Run(context.TODO(), nil, topology, nil, nil))
		assert.NilError(t, manager.GetClient().Get(context.TODO(), key, policy))
		assert.DeepEqual(t, policy.Spec, expected.Spec)
		assert.DeepEqual(t, policy.Labels, expected.Labels)
		assert.DeepEqual(t, policy.OwnerReferences, expected.OwnerReferences)
		assert.Assert(t, !consoleplugin.NetworkPolicyMutator(expected, policy))
		assert.NilError(t, manager.GetClient().Delete(context.TODO(), policy))
		assert.NilError(t, reconciler.Run(context.TODO(), nil, topology, nil, nil))
		assert.NilError(t, manager.GetClient().Get(context.TODO(), key, policy))
		empty, err := machinery.NewTopology()
		assert.NilError(t, err)
		assert.NilError(t, reconciler.Run(context.TODO(), nil, empty, nil, nil))
		assert.Assert(t, apierrors.IsNotFound(manager.GetClient().Get(context.TODO(), key, policy)))
	})

	t.Run("Create service", func(subT *testing.T) {
		topology := buildTopologyWithClusterVersion(subT)
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
		topology := buildTopologyWithClusterVersion(subT)
		assert.NilError(subT, reconciler.Run(context.TODO(), nil, topology, nil, nil))
		deployment := &appsv1.Deployment{}
		deploymentKey := client.ObjectKey{Name: consoleplugin.DeploymentName(), Namespace: TestNamespace}
		assert.NilError(subT, manager.GetClient().Get(context.TODO(), deploymentKey, deployment))
		assert.DeepEqual(subT, deployment.GetLabels(), consoleplugin.DeploymentLabels(TestNamespace))
		assert.DeepEqual(subT, deployment.Spec.Selector, consoleplugin.DeploymentSelector())
		assert.DeepEqual(subT, deployment.Spec.Strategy, consoleplugin.DeploymentStrategy())
		assert.Assert(subT, is.Len(deployment.Spec.Template.Spec.Containers, 1))
		assert.Assert(subT, deployment.Spec.Template.Spec.Containers[0].Image == ConsolePluginImageURL)
		assert.Equal(subT, deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy, corev1.PullAlways)
		assert.Assert(subT, is.Len(deployment.Spec.Template.Spec.Containers[0].VolumeMounts, 1))
		assert.Assert(subT, is.Len(deployment.Spec.Template.Spec.Volumes, 1))
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

	t.Run("Create consoleplugin", func(subT *testing.T) {
		topology := buildTopologyWithClusterVersion(subT)
		assert.NilError(subT, reconciler.Run(context.TODO(), nil, topology, nil, nil))
		consolePlugin := &consolev1.ConsolePlugin{}
		consolePluginKey := client.ObjectKey{Name: consoleplugin.Name()}
		assert.NilError(subT, manager.GetClient().Get(context.TODO(), consolePluginKey, consolePlugin))
		assert.DeepEqual(subT, consolePlugin.GetLabels(), consoleplugin.CommonLabels())
		assert.Assert(subT, consolePlugin.Spec.Backend.Service != nil)
		assert.Assert(subT, consolePlugin.Spec.Backend.Service.Name == consoleplugin.ServiceName())
		assert.Assert(subT, consolePlugin.Spec.Backend.Service.Namespace == TestNamespace)
		assert.Assert(subT, is.Len(consolePlugin.Spec.Proxy, 1))
		assert.Assert(subT, consolePlugin.Spec.Proxy[0].Alias == "backend")
		assert.Assert(subT, consolePlugin.Spec.Proxy[0].Authorization == consolev1.UserToken)
		assert.Assert(subT, consolePlugin.Spec.Proxy[0].Endpoint.Service != nil)
		assert.Assert(subT, consolePlugin.Spec.Proxy[0].Endpoint.Service.Name == consoleplugin.ServiceName())
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

func TestConsolePluginReconcilerWithDevelopmentImageOverride(t *testing.T) {
	const imageOverride = "localhost/kuadrant/console-plugin:dev"

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = consolev1.AddToScheme(scheme)
	_ = configv1.AddToScheme(scheme)

	legacyConfigMap := consoleplugin.LegacyNginxConfigMap(TestNamespace)
	legacyConfigMap.Data = map[string]string{"nginx.conf": "legacy"}
	manager := controllersfake.
		NewManagerBuilder().
		WithClient(fake.NewClientBuilder().WithScheme(scheme).WithObjects(legacyConfigMap).Build()).
		WithScheme(scheme).
		Build()
	reconciler := NewConsolePluginReconciler(manager, TestNamespace, imageOverride)

	topologyConfigMap := &controller.RuntimeObject{
		Object: &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{Kind: ConfigMapGroupKind.Kind, APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      TopologyConfigMapName,
				Namespace: TestNamespace,
				Labels:    map[string]string{kuadrant.TopologyLabel: "true"},
			},
		},
	}
	topology, err := machinery.NewTopology(machinery.WithObjects(topologyConfigMap))
	assert.NilError(t, err)
	assert.NilError(t, reconciler.Run(context.TODO(), nil, topology, nil, nil))

	deployment := &appsv1.Deployment{}
	deploymentKey := client.ObjectKey{Name: consoleplugin.DeploymentName(), Namespace: TestNamespace}
	assert.NilError(t, manager.GetClient().Get(context.TODO(), deploymentKey, deployment))
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Image, imageOverride)
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy, corev1.PullIfNotPresent)

	consolePlugin := &consolev1.ConsolePlugin{}
	assert.NilError(t, manager.GetClient().Get(context.TODO(), client.ObjectKey{Name: consoleplugin.Name()}, consolePlugin))
	err = manager.GetClient().Get(context.TODO(), client.ObjectKeyFromObject(legacyConfigMap), &corev1.ConfigMap{})
	assert.Assert(t, apierrors.IsNotFound(err))
	policy := &networkingv1.NetworkPolicy{}
	assert.NilError(t, manager.GetClient().Get(context.TODO(), client.ObjectKey{Name: consoleplugin.KuadrantConsoleName, Namespace: TestNamespace}, policy))
	assert.DeepEqual(t, policy.Spec, consoleplugin.NetworkPolicy(TestNamespace).Spec)
}
