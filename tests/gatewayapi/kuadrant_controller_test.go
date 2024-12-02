//go:build integration

package gatewayapi_test

import (
	"time"

	authorinoapi "github.com/kuadrant/authorino/api/v1beta3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/controllers"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Kuadrant controller when gateway provider is missing", func() {
	var (
		testNamespace    string
		testTimeOut      = SpecTimeout(15 * time.Second)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("when default kuadrant CR is created", func() {
		It("Status reports missing Gateway API provider (istio / envoy gateway)", func(ctx SpecContext) {
			kuadrantCR := &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "local",
					Namespace: testNamespace,
				},
			}
			Expect(testClient().Create(ctx, kuadrantCR)).ToNot(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)).ToNot(HaveOccurred())
				cond := meta.FindStatusCondition(kuadrantCR.Status.Conditions, controllers.ReadyConditionType)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("MissingDependency"))
				g.Expect(cond.Message).To(Equal("Gateway API provider (istio / envoy gateway) is not installed, please restart pod once dependency has been installed"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when rate limit policy is created", func() {
		It("Status is populated with missing Gateway API provider (istio / envoy gateway)", func(ctx SpecContext) {
			policy := &kuadrantv1.RateLimitPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rlp",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Kind:  "Gateway",
							Group: gatewayapiv1.GroupName,
							Name:  "test",
						},
					},
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{
							"test": {
								Rates: []kuadrantv1.Rate{
									{
										Limit:  10,
										Window: "10s",
									},
								},
							},
						},
					},
				},
			}

			Expect(testClient().Create(ctx, policy)).To(Succeed())

			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(policy), policy)
				g.Expect(err).ToNot(HaveOccurred())

				cond := meta.FindStatusCondition(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("MissingDependency"))
				g.Expect(cond.Message).To(Equal("Gateway API provider (istio / envoy gateway) is not installed, please restart pod once dependency has been installed"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when auth policy is created", func() {
		It("Status is populated with missing Gateway API provider (istio / envoy gateway)", func(ctx SpecContext) {
			policy := &kuadrantv1.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Kind:  "Gateway",
							Group: gatewayapiv1.GroupName,
							Name:  "test",
						},
					},
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: &kuadrantv1.AuthSchemeSpec{
							Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
								"anyonmous": {
									AuthenticationSpec: authorinoapi.AuthenticationSpec{
										AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
											AnonymousAccess: &authorinoapi.AnonymousAccessSpec{},
										},
									},
								},
							},
						},
					},
				},
			}

			Expect(testClient().Create(ctx, policy)).To(Succeed())

			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(policy), policy)
				g.Expect(err).ToNot(HaveOccurred())

				cond := meta.FindStatusCondition(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("MissingDependency"))
				g.Expect(cond.Message).To(Equal("Gateway API provider (istio / envoy gateway) is not installed, please restart pod once dependency has been installed"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
