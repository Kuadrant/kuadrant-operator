//go:build integration

package envoygateway_test

import (
	"context"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/pkg/utils"
)

// IsEnvoyExtenstionPolicyAccepted checks extension policy accepted status for a given target ref.
// This method can be useful for non-test code for the status controller that should
// check for extension policy accepted state. Currently status controller does not do that. Not yet.
func IsEnvoyExtensionPolicyAccepted(g Gomega, ctx context.Context, cl client.Client, key client.ObjectKey, gwKey client.ObjectKey) {
	policy := &egv1alpha1.EnvoyExtensionPolicy{}
	g.Expect(cl.Get(ctx, key, policy)).To(Succeed())

	ancestor, ok := utils.Find(policy.Status.Ancestors, func(ancestor gatewayapiv1alpha2.PolicyAncestorStatus) bool {
		// Only supporting gateways
		ancestorNamespace := ptr.Deref(ancestor.AncestorRef.Namespace, gatewayapiv1.Namespace(gwKey.Namespace))
		return string(ancestor.AncestorRef.Name) == gwKey.Name && string(ancestorNamespace) == gwKey.Namespace
	})
	g.Expect(ok).To(BeTrue())

	acceptedCond := meta.FindStatusCondition(ancestor.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
	g.Expect(acceptedCond).ToNot(BeNil())
	g.Expect(acceptedCond.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(acceptedCond.Reason).To(Equal(string(gatewayapiv1alpha2.PolicyReasonAccepted)))
}

// IsEnvoyPatchPolicyAccepted checks patch policy accepted status for a given target ref.
// This method can be useful for non-test code for the status controller that should
// check for the patch policy accepted state. Currently status controller does not do that. Not yet.
func IsEnvoyPatchPolicyAccepted(g Gomega, ctx context.Context, cl client.Client, key client.ObjectKey, gwKey client.ObjectKey) {
	policy := &egv1alpha1.EnvoyPatchPolicy{}
	g.Expect(cl.Get(ctx, key, policy)).To(Succeed())

	ancestor, ok := utils.Find(policy.Status.Ancestors, func(ancestor gatewayapiv1alpha2.PolicyAncestorStatus) bool {
		// Only supporting gateways
		ancestorNamespace := ptr.Deref(ancestor.AncestorRef.Namespace, gatewayapiv1.Namespace(gwKey.Namespace))
		return string(ancestor.AncestorRef.Name) == gwKey.Name && string(ancestorNamespace) == gwKey.Namespace
	})
	g.Expect(ok).To(BeTrue())

	acceptedCond := meta.FindStatusCondition(ancestor.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
	g.Expect(acceptedCond).ToNot(BeNil())
	g.Expect(acceptedCond.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(acceptedCond.Reason).To(Equal(string(gatewayapiv1alpha2.PolicyReasonAccepted)))
}
