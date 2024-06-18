//go:build integration

package ratelimitpolicy

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
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

	assertPolicyIsAcceptedAndEnforced := func(ctx context.Context, key client.ObjectKey) func() bool {
		return func() bool {
			return tests.RLPIsAccepted(ctx, testClient(), key)() && tests.RLPIsEnforced(ctx, testClient(), key)()
		}
	}

	limitadorContainsLimit := func(ctx context.Context, limits ...limitadorv1alpha1.RateLimit) func(g Gomega) {
		return func(g Gomega) {
			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantInstallationNS}
			existingLimitador := &limitadorv1alpha1.Limitador{}
			g.Expect(k8sClient.Get(ctx, limitadorKey, existingLimitador)).To(Succeed())
			for i := range limits {
				limit := limits[i]
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limit))
			}
		}
	}

	policyFactory := func(name string, mutateFns ...func(policy *kuadrantv1beta2.RateLimitPolicy)) *kuadrantv1beta2.RateLimitPolicy {
		policy := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RateLimitPolicy",
				APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{},
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
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
			})
			Expect(testClient().Create(ctx, rlp)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantInstallationNS}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err := testClient().Get(ctx, limitadorKey, existingLimitador)
				// must exist
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}))
			}).WithContext(ctx).Should(Succeed())
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
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
			})

			Expect(testClient().Create(ctx, rlp)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantInstallationNS}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err := testClient().Get(ctx, limitadorKey, existingLimitador)
				// must exist
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}))
			}).WithContext(ctx).Should(Succeed())
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
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
			})
			Expect(testClient().Create(ctx, rlp)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantInstallationNS}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err := testClient().Get(ctx, limitadorKey, existingLimitador)
				// must exist
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(BeEmpty())
			}).WithContext(ctx).Should(Succeed())
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
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwAName)
			})
			Expect(testClient().Create(ctx, rlp)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// HTTPRoute
			httpRoute := tests.BuildBasicHttpRoute(routeName, gwAName, testNamespace, []string{"*.example.com"})
			Expect(testClient().Create(ctx, httpRoute)).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// Initial state set.
			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, limitadorKey, existingLimitador)).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}))
			}).WithContext(ctx).Should(Succeed())

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
			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = testClient().Get(ctx, limitadorKey, existingLimitador)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(BeEmpty())
			}).WithContext(ctx).Should(Succeed())
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
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
			})
			Expect(testClient().Create(ctx, rlp)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// Initial state set.
			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, limitadorKey, existingLimitador)).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}))
			}).WithContext(ctx).Should(Succeed())

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
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, limitadorKey, existingLimitador)).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}))
			}).WithContext(ctx).Should(Succeed())
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
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeAName)
			})
			Expect(testClient().Create(ctx, rlp)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// Initial state set.
			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, limitadorKey, existingLimitador)).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}))
			}).WithContext(ctx).Should(Succeed())

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
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// The RLP targeting the new route is still effective
			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, limitadorKey, existingLimitador)).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}))
			}).WithContext(ctx).Should(Succeed())
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
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
			})
			Expect(testClient().Create(ctx, rlpA)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpAKey := client.ObjectKey{Name: rlpAName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpAKey)).WithContext(ctx).Should(BeTrue())

			// Initial state set.
			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, limitadorKey, existingLimitador)).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpA),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpAKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpA),
				}))
			}).WithContext(ctx).Should(Succeed())

			// Proceed with the update:
			// New RLP B -> Route A
			// the gateway no longer has untargeted routes
			// RLP A
			rlpB := policyFactory(rlpBName,
				func(policy *kuadrantv1beta2.RateLimitPolicy) {
					policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
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
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpBKey)).WithContext(ctx).Should(BeTrue())

			// The RLP targeting the new route is effective
			// The RLP targeting the gateway is no longer effective
			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, limitadorKey, existingLimitador)).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   15,
					Seconds:    5 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpB),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpBKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpB),
				}))
			}).WithContext(ctx).Should(Succeed())
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
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
			})
			Expect(testClient().Create(ctx, rlpA)).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpAKey := client.ObjectKey{Name: rlpAName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpAKey)).WithContext(ctx).Should(BeTrue())

			// RLP B
			rlpB := policyFactory(rlpBName,
				func(policy *kuadrantv1beta2.RateLimitPolicy) {
					policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
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
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpBKey)).WithContext(ctx).Should(BeTrue())

			// Initial state set.
			// check limits -> only RLP targeting the route adds configuration
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, limitadorKey, existingLimitador)).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   15,
					Seconds:    5 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpB),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpBKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpB),
				}))
			}).WithContext(ctx).Should(Succeed())

			// Proceed with the update:
			// New Route B -> Gw A
			// the gateway has a new untargeted route, thus RLP targeting the gateway takes effect
			// HTTPRoute B
			httpRouteB := tests.BuildBasicHttpRoute(routeBName, gwName, testNamespace, []string{"*.b.example.com"})
			Expect(testClient().Create(ctx, httpRouteB)).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteB))).WithContext(ctx).Should(BeTrue())

			// The RLP targeting the gateway is now effective
			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, limitadorKey, existingLimitador)).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(BeEmpty())
				g.Expect(existingLimitador.Spec.Limits).NotTo(HaveLen(1))
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   15,
					Seconds:    5 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpB),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpBKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpB),
				}))
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpA),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpAKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpA),
				}))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("RLP Defaults", func() {
		It("Route policy defaults taking precedence over Gateway policy defaults", func(ctx SpecContext) {
			var (
				gwRLP    *kuadrantv1beta2.RateLimitPolicy
				routeRLP *kuadrantv1beta2.RateLimitPolicy
			)

			// Common setup
			// GW policy defaults are overridden and not enforced when Route has their own policy attached

			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create GW RLP
			gwRLP = policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
			})
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Create HTTPRoute RLP with new default limits
			routeRLP = policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Name = "httproute-rlp"
				policy.Spec.CommonSpec().Limits = map[string]kuadrantv1beta2.Limit{
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
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, routeRLPKey)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.RLPIsEnforced(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeFalse())

			// check limits
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
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
		var gwRLP *kuadrantv1beta2.RateLimitPolicy
		var routeRLP *kuadrantv1beta2.RateLimitPolicy

		BeforeEach(func(ctx SpecContext) {
			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			gwRLP = policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				policy.Spec.Overrides = policy.Spec.Defaults.DeepCopy()
				policy.Spec.Defaults = nil
			})

			routeRLP = policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Name = "httproute-rlp"
				policy.Spec.CommonSpec().Limits = map[string]kuadrantv1beta2.Limit{
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
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Create HTTPRoute RLP
			Expect(k8sClient.Create(ctx, routeRLP)).To(Succeed())
			routeRLPKey := client.ObjectKeyFromObject(routeRLP)
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, routeRLPKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), routeRLPKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("RateLimitPolicy is overridden by [%s]", gwRLPKey)))

			// check limits - should contain override values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    180,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(routeRLPKey, "l1"))},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			})).WithContext(ctx).Should(Succeed())

			// Delete GW RLP -> Route RLP should be enforced
			Expect(k8sClient.Delete(ctx, gwRLP)).To(Succeed())
			Eventually(tests.RLPIsEnforced(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())
			// check limits - should be route RLP values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
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
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// create GW RLP with override
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKeyFromObject(gwRLP)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Route RLP should no longer be enforced
			Eventually(tests.RLPIsEnforced(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeFalse())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), routeRLPKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("RateLimitPolicy is overridden by [%s]", gwRLPKey)))

			// Should contain override values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
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
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// Create GW RLP with defaults
			gwRLP = policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
			})
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKeyFromObject(gwRLP)
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), gwRLPKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("RateLimitPolicy is overridden by [%s]", routeRLPKey)))

			// Route RLP should still be enforced
			Eventually(tests.RLPIsEnforced(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// Should contain Route RLP values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
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

			// GW RLP should now be enforced
			Eventually(tests.RLPIsEnforced(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeFalse())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), routeRLPKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("RateLimitPolicy is overridden by [%s]", gwRLPKey)))
			Eventually(tests.RLPIsEnforced(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Should contain override values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
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
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// create GW RLP with overrides
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Route RLP should not be enforced
			Eventually(tests.RLPIsEnforced(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeFalse())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), routeRLPKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("RateLimitPolicy is overridden by [%s]", gwRLPKey)))

			// Should contain override values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
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

			// Route RLP now takes precedence
			Eventually(tests.RLPIsEnforced(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeFalse())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), gwRLPKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("RateLimitPolicy is overridden by [%s]", routeRLPKey)))
			Eventually(tests.RLPIsEnforced(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// Should contain Route RLP values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   10,
				Seconds:    5,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(routeRLPKey, "route"))},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			})).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	CONTINUE!!

	Context("RLP accepted condition reasons", func() {
		assertAcceptedConditionTrue := func(rlp *kuadrantv1beta2.RateLimitPolicy) func() bool {
			return func() bool {
				rlpKey := client.ObjectKeyFromObject(rlp)
				existingRLP := &kuadrantv1beta2.RateLimitPolicy{}
				err := k8sClient.Get(context.Background(), rlpKey, existingRLP)
				if err != nil {
					return false
				}

				return meta.IsStatusConditionTrue(existingRLP.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
			}
		}

		assertAcceptedConditionFalse := func(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, reason, message string) func(g Gomega) {
			return func(g Gomega) {
				rlpKey := client.ObjectKeyFromObject(rlp)
				existingRLP := &kuadrantv1beta2.RateLimitPolicy{}
				g.Expect(k8sClient.Get(ctx, rlpKey, existingRLP)).To(Succeed())

				cond := meta.FindStatusCondition(existingRLP.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status == metav1.ConditionFalse && cond.Reason == reason && cond.Message == message).To(BeTrue())
			}
		}

		// Accepted reason is already tested generally by the existing tests

		It("Target not found reason", func(ctx SpecContext) {
			rlp := policyFactory()
			Expect(k8sClient.Create(ctx, rlp)).To(Succeed())

			Eventually(assertAcceptedConditionFalse(ctx, rlp, string(gatewayapiv1alpha2.PolicyReasonTargetNotFound),
				fmt.Sprintf("RateLimitPolicy target %s was not found", routeName)),
			).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Conflict reason", func(ctx SpecContext) {
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			rlp := policyFactory()
			Expect(k8sClient.Create(ctx, rlp)).To(Succeed())
			Eventually(tests.RLPIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(rlp))).WithContext(ctx).Should(BeTrue())

			Eventually(assertAcceptedConditionTrue(rlp), time.Minute, 5*time.Second).Should(BeTrue())

			rlp2 := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Name = "conflicting-rlp"
			})
			Expect(k8sClient.Create(ctx, rlp2)).To(Succeed())

			Eventually(assertAcceptedConditionFalse(ctx, rlp2, string(gatewayapiv1alpha2.PolicyReasonConflicted),
				fmt.Sprintf("RateLimitPolicy is conflicted by %[1]v/toystore-rlp: the gateway.networking.k8s.io/v1, Kind=HTTPRoute target %[1]v/toystore-route is already referenced by policy %[1]v/toystore-rlp", testNamespace)),
			).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Invalid reason", func(ctx SpecContext) {
			otherNamespace := tests.CreateNamespace(ctx, testClient())
			defer tests.DeleteNamespaceCallback(ctx, testClient(), otherNamespace)()

			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Namespace = otherNamespace // create the policy in a different namespace than the target
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gateway.Name)
				policy.Spec.TargetRef.Namespace = ptr.To(gatewayapiv1.Namespace(testNamespace))
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())

			Eventually(assertAcceptedConditionFalse(ctx, policy, string(gatewayapiv1alpha2.PolicyReasonInvalid), fmt.Sprintf("RateLimitPolicy target is invalid: invalid targetRef.Namespace %s. Currently only supporting references to the same namespace", testNamespace))).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("When RLP switches target from one HTTPRoute to another HTTPRoute", func() {
		var (
			routeAName = "route-a"
			routeBName = "route-b"
		)

		It("direct references are updated", func(ctx SpecContext) {
			// Initial state
			// Route A
			// RLP A -> Route A

			// Switch target to another route
			// Route A
			// Route B
			// RLP A -> Route B

			// create httproute A
			httpRouteA := tests.BuildBasicHttpRoute(routeAName, TestGatewayName, testNamespace, []string{"*.a.example.com"})
			err := k8sClient.Create(ctx, httpRouteA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteA))).WithContext(ctx).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeAName)
			})
			err = k8sClient.Create(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// Check HTTPRoute A direct back reference
			routeAKey := client.ObjectKey{Name: routeAName, Namespace: testNamespace}
			Eventually(
				tests.HTTPRouteHasDirectBackReference(testClient(),
					routeAKey, rlp.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlp).String(),
				)).WithContext(ctx).Should(BeTrue())

			// Proceed with the update:
			// From  RLP A -> Route A
			// To RLP A -> Route B

			// create httproute B
			httpRouteB := tests.BuildBasicHttpRoute(routeBName, TestGatewayName, testNamespace, []string{"*.b.example.com"})
			err = k8sClient.Create(ctx, httpRouteB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteB))).WithContext(ctx).Should(BeTrue())

			Eventually(func(g Gomega) {
				rlpUpdated := &kuadrantv1beta2.RateLimitPolicy{}
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(rlp), rlpUpdated)
				g.Expect(err).ToNot(HaveOccurred())
				rlpUpdated.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeBName)
				err = k8sClient.Update(ctx, rlpUpdated)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// Check RLP status is available
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// Check HTTPRoute A direct back reference is gone
			Eventually(
				tests.HTTPRouteWithoutDirectBackReference(testClient(), routeAKey, rlp.DirectReferenceAnnotationName())).WithContext(ctx).Should(BeTrue())

			// Check HTTPRoute B direct back reference
			routeBKey := client.ObjectKey{Name: routeBName, Namespace: testNamespace}
			Eventually(
				tests.HTTPRouteHasDirectBackReference(testClient(),
					routeBKey, rlp.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlp).String(),
				)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("When RLP switches target from one Gateway to another Gateway", func() {
		var (
			gwAName = "gw-a"
			gwBName = "gw-b"
		)

		It("direct references are updated", func(ctx SpecContext) {
			// Initial state
			// Gw A
			// RLP A -> Gw A

			// Switch target to another gw
			// Gw A
			// Gw B
			// RLP A -> Gw B

			// create Gw A
			gatewayA := tests.BuildBasicGateway(gwAName, testNamespace)
			err := k8sClient.Create(ctx, gatewayA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gatewayA)).WithContext(ctx).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwAName)
			})
			err = k8sClient.Create(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), rlpKey, kuadrant.PolicyReasonUnknown, "RateLimitPolicy has encountered some issues: no free routes to enforce policy"))

			// Check Gateway direct back reference
			gwAKey := client.ObjectKey{Name: gwAName, Namespace: testNamespace}
			Eventually(
				tests.GatewayHasDirectBackReference(testClient(),
					gwAKey, rlp.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlp).String(),
				)).WithContext(ctx).Should(BeTrue())

			// Proceed with the update:
			// From  RLP A -> Gw A
			// To RLP A -> Gw B

			// create Gw B
			gatewayB := tests.BuildBasicGateway(gwBName, testNamespace)
			err = k8sClient.Create(ctx, gatewayB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gatewayB)).WithContext(ctx).Should(BeTrue())

			Eventually(func(g Gomega) {
				rlpUpdated := &kuadrantv1beta2.RateLimitPolicy{}
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(rlp), rlpUpdated)
				g.Expect(err).ToNot(HaveOccurred())
				rlpUpdated.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwBName)
				err = k8sClient.Update(ctx, rlpUpdated)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// Check RLP status is available
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), rlpKey, kuadrant.PolicyReasonUnknown, "RateLimitPolicy has encountered some issues: no free routes to enforce policy"))

			// Check Gw A direct back reference is gone
			Eventually(
				tests.GatewayWithoutDirectBackReference(testClient(), gwAKey, rlp.DirectReferenceAnnotationName())).WithContext(ctx).Should(BeTrue())

			// Check Gateway B direct back reference
			gwBKey := client.ObjectKey{Name: gwBName, Namespace: testNamespace}
			Eventually(
				tests.GatewayHasDirectBackReference(testClient(),
					gwBKey, rlp.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlp).String(),
				)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("When RLP switches target from one HTTPRoute to another taken HTTPRoute", func() {
		var (
			routeAName = "route-a"
			routeBName = "route-b"
			rlpAName   = "rlp-a"
			rlpBName   = "rlp-b"
		)

		It("direct references are updated", func(ctx SpecContext) {
			// Initial state
			// Route A
			// Route B
			// RLP A -> Route A
			// RLP B -> Route B

			// Switch target to another route
			// Route A
			// Route B
			// RLP A -> Route B
			// RLP B -> Route B

			// create httproute A
			httpRouteA := tests.BuildBasicHttpRoute(routeAName, TestGatewayName, testNamespace, []string{"*.a.example.com"})
			err := k8sClient.Create(ctx, httpRouteA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteA))).WithContext(ctx).Should(BeTrue())

			// create httproute B
			httpRouteB := tests.BuildBasicHttpRoute(routeBName, TestGatewayName, testNamespace, []string{"*.b.example.com"})
			err = k8sClient.Create(ctx, httpRouteB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteB))).WithContext(ctx).Should(BeTrue())

			// create rlpA
			rlpA := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.ObjectMeta.Name = rlpAName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeAName)
			})
			err = k8sClient.Create(ctx, rlpA)
			Expect(err).ToNot(HaveOccurred())

			rlpAKey := client.ObjectKeyFromObject(rlpA)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpAKey)).WithContext(ctx).Should(BeTrue())

			// create rlpB
			rlpB := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.ObjectMeta.Name = rlpBName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeBName)
			})
			err = k8sClient.Create(ctx, rlpB)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpBKey := client.ObjectKeyFromObject(rlpB)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpBKey)).WithContext(ctx).Should(BeTrue())

			// Check HTTPRoute A direct back reference
			routeAKey := client.ObjectKey{Name: routeAName, Namespace: testNamespace}
			Eventually(
				tests.HTTPRouteHasDirectBackReference(testClient(),
					routeAKey, rlpA.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlpA).String(),
				)).WithContext(ctx).Should(BeTrue())

			// Check HTTPRoute B direct back reference
			routeBKey := client.ObjectKey{Name: routeBName, Namespace: testNamespace}
			Eventually(
				tests.HTTPRouteHasDirectBackReference(testClient(),
					routeBKey, rlpB.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlpB).String(),
				)).WithContext(ctx).Should(BeTrue())

			// Proceed with the update:
			// From  RLP A -> Route A
			// To RLP A -> Route B (already taken)

			Eventually(func(g Gomega) {
				rlpUpdated := &kuadrantv1beta2.RateLimitPolicy{}
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(rlpA), rlpUpdated)
				g.Expect(err).ToNot(HaveOccurred())
				rlpUpdated.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeBName)
				err = k8sClient.Update(ctx, rlpUpdated)
				g.Expect(err).ToNot(HaveOccurred())
				// Check RLP status is available
			}).WithContext(ctx).Should(Succeed())
			Eventually(tests.RLPIsNotAccepted(ctx, testClient(), rlpAKey)).WithContext(ctx).Should(BeTrue())

			// Check HTTPRoute A direct back reference is gone
			Eventually(
				tests.HTTPRouteWithoutDirectBackReference(testClient(), routeAKey, rlpA.DirectReferenceAnnotationName())).WithContext(ctx).Should(BeTrue())

			// Check HTTPRoute B direct back reference
			Eventually(
				tests.HTTPRouteHasDirectBackReference(testClient(),
					routeBKey, rlpB.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlpB).String(),
				)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("When target is deleted", func() {
		var (
			routeName = "route-a"
		)

		It("policy status reports error", func(ctx SpecContext) {
			// Initial state
			// Route A
			// RLP A -> Route A

			// Delete route
			// RLP A

			// create httproute A
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create rlp
			rlp := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
			})
			err = k8sClient.Create(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())

			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// Check HTTPRoute A direct back reference
			routeKey := client.ObjectKey{Name: routeName, Namespace: testNamespace}
			Eventually(
				tests.HTTPRouteHasDirectBackReference(testClient(),
					routeKey, rlp.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlp).String(),
				)).WithContext(ctx).Should(BeTrue())

			// Proceed with the update:
			// Delete Route A
			err = k8sClient.Delete(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.ObjectDoesNotExist(testClient(), httpRoute)).WithContext(ctx).Should(BeTrue())

			// Check RLP status is available
			Eventually(tests.RLPIsNotAccepted(ctx, testClient(), rlpKey)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("When RLP targets already taken HTTPRoute and the route is being released", func() {
		var (
			routeAName = "route-a"
			routeBName = "route-b"
			rlpAName   = "rlp-a"
			rlpBName   = "rlp-b"
		)

		It("direct references are updated and RLP status is ready", func(ctx SpecContext) {
			// Initial state
			// Route A
			// RLP A -> Route A

			// New RLP targets already taken route
			// Route A
			// RLP A -> Route A
			// RLP B -> Route A (already taken)

			// already taken route is being released by owner policy
			// Route A
			// Route B
			// RLP A -> Route B
			// RLP B -> Route A

			// create httproute A
			httpRouteA := tests.BuildBasicHttpRoute(routeAName, TestGatewayName, testNamespace, []string{"*.a.example.com"})
			err := k8sClient.Create(ctx, httpRouteA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteA))).WithContext(ctx).Should(BeTrue())

			// create rlpA
			rlpA := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.ObjectMeta.Name = rlpAName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeAName)
			})
			err = k8sClient.Create(ctx, rlpA)
			Expect(err).ToNot(HaveOccurred())

			rlpAKey := client.ObjectKeyFromObject(rlpA)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpAKey)).WithContext(ctx).Should(BeTrue())

			// Proceed with the update:
			// new RLP B -> Route A (already taken)

			// create rlpB
			rlpB := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.ObjectMeta.Name = rlpBName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeAName)
			})
			err = k8sClient.Create(ctx, rlpB)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is not available
			rlpBKey := client.ObjectKeyFromObject(rlpB)
			Eventually(tests.RLPIsNotAccepted(ctx, testClient(), rlpBKey)).WithContext(ctx).Should(BeTrue())

			// Check HTTPRoute A direct back reference to RLP A
			routeAKey := client.ObjectKey{Name: routeAName, Namespace: testNamespace}
			Eventually(
				tests.HTTPRouteHasDirectBackReference(testClient(),
					routeAKey, rlpA.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlpA).String(),
				)).WithContext(ctx).Should(BeTrue())

			// Proceed with the update:
			// new Route B
			// RLP A -> Route B
			// RLP A was the older owner of route A, and wiil be the new owner of route B

			// create httproute B
			httpRouteB := tests.BuildBasicHttpRoute(routeBName, TestGatewayName, testNamespace, []string{"*.b.example.com"})
			err = k8sClient.Create(ctx, httpRouteB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteB))).WithContext(ctx).Should(BeTrue())

			// RLP A -> Route B
			Eventually(func(g Gomega) {
				rlpUpdated := &kuadrantv1beta2.RateLimitPolicy{}
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(rlpA), rlpUpdated)
				g.Expect(err).ToNot(HaveOccurred())
				rlpUpdated.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeBName)
				err = k8sClient.Update(ctx, rlpUpdated)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// Check HTTPRoute A direct back reference to RLP B
			Eventually(
				tests.HTTPRouteHasDirectBackReference(testClient(),
					routeAKey, rlpB.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlpB).String(),
				)).WithContext(ctx).Should(BeTrue())

			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpBKey)).WithContext(ctx).Should(BeTrue())

			routeBKey := client.ObjectKey{Name: routeBName, Namespace: testNamespace}
			// Check HTTPRoute B direct back reference to RLP A
			Eventually(
				tests.HTTPRouteHasDirectBackReference(testClient(),
					routeBKey, rlpA.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlpA).String(),
				)).WithContext(ctx).Should(BeTrue())

			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpAKey)).WithContext(ctx).Should(BeTrue())
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
			rlpGatewayA := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.ObjectMeta.Name = gatewayAName
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

			rlpGatewayB := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.ObjectMeta.Name = gatewayBName
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

			rlpTargetedRoute := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.ObjectMeta.Name = targetedRouteName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(targetedRouteName)
				policy.Spec.CommonSpec().Limits = map[string]kuadrantv1beta2.Limit{
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

			Eventually(limitadorContainsLimit(
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
