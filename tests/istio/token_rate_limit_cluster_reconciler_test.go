//go:build integration

package istio_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("TokenRateLimitPolicy Limitador Cluster EnvoyFilter controller", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		trlpName      = "toystore-trlp"
	)

	beforeEachCallback := func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway := tests.BuildBasicGateway(TestGatewayName, testNamespace)
		err := testClient().Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

		Eventually(tests.LimitadorIsReady(testClient(), client.ObjectKey{
			Name:      kuadrant.LimitadorName,
			Namespace: kuadrantInstallationNS,
		})).WithContext(ctx).Should(Succeed())
	}

	BeforeEach(beforeEachCallback)
	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("TRLP targeting Gateway", func() {

		// kuadrant mTLS is off
		BeforeEach(func(ctx SpecContext) {
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: false}
			Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())
		})

		It("EnvoyFilter only created if TRLP is in the path to a route", func(ctx SpecContext) {
			// create tokenratelimitpolicy
			trlp := &kuadrantv1alpha1.TokenRateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "TokenRateLimitPolicy",
					APIVersion: kuadrantv1alpha1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      trlpName,
					Namespace: testNamespace,
				},
				Spec: kuadrantv1alpha1.TokenRateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"free-tier": {
								Rates: []kuadrantv1.Rate{
									{
										Limit: 20000, Window: "24h",
									},
								},
								When: kuadrantv1.WhenPredicates{
									{Predicate: `request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")`},
								},
								Counters: []kuadrantv1.Counter{
									{Expression: kuadrantv1.Expression("auth.identity.userid")},
								},
							},
						},
					},
				},
			}
			err := testClient().Create(ctx, trlp)
			Expect(err).ToNot(HaveOccurred())

			// Check TRLP status is available
			trlpKey := client.ObjectKey{Name: trlpName, Namespace: testNamespace}
			Eventually(tests.TokenRateLimitPolicyIsAccepted(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeFalse())
			Expect(tests.TokenRateLimitPolicyEnforcedCondition(ctx, testClient(), trlpKey, kuadrant.PolicyReasonUnknown, "TokenRateLimitPolicy is not in the path to any existing routes"))

			// no httproute and no filter
			efKey := client.ObjectKey{Name: controllers.RateLimitClusterName(TestGatewayName), Namespace: testNamespace}
			Eventually(func() bool {
				ef := &istioclientnetworkingv1alpha3.EnvoyFilter{}
				err := testClient().Get(ctx, efKey, ef)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())

			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.example.com"})
			err = testClient().Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// filter is created
			Eventually(func() bool {
				ef := &istioclientnetworkingv1alpha3.EnvoyFilter{}
				err := testClient().Get(ctx, efKey, ef)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			// EnvoyFilter contains trl configuration
			ef := &istioclientnetworkingv1alpha3.EnvoyFilter{}
			err = testClient().Get(ctx, efKey, ef)
			Expect(err).ToNot(HaveOccurred())

			Expect(ef.Spec.ConfigPatches).To(HaveLen(1))

			patchValue := ef.Spec.ConfigPatches[0].Patch.Value.AsMap()
			Expect(patchValue).To(HaveKey("name"))
			Expect(patchValue["name"]).To(Equal("kuadrant-ratelimit-service"))
		})

		It("EnvoyFilter removed when TRLP is deleted", func(ctx SpecContext) {
			// create tokenratelimitpolicy
			trlp := &kuadrantv1alpha1.TokenRateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "TokenRateLimitPolicy",
					APIVersion: kuadrantv1alpha1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      trlpName,
					Namespace: testNamespace,
				},
				Spec: kuadrantv1alpha1.TokenRateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"free-tier": {
								Rates: []kuadrantv1.Rate{
									{
										Limit: 20000, Window: "24h",
									},
								},
								When: kuadrantv1.WhenPredicates{
									{Predicate: `request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")`},
								},
								Counters: []kuadrantv1.Counter{
									{Expression: kuadrantv1.Expression("auth.identity.userid")},
								},
							},
						},
					},
				},
			}
			err := testClient().Create(ctx, trlp)
			Expect(err).ToNot(HaveOccurred())

			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.example.com"})
			err = testClient().Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// TRLP is ready
			trlpKey := client.ObjectKey{Name: trlpName, Namespace: testNamespace}
			Eventually(tests.TokenRateLimitPolicyIsReady(ctx, testClient(), trlpKey)).WithContext(ctx).Should(Succeed())

			// filter created
			efKey := client.ObjectKey{Name: controllers.RateLimitClusterName(TestGatewayName), Namespace: testNamespace}
			Eventually(func() bool {
				ef := &istioclientnetworkingv1alpha3.EnvoyFilter{}
				err := testClient().Get(ctx, efKey, ef)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			// delete TRLP
			err = testClient().Delete(ctx, trlp)
			Expect(err).ToNot(HaveOccurred())

			// filter should be removed
			Eventually(func() bool {
				ef := &istioclientnetworkingv1alpha3.EnvoyFilter{}
				err := testClient().Get(ctx, efKey, ef)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		})
	})

	Context("TRLP targeting HTTPRoute", func() {
		It("EnvoyFilter created when TRLP targets HTTPRoute", func(ctx SpecContext) {
			// create httproute first
			httpRoute := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.example.com"})
			err := testClient().Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create tokenratelimitpolicy targeting HTTPRoute
			trlp := &kuadrantv1alpha1.TokenRateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "TokenRateLimitPolicy",
					APIVersion: kuadrantv1alpha1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      trlpName,
					Namespace: testNamespace,
				},
				Spec: kuadrantv1alpha1.TokenRateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(TestHTTPRouteName),
						},
					},
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"free-tier": {
								Rates: []kuadrantv1.Rate{
									{
										Limit: 20000, Window: "24h",
									},
								},
								When: kuadrantv1.WhenPredicates{
									{Predicate: `request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")`},
								},
								Counters: []kuadrantv1.Counter{
									{Expression: kuadrantv1.Expression("auth.identity.userid")},
								},
							},
						},
					},
				},
			}
			err = testClient().Create(ctx, trlp)
			Expect(err).ToNot(HaveOccurred())

			// TRLP ready
			trlpKey := client.ObjectKey{Name: trlpName, Namespace: testNamespace}
			Eventually(tests.TokenRateLimitPolicyIsReady(ctx, testClient(), trlpKey)).WithContext(ctx).Should(Succeed())

			// filter should be created
			efKey := client.ObjectKey{Name: controllers.RateLimitClusterName(TestGatewayName), Namespace: testNamespace}
			Eventually(func() bool {
				ef := &istioclientnetworkingv1alpha3.EnvoyFilter{}
				err := testClient().Get(ctx, efKey, ef)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			ef := &istioclientnetworkingv1alpha3.EnvoyFilter{}
			err = testClient().Get(ctx, efKey, ef)
			Expect(err).ToNot(HaveOccurred())

			Expect(ef.Spec.ConfigPatches).To(HaveLen(1))
			patchValue := ef.Spec.ConfigPatches[0].Patch.Value.AsMap()
			Expect(patchValue).To(HaveKey("name"))
			Expect(patchValue["name"]).To(Equal("kuadrant-ratelimit-service"))
		})
	})
})
