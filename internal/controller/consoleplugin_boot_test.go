//go:build unit

package controllers

import (
	"context"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	consolev1 "github.com/openshift/api/console/v1"
	"gotest.tools/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	controllersfake "github.com/kuadrant/kuadrant-operator/internal/controller/fake"
	"github.com/kuadrant/kuadrant-operator/internal/openshift"
	"github.com/kuadrant/kuadrant-operator/internal/openshift/consoleplugin"
)

type consolePluginBootManager struct {
	ctrlruntime.Manager
	mapper meta.RESTMapper
}

func (m consolePluginBootManager) GetRESTMapper() meta.RESTMapper { return m.mapper }

func TestConsolePluginBootCleansUpRemovedImageOverride(t *testing.T) {
	scheme := runtime.NewScheme()
	assert.NilError(t, corev1.AddToScheme(scheme))
	assert.NilError(t, appsv1.AddToScheme(scheme))
	assert.NilError(t, networkingv1.AddToScheme(scheme))
	assert.NilError(t, consolev1.AddToScheme(scheme))
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{consolev1.SchemeGroupVersion})
	mapper.Add(openshift.ConsolePluginGVK, meta.RESTScopeRoot)
	manager := consolePluginBootManager{
		Manager: controllersfake.NewManagerBuilder().WithScheme(scheme).
			WithClient(fake.NewClientBuilder().WithScheme(scheme).Build()).Build(),
		mapper: mapper,
	}
	topologyConfigMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name: TopologyConfigMapName, Namespace: operatorNamespace, UID: "topology-uid",
		},
	}
	topology, err := machinery.NewTopology(machinery.WithObjects(&controller.RuntimeObject{Object: topologyConfigMap}))
	assert.NilError(t, err)
	// Keep the unrelated topology writer idle while running the real boot workflow.
	topologyConfigMap.Data = map[string]string{"topology": topology.ToDot()}
	assert.NilError(t, manager.GetClient().Create(context.Background(), topologyConfigMap.DeepCopy()))
	events := []controller.ResourceEvent{{
		Kind: openshift.ConsolePluginGVK.GroupKind(), EventType: controller.CreateEvent,
		NewObject: consoleplugin.ConsolePlugin(operatorNamespace),
	}}
	resources := []client.Object{
		consoleplugin.Service(operatorNamespace),
		consoleplugin.Deployment(operatorNamespace, "", TopologyConfigMapName),
		consoleplugin.ConsolePlugin(operatorNamespace),
		consoleplugin.NetworkPolicy(operatorNamespace),
	}

	for _, imageOverride := range []string{"localhost/kuadrant/console-plugin:dev", ""} {
		t.Setenv(openshift.ConsolePluginImageOverrideEnvVar, imageOverride)
		// Rebuild options and the workflow as an operator restart would.
		builder := &BootOptionsBuilder{
			manager: manager, logger: logr.Discard(), errorTracker: NewPersistentErrorTracker(logr.Discard()),
		}
		opts, err := builder.getConsolePluginOptions()
		assert.NilError(t, err)
		assert.Assert(t, len(opts) > 0, "ConsolePlugin watches must remain registered for cleanup")
		assert.Assert(t, !builder.isClusterVersionInstalled)
		assert.NilError(t, builder.Reconciler()(context.Background(), events, topology, nil, &sync.Map{}))
		for _, resource := range resources {
			err := manager.GetClient().Get(context.Background(), client.ObjectKeyFromObject(resource), resource)
			if imageOverride == "" {
				assert.Assert(t, apierrors.IsNotFound(err), "%T should be deleted after removing the override", resource)
			} else {
				assert.NilError(t, err)
			}
		}
	}
	assert.NilError(t, manager.GetClient().Get(context.Background(), client.ObjectKeyFromObject(topologyConfigMap), topologyConfigMap))
}
