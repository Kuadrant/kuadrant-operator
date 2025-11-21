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
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	controllersfake "github.com/kuadrant/kuadrant-operator/internal/controller/fake"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
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
			Components: kuadrantv1beta1.Components{
				DeveloperPortal: kuadrantv1beta1.DeveloperPortal{
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
	_ = rbacv1.AddToScheme(scheme)
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
		assert.Assert(subT, is.Len(events, 9))
		// Kuadrant resource
		assert.DeepEqual(subT, events[0].Kind, &kuadrantv1beta1.KuadrantGroupKind)
		// ClusterRole events
		assert.DeepEqual(subT, events[1].Kind, ptr.To(ClusterRoleGroupKind))
		assert.DeepEqual(subT, events[1].EventType, ptr.To(controller.DeleteEvent))
		assert.DeepEqual(subT, events[2].Kind, ptr.To(ClusterRoleGroupKind))
		assert.DeepEqual(subT, events[2].EventType, ptr.To(controller.UpdateEvent))
		// ClusterRoleBinding events
		assert.DeepEqual(subT, events[3].Kind, ptr.To(ClusterRoleBindingGroupKind))
		assert.DeepEqual(subT, events[3].EventType, ptr.To(controller.DeleteEvent))
		assert.DeepEqual(subT, events[4].Kind, ptr.To(ClusterRoleBindingGroupKind))
		assert.DeepEqual(subT, events[4].EventType, ptr.To(controller.UpdateEvent))
		// ServiceAccount events
		assert.DeepEqual(subT, events[5].Kind, ptr.To(ServiceAccountGroupKind))
		assert.DeepEqual(subT, events[5].EventType, ptr.To(controller.DeleteEvent))
		assert.DeepEqual(subT, events[6].Kind, ptr.To(ServiceAccountGroupKind))
		assert.DeepEqual(subT, events[6].EventType, ptr.To(controller.UpdateEvent))
		// Deployment events
		assert.DeepEqual(subT, events[7].Kind, &kuadrantv1beta1.DeploymentGroupKind)
		assert.DeepEqual(subT, events[7].EventType, ptr.To(controller.DeleteEvent))
		assert.DeepEqual(subT, events[8].Kind, &kuadrantv1beta1.DeploymentGroupKind)
		assert.DeepEqual(subT, events[8].EventType, ptr.To(controller.UpdateEvent))
	})

	t.Run("Topology with Kuadrant CR", func(subT *testing.T) {
		topology := buildTopologyWithKuadrant(subT, true)
		kuadrantCR := GetKuadrantFromTopology(topology)
		assert.Assert(subT, kuadrantCR != nil, "GetKuadrantFromTopology should return Kuadrant CR")
		assert.Equal(subT, kuadrantCR.Name, "kuadrant")
		assert.Equal(subT, kuadrantCR.IsDeveloperPortalEnabled(), true)
	})

	t.Run("Create ClusterRole", func(subT *testing.T) {
		topology := buildTopologyWithKuadrant(subT, true)
		assert.NilError(subT, reconciler.Reconcile(context.TODO(), nil, topology, nil, nil))
		clusterRole := &rbacv1.ClusterRole{}
		crKey := client.ObjectKey{Name: "kuadrant-operator-developer-portal-controller-manager-role"}
		assert.NilError(subT, manager.GetClient().Get(context.TODO(), crKey, clusterRole))
		assert.DeepEqual(subT, clusterRole.Labels, map[string]string{
			"app":                         "developer-portal-controller",
			kuadrant.DeveloperPortalLabel: "true",
		})
		// Verify it has the necessary RBAC permissions
		assert.Assert(subT, is.Len(clusterRole.Rules, 6))
		// Check for devportal.kuadrant.io resources
		found := false
		for _, rule := range clusterRole.Rules {
			if len(rule.APIGroups) > 0 && rule.APIGroups[0] == "devportal.kuadrant.io" {
				found = true
				assert.Assert(subT, is.Contains(rule.Resources, "apiproducts"))
				assert.Assert(subT, is.Contains(rule.Resources, "apikeyrequests"))
				assert.Assert(subT, is.Contains(rule.Verbs, "get"))
				assert.Assert(subT, is.Contains(rule.Verbs, "list"))
				assert.Assert(subT, is.Contains(rule.Verbs, "watch"))
				break
			}
		}
		assert.Assert(subT, found, "devportal.kuadrant.io resources not found in ClusterRole rules")
	})

	t.Run("Delete ClusterRole when disabled", func(subT *testing.T) {
		topology := buildTopologyWithKuadrant(subT, false)
		assert.NilError(subT, reconciler.Reconcile(context.TODO(), nil, topology, nil, nil))
		clusterRole := &rbacv1.ClusterRole{}
		crKey := client.ObjectKey{Name: "kuadrant-operator-developer-portal-controller-manager-role"}
		err := manager.GetClient().Get(context.TODO(), crKey, clusterRole)
		assert.Assert(subT, apierrors.IsNotFound(err))
	})

	t.Run("Create ServiceAccount", func(subT *testing.T) {
		topology := buildTopologyWithKuadrant(subT, true)
		assert.NilError(subT, reconciler.Reconcile(context.TODO(), nil, topology, nil, nil))
		serviceAccount := &corev1.ServiceAccount{}
		saKey := client.ObjectKey{Name: "developer-portal-controller", Namespace: developerPortalTestNamespace}
		assert.NilError(subT, manager.GetClient().Get(context.TODO(), saKey, serviceAccount))
		assert.DeepEqual(subT, serviceAccount.Labels, map[string]string{
			"app":                         "developer-portal-controller",
			kuadrant.DeveloperPortalLabel: "true",
		})
		// Verify owner reference
		assert.Assert(subT, is.Len(serviceAccount.OwnerReferences, 1))
		assert.Equal(subT, serviceAccount.OwnerReferences[0].Name, "kuadrant")
		assert.Equal(subT, serviceAccount.OwnerReferences[0].Kind, "Kuadrant")
		assert.Equal(subT, *serviceAccount.OwnerReferences[0].Controller, true)
	})

	t.Run("Delete ServiceAccount when disabled", func(subT *testing.T) {
		topology := buildTopologyWithKuadrant(subT, false)
		assert.NilError(subT, reconciler.Reconcile(context.TODO(), nil, topology, nil, nil))
		serviceAccount := &corev1.ServiceAccount{}
		saKey := client.ObjectKey{Name: "developer-portal-controller", Namespace: developerPortalTestNamespace}
		err := manager.GetClient().Get(context.TODO(), saKey, serviceAccount)
		assert.Assert(subT, apierrors.IsNotFound(err))
	})

	t.Run("Create ClusterRoleBinding", func(subT *testing.T) {
		topology := buildTopologyWithKuadrant(subT, true)
		assert.NilError(subT, reconciler.Reconcile(context.TODO(), nil, topology, nil, nil))
		clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
		crbKey := client.ObjectKey{Name: "developer-portal-controller-rolebinding"}
		assert.NilError(subT, manager.GetClient().Get(context.TODO(), crbKey, clusterRoleBinding))
		assert.DeepEqual(subT, clusterRoleBinding.Labels, map[string]string{
			"app":                         "developer-portal-controller",
			kuadrant.DeveloperPortalLabel: "true",
		})
		// Verify RoleRef
		assert.Equal(subT, clusterRoleBinding.RoleRef.Name, "kuadrant-operator-developer-portal-controller-manager-role")
		assert.Equal(subT, clusterRoleBinding.RoleRef.Kind, "ClusterRole")
		assert.Equal(subT, clusterRoleBinding.RoleRef.APIGroup, "rbac.authorization.k8s.io")
		// Verify Subjects
		assert.Assert(subT, is.Len(clusterRoleBinding.Subjects, 1))
		assert.Equal(subT, clusterRoleBinding.Subjects[0].Kind, "ServiceAccount")
		assert.Equal(subT, clusterRoleBinding.Subjects[0].Name, "developer-portal-controller")
		assert.Equal(subT, clusterRoleBinding.Subjects[0].Namespace, developerPortalTestNamespace)
	})

	t.Run("Delete ClusterRoleBinding when disabled", func(subT *testing.T) {
		topology := buildTopologyWithKuadrant(subT, false)
		assert.NilError(subT, reconciler.Reconcile(context.TODO(), nil, topology, nil, nil))
		clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
		crbKey := client.ObjectKey{Name: "developer-portal-controller-rolebinding"}
		err := manager.GetClient().Get(context.TODO(), crbKey, clusterRoleBinding)
		assert.Assert(subT, apierrors.IsNotFound(err))
	})

	t.Run("Create Deployment", func(subT *testing.T) {
		topology := buildTopologyWithKuadrant(subT, true)
		assert.NilError(subT, reconciler.Reconcile(context.TODO(), nil, topology, nil, nil))
		deployment := &appsv1.Deployment{}
		depKey := client.ObjectKey{Name: "developer-portal-controller", Namespace: developerPortalTestNamespace}
		assert.NilError(subT, manager.GetClient().Get(context.TODO(), depKey, deployment))
		assert.DeepEqual(subT, deployment.Labels, map[string]string{
			"app":                         "developer-portal-controller",
			"control-plane":               "controller-manager",
			kuadrant.DeveloperPortalLabel: "true",
		})
		// Verify owner reference
		assert.Assert(subT, is.Len(deployment.OwnerReferences, 1))
		assert.Equal(subT, deployment.OwnerReferences[0].Name, "kuadrant")
		// Verify deployment spec
		assert.Equal(subT, *deployment.Spec.Replicas, int32(1))
		assert.Equal(subT, deployment.Spec.Template.Spec.ServiceAccountName, "developer-portal-controller")
		// Verify container
		assert.Assert(subT, is.Len(deployment.Spec.Template.Spec.Containers, 1))
		container := deployment.Spec.Template.Spec.Containers[0]
		assert.Equal(subT, container.Name, "manager")
		assert.Equal(subT, container.Image, "quay.io/kuadrant/developer-portal-controller:latest")
		assert.DeepEqual(subT, container.Command, []string{"/manager"})
		assert.DeepEqual(subT, container.Args, []string{
			"--leader-elect",
			"--health-probe-bind-address=:8081",
		})
		// Verify probes
		assert.Assert(subT, container.LivenessProbe != nil)
		assert.Equal(subT, container.LivenessProbe.HTTPGet.Path, "/healthz")
		assert.Equal(subT, container.LivenessProbe.HTTPGet.Port, intstr.FromInt(8081))
		assert.Assert(subT, container.ReadinessProbe != nil)
		assert.Equal(subT, container.ReadinessProbe.HTTPGet.Path, "/readyz")
		assert.Equal(subT, container.ReadinessProbe.HTTPGet.Port, intstr.FromInt(8081))
		// Verify resources
		assert.DeepEqual(subT, container.Resources.Limits[corev1.ResourceCPU], resource.MustParse("500m"))
		assert.DeepEqual(subT, container.Resources.Limits[corev1.ResourceMemory], resource.MustParse("128Mi"))
		assert.DeepEqual(subT, container.Resources.Requests[corev1.ResourceCPU], resource.MustParse("10m"))
		assert.DeepEqual(subT, container.Resources.Requests[corev1.ResourceMemory], resource.MustParse("64Mi"))
		// Verify security context
		assert.Assert(subT, container.SecurityContext != nil)
		assert.Equal(subT, *container.SecurityContext.AllowPrivilegeEscalation, false)
		assert.Assert(subT, is.Len(container.SecurityContext.Capabilities.Drop, 1))
		assert.Equal(subT, string(container.SecurityContext.Capabilities.Drop[0]), "ALL")
	})

	t.Run("Delete Deployment when disabled", func(subT *testing.T) {
		topology := buildTopologyWithKuadrant(subT, false)
		assert.NilError(subT, reconciler.Reconcile(context.TODO(), nil, topology, nil, nil))
		deployment := &appsv1.Deployment{}
		depKey := client.ObjectKey{Name: "developer-portal-controller", Namespace: developerPortalTestNamespace}
		err := manager.GetClient().Get(context.TODO(), depKey, deployment)
		assert.Assert(subT, apierrors.IsNotFound(err))
	})

	t.Run("No Kuadrant CR in topology", func(subT *testing.T) {
		topology, err := machinery.NewTopology()
		assert.Assert(subT, err == nil)
		// Should not error when no Kuadrant CR exists
		assert.NilError(subT, reconciler.Reconcile(context.TODO(), nil, topology, nil, nil))
		// Verify no resources were created
		clusterRole := &rbacv1.ClusterRole{}
		crKey := client.ObjectKey{Name: "kuadrant-operator-developer-portal-controller-manager-role"}
		err = manager.GetClient().Get(context.TODO(), crKey, clusterRole)
		assert.Assert(subT, apierrors.IsNotFound(err))
	})
}
