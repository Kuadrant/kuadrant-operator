//go:build unit

package reconcilers

import (
	"testing"

	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterRoleRulesMutator(t *testing.T) {
	t.Run("no changes needed", func(subT *testing.T) {
		desired := &rbacv1.ClusterRole{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"get", "list"},
				},
			},
		}
		existing := &rbacv1.ClusterRole{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"get", "list"},
				},
			},
		}
		updated := ClusterRoleRulesMutator(desired, existing)
		assert.Equal(subT, updated, false)
	})

	t.Run("rules different - should update", func(subT *testing.T) {
		desired := &rbacv1.ClusterRole{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"get", "list", "watch"},
				},
			},
		}
		existing := &rbacv1.ClusterRole{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"get", "list"},
				},
			},
		}
		updated := ClusterRoleRulesMutator(desired, existing)
		assert.Equal(subT, updated, true)
		assert.DeepEqual(subT, existing.Rules, desired.Rules)
	})

	t.Run("different number of rules", func(subT *testing.T) {
		desired := &rbacv1.ClusterRole{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"get"},
				},
				{
					APIGroups: []string{"apps"},
					Resources: []string{"deployments"},
					Verbs:     []string{"get"},
				},
			},
		}
		existing := &rbacv1.ClusterRole{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"get"},
				},
			},
		}
		updated := ClusterRoleRulesMutator(desired, existing)
		assert.Equal(subT, updated, true)
	})
}

func TestClusterRoleBindingSubjectsMutator(t *testing.T) {
	t.Run("no changes needed", func(subT *testing.T) {
		desired := &rbacv1.ClusterRoleBinding{
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "test-sa",
					Namespace: "test-ns",
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "test-role",
			},
		}
		existing := &rbacv1.ClusterRoleBinding{
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "test-sa",
					Namespace: "test-ns",
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "test-role",
			},
		}
		updated := ClusterRoleBindingSubjectsMutator(desired, existing)
		assert.Equal(subT, updated, false)
	})

	t.Run("subjects different - should update", func(subT *testing.T) {
		desired := &rbacv1.ClusterRoleBinding{
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "new-sa",
					Namespace: "test-ns",
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "test-role",
			},
		}
		existing := &rbacv1.ClusterRoleBinding{
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "old-sa",
					Namespace: "test-ns",
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "test-role",
			},
		}
		updated := ClusterRoleBindingSubjectsMutator(desired, existing)
		assert.Equal(subT, updated, true)
		assert.DeepEqual(subT, existing.Subjects, desired.Subjects)
	})

	t.Run("roleRef different - should update", func(subT *testing.T) {
		desired := &rbacv1.ClusterRoleBinding{
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "test-sa",
					Namespace: "test-ns",
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "new-role",
			},
		}
		existing := &rbacv1.ClusterRoleBinding{
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "test-sa",
					Namespace: "test-ns",
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "old-role",
			},
		}
		updated := ClusterRoleBindingSubjectsMutator(desired, existing)
		assert.Equal(subT, updated, true)
		assert.DeepEqual(subT, existing.RoleRef, desired.RoleRef)
	})
}

func TestServiceAccountMutator(t *testing.T) {
	t.Run("no options provided", func(subT *testing.T) {
		desired := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: "test-ns",
			},
		}
		existing := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: "test-ns",
			},
		}
		mutator := ServiceAccountMutator()
		updated, err := mutator(existing, desired)
		assert.NilError(subT, err)
		assert.Equal(subT, updated, false)
	})
}

func TestClusterRoleMutator(t *testing.T) {
	t.Run("with rules mutator", func(subT *testing.T) {
		desired := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-role",
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"get", "list", "watch"},
				},
			},
		}
		existing := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-role",
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"get"},
				},
			},
		}
		mutator := ClusterRoleMutator(ClusterRoleRulesMutator)
		updated, err := mutator(existing, desired)
		assert.NilError(subT, err)
		assert.Equal(subT, updated, true)
		assert.DeepEqual(subT, existing.Rules, desired.Rules)
	})

	t.Run("wrong type - should error", func(subT *testing.T) {
		desired := &rbacv1.ClusterRole{}
		existing := &corev1.ServiceAccount{}
		mutator := ClusterRoleMutator(ClusterRoleRulesMutator)
		_, err := mutator(existing, desired)
		assert.Error(subT, err, "*v1.ServiceAccount is not a *rbacv1.ClusterRole")
	})
}

func TestClusterRoleBindingMutator(t *testing.T) {
	t.Run("with subjects mutator", func(subT *testing.T) {
		desired := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-binding",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "new-sa",
					Namespace: "test-ns",
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "test-role",
			},
		}
		existing := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-binding",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "old-sa",
					Namespace: "test-ns",
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "test-role",
			},
		}
		mutator := ClusterRoleBindingMutator(ClusterRoleBindingSubjectsMutator)
		updated, err := mutator(existing, desired)
		assert.NilError(subT, err)
		assert.Equal(subT, updated, true)
		assert.DeepEqual(subT, existing.Subjects, desired.Subjects)
	})

	t.Run("wrong type - should error", func(subT *testing.T) {
		desired := &rbacv1.ClusterRoleBinding{}
		existing := &corev1.ServiceAccount{}
		mutator := ClusterRoleBindingMutator(ClusterRoleBindingSubjectsMutator)
		_, err := mutator(existing, desired)
		assert.Error(subT, err, "*v1.ServiceAccount is not a *rbacv1.ClusterRoleBinding")
	})
}
