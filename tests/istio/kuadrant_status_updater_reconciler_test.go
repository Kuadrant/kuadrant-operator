//go:build integration

package istio_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/tests"
)

// The tests need to be run in serial as kuadrant CR namespace is shared
var _ = Describe("kuadrant status reconciler", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(3 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
		rlpName          = "toystore-rlp"
		kapName          = "toystore-kap"
	)

	var (
		testNamespace string
	)

	beforeEachCallback := func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway := tests.BuildBasicGateway(TestGatewayName, testNamespace)
		Expect(testClient().Create(ctx, gateway)).ToNot(HaveOccurred())

		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

		route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.toystore.com"})
		Expect(k8sClient.Create(ctx, route)).To(Succeed())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

		// create ratelimitpolicy
		rlp := &kuadrantv1.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RateLimitPolicy",
				APIVersion: kuadrantv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      rlpName,
				Namespace: testNamespace,
			},
			Spec: kuadrantv1.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "HTTPRoute",
						Name:  gatewayapiv1.ObjectName(TestHTTPRouteName),
					},
				},
				RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
					Limits: map[string]kuadrantv1.Limit{
						"l1": {
							Rates: []kuadrantv1.Rate{
								{
									Limit: 1, Window: kuadrantv1.Duration("3m"),
								},
							},
						},
					},
				},
			},
		}
		Expect(testClient().Create(ctx, rlp)).ToNot(HaveOccurred())
		// Check RLP status is available
		rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
		Eventually(tests.RLPIsAccepted(ctx, testClient(), rlpKey)).WithContext(ctx).Should(BeTrue())
		Eventually(tests.RLPIsEnforced(ctx, testClient(), rlpKey)).WithContext(ctx).Should(BeTrue())

		authPolicy := &kuadrantv1.AuthPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AuthPolicy",
				APIVersion: kuadrantv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      kapName,
				Namespace: testNamespace,
			},
			Spec: kuadrantv1.AuthPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "HTTPRoute",
						Name:  gatewayapiv1.ObjectName(TestHTTPRouteName),
					},
				},
				Defaults: &kuadrantv1.MergeableAuthPolicySpec{
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: tests.BuildBasicAuthScheme(),
					},
				},
			},
		}

		Expect(testClient().Create(ctx, authPolicy)).ToNot(HaveOccurred())
		// check policy status
		Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), authPolicy)).WithContext(ctx).Should(BeTrue())
	}

	BeforeEach(beforeEachCallback)
	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("when mTLS is on", func() {
		BeforeEach(func(ctx SpecContext) {
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, kuadrantKey, kuadrantObj)).To(Succeed())
				kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: true}
				g.Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())
		})

		It("reconciles status mtls fields", func(ctx SpecContext) {
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(func(g Gomega, ctx context.Context) {
				kuadrantObj := &kuadrantv1beta1.Kuadrant{}
				g.Expect(testClient().Get(ctx, kuadrantKey, kuadrantObj)).NotTo(HaveOccurred())
				g.Expect(kuadrantObj.Status.MtlsAuthorino).To(Equal(ptr.To(true)))
				g.Expect(kuadrantObj.Status.MtlsLimitador).To(Equal(ptr.To(true)))
			})

			// Delete the RLP
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			rlp := &kuadrantv1.RateLimitPolicy{}
			Expect(testClient().Get(ctx, rlpKey, rlp)).NotTo(HaveOccurred())
			Expect(testClient().Delete(ctx, rlp)).ToNot(HaveOccurred())

			Eventually(func(g Gomega, ctx context.Context) {
				newK := &kuadrantv1beta1.Kuadrant{}
				g.Expect(testClient().Get(ctx, kuadrantKey, newK)).NotTo(HaveOccurred())
				g.Expect(newK.Status.MtlsAuthorino).To(Equal(ptr.To(true)))
				g.Expect(newK.Status.MtlsLimitador).To(Equal(ptr.To(false)))
			}).WithContext(ctx).Should(Succeed())

			// Delete the AuthPolicy
			kapKey := client.ObjectKey{Name: kapName, Namespace: testNamespace}
			kap := &kuadrantv1.AuthPolicy{}
			Expect(testClient().Get(ctx, kapKey, kap)).NotTo(HaveOccurred())
			Expect(testClient().Delete(ctx, kap)).ToNot(HaveOccurred())

			Eventually(func(g Gomega, ctx context.Context) {
				newK := &kuadrantv1beta1.Kuadrant{}
				g.Expect(testClient().Get(ctx, kuadrantKey, newK)).NotTo(HaveOccurred())
				g.Expect(newK.Status.MtlsAuthorino).To(Equal(ptr.To(false)))
				g.Expect(newK.Status.MtlsLimitador).To(Equal(ptr.To(false)))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when mTLS is off", func() {
		BeforeEach(func(ctx SpecContext) {
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, kuadrantKey, kuadrantObj)).To(Succeed())
				kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: false}
				g.Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())
		})

		It("reconciles status mtls fields", func(ctx SpecContext) {
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(func(g Gomega, ctx context.Context) {
				kuadrantObj := &kuadrantv1beta1.Kuadrant{}
				g.Expect(testClient().Get(ctx, kuadrantKey, kuadrantObj)).NotTo(HaveOccurred())
				g.Expect(kuadrantObj.Status.MtlsAuthorino).To(Equal(ptr.To(false)))
				g.Expect(kuadrantObj.Status.MtlsLimitador).To(Equal(ptr.To(false)))
			})
		}, testTimeOut)
	})
})
