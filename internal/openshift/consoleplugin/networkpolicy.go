package consoleplugin

import (
	"maps"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func NetworkPolicy(namespace string) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: KuadrantConsoleName, Namespace: namespace, Labels: CommonLabels()},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: *DeploymentSelector(),
			// Do not override administrator-managed egress restrictions. The backend
			// needs DNS, the Kubernetes API and the selected Gateway listener.
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "openshift-console"}},
					PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app": "console"}},
				}},
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP), Port: ptr.To(intstr.FromInt32(9443))}},
			}},
		},
	}
}

func NetworkPolicyMutator(desired, existing *networkingv1.NetworkPolicy) bool {
	if equality.Semantic.DeepEqual(desired.Spec, existing.Spec) && maps.Equal(desired.Labels, existing.Labels) &&
		equality.Semantic.DeepEqual(desired.OwnerReferences, existing.OwnerReferences) {
		return false
	}
	existing.Spec = *desired.Spec.DeepCopy()
	existing.Labels = maps.Clone(desired.Labels)
	existing.OwnerReferences = append([]metav1.OwnerReference(nil), desired.OwnerReferences...)
	return true
}
