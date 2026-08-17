//go:build unit

package controllers

import (
	"context"
	"reflect"
	"testing"

	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantauthorino "github.com/kuadrant/kuadrant-operator/internal/authorino"
)

func buildKuadrantCR() *kuadrantv1beta1.Kuadrant {
	return &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantv1beta1.KuadrantGroupKind.Kind,
			APIVersion: kuadrantv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kuadrant",
			Namespace: "kuadrant-system",
			UID:       "test-uid-123",
		},
	}
}

func TestAuthConfigOwnerReferences(t *testing.T) {
	t.Run("returns nil when Kuadrant CR is nil", func(t *testing.T) {
		var nilCR *kuadrantv1beta1.Kuadrant
		if refs := nilCR.GetOwnerReference(); refs != nil {
			t.Errorf("expected nil owner references, got %v", refs)
		}
	})

	t.Run("builds a controller owner reference to the Kuadrant CR", func(t *testing.T) {
		kuadrantCR := buildKuadrantCR()
		refs := kuadrantCR.GetOwnerReference()

		if len(refs) != 1 {
			t.Fatalf("expected exactly one owner reference, got %d", len(refs))
		}
		ref := refs[0]
		if ref.APIVersion != kuadrantv1beta1.GroupVersion.String() {
			t.Errorf("unexpected APIVersion: got %q", ref.APIVersion)
		}
		if ref.Kind != kuadrantv1beta1.KuadrantGroupKind.Kind {
			t.Errorf("unexpected Kind: got %q", ref.Kind)
		}
		if ref.Name != kuadrantCR.GetName() {
			t.Errorf("unexpected Name: got %q", ref.Name)
		}
		if ref.UID != kuadrantCR.GetUID() {
			t.Errorf("unexpected UID: got %q", ref.UID)
		}
		if ref.BlockOwnerDeletion == nil || !*ref.BlockOwnerDeletion {
			t.Errorf("expected BlockOwnerDeletion to be true")
		}
		if ref.Controller == nil || !*ref.Controller {
			t.Errorf("expected Controller to be true")
		}
	})
}

func TestEqualAuthConfigs(t *testing.T) {
	ownerRefs := buildKuadrantCR().GetOwnerReference()

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
		{
			name:     "desired owner reference missing from existing",
			existing: baseAuthConfig.DeepCopy(),
			desired: func() *authorinov1beta3.AuthConfig {
				ac := baseAuthConfig.DeepCopy()
				ac.OwnerReferences = ownerRefs
				return ac
			}(),
			want: false,
		},
		{
			name: "matching owner references",
			existing: func() *authorinov1beta3.AuthConfig {
				ac := baseAuthConfig.DeepCopy()
				ac.OwnerReferences = ownerRefs
				return ac
			}(),
			desired: func() *authorinov1beta3.AuthConfig {
				ac := baseAuthConfig.DeepCopy()
				ac.OwnerReferences = ownerRefs
				return ac
			}(),
			want: true,
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
	ownerRefs := buildKuadrantCR().GetOwnerReference()

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
			name: "sets owner reference from desired",
			existing: &authorinov1beta3.AuthConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					Annotations: map[string]string{
						kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation: "default/route#rule-1",
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
						kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation: "default/route#rule-1",
					},
					OwnerReferences: ownerRefs,
				},
				Spec: authorinov1beta3.AuthConfigSpec{
					Hosts: []string{"test.example.com"},
				},
			},
			validate: func(t *testing.T, updated *authorinov1beta3.AuthConfig, desired *authorinov1beta3.AuthConfig) {
				if !reflect.DeepEqual(updated.GetOwnerReferences(), ownerRefs) {
					t.Errorf("owner reference not applied: got %v", updated.GetOwnerReferences())
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
					OwnerReferences: ownerRefs,
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
