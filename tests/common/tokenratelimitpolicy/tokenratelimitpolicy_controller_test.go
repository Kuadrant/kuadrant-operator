//go:build integration

package tokenratelimitpolicy

import (
	"context"
	"fmt"
	"strings"
	"time"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("TokenRateLimitPolicy controller (Serial)", Serial, Labels{"common", "tokenratelimitpolicy"}, func() {
	const (
		testTimeOut       = NodeTimeout(2 * time.Minute)
		beforeEachTimeOut = NodeTimeout(1 * time.Minute)
		afterEachTimeOut  = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		routeName     = "toystore-route"
		trlpName      = "toystore-trlp"
		gateway       *gatewayapiv1.Gateway
	)

	policyFactory := func() *kuadrantv1alpha1.TokenRateLimitPolicy {
		return &kuadrantv1alpha1.TokenRateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "TokenRateLimitPolicy", APIVersion: kuadrantv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: trlpName, Namespace: testNamespace},
			Spec: kuadrantv1alpha1.TokenRateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "HTTPRoute",
						Name:  gatewayapiv1.ObjectName(routeName),
					},
				},
				TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
					Limits: map[string]kuadrantv1alpha1.TokenLimit{
						"l1": {
							Rates: []kuadrantv1.Rate{
								{Limit: 5000, Window: kuadrantv1.Duration("1m")},
							},
						},
					},
				},
			},
		}
	}

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)
		Expect(testClient().Create(ctx, gateway)).To(Succeed())
		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

		route := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.toystore.com"})
		Expect(testClient().Create(ctx, route)).To(Succeed())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())
	}, beforeEachTimeOut)

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	It("is not enforced when Limitador has reservations disabled", func(ctx SpecContext) {
		trlp := policyFactory()
		Expect(testClient().Create(ctx, trlp)).To(Succeed())

		trlpKey := client.ObjectKeyFromObject(trlp)
		Eventually(tests.TokenRateLimitPolicyIsAccepted(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())
		Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())

		// Limitador is a singleton owned by the suite-wide Kuadrant CR (kuadrantInstallationNS),
		// not by the per-test namespace, so restore it on cleanup to avoid poisoning other specs.
		limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
		DeferCleanup(func(ctx SpecContext) {
			limitadorObj := &limitadorv1alpha1.Limitador{}
			Expect(testClient().Get(ctx, limitadorKey, limitadorObj)).To(Succeed())
			limitadorObj.Spec.Reservations = nil
			Expect(testClient().Update(ctx, limitadorObj)).To(Succeed())
		})

		limitadorObj := &limitadorv1alpha1.Limitador{}
		Expect(testClient().Get(ctx, limitadorKey, limitadorObj)).To(Succeed())
		limitadorObj.Spec.Reservations = &limitadorv1alpha1.Reservations{Enabled: ptr.To(false)}
		Expect(testClient().Update(ctx, limitadorObj)).To(Succeed())

		Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeFalse())
		Eventually(func() bool {
			return tests.TokenRateLimitPolicyEnforcedCondition(ctx, testClient(), trlpKey, kuadrant.PolicyReasonReservationsDisabled,
				"TokenRateLimitPolicy cannot be enforced: Kuadrant spec.tokenRateLimiting.mode is Reservation, but Limitador has reservations disabled (spec.reservations.enabled=false)")
		}).WithContext(ctx).Should(BeTrue())
	}, testTimeOut)

	Context("TRLP Enforced Reasons", func() {
		const limitadorDeploymentName = "limitador-limitador"

		assertAcceptedCondTrueAndEnforcedCond := func(ctx context.Context, policy *kuadrantv1alpha1.TokenRateLimitPolicy, conditionStatus metav1.ConditionStatus, reason, message string) func(g Gomega) {
			return func(g Gomega) {
				existingPolicy := &kuadrantv1alpha1.TokenRateLimitPolicy{}
				g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(policy), existingPolicy)).To(Succeed())
				acceptedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				g.Expect(acceptedCond).ToNot(BeNil())

				acceptedCondMatch := acceptedCond.Status == metav1.ConditionTrue && acceptedCond.Reason == string(gatewayapiv1alpha2.PolicyReasonAccepted)

				enforcedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(kuadrant.PolicyReasonEnforced))
				g.Expect(enforcedCond).ToNot(BeNil())
				enforcedCondMatch := enforcedCond.Status == conditionStatus && enforcedCond.Reason == reason && enforcedCond.Message == message

				g.Expect(acceptedCondMatch && enforcedCondMatch).To(BeTrue())
			}
		}

		It("Enforced Reason", func(ctx SpecContext) {
			policy := policyFactory()
			Expect(testClient().Create(ctx, policy)).To(Succeed())

			Eventually(assertAcceptedCondTrueAndEnforcedCond(ctx, policy, metav1.ConditionTrue, string(kuadrant.PolicyReasonEnforced),
				"TokenRateLimitPolicy has been successfully enforced")).WithContext(ctx).Should(Succeed())

			// Remove limitador deployment to simulate enforcement error
			// TRLP should transition to enforcement false in this case
			limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
			Expect(client.IgnoreNotFound(testClient().Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: limitadorDeploymentName, Namespace: kuadrantInstallationNS}}))).To(Succeed())
			// Limitador is a singleton shared across specs - make sure it is back to Ready
			// before this spec finishes, so the next spec deleting it again doesn't race a NotFound.
			DeferCleanup(func(ctx SpecContext) {
				Eventually(tests.LimitadorIsReady(testClient(), limitadorKey)).WithContext(ctx).Should(Succeed())
			})

			Eventually(assertAcceptedCondTrueAndEnforcedCond(ctx, policy, metav1.ConditionFalse, string(kuadrant.PolicyReasonUnknown),
				"TokenRateLimitPolicy waiting for the following components to sync: [Limitador]")).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Unknown Reason", func(ctx SpecContext) {
			// Remove limitador deployment to simulate enforcement error
			Expect(client.IgnoreNotFound(testClient().Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: limitadorDeploymentName, Namespace: kuadrantInstallationNS}}))).To(Succeed())

			// Enforced false as limitador is not ready
			policy := policyFactory()
			Expect(testClient().Create(ctx, policy)).To(Succeed())
			Eventually(assertAcceptedCondTrueAndEnforcedCond(ctx, policy, metav1.ConditionFalse, string(kuadrant.PolicyReasonUnknown),
				"TokenRateLimitPolicy waiting for the following components to sync: [Limitador]")).WithContext(ctx).Should(Succeed())

			// Enforced true once limitador is ready
			Eventually(assertAcceptedCondTrueAndEnforcedCond(ctx, policy, metav1.ConditionTrue, string(kuadrant.PolicyReasonEnforced),
				"TokenRateLimitPolicy has been successfully enforced")).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})

var _ = Describe("TokenRateLimitPolicy controller", Labels{"common", "tokenratelimitpolicy"}, func() {
	const (
		testTimeOut       = NodeTimeout(2 * time.Minute)
		beforeEachTimeOut = NodeTimeout(1 * time.Minute)
		afterEachTimeOut  = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		routeName     = "toystore-route"
		trlpName      = "toystore-trlp"
		gateway       *gatewayapiv1.Gateway
	)

	policyFactory := func(mutateFns ...func(policy *kuadrantv1alpha1.TokenRateLimitPolicy)) *kuadrantv1alpha1.TokenRateLimitPolicy {
		policy := &kuadrantv1alpha1.TokenRateLimitPolicy{
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
						Name:  gatewayapiv1.ObjectName(routeName),
					},
				},
				Defaults: &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
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
			},
		}
		for _, mutateFn := range mutateFns {
			mutateFn(policy)
		}

		return policy
	}

	assertPolicyIsAcceptedAndEnforced := func(ctx context.Context, key client.ObjectKey) func() bool {
		return func() bool {
			return tests.TokenRateLimitPolicyIsAccepted(ctx, testClient(), key)() && tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), key)()
		}
	}

	assertPolicyIsAcceptedAndNotEnforced := func(ctx context.Context, key client.ObjectKey) func() bool {
		return func() bool {
			return tests.TokenRateLimitPolicyIsAccepted(ctx, testClient(), key)() && !tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), key)()
		}
	}

	limitadorContainsLimit := func(ctx context.Context, limits ...limitadorv1alpha1.RateLimit) func(g Gomega) {
		return func(g Gomega) {
			limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
			existingLimitador := &limitadorv1alpha1.Limitador{}
			g.Expect(testClient().Get(ctx, limitadorKey, existingLimitador)).To(Succeed())
			for i := range limits {
				limit := limits[i]
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limit))
			}
		}
	}

	beforeEachCallback := func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)

		Expect(testClient().Create(ctx, gateway)).To(Succeed())
		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
	}

	BeforeEach(beforeEachCallback, beforeEachTimeOut)
	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("TRLP targeting HTTPRoute", func() {
		It("Creates all the resources for a basic HTTPRoute and TokenRateLimitPolicy", func(ctx SpecContext) {
			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			err := testClient().Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create tokenratelimitpolicy
			trlp := policyFactory()
			err = testClient().Create(ctx, trlp)
			Expect(err).ToNot(HaveOccurred())

			// Check TRLP status is available
			trlpKey := client.ObjectKeyFromObject(trlp)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, trlpKey)).WithContext(ctx).Should(BeTrue())

			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = testClient().Get(ctx, limitadorKey, existingLimitador)
				// must exist
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					Name:       "l1",
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  string(controllers.LimitsNamespaceFromRoute(httpRoute)),
					Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, controllers.TokenLimitNameToLimitadorIdentifier(trlpKey, "l1"))},
					Variables:  []string{},
				}))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("TRLP targeting Gateway", func() {
		It("Creates all the resources for a basic Gateway and TokenRateLimitPolicy", func(ctx SpecContext) {
			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			err := testClient().Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

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
							Group: gatewayapiv1.Group("gateway.networking.k8s.io"),
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					Defaults: &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
						TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
							Limits: map[string]kuadrantv1alpha1.TokenLimit{
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
				},
			}
			err = testClient().Create(ctx, trlp)
			Expect(err).ToNot(HaveOccurred())

			// Check TRLP status is available
			trlpKey := client.ObjectKey{Name: trlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, trlpKey)).WithContext(ctx).Should(BeTrue())

			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1.Gateway{}
			Eventually(func(g Gomega) {
				err = testClient().Get(ctx, gwKey, existingGateway)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = testClient().Get(ctx, limitadorKey, existingLimitador)
				// must exist
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					Name:       "l1",
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  string(controllers.LimitsNamespaceFromRoute(httpRoute)),
					Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, controllers.TokenLimitNameToLimitadorIdentifier(trlpKey, "l1"))},
					Variables:  []string{},
				}))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Creates all the resources for a basic Gateway and TokenRateLimitPolicy when missing a HTTPRoute attached to the Gateway", func(ctx SpecContext) {
			// create tokenratelimitpolicy
			trlp := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
			})
			err := testClient().Create(ctx, trlp)
			Expect(err).ToNot(HaveOccurred())

			// Check TRLP status is available
			trlpKey := client.ObjectKey{Name: trlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, trlpKey)).WithContext(ctx).Should(BeTrue())
			Eventually(func() bool {
				return tests.TokenRateLimitPolicyEnforcedCondition(ctx, testClient(), trlpKey, kuadrant.PolicyReasonUnknown, "TokenRateLimitPolicy is not in the path to any existing routes")
			}).WithContext(ctx).Should(BeTrue())

			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = testClient().Get(ctx, limitadorKey, existingLimitador)
				// must exist
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(lo.Filter(existingLimitador.Spec.Limits, func(l limitadorv1alpha1.RateLimit, _ int) bool { // a hack to isolate test namespaces sharing the same limitador cr
					return strings.HasPrefix(l.Namespace, fmt.Sprintf("%s/", testNamespace))
				})).To(BeEmpty())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("TRLP Defaults", func() {
		Describe("Route policy defaults taking precedence over Gateway policy defaults", func() {
			var (
				gwTRLP    *kuadrantv1alpha1.TokenRateLimitPolicy
				routeTRLP *kuadrantv1alpha1.TokenRateLimitPolicy
			)

			BeforeEach(func(ctx SpecContext) {
				// Common setup
				// GW policy defaults are overridden and not enforced when Route has their own policy attached

				// create httproute
				httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
				Expect(testClient().Create(ctx, httpRoute)).To(Succeed())
				Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

				// create GW TRLP
				gwTRLP = policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
					policy.Spec.TargetRef.Kind = "Gateway"
					policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				})
				Expect(testClient().Create(ctx, gwTRLP)).To(Succeed())
				gwTRLPKey := client.ObjectKey{Name: gwTRLP.Name, Namespace: testNamespace}
				Eventually(assertPolicyIsAcceptedAndEnforced(ctx, gwTRLPKey)).WithContext(ctx).Should(BeTrue())

				// Create HTTPRoute TRLP with new default limits
				routeTRLP = policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
					policy.Name = "httproute-trlp"
					policy.Spec.Proper().Limits = map[string]kuadrantv1alpha1.TokenLimit{
						"l1": {
							Rates: []kuadrantv1.Rate{
								{
									Limit: 10, Window: kuadrantv1.Duration("5s"),
								},
							},
						},
					}
				})
				Expect(testClient().Create(ctx, routeTRLP)).To(Succeed())
				routeTRLPKey := client.ObjectKey{Name: routeTRLP.Name, Namespace: testNamespace}
				Eventually(assertPolicyIsAcceptedAndEnforced(ctx, routeTRLPKey)).WithContext(ctx).Should(BeTrue())
				Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), gwTRLPKey)).WithContext(ctx).Should(BeFalse())

				// check limits
				Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
					Name:       "l1",
					MaxValue:   10,
					Seconds:    5,
					Namespace:  string(controllers.LimitsNamespaceFromRoute(httpRoute)),
					Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, controllers.TokenLimitNameToLimitadorIdentifier(routeTRLPKey, "l1"))},
					Variables:  []string{},
				})).WithContext(ctx).Should(Succeed())
			}, beforeEachTimeOut)

			When("Free route is created", func() {
				It("Gateway policy should now be enforced", func(ctx SpecContext) {
					route2 := tests.BuildBasicHttpRoute("route2", TestGatewayName, testNamespace, []string{"*.car.com"})
					Expect(testClient().Create(ctx, route2)).To(Succeed())
					Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), client.ObjectKeyFromObject(gwTRLP))).WithContext(ctx).Should(BeTrue())
				}, testTimeOut)
			})

			When("Route policy is deleted", func() {
				It("Gateway policy should now be enforced", func(ctx SpecContext) {
					Expect(testClient().Delete(ctx, routeTRLP)).To(Succeed())
					Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), client.ObjectKeyFromObject(gwTRLP))).WithContext(ctx).Should(BeTrue())
				}, testTimeOut)
			})
		})

		It("Explicit defaults - no underlying routes to enforce policy", func(ctx SpecContext) {
			gwTRLP := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
			})

			Expect(testClient().Create(ctx, gwTRLP)).To(Succeed())
			gwTRLPKey := client.ObjectKey{Name: gwTRLP.Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, gwTRLPKey)).WithContext(ctx).Should(BeTrue())
			Eventually(func() bool {
				return tests.TokenRateLimitPolicyEnforcedCondition(ctx, testClient(), gwTRLPKey, kuadrant.PolicyReasonUnknown, "TokenRateLimitPolicy is not in the path to any existing routes")
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Implicit defaults - no underlying routes to enforce policy", func(ctx SpecContext) {
			gwTRLP := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				policy.Spec.TokenRateLimitPolicySpecProper = *policy.Spec.Defaults.TokenRateLimitPolicySpecProper.DeepCopy()
				policy.Spec.Defaults = nil
			})

			Expect(testClient().Create(ctx, gwTRLP)).To(Succeed())
			gwTRLPKey := client.ObjectKey{Name: gwTRLP.Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, gwTRLPKey)).WithContext(ctx).Should(BeTrue())
			Eventually(func() bool {
				return tests.TokenRateLimitPolicyEnforcedCondition(ctx, testClient(), gwTRLPKey, kuadrant.PolicyReasonUnknown, "TokenRateLimitPolicy is not in the path to any existing routes")
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("TRLP Overrides", func() {
		var httpRoute *gatewayapiv1.HTTPRoute
		var gwTRLP *kuadrantv1alpha1.TokenRateLimitPolicy
		var routeTRLP *kuadrantv1alpha1.TokenRateLimitPolicy

		BeforeEach(func(ctx SpecContext) {
			// create httproute
			httpRoute = tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			Expect(testClient().Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			gwTRLP = policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				policy.Spec.Overrides = policy.Spec.Defaults.DeepCopy()
				policy.Spec.Defaults = nil
			})

			routeTRLP = policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Name = "httproute-trlp"
				policy.Spec.Proper().Limits = map[string]kuadrantv1alpha1.TokenLimit{
					"route": {
						Rates: []kuadrantv1.Rate{
							{
								Limit: 10, Window: kuadrantv1.Duration("5s"),
							},
						},
					},
				}
			})
		}, beforeEachTimeOut)

		It("Gateway atomic override - gateway overrides exist and then route policy created", func(ctx SpecContext) {
			// create GW TRLP with overrides
			Expect(testClient().Create(ctx, gwTRLP)).To(Succeed())
			gwTRLPKey := client.ObjectKeyFromObject(gwTRLP)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, gwTRLPKey)).WithContext(ctx).Should(BeTrue())

			// Create HTTPRoute TRLP
			Expect(testClient().Create(ctx, routeTRLP)).To(Succeed())
			routeTRLPKey := client.ObjectKeyFromObject(routeTRLP)
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, routeTRLPKey)).WithContext(ctx).Should(BeTrue())
			Eventually(func() bool {
				return tests.TokenRateLimitPolicyEnforcedCondition(ctx, testClient(), routeTRLPKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("TokenRateLimitPolicy is overridden by [%s]", gwTRLPKey))
			}).WithContext(ctx).Should(BeTrue())

			limitsNamespace := controllers.LimitsNamespaceFromRoute(httpRoute)

			// check limits - should contain override values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				Name:       "l1",
				MaxValue:   1,
				Seconds:    180,
				Namespace:  string(limitsNamespace),
				Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, controllers.TokenLimitNameToLimitadorIdentifier(gwTRLPKey, "l1"))},
				Variables:  []string{},
			})).WithContext(ctx).Should(Succeed())

			// Delete GW TRLP -> Route TRLP should be enforced
			Expect(testClient().Delete(ctx, gwTRLP)).To(Succeed())
			Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), routeTRLPKey)).WithContext(ctx).Should(BeTrue())
			// check limits - should be route TRLP values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				Name:       "route",
				MaxValue:   10,
				Seconds:    5,
				Namespace:  string(limitsNamespace),
				Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, controllers.TokenLimitNameToLimitadorIdentifier(routeTRLPKey, "route"))},
				Variables:  []string{},
			})).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Gateway atomic override - route policy exits and then gateway policy created", func(ctx SpecContext) {
			// Create Route TRLP
			Expect(testClient().Create(ctx, routeTRLP)).To(Succeed())
			routeTRLPKey := client.ObjectKeyFromObject(routeTRLP)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, routeTRLPKey)).WithContext(ctx).Should(BeTrue())

			// create GW TRLP with override
			Expect(testClient().Create(ctx, gwTRLP)).To(Succeed())
			gwTRLPKey := client.ObjectKeyFromObject(gwTRLP)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, gwTRLPKey)).WithContext(ctx).Should(BeTrue())

			// Route TRLP should no longer be enforced
			Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), routeTRLPKey)).WithContext(ctx).Should(BeFalse())
			Eventually(func() bool {
				return tests.TokenRateLimitPolicyEnforcedCondition(ctx, testClient(), routeTRLPKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("TokenRateLimitPolicy is overridden by [%s]", gwTRLPKey))
			}).WithContext(ctx).Should(BeTrue())

			// Should contain override values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				Name:       "l1",
				MaxValue:   1,
				Seconds:    180,
				Namespace:  string(controllers.LimitsNamespaceFromRoute(httpRoute)),
				Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, controllers.TokenLimitNameToLimitadorIdentifier(gwTRLPKey, "l1"))},
				Variables:  []string{},
			})).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Gateway atomic override - gateway defaults turned into overrides later on", func(ctx SpecContext) {
			// Create Route TRLP
			Expect(testClient().Create(ctx, routeTRLP)).To(Succeed())
			routeTRLPKey := client.ObjectKeyFromObject(routeTRLP)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, routeTRLPKey)).WithContext(ctx).Should(BeTrue())

			// Create GW TRLP with defaults
			gwTRLP = policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
			})
			Expect(testClient().Create(ctx, gwTRLP)).To(Succeed())
			gwTRLPKey := client.ObjectKeyFromObject(gwTRLP)
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, gwTRLPKey)).WithContext(ctx).Should(BeTrue())
			Eventually(func() bool {
				return tests.TokenRateLimitPolicyEnforcedCondition(ctx, testClient(), gwTRLPKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("TokenRateLimitPolicy is overridden by [%s]", routeTRLPKey))
			}).WithContext(ctx).Should(BeTrue())

			// Route TRLP should still be enforced
			Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), routeTRLPKey)).WithContext(ctx).Should(BeTrue())

			limitsNamespace := controllers.LimitsNamespaceFromRoute(httpRoute)

			// Should contain Route TRLP values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				Name:       "route",
				MaxValue:   10,
				Seconds:    5,
				Namespace:  string(limitsNamespace),
				Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, controllers.TokenLimitNameToLimitadorIdentifier(routeTRLPKey, "route"))},
				Variables:  []string{},
			})).WithContext(ctx).Should(Succeed())

			// Update GW TRLP defaults to overrides
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, gwTRLPKey, gwTRLP)).To(Succeed())
				gwTRLP.Spec.Overrides = gwTRLP.Spec.Defaults.DeepCopy()
				gwTRLP.Spec.Defaults = nil
				g.Expect(testClient().Update(ctx, gwTRLP)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			// GW TRLP should now be enforced
			Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), routeTRLPKey)).WithContext(ctx).Should(BeFalse())
			Eventually(func() bool {
				return tests.TokenRateLimitPolicyEnforcedCondition(ctx, testClient(), routeTRLPKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("TokenRateLimitPolicy is overridden by [%s]", gwTRLPKey))
			}).WithContext(ctx).Should(BeTrue())
			Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), gwTRLPKey)).WithContext(ctx).Should(BeTrue())

			// Should contain override values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				Name:       "l1",
				MaxValue:   1,
				Seconds:    180,
				Namespace:  string(limitsNamespace),
				Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, controllers.TokenLimitNameToLimitadorIdentifier(gwTRLPKey, "l1"))},
				Variables:  []string{},
			})).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Gateway atomic override - gateway overrides turned into defaults later on", func(ctx SpecContext) {
			// Create HTTPRoute TRLP
			Expect(testClient().Create(ctx, routeTRLP)).To(Succeed())
			routeTRLPKey := client.ObjectKeyFromObject(routeTRLP)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, routeTRLPKey)).WithContext(ctx).Should(BeTrue())

			// create GW TRLP with overrides
			Expect(testClient().Create(ctx, gwTRLP)).To(Succeed())
			gwTRLPKey := client.ObjectKey{Name: gwTRLP.Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, gwTRLPKey)).WithContext(ctx).Should(BeTrue())

			// Route TRLP should not be enforced
			Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), routeTRLPKey)).WithContext(ctx).Should(BeFalse())
			Eventually(func() bool {
				return tests.TokenRateLimitPolicyEnforcedCondition(ctx, testClient(), routeTRLPKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("TokenRateLimitPolicy is overridden by [%s]", gwTRLPKey))
			}).WithContext(ctx).Should(BeTrue())

			limitsNamespace := controllers.LimitsNamespaceFromRoute(httpRoute)

			// Should contain override values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				Name:       "l1",
				MaxValue:   1,
				Seconds:    180,
				Namespace:  string(limitsNamespace),
				Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, controllers.TokenLimitNameToLimitadorIdentifier(gwTRLPKey, "l1"))},
				Variables:  []string{},
			})).WithContext(ctx).Should(Succeed())

			// Update GW TRLP overrides to defaults
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, gwTRLPKey, gwTRLP)).To(Succeed())
				gwTRLP.Spec.Defaults = gwTRLP.Spec.Overrides.DeepCopy()
				gwTRLP.Spec.Overrides = nil
				g.Expect(testClient().Update(ctx, gwTRLP)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			// Route TRLP now takes precedence
			Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), gwTRLPKey)).WithContext(ctx).Should(BeFalse())
			Eventually(func() bool {
				return tests.TokenRateLimitPolicyEnforcedCondition(ctx, testClient(), gwTRLPKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("TokenRateLimitPolicy is overridden by [%s]", routeTRLPKey))
			}).WithContext(ctx).Should(BeTrue())
			Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), routeTRLPKey)).WithContext(ctx).Should(BeTrue())

			// Should contain Route TRLP values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				Name:       "route",
				MaxValue:   10,
				Seconds:    5,
				Namespace:  string(limitsNamespace),
				Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, controllers.TokenLimitNameToLimitadorIdentifier(routeTRLPKey, "route"))},
				Variables:  []string{},
			})).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Gateway atomic override - no underlying routes to enforce policy", func(ctx SpecContext) {
			// Delete HTTPRoute
			Expect(testClient().Delete(ctx, httpRoute)).To(Succeed())

			Eventually(func() bool {
				route := &gatewayapiv1.HTTPRoute{}
				err := testClient().Get(ctx, client.ObjectKeyFromObject(httpRoute), route)
				// Either deleted OR has deletionTimestamp
				return apierrors.IsNotFound(err) ||
					(err == nil && route.GetDeletionTimestamp() != nil)
			}).WithContext(ctx).WithTimeout(5 * time.Second).Should(BeTrue())

			// create GW TRLP with overrides
			Expect(testClient().Create(ctx, gwTRLP)).To(Succeed())
			gwTRLPKey := client.ObjectKey{Name: gwTRLP.Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, gwTRLPKey)).WithContext(ctx).Should(BeTrue())
			Eventually(func() bool {
				return tests.TokenRateLimitPolicyEnforcedCondition(ctx, testClient(), gwTRLPKey, kuadrant.PolicyReasonUnknown, "TokenRateLimitPolicy is not in the path to any existing routes")
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("TRLP accepted condition reasons", func() {
		assertAcceptedConditionTrue := func(trlp *kuadrantv1alpha1.TokenRateLimitPolicy) func() bool {
			return func() bool {
				trlpKey := client.ObjectKeyFromObject(trlp)
				existingTRLP := &kuadrantv1alpha1.TokenRateLimitPolicy{}
				err := testClient().Get(context.Background(), trlpKey, existingTRLP)
				if err != nil {
					return false
				}

				return meta.IsStatusConditionTrue(existingTRLP.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
			}
		}

		assertAcceptedConditionFalse := func(ctx context.Context, trlp *kuadrantv1alpha1.TokenRateLimitPolicy, reason, message string) func(g Gomega) {
			return func(g Gomega) {
				trlpKey := client.ObjectKeyFromObject(trlp)
				existingTRLP := &kuadrantv1alpha1.TokenRateLimitPolicy{}
				g.Expect(testClient().Get(ctx, trlpKey, existingTRLP)).To(Succeed())

				cond := meta.FindStatusCondition(existingTRLP.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status == metav1.ConditionFalse && cond.Reason == reason && cond.Message == message).To(BeTrue())
			}
		}

		// Accepted reason is already tested generally by the existing tests

		It("Target not found reason", func(ctx SpecContext) {
			trlp := policyFactory()
			Expect(testClient().Create(ctx, trlp)).To(Succeed())

			Eventually(assertAcceptedConditionFalse(ctx, trlp, string(gatewayapiv1alpha2.PolicyReasonTargetNotFound),
				fmt.Sprintf("TokenRateLimitPolicy target %s was not found", routeName)),
			).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Multiple policies can target a same resource", func(ctx SpecContext) {
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			Expect(testClient().Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			trlp := policyFactory()
			Expect(testClient().Create(ctx, trlp)).To(Succeed())
			Eventually(tests.TokenRateLimitPolicyIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(trlp))).WithContext(ctx).Should(BeTrue())

			Eventually(assertAcceptedConditionTrue(trlp), time.Minute, 5*time.Second).Should(BeTrue())

			trlp2 := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Name = "conflicting-trlp"
			})
			Expect(testClient().Create(ctx, trlp2)).To(Succeed())

			Eventually(assertAcceptedConditionTrue(trlp), time.Minute, 5*time.Second).Should(BeTrue())
			Eventually(assertAcceptedConditionTrue(trlp2), time.Minute, 5*time.Second).Should(BeTrue())
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
			err := testClient().Create(ctx, gatewayA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gatewayA)).WithContext(ctx).Should(BeTrue())

			gatewayB = tests.BuildBasicGateway(gatewayBName, testNamespace, func(g *gatewayapiv1.Gateway) {
				g.Spec.Listeners[0].Hostname = ptr.To(gatewayapiv1.Hostname("*.b.example.com"))
			})
			err = testClient().Create(ctx, gatewayB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gatewayB)).WithContext(ctx).Should(BeTrue())

			gatewayParentsFunc := func(r *gatewayapiv1.HTTPRoute) {
				r.Spec.ParentRefs = []gatewayapiv1.ParentReference{
					{Name: gatewayapiv1.ObjectName(gatewayAName), Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace))},
					{Name: gatewayapiv1.ObjectName(gatewayBName), Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace))},
				}
			}

			targetedRoute = tests.BuildBasicHttpRoute(targetedRouteName, gatewayAName, testNamespace, []string{"targeted.a.example.com", "targeted.b.example.com"}, gatewayParentsFunc)
			err = testClient().Create(ctx, targetedRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(targetedRoute))).WithContext(ctx).Should(BeTrue())

			untargetedRoute = tests.BuildBasicHttpRoute(untargetedRouteName, gatewayAName, testNamespace, []string{"untargeted.a.example.com", "untargeted.b.example.com"}, gatewayParentsFunc)
			err = testClient().Create(ctx, untargetedRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(untargetedRoute))).WithContext(ctx).Should(BeTrue())
		}, beforeEachTimeOut)

		It("It defines route policy limits with gateway policy overrides", func(ctx SpecContext) {
			trlpGatewayA := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.ObjectMeta.Name = gatewayAName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gatewayAName)
				policy.Spec.Defaults = nil
				policy.Spec.Overrides = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"gw-a-1000rps": {
								Rates: []kuadrantv1.Rate{
									{
										Limit: 1000, Window: kuadrantv1.Duration("1s"),
									},
								},
							},
						},
					},
				}
			})
			err := testClient().Create(ctx, trlpGatewayA)
			Expect(err).ToNot(HaveOccurred())

			trlpGatewayB := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.ObjectMeta.Name = gatewayBName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gatewayBName)
				policy.Spec.Defaults = nil
				policy.Spec.Overrides = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"gw-b-100rps": {
								Rates: []kuadrantv1.Rate{
									{
										Limit: 100, Window: kuadrantv1.Duration("1s"),
									},
								},
							},
						},
					},
				}
			})
			err = testClient().Create(ctx, trlpGatewayB)
			Expect(err).ToNot(HaveOccurred())

			trlpTargetedRoute := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.ObjectMeta.Name = targetedRouteName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(targetedRouteName)
				policy.Spec.Proper().Limits = map[string]kuadrantv1alpha1.TokenLimit{
					"route-10rps": {
						Rates: []kuadrantv1.Rate{
							{
								Limit: 10, Window: kuadrantv1.Duration("1s"),
							},
						},
					},
				}
			})
			err = testClient().Create(ctx, trlpTargetedRoute)
			Expect(err).ToNot(HaveOccurred())

			limitIdentifierGwA := controllers.TokenLimitNameToLimitadorIdentifier(client.ObjectKeyFromObject(trlpGatewayA), "gw-a-1000rps")
			limitIdentifierGwB := controllers.TokenLimitNameToLimitadorIdentifier(client.ObjectKeyFromObject(trlpGatewayB), "gw-b-100rps")

			Eventually(limitadorContainsLimit(
				ctx,
				limitadorv1alpha1.RateLimit{
					Name:       "gw-a-1000rps",
					MaxValue:   1000,
					Seconds:    1,
					Namespace:  string(controllers.LimitsNamespaceFromRoute(targetedRoute)),
					Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, limitIdentifierGwA)},
					Variables:  []string{},
				},
				limitadorv1alpha1.RateLimit{
					Name:       "gw-b-100rps",
					MaxValue:   100,
					Seconds:    1,
					Namespace:  string(controllers.LimitsNamespaceFromRoute(targetedRoute)),
					Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, limitIdentifierGwB)},
					Variables:  []string{},
				},
				limitadorv1alpha1.RateLimit{ // FIXME(@guicassolato): we need to create one limit definition per gateway × route combination, not one per gateway × policy combination
					Name:       "gw-a-1000rps",
					MaxValue:   1000,
					Seconds:    1,
					Namespace:  string(controllers.LimitsNamespaceFromRoute(untargetedRoute)),
					Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, limitIdentifierGwA)},
					Variables:  []string{},
				},
				limitadorv1alpha1.RateLimit{
					Name:       "gw-b-100rps",
					MaxValue:   100,
					Seconds:    1,
					Namespace:  string(controllers.LimitsNamespaceFromRoute(untargetedRoute)),
					Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, limitIdentifierGwB)},
					Variables:  []string{},
				},
			)).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})

var _ = Describe("TokenRateLimitPolicy CEL Validations", Labels{"common", "tokenratelimitpolicy"}, func() {
	const (
		testTimeOut       = NodeTimeout(2 * time.Minute)
		beforeEachTimeOut = NodeTimeout(1 * time.Minute)
		afterEachTimeOut  = NodeTimeout(3 * time.Minute)
	)

	var testNamespace string

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	}, beforeEachTimeOut)

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	policyFactory := func(mutateFns ...func(policy *kuadrantv1alpha1.TokenRateLimitPolicy)) *kuadrantv1alpha1.TokenRateLimitPolicy {
		policy := &kuadrantv1alpha1.TokenRateLimitPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-policy",
				Namespace: testNamespace,
			},
			Spec: kuadrantv1alpha1.TokenRateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "HTTPRoute",
						Name:  "my-target",
					},
				},
			},
		}
		for _, mutateFn := range mutateFns {
			mutateFn(policy)
		}

		return policy
	}

	Context("Spec TargetRef Validations", func() {
		It("Valid policy targeting HTTPRoute", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.Limits = map[string]kuadrantv1alpha1.TokenLimit{
					"implicit": {
						Rates: []kuadrantv1.Rate{{Limit: 2, Window: kuadrantv1.Duration("20s")}},
					},
				}
			})
			err := testClient().Create(ctx, policy)
			Expect(err).To(BeNil())
		}, testTimeOut)

		It("Valid policy targeting Gateway", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.Limits = map[string]kuadrantv1alpha1.TokenLimit{
					"implicit": {
						Rates: []kuadrantv1.Rate{{Limit: 2, Window: kuadrantv1.Duration("20s")}},
					},
				}
			})
			err := testClient().Create(ctx, policy)
			Expect(err).To(BeNil())
		}, testTimeOut)

		It("Invalid Target Ref Group", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.TargetRef.Group = "not-gateway.networking.k8s.io"
			})
			err := testClient().Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'")).To(BeTrue())
		}, testTimeOut)

		It("Invalid Target Ref Kind", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "GRPCRoute"
			})
			err := testClient().Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'")).To(BeTrue())
		}, testTimeOut)
	})

	Context("Limits missing from configuration", func() {
		It("Missing limits object", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.Limits = nil
			})
			err := testClient().Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.limits must be defined")).To(BeTrue())
		}, testTimeOut)

		It("Empty limits object created", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.Limits = map[string]kuadrantv1alpha1.TokenLimit{}
			})
			err := testClient().Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.limits must be defined")).To(BeTrue())
		}, testTimeOut)

		It("Missing defaults.limits object", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.Limits = nil
				policy.Spec.Defaults = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: nil,
					},
				}
			})
			err := testClient().Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.defaults.limits must be defined")).To(BeTrue())
		}, testTimeOut)

		It("Empty defaults.limits object created", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.Limits = nil
				policy.Spec.Defaults = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{},
					},
				}
			})
			err := testClient().Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.defaults.limits must be defined")).To(BeTrue())
		}, testTimeOut)

		It("Missing overrides.limits object", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.Limits = nil
				policy.Spec.Overrides = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: nil,
					},
				}
			})
			err := testClient().Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.overrides.limits must be defined")).To(BeTrue())
		}, testTimeOut)

		It("Empty overrides.limits object created", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.Limits = nil
				policy.Spec.Overrides = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{},
					},
				}
			})
			err := testClient().Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.overrides.limits must be defined")).To(BeTrue())
		}, testTimeOut)
	})

	Context("Defaults / Override validation", func() {
		It("Valid - only implicit defaults defined", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.Limits = map[string]kuadrantv1alpha1.TokenLimit{
					"implicit": {
						Rates: []kuadrantv1.Rate{{Limit: 2, Window: kuadrantv1.Duration("20s")}},
					},
				}
			})
			Expect(testClient().Create(ctx, policy)).To(Succeed())
		}, testTimeOut)

		It("Valid - only explicit defaults defined", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.Defaults = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"explicit": {
								Rates: []kuadrantv1.Rate{{Limit: 1, Window: kuadrantv1.Duration("10s")}},
							},
						},
					},
				}
			})
			Expect(testClient().Create(ctx, policy)).To(Succeed())
		}, testTimeOut)

		It("Invalid - implicit and explicit defaults are mutually exclusive", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.Defaults = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"explicit": {
								Rates: []kuadrantv1.Rate{{Limit: 1, Window: kuadrantv1.Duration("10s")}},
							},
						},
					},
				}
				policy.Spec.Limits = map[string]kuadrantv1alpha1.TokenLimit{
					"implicit": {
						Rates: []kuadrantv1.Rate{{Limit: 2, Window: kuadrantv1.Duration("20s")}},
					},
				}
			})
			err := testClient().Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("Implicit and explicit defaults are mutually exclusive"))
		}, testTimeOut)

		It("Invalid - explicit default and override defined", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.Defaults = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"implicit": {
								Rates: []kuadrantv1.Rate{{Limit: 2, Window: kuadrantv1.Duration("20s")}},
							},
						},
					},
				}
				policy.Spec.Overrides = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"explicit": {
								Rates: []kuadrantv1.Rate{{Limit: 1, Window: kuadrantv1.Duration("10s")}},
							},
						},
					},
				}
			})
			err := testClient().Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("Overrides and explicit defaults are mutually exclusive"))
		}, testTimeOut)

		It("Invalid - implicit default and override defined", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.Limits = map[string]kuadrantv1alpha1.TokenLimit{
					"implicit": {
						Rates: []kuadrantv1.Rate{{Limit: 2, Window: kuadrantv1.Duration("20s")}},
					},
				}
				policy.Spec.Overrides = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"overrides": {
								Rates: []kuadrantv1.Rate{{Limit: 1, Window: kuadrantv1.Duration("10s")}},
							},
						},
					},
				}
			})
			err := testClient().Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("Overrides and implicit defaults are mutually exclusive"))
		}, testTimeOut)

		It("Valid - policy override targeting Gateway", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.Overrides = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"override": {
								Rates: []kuadrantv1.Rate{{Limit: 1, Window: kuadrantv1.Duration("10s")}},
							},
						},
					},
				}
			})
			Expect(testClient().Create(ctx, policy)).To(Succeed())
		}, testTimeOut)
	})

	Context("DataExtraction / Defaults / Override validation", func() {
		It("Invalid - implicit dataExtraction and explicit defaults are mutually exclusive", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.DataExtraction = &kuadrantv1alpha1.DataExtraction{
					Response: kuadrantv1alpha1.ResponseDataExtraction{
						kuadrantv1alpha1.ResponseDataExtractionKeyTotalTokens: []string{"/usage/total_tokens"},
					},
				}
				policy.Spec.Defaults = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"explicit": {
								Rates: []kuadrantv1.Rate{{Limit: 1, Window: kuadrantv1.Duration("10s")}},
							},
						},
					},
				}
			})
			err := testClient().Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("Implicit dataExtraction and explicit defaults are mutually exclusive"))
		}, testTimeOut)

		It("Invalid - implicit dataExtraction and overrides are mutually exclusive", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.DataExtraction = &kuadrantv1alpha1.DataExtraction{
					Response: kuadrantv1alpha1.ResponseDataExtraction{
						kuadrantv1alpha1.ResponseDataExtractionKeyTotalTokens: []string{"/usage/total_tokens"},
					},
				}
				policy.Spec.Overrides = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"override": {
								Rates: []kuadrantv1.Rate{{Limit: 1, Window: kuadrantv1.Duration("10s")}},
							},
						},
					},
				}
			})
			err := testClient().Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("Implicit dataExtraction and overrides are mutually exclusive"))
		}, testTimeOut)

		It("Valid - dataExtraction defined alongside implicit defaults", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.DataExtraction = &kuadrantv1alpha1.DataExtraction{
					Response: kuadrantv1alpha1.ResponseDataExtraction{
						kuadrantv1alpha1.ResponseDataExtractionKeyTotalTokens: []string{"/usage/total_tokens"},
					},
				}
				policy.Spec.Limits = map[string]kuadrantv1alpha1.TokenLimit{
					"implicit": {
						Rates: []kuadrantv1.Rate{{Limit: 2, Window: kuadrantv1.Duration("20s")}},
					},
				}
			})
			Expect(testClient().Create(ctx, policy)).To(Succeed())
		}, testTimeOut)

		It("Valid - dataExtraction defined inside the defaults block", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) {
				policy.Spec.Defaults = &kuadrantv1alpha1.MergeableTokenRateLimitPolicySpec{
					TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
						DataExtraction: &kuadrantv1alpha1.DataExtraction{
							Response: kuadrantv1alpha1.ResponseDataExtraction{
								kuadrantv1alpha1.ResponseDataExtractionKeyTotalTokens: []string{"/usage/total_tokens"},
							},
						},
						Limits: map[string]kuadrantv1alpha1.TokenLimit{
							"explicit": {
								Rates: []kuadrantv1.Rate{{Limit: 1, Window: kuadrantv1.Duration("10s")}},
							},
						},
					},
				}
			})
			Expect(testClient().Create(ctx, policy)).To(Succeed())
		}, testTimeOut)
	})
})
