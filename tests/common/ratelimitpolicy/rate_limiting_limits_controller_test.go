//go:build integration

package ratelimitpolicy

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
	"github.com/kuadrant/kuadrant-operator/tests"
)

// Flagged as Serial, as all the tests are using same Limitador CR (from the kuadrant NS)
var _ = Describe("Rate Limiting limits controller", Serial, func() {
	const (
		testTimeOut       = SpecTimeout(2 * time.Minute)
		beforeEachTimeOut = NodeTimeout(2 * time.Minute)
		afterEachTimeOut  = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
	)

	limitadorHasLimits := func(ctx context.Context, limits ...limitadorv1alpha1.RateLimit) func(g Gomega) {
		return func(g Gomega) {
			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantInstallationNS}
			existingLimitador := &limitadorv1alpha1.Limitador{}
			g.Expect(k8sClient.Get(ctx, limitadorKey, existingLimitador)).To(Succeed())
			g.Expect(existingLimitador.Spec.Limits).To(HaveLen(len(limits)))
			for i := range limits {
				limit := limits[i]
				g.Expect(existingLimitador.Spec.Limits).To(ContainElement(limit))
			}
		}
	}

	limitadorHasEmptyLimits := func(ctx context.Context) func(g Gomega) {
		return limitadorHasLimits(ctx)
	}

	policyFactory := func(name string, mutateFns ...func(policy *kuadrantv1beta2.RateLimitPolicy)) *kuadrantv1beta2.RateLimitPolicy {
		policy := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RateLimitPolicy",
				APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
				},
				RateLimitPolicyCommonSpec: kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"l1": {
							Rates: []kuadrantv1beta2.Rate{
								{
									Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
								},
							},
						},
					},
				},
			},
		}

		for _, mutateFn := range mutateFns {
			mutateFn(policy)
		}

		return policy
	}

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		// wait for limitador to be empty
		Eventually(func(g Gomega) {
			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantInstallationNS}
			existingLimitador := &limitadorv1alpha1.Limitador{}
			g.Expect(testClient().Get(ctx, limitadorKey, existingLimitador)).To(Succeed())
			g.Expect(existingLimitador.Spec.Limits).To(BeEmpty())
		}).WithContext(ctx).Should(Succeed())
	}, beforeEachTimeOut)

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("RLP targeting HTTPRoute", func() {
		var (
			routeName = "toystore-route"
			rlpName   = "toystore-rlp"
			gwName    = "toystore-gw"
			gateway   *gatewayapiv1.Gateway
		)

		BeforeEach(func(ctx SpecContext) {
			gateway = tests.BuildBasicGateway(gwName, testNamespace)
			Expect(testClient().Create(ctx, gateway)).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
		}, beforeEachTimeOut)

		It("limitador captures the policy configuration", func(ctx SpecContext) {
			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			Expect(testClient().Create(ctx, httpRoute)).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory(rlpName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
							},
						},
					},
				}
			})
			Expect(testClient().Create(ctx, rlp)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(tests.RLPIsAccepted(ctx, testClient(), rlpKey)).WithContext(ctx).Should(BeTrue())

			// check limits
			Eventually(limitadorHasLimits(
				ctx,
				limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				},
			)).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("RLP targeting Gateway", func() {
		var (
			routeName = "toystore-route"
			gwName    = "toystore-gw"
			rlpName   = "toystore-rlp"
			gateway   *gatewayapiv1.Gateway
		)

		BeforeEach(func(ctx SpecContext) {
			gateway = tests.BuildBasicGateway(gwName, testNamespace)
			Expect(testClient().Create(ctx, gateway)).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
		}, beforeEachTimeOut)

		It("limitador captures the policy configuration", func(ctx SpecContext) {
			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			Expect(testClient().Create(ctx, httpRoute)).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory(rlpName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
							},
						},
					},
				}
			})

			Expect(testClient().Create(ctx, rlp)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(tests.RLPIsAccepted(ctx, testClient(), rlpKey)).WithContext(ctx).Should(BeTrue())

			// check limits
			Eventually(limitadorHasLimits(
				ctx,
				limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				},
			)).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("RLP targeting Gateway when there is no HTTPRoute attached to the gateway", func() {
		var (
			gwName  = "toystore-gw"
			rlpName = "toystore-rlp"
			gateway *gatewayapiv1.Gateway
		)

		BeforeEach(func(ctx SpecContext) {
			gateway = tests.BuildBasicGateway(gwName, testNamespace)
			Expect(testClient().Create(ctx, gateway)).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
		}, beforeEachTimeOut)

		It("limitador does not capture the policy configuration", func(ctx SpecContext) {
			// create ratelimitpolicy
			rlp := policyFactory(rlpName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
							},
						},
					},
				}
			})
			Expect(testClient().Create(ctx, rlp)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(tests.RLPIsAccepted(ctx, testClient(), rlpKey)).WithContext(ctx).Should(BeTrue())

			// check limits are empty
			Eventually(limitadorHasEmptyLimits(ctx)).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("When HTTPRoute switches from one gateway into another and RLP targets the gateway", func() {
		// Initial state
		// Gw A
		// Gw B
		// RLP A -> Gw A
		// Route A -> Gw A
		//
		// Switch route parentship
		// Gw A
		// Gw B
		// RLP A -> Gw A
		// Route A -> Gw B

		var (
			gwAName   = "gw-a"
			gwBName   = "gw-b"
			routeName = "route-a"
			rlpName   = "rlp-a"
		)

		It("limitador does not capture the policy configuration", func(ctx SpecContext) {
			// gwA
			gatewayA := tests.BuildBasicGateway(gwAName, testNamespace)
			Expect(testClient().Create(ctx, gatewayA)).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gatewayA)).WithContext(ctx).Should(BeTrue())

			// gwB
			gatewayB := tests.BuildBasicGateway(gwBName, testNamespace)
			Expect(testClient().Create(ctx, gatewayB)).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gatewayB)).WithContext(ctx).Should(BeTrue())

			// RLP
			rlp := policyFactory(rlpName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwAName)
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
							},
						},
					},
				}
			})
			Expect(testClient().Create(ctx, rlp)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(tests.RLPIsAccepted(ctx, testClient(), rlpKey)).WithContext(ctx).Should(BeTrue())

			// HTTPRoute
			httpRoute := tests.BuildBasicHttpRoute(routeName, gwAName, testNamespace, []string{"*.example.com"})
			Expect(testClient().Create(ctx, httpRoute)).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// Initial state set.
			// check limits
			Eventually(limitadorHasLimits(
				ctx,
				limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				},
			)).WithContext(ctx).Should(Succeed())

			// Proceed with the update:
			// From Route A -> Gw A
			// To Route A -> Gw B
			httpRouteUpdated := &gatewayapiv1.HTTPRoute{}
			err := testClient().Get(ctx, client.ObjectKeyFromObject(httpRoute), httpRouteUpdated)
			Expect(err).ToNot(HaveOccurred())
			httpRouteUpdated.Spec.CommonRouteSpec.ParentRefs[0].Name = gatewayapiv1.ObjectName(gwBName)
			err = testClient().Update(ctx, httpRouteUpdated)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// The RLP targeting the gateway no longer is effective, as the gateway does not have any route
			// check limits are empty
			Eventually(limitadorHasEmptyLimits(ctx)).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("When HTTPRoute switches from one gateway into another and RLP targets the route", func() {
		// Initial state
		// Gw A
		// Gw B
		// Route A -> Gw A
		// RLP A -> Route A
		//
		// Switch route parentship
		// Gw A
		// Gw B
		// Route A -> Gw B
		// RLP A -> Route A

		var (
			gwAName   = "gw-a"
			gwBName   = "gw-b"
			routeName = "route-a"
			rlpName   = "rlp-a"
		)

		It("limitador captures the policy configuration", func(ctx SpecContext) {
			// gwA
			gatewayA := tests.BuildBasicGateway(gwAName, testNamespace)
			Expect(testClient().Create(ctx, gatewayA)).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gatewayA)).WithContext(ctx).Should(BeTrue())

			// gwB
			gatewayB := tests.BuildBasicGateway(gwBName, testNamespace)
			Expect(testClient().Create(ctx, gatewayB)).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gatewayB)).WithContext(ctx).Should(BeTrue())

			// HTTPRoute
			httpRoute := tests.BuildBasicHttpRoute(routeName, gwAName, testNamespace, []string{"*.example.com"})
			Expect(testClient().Create(ctx, httpRoute)).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// RLP
			rlp := policyFactory(rlpName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
							},
						},
					},
				}
			})
			Expect(testClient().Create(ctx, rlp)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(tests.RLPIsAccepted(ctx, testClient(), rlpKey)).WithContext(ctx).Should(BeTrue())

			// Initial state set.
			// check limits
			Eventually(limitadorHasLimits(
				ctx,
				limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				},
			)).WithContext(ctx).Should(Succeed())

			// Proceed with the update:
			// From Route A -> Gw A
			// To Route A -> Gw B
			httpRouteUpdated := &gatewayapiv1.HTTPRoute{}
			err := testClient().Get(ctx, client.ObjectKeyFromObject(httpRoute), httpRouteUpdated)
			Expect(err).ToNot(HaveOccurred())
			httpRouteUpdated.Spec.CommonRouteSpec.ParentRefs[0].Name = gatewayapiv1.ObjectName(gwBName)
			err = testClient().Update(ctx, httpRouteUpdated)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// The RLP targeting the new route is still effective
			// check limits
			Eventually(limitadorHasLimits(
				ctx,
				limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				},
			)).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("When the policy switches from one route into another", func() {
		// Initial state
		// Gw A
		// Route A -> Gw A
		// Route B -> Gw A
		// RLP A -> Route A
		//
		// Switch RLP target
		// Gw A
		// Route A -> Gw A
		// Route B -> Gw A
		// RLP A -> Route B

		var (
			gwName     = "gw-a"
			routeAName = "route-a"
			routeBName = "route-b"
			rlpName    = "rlp-a"
		)

		It("limitador captures the policy configuration", func(ctx SpecContext) {
			// gw
			gateway := tests.BuildBasicGateway(gwName, testNamespace)
			Expect(testClient().Create(ctx, gateway)).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

			// HTTPRoute A
			httpRouteA := tests.BuildBasicHttpRoute(routeAName, gwName, testNamespace, []string{"*.a.example.com"})
			Expect(testClient().Create(ctx, httpRouteA)).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteA))).WithContext(ctx).Should(BeTrue())

			// HTTPRoute B
			httpRouteB := tests.BuildBasicHttpRoute(routeBName, gwName, testNamespace, []string{"*.b.example.com"})
			Expect(testClient().Create(ctx, httpRouteA)).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteB))).WithContext(ctx).Should(BeTrue())

			// RLP
			rlp := policyFactory(rlpName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeAName)
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
							},
						},
					},
				}
			})
			Expect(testClient().Create(ctx, rlp)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(tests.RLPIsAccepted(ctx, testClient(), rlpKey)).WithContext(ctx).Should(BeTrue())

			// Initial state set.
			// check limits
			Eventually(limitadorHasLimits(
				ctx,
				limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				},
			)).WithContext(ctx).Should(Succeed())

			// Proceed with the update:
			// From RLP -> Route A
			// To RLP -> Route B
			rlpUpdated := &kuadrantv1beta2.RateLimitPolicy{}
			err := testClient().Get(ctx, client.ObjectKeyFromObject(rlp), rlpUpdated)
			Expect(err).ToNot(HaveOccurred())
			rlpUpdated.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeBName)
			err = testClient().Update(ctx, rlpUpdated)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			Eventually(tests.RLPIsAccepted(ctx, testClient(), rlpKey)).WithContext(ctx).Should(BeTrue())

			// The RLP targeting the new route is still effective
			// check limits
			Eventually(limitadorHasLimits(
				ctx,
				limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				},
			)).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("When adding a new policy to the route when another policy targets the gateway", func() {
		// Initial state
		// Gw A
		// Route A -> Gw A
		// RLP A -> Gw A
		//
		// Add new RLP
		// Gw A
		// Route A -> Gw A
		// RLP A -> Gw A
		// RLP B -> Route A

		var (
			gwName    = "gw-a"
			routeName = "route-a"
			rlpAName  = "rlp-a"
			rlpBName  = "rlp-b"
		)

		It("limitador only captures the policy configuration that targets the route", func(ctx SpecContext) {
			// gw
			gateway := tests.BuildBasicGateway(gwName, testNamespace)
			Expect(testClient().Create(ctx, gateway)).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

			// HTTPRoute
			httpRoute := tests.BuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			Expect(testClient().Create(ctx, httpRoute)).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// RLP A
			rlpA := policyFactory(rlpAName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
							},
						},
					},
				}
			})
			Expect(testClient().Create(ctx, rlpA)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpAKey := client.ObjectKey{Name: rlpAName, Namespace: testNamespace}
			Eventually(tests.RLPIsAccepted(ctx, testClient(), rlpAKey)).WithContext(ctx).Should(BeTrue())

			// Initial state set.
			// check limits
			Eventually(limitadorHasLimits(
				ctx,
				limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpA),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpAKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpA),
				},
			)).WithContext(ctx).Should(Succeed())

			// Proceed with the update:
			// New RLP B -> Route A
			// the gateway no longer has untargeted routes
			// RLP A
			rlpB := policyFactory(rlpBName,
				func(policy *kuadrantv1beta2.RateLimitPolicy) {
					policy.Spec.TargetRef.Kind = "HTTPRoute"
					policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
				},
				func(policy *kuadrantv1beta2.RateLimitPolicy) {
					policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
						"l2": {
							Rates: []kuadrantv1beta2.Rate{
								{
									Limit: 15, Duration: 5, Unit: kuadrantv1beta2.TimeUnit("minute"),
								},
							},
						},
					}
				})
			Expect(testClient().Create(ctx, rlpB)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpBKey := client.ObjectKey{Name: rlpBName, Namespace: testNamespace}
			Eventually(tests.RLPIsAccepted(ctx, testClient(), rlpBKey)).WithContext(ctx).Should(BeTrue())

			// The RLP targeting the new route is effective
			// The RLP targeting the gateway is no longer effective
			// check limits
			Eventually(limitadorHasLimits(
				ctx,
				limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpB),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpBKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpB),
				},
			)).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("When adding a new route to the gateway", func() {
		// Initial state
		// Gw A
		// Route A -> Gw A
		// RLP A -> Gw A
		// RLP B -> Route A
		//
		// Add new Route B
		// Gw A
		// Route A -> Gw A
		// Route B -> Gw A
		// RLP A -> Gw A
		// RLP B -> Route A

		var (
			gwName     = "gw-a"
			routeAName = "route-a"
			routeBName = "route-b"
			rlpAName   = "rlp-a"
			rlpBName   = "rlp-b"
		)

		It("limitador captures the policy configuration from both policies", func(ctx SpecContext) {
			// gw
			gateway := tests.BuildBasicGateway(gwName, testNamespace)
			Expect(testClient().Create(ctx, gateway)).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

			// HTTPRoute A
			httpRouteA := tests.BuildBasicHttpRoute(routeAName, gwName, testNamespace, []string{"*.a.example.com"})
			Expect(testClient().Create(ctx, httpRouteA)).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteA))).WithContext(ctx).Should(BeTrue())

			// RLP A
			rlpA := policyFactory(rlpAName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
							},
						},
					},
				}
			})
			Expect(testClient().Create(ctx, rlpA)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpAKey := client.ObjectKey{Name: rlpAName, Namespace: testNamespace}
			Eventually(tests.RLPIsAccepted(ctx, testClient(), rlpAKey)).WithContext(ctx).Should(BeTrue())

			// RLP B
			rlpB := policyFactory(rlpBName,
				func(policy *kuadrantv1beta2.RateLimitPolicy) {
					policy.Spec.TargetRef.Kind = "HTTPRoute"
					policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeAName)
				},
				func(policy *kuadrantv1beta2.RateLimitPolicy) {
					policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
						"l2": {
							Rates: []kuadrantv1beta2.Rate{
								{
									Limit: 15, Duration: 5, Unit: kuadrantv1beta2.TimeUnit("minute"),
								},
							},
						},
					}
				})
			Expect(testClient().Create(ctx, rlpB)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpBKey := client.ObjectKey{Name: rlpBName, Namespace: testNamespace}
			Eventually(tests.RLPIsAccepted(ctx, testClient(), rlpBKey)).WithContext(ctx).Should(BeTrue())

			// Initial state set.
			// check limits -> only RLP targeting the route adds configuration
			Eventually(limitadorHasLimits(
				ctx,
				limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpB),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpBKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpB),
				},
			)).WithContext(ctx).Should(Succeed())

			// Proceed with the update:
			// New Route B -> Gw A
			// the gateway has a new untargeted route, thus RLP targeting the gateway takes effect
			// HTTPRoute B
			httpRouteB := tests.BuildBasicHttpRoute(routeBName, gwName, testNamespace, []string{"*.b.example.com"})
			Expect(testClient().Create(ctx, httpRouteB)).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteB))).WithContext(ctx).Should(BeTrue())

			// The RLP targeting the gateway is now effective
			// check limits
			Eventually(limitadorHasLimits(
				ctx,
				limitadorv1alpha1.RateLimit{
					MaxValue:   15,
					Seconds:    5 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpB),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpBKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpB),
				},
				limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpA),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpAKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpA),
				},
			)).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("RLP Defaults", func() {
		var (
			routeName    = "toystore-route"
			gwRLPName    = "toystore-gw-rlp"
			routeRLPName = "toystore-route-rlp"
		)

		It("Route policy defaults taking precedence over Gateway policy defaults", func(ctx SpecContext) {
			var (
				gwRLP    *kuadrantv1beta2.RateLimitPolicy
				routeRLP *kuadrantv1beta2.RateLimitPolicy
			)

			// Common setup
			// GW policy defaults are overridden and not enforced when Route has their own policy attached

			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.example.com"})
			Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create GW RLP
			gwRLP = policyFactory(gwRLPName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				policy.Spec.Defaults.Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
							},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(tests.RLPIsAccepted(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Create HTTPRoute RLP with new default limits
			routeRLP = policyFactory(routeRLPName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
				policy.Spec.Defaults.Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 10, Duration: 5, Unit: "second",
							},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, routeRLP)).To(Succeed())
			routeRLPKey := client.ObjectKey{Name: routeRLP.Name, Namespace: testNamespace}
			Eventually(tests.RLPIsAccepted(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// check limits
			Eventually(limitadorHasLimits(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   10,
				Seconds:    5,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(routeRLPKey, "l1"))},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			})).WithContext(ctx).Should(Succeed())
		})
	})

	Context("RLP Overrides", func() {
		var (
			gwRLPName    = "toystore-gw-rlp"
			routeRLPName = "toystore-route-rlp"
			gwRLP        *kuadrantv1beta2.RateLimitPolicy
			routeRLP     *kuadrantv1beta2.RateLimitPolicy
		)

		BeforeEach(func(ctx SpecContext) {
			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.example.com"})
			Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			gwRLP = policyFactory(gwRLPName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				policy.Spec.Defaults = nil
				policy.Spec.Overrides.Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
							},
						},
					},
				}
			})

			routeRLP = policyFactory(routeRLPName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Name = "httproute-rlp"
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"route": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 10, Duration: 5, Unit: "second",
							},
						},
					},
				}
			})
		})

		It("Gateway atomic override - gateway overrides exist and then route policy created", func(ctx SpecContext) {
			// create GW RLP with overrides
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKeyFromObject(gwRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Create HTTPRoute RLP
			Expect(k8sClient.Create(ctx, routeRLP)).To(Succeed())
			routeRLPKey := client.ObjectKeyFromObject(routeRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// check limits - should contain override values
			Eventually(limitadorHasLimits(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    180,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(routeRLPKey, "l1"))},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			})).WithContext(ctx).Should(Succeed())

			// Delete GW RLP
			Expect(k8sClient.Delete(ctx, gwRLP)).To(Succeed())
			Eventually(tests.RLPIsAccepted(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// check limits - should be route RLP values
			Eventually(limitadorHasLimits(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   10,
				Seconds:    5,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(routeRLPKey, "route"))},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			})).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Gateway atomic override - route policy exits and then gateway policy created", func(ctx SpecContext) {
			// Create Route RLP
			Expect(k8sClient.Create(ctx, routeRLP)).To(Succeed())
			routeRLPKey := client.ObjectKeyFromObject(routeRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// create GW RLP with override
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKeyFromObject(gwRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Should contain override values
			Eventually(limitadorHasLimits(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    180,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(routeRLPKey, "l1"))},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			})).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Gateway atomic override - gateway defaults turned into overrides later on", func(ctx SpecContext) {
			// Create Route RLP
			Expect(k8sClient.Create(ctx, routeRLP)).To(Succeed())
			routeRLPKey := client.ObjectKeyFromObject(routeRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// Create GW RLP with defaults
			gwRLP = policyFactory(gwRLPName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				policy.Spec.Defaults = nil
				policy.Spec.Overrides.Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
							},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKeyFromObject(gwRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Should contain Route RLP values
			Eventually(limitadorHasLimits(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   10,
				Seconds:    5,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(routeRLPKey, "route"))},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			})).WithContext(ctx).Should(Succeed())

			// Update GW RLP defaults to overrides
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, gwRLPKey, gwRLP)).To(Succeed())
				gwRLP.Spec.Overrides = gwRLP.Spec.Defaults.DeepCopy()
				gwRLP.Spec.Defaults = nil
				g.Expect(k8sClient.Update(ctx, gwRLP)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			// Should contain override values
			Eventually(limitadorHasLimits(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    180,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(routeRLPKey, "l1"))},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			})).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Gateway atomic override - gateway overrides turned into defaults later on", func(ctx SpecContext) {
			// Create HTTPRoute RLP
			Expect(k8sClient.Create(ctx, routeRLP)).To(Succeed())
			routeRLPKey := client.ObjectKeyFromObject(routeRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// create GW RLP with overrides
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(tests.RLPIsAccepted(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Should contain override values
			Eventually(limitadorHasLimits(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    180,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(routeRLPKey, "l1"))},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			})).WithContext(ctx).Should(Succeed())

			// Update GW RLP overrides to defaults
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, gwRLPKey, gwRLP)).To(Succeed())
				gwRLP.Spec.Defaults = gwRLP.Spec.Overrides.DeepCopy()
				gwRLP.Spec.Overrides = nil
				g.Expect(k8sClient.Update(ctx, gwRLP)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			// Should contain Route RLP values
			Eventually(limitadorHasLimits(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   10,
				Seconds:    5,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(routeRLPKey, "route"))},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			})).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("HTTPRoute with multiple gateway parents", func() {
		var (
			gatewayAName        = "gateway-a"
			gatewayBName        = "gateway-b"
			targetedRouteName   = "targeted-route"
			untargetedRouteName = "untargeted-route"

			gatewayA        *gatewayapiv1.Gateway
			gatewayB        *gatewayapiv1.Gateway
			targetedRoute   *gatewayapiv1.HTTPRoute
			untargetedRoute *gatewayapiv1.HTTPRoute
		)

		BeforeEach(func(ctx SpecContext) {
			gatewayA = tests.BuildBasicGateway(gatewayAName, testNamespace, func(g *gatewayapiv1.Gateway) {
				g.Spec.Listeners[0].Hostname = ptr.To(gatewayapiv1.Hostname("*.a.example.com"))
			})
			err := k8sClient.Create(ctx, gatewayA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gatewayA)).WithContext(ctx).Should(BeTrue())

			gatewayB = tests.BuildBasicGateway(gatewayBName, testNamespace, func(g *gatewayapiv1.Gateway) {
				g.Spec.Listeners[0].Hostname = ptr.To(gatewayapiv1.Hostname("*.b.example.com"))
			})
			err = k8sClient.Create(ctx, gatewayB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gatewayB)).WithContext(ctx).Should(BeTrue())

			gatewayParentsFunc := func(r *gatewayapiv1.HTTPRoute) {
				r.Spec.ParentRefs = []gatewayapiv1.ParentReference{
					{Name: gatewayapiv1.ObjectName(gatewayAName)},
					{Name: gatewayapiv1.ObjectName(gatewayBName)},
				}
			}

			targetedRoute = tests.BuildBasicHttpRoute(targetedRouteName, gatewayAName, testNamespace, []string{"targeted.a.example.com", "targeted.b.example.com"}, gatewayParentsFunc)
			err = k8sClient.Create(ctx, targetedRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(targetedRoute))).WithContext(ctx).Should(BeTrue())

			untargetedRoute = tests.BuildBasicHttpRoute(untargetedRouteName, gatewayAName, testNamespace, []string{"untargeted.a.example.com", "untargeted.b.example.com"}, gatewayParentsFunc)
			err = k8sClient.Create(ctx, untargetedRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(untargetedRoute))).WithContext(ctx).Should(BeTrue())
		})

		It("It defines route policy limits with gateway policy overrides", func(ctx SpecContext) {
			rlpGatewayA := policyFactory(gatewayAName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gatewayAName)
				policy.Spec.Defaults = nil
				policy.Spec.Overrides = &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"gw-a-1000rps": {
							Rates: []kuadrantv1beta2.Rate{
								{
									Limit: 1000, Duration: 1, Unit: "second",
								},
							},
						},
					},
				}
			})
			err := k8sClient.Create(ctx, rlpGatewayA)
			Expect(err).ToNot(HaveOccurred())

			rlpGatewayB := policyFactory(gatewayBName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gatewayBName)
				policy.Spec.Defaults = nil
				policy.Spec.Overrides = &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"gw-b-100rps": {
							Rates: []kuadrantv1beta2.Rate{
								{
									Limit: 100, Duration: 1, Unit: "second",
								},
							},
						},
					},
				}
			})
			err = k8sClient.Create(ctx, rlpGatewayB)
			Expect(err).ToNot(HaveOccurred())

			rlpTargetedRoute := policyFactory(targetedRouteName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(targetedRouteName)
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"route-10rps": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 10, Duration: 1, Unit: "second",
							},
						},
					},
				}
			})
			err = k8sClient.Create(ctx, rlpTargetedRoute)
			Expect(err).ToNot(HaveOccurred())

			Eventually(limitadorHasLimits(
				ctx,
				limitadorv1alpha1.RateLimit{
					MaxValue:   1000,
					Seconds:    1,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpTargetedRoute),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(client.ObjectKeyFromObject(rlpTargetedRoute), "gw-a-1000rps"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpTargetedRoute),
				},
				limitadorv1alpha1.RateLimit{
					MaxValue:   100,
					Seconds:    1,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpTargetedRoute),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(client.ObjectKeyFromObject(rlpTargetedRoute), "gw-b-100rps"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpTargetedRoute),
				},
				limitadorv1alpha1.RateLimit{ // FIXME(@guicassolato): we need to create one limit definition per gateway × route combination, not one per gateway × policy combination
					MaxValue:   1000,
					Seconds:    1,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpGatewayA),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(client.ObjectKeyFromObject(rlpGatewayA), "gw-a-1000rps"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpGatewayA),
				},
				limitadorv1alpha1.RateLimit{
					MaxValue:   100,
					Seconds:    1,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpGatewayB),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(client.ObjectKeyFromObject(rlpGatewayB), "gw-b-100rps"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpGatewayB),
				},
			)).WithContext(ctx).Should(Succeed())
		})
	})
})
