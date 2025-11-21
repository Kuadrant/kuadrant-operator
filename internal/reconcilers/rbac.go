package reconcilers

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ClusterRoleRulesMutator(desired, existing *rbacv1.ClusterRole) bool {
	if !rulesEqual(existing.Rules, desired.Rules) {
		existing.Rules = desired.Rules
		return true
	}
	return false
}

func rulesEqual(a, b []rbacv1.PolicyRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !policyRuleEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func policyRuleEqual(a, b rbacv1.PolicyRule) bool {
	return sliceEqual(a.Verbs, b.Verbs) &&
		sliceEqual(a.APIGroups, b.APIGroups) &&
		sliceEqual(a.Resources, b.Resources) &&
		sliceEqual(a.ResourceNames, b.ResourceNames) &&
		sliceEqual(a.NonResourceURLs, b.NonResourceURLs)
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type ClusterRoleMutateFn func(desired, existing *rbacv1.ClusterRole) bool

func ClusterRoleMutator(opts ...ClusterRoleMutateFn) MutateFn {
	return func(existingObj, desiredObj client.Object) (bool, error) {
		existing, ok := existingObj.(*rbacv1.ClusterRole)
		if !ok {
			return false, fmt.Errorf("%T is not a *rbacv1.ClusterRole", existingObj)
		}
		desired, ok := desiredObj.(*rbacv1.ClusterRole)
		if !ok {
			return false, fmt.Errorf("%T is not a *rbacv1.ClusterRole", desiredObj)
		}

		update := false

		for _, opt := range opts {
			tmpUpdate := opt(desired, existing)
			update = update || tmpUpdate
		}

		return update, nil
	}
}

func ClusterRoleBindingSubjectsMutator(desired, existing *rbacv1.ClusterRoleBinding) bool {
	update := false

	if !subjectsEqual(existing.Subjects, desired.Subjects) {
		existing.Subjects = desired.Subjects
		update = true
	}

	if existing.RoleRef != desired.RoleRef {
		existing.RoleRef = desired.RoleRef
		update = true
	}

	return update
}

func subjectsEqual(a, b []rbacv1.Subject) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type ClusterRoleBindingMutateFn func(desired, existing *rbacv1.ClusterRoleBinding) bool

func ClusterRoleBindingMutator(opts ...ClusterRoleBindingMutateFn) MutateFn {
	return func(existingObj, desiredObj client.Object) (bool, error) {
		existing, ok := existingObj.(*rbacv1.ClusterRoleBinding)
		if !ok {
			return false, fmt.Errorf("%T is not a *rbacv1.ClusterRoleBinding", existingObj)
		}
		desired, ok := desiredObj.(*rbacv1.ClusterRoleBinding)
		if !ok {
			return false, fmt.Errorf("%T is not a *rbacv1.ClusterRoleBinding", desiredObj)
		}

		update := false

		for _, opt := range opts {
			tmpUpdate := opt(desired, existing)
			update = update || tmpUpdate
		}

		return update, nil
	}
}

type ServiceAccountMutateFn func(desired, existing *corev1.ServiceAccount) bool

func ServiceAccountMutator(opts ...ServiceAccountMutateFn) MutateFn {
	return func(existingObj, desiredObj client.Object) (bool, error) {
		existing, ok := existingObj.(*corev1.ServiceAccount)
		if !ok {
			return false, fmt.Errorf("%T is not a *corev1.ServiceAccount", existingObj)
		}
		desired, ok := desiredObj.(*corev1.ServiceAccount)
		if !ok {
			return false, fmt.Errorf("%T is not a *corev1.ServiceAccount", desiredObj)
		}

		update := false

		for _, opt := range opts {
			tmpUpdate := opt(desired, existing)
			update = update || tmpUpdate
		}

		return update, nil
	}
}
