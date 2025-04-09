//go:build integration

package istio_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istiosecurityapiv1 "istio.io/api/security/v1"
	istiosecurityv1 "istio.io/client-go/pkg/apis/security/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

// The tests need to be run in serial as kuadrant CR namespace is shared
var _ = Describe("PeerAuthentication reconciler", Serial, func() {
	const (
		testTimeOut = SpecTimeout(3 * time.Minute)
	)

	Context("when mTLS is on", func() {
		BeforeEach(func(ctx SpecContext) {
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: true}
			Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())
		})

		It("peerauthentication is created", func(ctx SpecContext) {
			peerAuth := &istiosecurityv1.PeerAuthentication{}
			key := client.ObjectKey{Name: "default", Namespace: kuadrantInstallationNS}
			Eventually(func(g Gomega, ctx context.Context) {
				g.Expect(testClient().Get(ctx, key, peerAuth)).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())
			Expect(peerAuth.Spec.Mtls).NotTo(BeNil())
			Expect(peerAuth.Spec.Mtls.Mode).To(Equal(istiosecurityapiv1.PeerAuthentication_MutualTLS_STRICT))
			Expect(peerAuth.Spec.Selector).NotTo(BeNil())
			Expect(peerAuth.Spec.Selector.MatchLabels).To(Equal(map[string]string{
				"kuadrant.io/managed": "true",
			}))
		}, testTimeOut)
	})

	Context("when mTLS is off", func() {
		BeforeEach(func(ctx SpecContext) {
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: false}
			Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())
		})

		It("peerauthentication does not exist", func(ctx SpecContext) {
			Eventually(func(g Gomega, ctx context.Context) {
				peerAuthList := &istiosecurityv1.PeerAuthenticationList{}
				g.Expect(
					testClient().List(ctx, peerAuthList,
						client.InNamespace(kuadrantInstallationNS),
						client.MatchingLabels{"kuadrant.io/managed": "true"},
					),
				).NotTo(HaveOccurred())
				g.Expect(peerAuthList.Items).To(HaveLen(0))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
