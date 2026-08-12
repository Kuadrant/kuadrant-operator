//go:build unit

package controllers

import (
	"context"
	"sync"
	"testing"

	authorinooperatorv1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantauthorino "github.com/kuadrant/kuadrant-operator/internal/authorino"
	"github.com/kuadrant/kuadrant-operator/internal/reconcilers"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"gotest.tools/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dfake "k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"slices"
)

func buildKuadrantCR(shouldIncludeFinalizer bool, shouldIncludeTimestamp bool) *kuadrantv1beta1.Kuadrant {
	kuadrantCR := &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantv1beta1.KuadrantGroupKind.Kind,
			APIVersion: kuadrantv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kuadrant",
			Namespace: "kuadrant-system",
		},
	}
	if shouldIncludeTimestamp {
		now := metav1.Now()
		kuadrantCR.DeletionTimestamp = &now
	}

	if shouldIncludeFinalizer && shouldIncludeTimestamp {
		kuadrantCR.Finalizers = []string{authConfigFinalizer}
	}

	return kuadrantCR
}

func buildTopology(t *testing.T, kuadrantCR *kuadrantv1beta1.Kuadrant, objs ...client.Object) *machinery.Topology {
	var opts []machinery.TopologyOptionsFunc

	if kuadrantCR != nil {
		opts = append(opts, machinery.WithObjects(kuadrantCR))
	}

	for _, obj := range objs {
		opts = append(opts, machinery.WithObjects(&controller.RuntimeObject{Object: obj}))
	}

	if kuadrantCR != nil && slices.ContainsFunc(objs, func(obj client.Object) bool {
		return obj.GetObjectKind().GroupVersionKind().Kind == "Authorino"
	}) {
		store := controller.Store{"kuadrant": kuadrantCR}
		opts = append(opts, machinery.WithLinks(
			kuadrantv1beta1.LinkKuadrantToAuthorino(store),
		))
	}

	topology, err := machinery.NewTopology(opts...)
	if err != nil {
		t.Fatalf("failed to build topology %v", err)
	}
	return topology
}

func TestAuthConfigsReconciler(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kuadrantv1beta1.AddToScheme(scheme)
	_ = authorinov1beta3.AddToScheme(scheme)
	_ = authorinooperatorv1beta1.AddToScheme(scheme)

	authConfig1 := &authorinov1beta3.AuthConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AuthConfig",
			APIVersion: authorinov1beta3.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "authconfig1",
			Namespace: "kuadrant-system",
			Labels:    AuthObjectLabels(),
		},
	}

	authConfig2 := &authorinov1beta3.AuthConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AuthConfig",
			APIVersion: authorinov1beta3.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "authconfig2",
			Namespace: "kuadrant-system",
			Labels:    AuthObjectLabels(),
		},
	}

	authorino := &authorinooperatorv1beta1.Authorino{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Authorino",
			APIVersion: authorinooperatorv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "authorino",
			Namespace: "kuadrant-system",
		},
	}

	t.Run("topology contains Kuadrant CR marked for deletion", func(subT *testing.T) {
		kuadrantCR := buildKuadrantCR(false, true)
		topology := buildTopology(subT, kuadrantCR, authConfig1, authConfig2)
		kuadrantFromTopology := GetKuadrantFromTopologyDuringDeletion(topology)
		assert.Assert(subT, kuadrantFromTopology != nil, "should find Kuadrant CR in topology")
		assert.Assert(subT, kuadrantFromTopology.GetDeletionTimestamp() != nil, "Kuadrant CR should have deletion timestamp")
	})

	t.Run("reconcile deletes AuthConfigs when Kuadrant CR is being deleted", func(subT *testing.T) {
		kuadrantCR := buildKuadrantCR(true, true)
		fakeDynClient := dfake.NewSimpleDynamicClient(scheme, authConfig1, authConfig2)
		fakeCtrlClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(kuadrantCR).Build()

		reconciler := &AuthConfigsReconciler{
			client:         fakeDynClient,
			BaseReconciler: reconcilers.NewBaseReconciler(fakeCtrlClient, scheme, fakeCtrlClient),
		}

		// verify AuthConfigs exist before reconciliation
		_, err := fakeDynClient.Resource(kuadrantauthorino.AuthConfigsResource).Namespace("kuadrant-system").Get(context.TODO(), "authconfig1", metav1.GetOptions{})
		assert.NilError(subT, err, "authconfig1 should exist before reconciliation")
		_, err = fakeDynClient.Resource(kuadrantauthorino.AuthConfigsResource).Namespace("kuadrant-system").Get(context.TODO(), "authconfig2", metav1.GetOptions{})
		assert.NilError(subT, err, "authconfig2 should exist before reconciliation")

		// verify Kuadrant CR is present in the fake server with finalizer
		updatedKuadrant := &kuadrantv1beta1.Kuadrant{}
		err = fakeCtrlClient.Get(context.TODO(), client.ObjectKeyFromObject(kuadrantCR), updatedKuadrant)
		assert.NilError(subT, err)
		assert.Assert(subT, controllerutil.ContainsFinalizer(updatedKuadrant, authConfigFinalizer),
			"finalizer should be added to Kuadrant CR")

		// build topology and call reconcile, so AuthConfigs get deleted
		topology := buildTopology(subT, kuadrantCR, authConfig1, authConfig2)
		state := &sync.Map{}
		err = reconciler.Reconcile(context.TODO(), nil, topology, nil, state)
		assert.NilError(subT, err)

		errorRegistry := GetOrCreateErrorRegistry(state)
		assert.Assert(subT, !errorRegistry.HasErrors(), "expected no errors in registry")

		// verify AuthConfigs were deleted
		_, err = fakeDynClient.Resource(kuadrantauthorino.AuthConfigsResource).Namespace("kuadrant-system").Get(context.TODO(), "authconfig1", metav1.GetOptions{})
		assert.Assert(subT, apierrors.IsNotFound(err), "authconfig1 should have been deleted")
		_, err = fakeDynClient.Resource(kuadrantauthorino.AuthConfigsResource).Namespace("kuadrant-system").Get(context.TODO(), "authconfig2", metav1.GetOptions{})
		assert.Assert(subT, apierrors.IsNotFound(err), "authconfig2 should have been deleted")

		//verify Kuadrant CR was removed
		postReconcile := &kuadrantv1beta1.Kuadrant{}
		err = fakeCtrlClient.Get(context.TODO(), client.ObjectKeyFromObject(kuadrantCR), postReconcile)
		assert.Assert(subT, apierrors.IsNotFound(err), "Kuadrant CR should have been deleted after finalizer removal")
	})

	t.Run("reconcile adds finalizer when Kuadrant CR is active", func(subT *testing.T) {
		kuadrantCR := buildKuadrantCR(false, false)
		fakeDynClient := dfake.NewSimpleDynamicClient(scheme, authConfig1, authConfig2, authorino)
		fakeCtrlClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(kuadrantCR, authorino).Build()

		reconciler := &AuthConfigsReconciler{
			client:         fakeDynClient,
			BaseReconciler: reconcilers.NewBaseReconciler(fakeCtrlClient, scheme, fakeCtrlClient),
		}

		// verify finalizer is not present before reconciliation
		preReconcile := &kuadrantv1beta1.Kuadrant{}
		err := fakeCtrlClient.Get(context.TODO(), client.ObjectKeyFromObject(kuadrantCR), preReconcile)
		assert.NilError(subT, err)
		assert.Assert(subT, !controllerutil.ContainsFinalizer(preReconcile, authConfigFinalizer),
			"finalizer should not be present before reconciliation")

		// build topology with all components
		topology := buildTopology(subT, kuadrantCR, authConfig1, authConfig2, authorino)

		// call reconcile, so the finalizer is added
		state := &sync.Map{}
		err = reconciler.Reconcile(context.TODO(), nil, topology, nil, state)
		assert.NilError(subT, err)

		errorRegistry := GetOrCreateErrorRegistry(state)
		assert.Assert(subT, !errorRegistry.HasErrors(), "expected no errors in registry")

		// verify finalizer was added
		postReconcile := &kuadrantv1beta1.Kuadrant{}
		err = fakeCtrlClient.Get(context.TODO(), client.ObjectKeyFromObject(kuadrantCR), postReconcile)
		assert.NilError(subT, err)
		assert.Assert(subT, controllerutil.ContainsFinalizer(postReconcile, authConfigFinalizer),
			"finalizer should have been added to Kuadrant CR")

		// AuthConfigs and authorino should still exist
		_, err = fakeDynClient.Resource(kuadrantauthorino.AuthConfigsResource).Namespace("kuadrant-system").Get(context.TODO(), "authconfig1", metav1.GetOptions{})
		assert.NilError(subT, err, "authconfig1 should not have been deleted")
		_, err = fakeDynClient.Resource(kuadrantauthorino.AuthConfigsResource).Namespace("kuadrant-system").Get(context.TODO(), "authconfig2", metav1.GetOptions{})
		assert.NilError(subT, err, "authconfig2 should not have been deleted")
		_, err = fakeDynClient.Resource(kuadrantv1beta1.AuthorinosResource).Namespace("kuadrant-system").Get(context.TODO(), "authorino", metav1.GetOptions{})
		assert.NilError(subT, err, "authorino should exist before reconciliation")
	})

	t.Run("reconcile deletes AuthConfigs when Kuadrant CR is being deleted and Authorino exists", func(subT *testing.T) {
		kuadrantCR := buildKuadrantCR(true, true)
		fakeDynClient := dfake.NewSimpleDynamicClient(scheme, authConfig1, authConfig2, authorino)
		fakeCtrlClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(kuadrantCR, authorino).Build()

		reconciler := &AuthConfigsReconciler{
			client:         fakeDynClient,
			BaseReconciler: reconcilers.NewBaseReconciler(fakeCtrlClient, scheme, fakeCtrlClient),
		}

		// verify AuthConfigs and authorino exist before reconciliation
		_, err := fakeDynClient.Resource(kuadrantauthorino.AuthConfigsResource).Namespace("kuadrant-system").Get(context.TODO(), "authconfig1", metav1.GetOptions{})
		assert.NilError(subT, err, "authconfig1 should exist before reconciliation")
		_, err = fakeDynClient.Resource(kuadrantauthorino.AuthConfigsResource).Namespace("kuadrant-system").Get(context.TODO(), "authconfig2", metav1.GetOptions{})
		assert.NilError(subT, err, "authconfig2 should exist before reconciliation")
		_, err = fakeDynClient.Resource(kuadrantv1beta1.AuthorinosResource).Namespace("kuadrant-system").Get(context.TODO(), "authorino", metav1.GetOptions{})
		assert.NilError(subT, err, "authorino should exist before reconciliation")

		// verify Kuadrant CR is present in the fake server with finalizer
		updatedKuadrant := &kuadrantv1beta1.Kuadrant{}
		err = fakeCtrlClient.Get(context.TODO(), client.ObjectKeyFromObject(kuadrantCR), updatedKuadrant)
		assert.NilError(subT, err)
		assert.Assert(subT, controllerutil.ContainsFinalizer(updatedKuadrant, authConfigFinalizer),
			"finalizer should be added to Kuadrant CR")

		// build topology and call reconcile so we delete AuthConfigs
		topology := buildTopology(subT, kuadrantCR, authConfig1, authConfig2, authorino)
		state := &sync.Map{}
		err = reconciler.Reconcile(context.TODO(), nil, topology, nil, state)
		assert.NilError(subT, err)

		errorRegistry := GetOrCreateErrorRegistry(state)
		assert.Assert(subT, !errorRegistry.HasErrors(), "expected no errors in registry")

		// verify that it was deleted
		_, err = fakeDynClient.Resource(kuadrantauthorino.AuthConfigsResource).Namespace("kuadrant-system").Get(context.TODO(), "authconfig1", metav1.GetOptions{})
		assert.Assert(subT, apierrors.IsNotFound(err), "authconfig1 should have been deleted")
		_, err = fakeDynClient.Resource(kuadrantauthorino.AuthConfigsResource).Namespace("kuadrant-system").Get(context.TODO(), "authconfig2", metav1.GetOptions{})
		assert.Assert(subT, apierrors.IsNotFound(err), "authconfig2 should have been deleted")

		//verify Kuadrant CR was removed
		postReconcile := &kuadrantv1beta1.Kuadrant{}
		err = fakeCtrlClient.Get(context.TODO(), client.ObjectKeyFromObject(kuadrantCR), postReconcile)
		assert.Assert(subT, apierrors.IsNotFound(err), "Kuadrant CR should have been deleted after finalizer removal")
	})

	t.Run("reconcile does nothing when no Kuadrant CR in topology", func(subT *testing.T) {
		kuadrantCR := buildKuadrantCR(false, false)
		fakeDynClient := dfake.NewSimpleDynamicClient(scheme, authConfig1, authConfig2)
		fakeCtrlClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		reconciler := &AuthConfigsReconciler{
			client:         fakeDynClient,
			BaseReconciler: reconcilers.NewBaseReconciler(fakeCtrlClient, scheme, fakeCtrlClient),
		}

		// verify Kuadrant CR is not present in the fake server with finalizer
		updatedKuadrant := &kuadrantv1beta1.Kuadrant{}
		err := fakeCtrlClient.Get(context.TODO(), client.ObjectKeyFromObject(kuadrantCR), updatedKuadrant)
		assert.Assert(subT, apierrors.IsNotFound(err), "kuadrant CR should not be present in the fake server")

		// build topology, now without KuadrantCR, so we can test the case
		topology := buildTopology(subT, nil, authConfig1, authConfig2)
		state := &sync.Map{}
		err = reconciler.Reconcile(context.TODO(), nil, topology, nil, state)
		assert.NilError(subT, err)

		errorRegistry := GetOrCreateErrorRegistry(state)
		assert.Assert(subT, !errorRegistry.HasErrors(), "expected no errors in registry")
		// AuthConfigs should still exist
		_, err = fakeDynClient.Resource(kuadrantauthorino.AuthConfigsResource).Namespace("kuadrant-system").Get(context.TODO(), "authconfig1", metav1.GetOptions{})
		assert.NilError(subT, err, "authconfig1 should not have been deleted")
		_, err = fakeDynClient.Resource(kuadrantauthorino.AuthConfigsResource).Namespace("kuadrant-system").Get(context.TODO(), "authconfig2", metav1.GetOptions{})
		assert.NilError(subT, err, "authconfig2 should not have been deleted")
	})
}

func TestEqualAuthConfigs(t *testing.T) {
	baseAuthConfig := &authorinov1beta3.AuthConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test",
			},
			Annotations: map[string]string{
				kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation: "default/test-route#rule-1",
			},
		},
		Spec: authorinov1beta3.AuthConfigSpec{
			Hosts: []string{"test.example.com"},
		},
	}

	tests := []struct {
		name     string
		existing *authorinov1beta3.AuthConfig
		desired  *authorinov1beta3.AuthConfig
		want     bool
	}{
		{
			name:     "identical authconfigs",
			existing: baseAuthConfig.DeepCopy(),
			desired:  baseAuthConfig.DeepCopy(),
			want:     true,
		},
		{
			name:     "different spec",
			existing: baseAuthConfig.DeepCopy(),
			desired: func() *authorinov1beta3.AuthConfig {
				ac := baseAuthConfig.DeepCopy()
				ac.Spec.Hosts = []string{"different.example.com"}
				return ac
			}(),
			want: false,
		},
		{
			name:     "different labels",
			existing: baseAuthConfig.DeepCopy(),
			desired: func() *authorinov1beta3.AuthConfig {
				ac := baseAuthConfig.DeepCopy()
				ac.Labels["env"] = "prod"
				return ac
			}(),
			want: false,
		},
		{
			name:     "different HTTP route rule annotation",
			existing: baseAuthConfig.DeepCopy(),
			desired: func() *authorinov1beta3.AuthConfig {
				ac := baseAuthConfig.DeepCopy()
				ac.Annotations[kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation] = "default/other-route#rule-1"
				return ac
			}(),
			want: false,
		},
		{
			name: "missing HTTP route rule annotation in existing",
			existing: func() *authorinov1beta3.AuthConfig {
				ac := baseAuthConfig.DeepCopy()
				delete(ac.Annotations, kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation)
				return ac
			}(),
			desired: baseAuthConfig.DeepCopy(),
			want:    false,
		},
		{
			name: "grpc route rule annotation present",
			existing: func() *authorinov1beta3.AuthConfig {
				ac := baseAuthConfig.DeepCopy()
				delete(ac.Annotations, kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation)
				ac.Annotations[kuadrantauthorino.AuthConfigGRPCRouteRuleAnnotation] = "default/grpc-route#rule-1"
				return ac
			}(),
			desired: func() *authorinov1beta3.AuthConfig {
				ac := baseAuthConfig.DeepCopy()
				delete(ac.Annotations, kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation)
				ac.Annotations[kuadrantauthorino.AuthConfigGRPCRouteRuleAnnotation] = "default/grpc-route#rule-1"
				return ac
			}(),
			want: true,
		},
		{
			name: "different grpc route rule annotation",
			existing: func() *authorinov1beta3.AuthConfig {
				ac := baseAuthConfig.DeepCopy()
				delete(ac.Annotations, kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation)
				ac.Annotations[kuadrantauthorino.AuthConfigGRPCRouteRuleAnnotation] = "default/grpc-route#rule-1"
				return ac
			}(),
			desired: func() *authorinov1beta3.AuthConfig {
				ac := baseAuthConfig.DeepCopy()
				delete(ac.Annotations, kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation)
				ac.Annotations[kuadrantauthorino.AuthConfigGRPCRouteRuleAnnotation] = "default/other-grpc-route#rule-1"
				return ac
			}(),
			want: false,
		},
		{
			name: "nil annotations in existing",
			existing: func() *authorinov1beta3.AuthConfig {
				ac := baseAuthConfig.DeepCopy()
				ac.Annotations = nil
				return ac
			}(),
			desired: baseAuthConfig.DeepCopy(),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := equalAuthConfigs(tt.existing, tt.desired)
			if got != tt.want {
				t.Errorf("equalAuthConfigs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyAuthConfigUpdate(t *testing.T) {
	tests := []struct {
		name     string
		existing *authorinov1beta3.AuthConfig
		desired  *authorinov1beta3.AuthConfig
		validate func(t *testing.T, updated *authorinov1beta3.AuthConfig, desired *authorinov1beta3.AuthConfig)
	}{
		{
			name: "updates spec, labels and annotations",
			existing: &authorinov1beta3.AuthConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					Labels: map[string]string{
						"app": "old-value",
					},
					Annotations: map[string]string{
						kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation: "default/old-route#rule-1",
						"other-annotation": "should-be-preserved",
					},
				},
				Spec: authorinov1beta3.AuthConfigSpec{
					Hosts: []string{"old.example.com"},
				},
			},
			desired: &authorinov1beta3.AuthConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					Labels: map[string]string{
						"app": "new-value",
						"env": "prod",
					},
					Annotations: map[string]string{
						kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation: "default/new-route#rule-2",
					},
				},
				Spec: authorinov1beta3.AuthConfigSpec{
					Hosts: []string{"new.example.com"},
				},
			},
			validate: func(t *testing.T, updated *authorinov1beta3.AuthConfig, desired *authorinov1beta3.AuthConfig) {
				if len(updated.Spec.Hosts) != 1 || updated.Spec.Hosts[0] != "new.example.com" {
					t.Errorf("Spec not updated correctly: got %v", updated.Spec.Hosts)
				}
				if updated.Labels["app"] != "new-value" || updated.Labels["env"] != "prod" {
					t.Errorf("Labels not updated correctly: got %v", updated.Labels)
				}
				if updated.Annotations[kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation] != "default/new-route#rule-2" {
					t.Errorf("HTTP route annotation not updated: got %v", updated.Annotations[kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation])
				}
				if updated.Annotations["other-annotation"] != "should-be-preserved" {
					t.Errorf("Other annotations should be preserved: got %v", updated.Annotations["other-annotation"])
				}
				if _, exists := updated.Annotations[kuadrantauthorino.AuthConfigGRPCRouteRuleAnnotation]; exists {
					t.Errorf("gRPC annotation should not exist")
				}
			},
		},
		{
			name: "switches from HTTP to gRPC annotation",
			existing: &authorinov1beta3.AuthConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					Annotations: map[string]string{
						kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation: "default/http-route#rule-1",
					},
				},
				Spec: authorinov1beta3.AuthConfigSpec{
					Hosts: []string{"test.example.com"},
				},
			},
			desired: &authorinov1beta3.AuthConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					Annotations: map[string]string{
						kuadrantauthorino.AuthConfigGRPCRouteRuleAnnotation: "default/grpc-route#rule-1",
					},
				},
				Spec: authorinov1beta3.AuthConfigSpec{
					Hosts: []string{"test.example.com"},
				},
			},
			validate: func(t *testing.T, updated *authorinov1beta3.AuthConfig, desired *authorinov1beta3.AuthConfig) {
				if _, exists := updated.Annotations[kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation]; exists {
					t.Errorf("HTTP annotation should be removed")
				}
				if updated.Annotations[kuadrantauthorino.AuthConfigGRPCRouteRuleAnnotation] != "default/grpc-route#rule-1" {
					t.Errorf("gRPC annotation not set: got %v", updated.Annotations[kuadrantauthorino.AuthConfigGRPCRouteRuleAnnotation])
				}
			},
		},
		{
			name: "handles nil annotations in existing",
			existing: &authorinov1beta3.AuthConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: authorinov1beta3.AuthConfigSpec{
					Hosts: []string{"test.example.com"},
				},
			},
			desired: &authorinov1beta3.AuthConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					Annotations: map[string]string{
						kuadrantauthorino.AuthConfigGRPCRouteRuleAnnotation: "default/grpc-route#rule-1",
					},
				},
				Spec: authorinov1beta3.AuthConfigSpec{
					Hosts: []string{"test.example.com"},
				},
			},
			validate: func(t *testing.T, updated *authorinov1beta3.AuthConfig, desired *authorinov1beta3.AuthConfig) {
				if updated.Annotations[kuadrantauthorino.AuthConfigGRPCRouteRuleAnnotation] != "default/grpc-route#rule-1" {
					t.Errorf("gRPC annotation not set: got %v", updated.Annotations)
				}
			},
		},
		{
			name: "ensures reconciliation converges",
			existing: &authorinov1beta3.AuthConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					Labels: map[string]string{
						"app": "test",
					},
					Annotations: map[string]string{
						kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation: "default/old-route#rule-1",
					},
				},
				Spec: authorinov1beta3.AuthConfigSpec{
					Hosts: []string{"test.example.com"},
				},
			},
			desired: &authorinov1beta3.AuthConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					Labels: map[string]string{
						"app": "test",
					},
					Annotations: map[string]string{
						kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation: "default/new-route#rule-2",
					},
				},
				Spec: authorinov1beta3.AuthConfigSpec{
					Hosts: []string{"test.example.com"},
				},
			},
			validate: func(t *testing.T, updated *authorinov1beta3.AuthConfig, desired *authorinov1beta3.AuthConfig) {
				// After update, they should be equal (convergence test)
				if !equalAuthConfigs(updated, desired) {
					t.Errorf("After update, authconfigs should be equal (reconciliation should converge)")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify they're different before update
			if equalAuthConfigs(tt.existing, tt.desired) {
				t.Logf("Note: existing and desired are already equal before update")
			}

			// Apply the update
			applyAuthConfigUpdate(context.Background(), tt.existing, tt.desired)

			// Run validation
			tt.validate(t, tt.existing, tt.desired)
		})
	}
}
