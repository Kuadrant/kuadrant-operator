//go:build integration

package bare_k8s_test

import (
	"time"

	authorinoapi "github.com/kuadrant/authorino/api/v1beta3"
	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Kuadrant controller when Gateway API is missing", func() {
	var (
		testNamespace    string
		testTimeOut      = SpecTimeout(30 * time.Second)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("when default kuadrant CR is created", func() {
		var kuadrantCR *kuadrantv1beta1.Kuadrant

		BeforeEach(func(ctx SpecContext) {
			kuadrantCR = &kuadrantv1beta1.Kuadrant{
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
		})

		It("Status is populated with missing Gateway API", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)).To(Succeed())

				cond := meta.FindStatusCondition(kuadrantCR.Status.Conditions, controllers.ReadyConditionType)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("MissingDependency"))
				g.Expect(cond.Message).To(Equal("[Gateway API, Gateway API provider (istio / envoy gateway)] is not installed, please restart Kuadrant Operator pod once dependency has been installed"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Limitador CR is ready", func(ctx SpecContext) {
			limitador := &limitadorv1alpha1.Limitador{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Limitador",
					APIVersion: limitadorv1alpha1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      kuadrant.LimitadorName,
					Namespace: testNamespace,
				},
			}

			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(limitador), limitador)).To(Succeed())
				g.Expect(limitador.Spec.MetricLabelsDefault).ToNot(BeNil())
				g.Expect(limitador.Spec.MetricLabelsDefault).To(Equal(ptr.To("descriptors[1]")))
				g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(limitador), limitador)).To(Succeed())

				cond := meta.FindStatusCondition(limitador.Status.Conditions, limitadorv1alpha1.StatusConditionReady)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Limitador CR should retain user fields and restore default", func(ctx SpecContext) {
			By("Patching Limitador CR with user fields")
			limitador := &limitadorv1alpha1.Limitador{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Limitador",
					APIVersion: limitadorv1alpha1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      kuadrant.LimitadorName,
					Namespace: testNamespace,
				},
				Spec: limitadorv1alpha1.LimitadorSpec{
					MetricLabelsDefault: ptr.To("descriptors[0]"),
					Listener: &limitadorv1alpha1.Listener{
						HTTP: &limitadorv1alpha1.TransportProtocol{
							Port: ptr.To(int32(9000)),
						},
					},
				},
			}

			Expect(testClient().Patch(ctx, limitador, client.Apply, &client.PatchOptions{FieldManager: "test-manager"})).ToNot(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(limitador), limitador)).To(Succeed())

				// Check that managed fields are restored
				g.Expect(limitador.Spec.MetricLabelsDefault).ToNot(BeNil())
				g.Expect(limitador.Spec.MetricLabelsDefault).To(Equal(ptr.To("descriptors[1]")))

				// Check that user added fields are retained
				g.Expect(limitador.Spec.Listener).ToNot(BeNil())
				g.Expect(limitador.Spec.Listener.HTTP).ToNot(BeNil())
				g.Expect(limitador.Spec.Listener.HTTP.Port).ToNot(BeNil())
				g.Expect(*limitador.Spec.Listener.HTTP.Port).To(Equal(int32(9000)))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when dns policy is created", func() {
		It("Status is populated with missing Gateway API", func(ctx SpecContext) {
			policy := kuadrantv1.NewDNSPolicy("dns", testNamespace).WithTargetGateway("test")
			policy.Spec.ProviderRefs = append(policy.Spec.ProviderRefs, kuadrantdnsv1alpha1.ProviderRef{
				Name: "dnsProviderSecret",
			})
			Expect(testClient().Create(ctx, policy)).To(Succeed())

			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(policy), policy)
				g.Expect(err).ToNot(HaveOccurred())

				cond := meta.FindStatusCondition(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("MissingDependency"))
				g.Expect(cond.Message).To(Equal("[Gateway API] is not installed, please restart Kuadrant Operator pod once dependency has been installed"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when tls policy is created", func() {
		It("Status is populated with missing Gateway API", func(ctx SpecContext) {
			policy := kuadrantv1.NewTLSPolicy("tls", testNamespace).
				WithTargetGateway("test")

			Expect(testClient().Create(ctx, policy)).To(Succeed())

			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(policy), policy)
				g.Expect(err).ToNot(HaveOccurred())

				cond := meta.FindStatusCondition(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("MissingDependency"))
				g.Expect(cond.Message).To(Equal("[Gateway API] is not installed, please restart Kuadrant Operator pod once dependency has been installed"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when rate limit policy is created", func() {
		It("Status is populated with missing Gateway API", func(ctx SpecContext) {
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
				g.Expect(cond.Message).To(Equal("[Gateway API, Gateway API provider (istio / envoy gateway)] is not installed, please restart Kuadrant Operator pod once dependency has been installed"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when auth policy is created", func() {
		It("Status is populated with missing Gateway API", func(ctx SpecContext) {
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
				g.Expect(cond.Message).To(Equal("[Gateway API, Gateway API provider (istio / envoy gateway)] is not installed, please restart Kuadrant Operator pod once dependency has been installed"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
