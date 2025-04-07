//go:build integration

package istio_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/tests"
)

// The tests need to be run in serial as kuadrant CR namespace is shared
var _ = Describe("Authorino Istio integration reconciler", Serial, func() {
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

			Eventually(tests.IsAuthorionReady(testClient(), client.ObjectKey{
				Name:      "authorino",
				Namespace: kuadrantInstallationNS,
			})).WithContext(ctx).Should(Succeed())
		})

		It("deployment pod template labels are correct", func(ctx SpecContext) {
			Eventually(func(g Gomega, ctx context.Context) {
				deployment := &appsv1.Deployment{}
				deploymentKey := client.ObjectKey{Name: "authorino", Namespace: kuadrantInstallationNS}
				g.Expect(testClient().Get(ctx, deploymentKey, deployment)).NotTo(HaveOccurred())
				g.Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("sidecar.istio.io/inject", "true"))
				g.Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("kuadrant.io/managed", "true"))
			}).WithContext(ctx).Should(Succeed())

		}, testTimeOut)
	})

	Context("when mTLS is off", func() {
		BeforeEach(func(ctx SpecContext) {
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: false}
			Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())

			Eventually(tests.IsAuthorionReady(testClient(), client.ObjectKey{
				Name:      "authorino",
				Namespace: kuadrantInstallationNS,
			})).WithContext(ctx).Should(Succeed())
		})
		It("deployment pod template labels are correct", func(ctx SpecContext) {
			Eventually(func(g Gomega, ctx context.Context) {
				deployment := &appsv1.Deployment{}
				deploymentKey := client.ObjectKey{Name: "authorino", Namespace: kuadrantInstallationNS}
				g.Expect(testClient().Get(ctx, deploymentKey, deployment)).NotTo(HaveOccurred())
				g.Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("sidecar.istio.io/inject", "false"))
				g.Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("kuadrant.io/managed", "true"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
