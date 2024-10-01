//go:build unit

package controllers

import (
	"context"
	"testing"

	consolev1 "github.com/openshift/api/console/v1"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	controllersfake "github.com/kuadrant/kuadrant-operator/controllers/fake"
	"github.com/kuadrant/kuadrant-operator/pkg/openshift"
	"github.com/kuadrant/kuadrant-operator/pkg/openshift/consoleplugin"
)

var (
	TestNamespace = "test-namespace"
)

// Since this task only runs on Openshift,
// this unit test will add some coverage
func TestConsolePluginTask(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = gatewayapiv1.AddToScheme(scheme)
	_ = consolev1.AddToScheme(scheme)

	manager := controllersfake.
		NewManagerBuilder().
		WithClient(fake.NewClientBuilder().WithScheme(scheme).Build()).
		WithScheme(scheme).
		Build()

	task := NewConsolePluginTask(manager, TestNamespace)
	assert.Assert(t, task != nil)

	t.Run("Events", func(subT *testing.T) {
		events := task.Events()
		assert.Assert(subT, is.Len(events, 1))
		assert.DeepEqual(subT, events[0].Kind, ptr.To(openshift.ConsolePluginGVK.GroupKind()))
	})

	t.Run("Create service", func(subT *testing.T) {
		assert.NilError(subT, task.Run(context.TODO(), nil, nil, nil, nil))
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

	t.Run("Create deployment", func(subT *testing.T) {
		assert.NilError(subT, task.Run(context.TODO(), nil, nil, nil, nil))
		deployment := &appsv1.Deployment{}
		deploymentKey := client.ObjectKey{Name: consoleplugin.DeploymentName(), Namespace: TestNamespace}
		assert.NilError(subT, manager.GetClient().Get(context.TODO(), deploymentKey, deployment))
		assert.DeepEqual(subT, deployment.GetLabels(), consoleplugin.DeploymentLabels(TestNamespace))
		assert.DeepEqual(subT, deployment.Spec.Selector, consoleplugin.DeploymentSelector())
		assert.DeepEqual(subT, deployment.Spec.Strategy, consoleplugin.DeploymentStrategy())
		assert.Assert(subT, is.Len(deployment.Spec.Template.Spec.Containers, 1))
		assert.Assert(subT, deployment.Spec.Template.Spec.Containers[0].Image == ConsolePluginImageURL)
	})

	t.Run("Create nginx configmap", func(subT *testing.T) {
		assert.NilError(subT, task.Run(context.TODO(), nil, nil, nil, nil))
		configMap := &corev1.ConfigMap{}
		cmKey := client.ObjectKey{Name: consoleplugin.NginxConfigMapName(), Namespace: TestNamespace}
		assert.NilError(subT, manager.GetClient().Get(context.TODO(), cmKey, configMap))
		assert.DeepEqual(subT, configMap.GetLabels(), consoleplugin.CommonLabels())
		_, ok := configMap.Data["nginx.conf"]
		assert.Assert(subT, ok)
	})

	t.Run("Create consoleplugin", func(subT *testing.T) {
		assert.NilError(subT, task.Run(context.TODO(), nil, nil, nil, nil))
		consolePlugin := &consolev1.ConsolePlugin{}
		consolePluginKey := client.ObjectKey{Name: consoleplugin.Name()}
		assert.NilError(subT, manager.GetClient().Get(context.TODO(), consolePluginKey, consolePlugin))
		assert.DeepEqual(subT, consolePlugin.GetLabels(), consoleplugin.CommonLabels())
		assert.Assert(subT, consolePlugin.Spec.Backend.Service != nil)
		assert.Assert(subT, consolePlugin.Spec.Backend.Service.Name == consoleplugin.ServiceName())
		assert.Assert(subT, consolePlugin.Spec.Backend.Service.Namespace == TestNamespace)
	})
}
