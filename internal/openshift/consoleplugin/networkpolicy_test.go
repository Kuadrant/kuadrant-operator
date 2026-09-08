//go:build unit

package consoleplugin

import (
	"testing"

	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNetworkPolicy(t *testing.T) {
	policy := NetworkPolicy("custom-kuadrant-namespace")
	assert.Equal(t, policy.Namespace, "custom-kuadrant-namespace")
	assert.DeepEqual(t, policy.Spec.PodSelector, *DeploymentSelector())
	assert.DeepEqual(t, policy.Spec.PolicyTypes, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})
	assert.Assert(t, len(policy.Spec.Egress) == 0)
	assert.Assert(t, len(policy.Spec.Ingress) == 1)
	rule := policy.Spec.Ingress[0]
	assert.Assert(t, len(rule.From) == 1)
	assert.DeepEqual(t, rule.From[0], networkingv1.NetworkPolicyPeer{
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "openshift-console"}},
		PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app": "console"}},
	})
	assert.Assert(t, len(rule.Ports) == 1)
	assert.Equal(t, *rule.Ports[0].Protocol, corev1.ProtocolTCP)
	assert.Equal(t, rule.Ports[0].Port.IntValue(), 9443)
}
