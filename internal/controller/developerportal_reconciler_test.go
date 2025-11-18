//go:build unit

package controllers

import (
	"context"
	"testing"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	controllersfake "github.com/kuadrant/kuadrant-operator/internal/controller/fake"
)

const (
	developerPortalTestNamespace = "test-namespace"
)

func buildTopologyWithKuadrant(t *testing.T, enabled bool) *machinery.Topology {
	kuadrantCR := &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantv1beta1.KuadrantGroupKind.Kind,
			APIVersion: kuadrantv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kuadrant",
			Namespace: developerPortalTestNamespace,
			UID:       "test-uid",
		},
		Spec: kuadrantv1beta1.KuadrantSpec{
			Components: &kuadrantv1beta1.Components{
				DeveloperPortal: &kuadrantv1beta1.DeveloperPortal{
					Enabled: enabled,
				},
			},
		},
	}

	topology, err := machinery.NewTopology(machinery.WithObjects(kuadrantCR))
	if err != nil {
		t.Fatalf("failed to create topology: %v", err)
	}
	return topology
}

func TestDeveloperPortalReconciler(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = gatewayapiv1.AddToScheme(scheme)
	_ = kuadrantv1beta1.AddToScheme(scheme)

	manager := controllersfake.
		NewManagerBuilder().
		WithClient(fake.NewClientBuilder().WithScheme(scheme).Build()).
		WithScheme(scheme).
		Build()

	reconciler := NewDeveloperPortalReconciler(manager)
	assert.Assert(t, reconciler != nil)

	t.Run("Subscription", func(subT *testing.T) {
		subscription := reconciler.Subscription()
		assert.Assert(subT, subscription != nil)
		events := subscription.Events
		assert.Assert(subT, is.Len(events, 3))
		// Kuadrant resource
		assert.DeepEqual(subT, events[0].Kind, &kuadrantv1beta1.KuadrantGroupKind)
		// Deployment events (managed by reconciler)
		assert.DeepEqual(subT, events[1].Kind, &kuadrantv1beta1.DeploymentGroupKind)
		assert.DeepEqual(subT, events[1].EventType, ptr.To(controller.DeleteEvent))
		assert.DeepEqual(subT, events[2].Kind, &kuadrantv1beta1.DeploymentGroupKind)
		assert.DeepEqual(subT, events[2].EventType, ptr.To(controller.UpdateEvent))
	})

	t.Run("Topology with Kuadrant CR", func(subT *testing.T) {
		topology := buildTopologyWithKuadrant(subT, true)
		kuadrantCR := GetKuadrantFromTopology(topology)
		assert.Assert(subT, kuadrantCR != nil, "GetKuadrantFromTopology should return Kuadrant CR")
		assert.Equal(subT, kuadrantCR.Name, "kuadrant")
		assert.Equal(subT, kuadrantCR.IsDeveloperPortalEnabled(), true)
	})

	t.Run("Reconcile deployment when enabled", func(subT *testing.T) {
		topology := buildTopologyWithKuadrant(subT, true)
		err := reconciler.Reconcile(context.TODO(), nil, topology, nil, nil)
		assert.NilError(subT, err, "Reconcile should succeed")
		// Verify Deployment was created
		deployment := &appsv1.Deployment{}
		deployKey := client.ObjectKey{Name: "developer-portal-controller", Namespace: kuadrantOperatorNamespace}
		err = manager.GetClient().Get(context.TODO(), deployKey, deployment)
		assert.NilError(subT, err, "Deployment should be created")
	})

	t.Run("No Kuadrant CR in topology", func(subT *testing.T) {
		topology, err := machinery.NewTopology()
		assert.Assert(subT, err == nil)
		// Should not error when no Kuadrant CR exists
		assert.NilError(subT, reconciler.Reconcile(context.TODO(), nil, topology, nil, nil))
	})
}
