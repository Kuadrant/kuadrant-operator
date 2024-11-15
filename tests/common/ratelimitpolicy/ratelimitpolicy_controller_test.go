//go:build integration

package ratelimitpolicy

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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/controllers"
	"github.com/kuadrant/kuadrant-operator/pkg/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("RateLimitPolicy controller (Serial)", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		routeName     = "toystore-route"
		rlpName       = "toystore-rlp"
		gateway       *gatewayapiv1.Gateway
	)

	policyFactory := func(mutateFns ...func(policy *kuadrantv1.RateLimitPolicy)) *kuadrantv1.RateLimitPolicy {
		policy := &kuadrantv1.RateLimitPolicy{
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
						Name:  gatewayapiv1.ObjectName(routeName),
					},
				},
				Defaults: &kuadrantv1.MergeableRateLimitPolicySpec{
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
			},
		}
		for _, mutateFn := range mutateFns {
			mutateFn(policy)
		}

		return policy
	}

	beforeEachCallback := func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)

		Expect(k8sClient.Create(ctx, gateway)).To(Succeed())
		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
	}

	BeforeEach(beforeEachCallback)
	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("RLP Enforced Reasons", func() {
		const limitadorDeploymentName = "limitador-limitador"

		assertAcceptedCondTrueAndEnforcedCond := func(ctx context.Context, policy *kuadrantv1.RateLimitPolicy, conditionStatus metav1.ConditionStatus, reason, message string) func(g Gomega) {
			return func(g Gomega) {
				existingPolicy := &kuadrantv1.RateLimitPolicy{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), existingPolicy)).To(Succeed())
				acceptedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				g.Expect(acceptedCond).ToNot(BeNil())

				acceptedCondMatch := acceptedCond.Status == metav1.ConditionTrue && acceptedCond.Reason == string(gatewayapiv1alpha2.PolicyReasonAccepted)

				enforcedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(kuadrant.PolicyReasonEnforced))
				g.Expect(enforcedCond).ToNot(BeNil())
				enforcedCondMatch := enforcedCond.Status == conditionStatus && enforcedCond.Reason == reason && enforcedCond.Message == message

				g.Expect(acceptedCondMatch && enforcedCondMatch).To(BeTrue())
			}
		}

		BeforeEach(func(ctx SpecContext) {
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.toystore.com"})
			Expect(k8sClient.Create(ctx, route)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())
		})

		It("Enforced Reason", func(ctx SpecContext) {
			policy := policyFactory()
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())

			Eventually(assertAcceptedCondTrueAndEnforcedCond(ctx, policy, metav1.ConditionTrue, string(kuadrant.PolicyReasonEnforced),
				"RateLimitPolicy has been successfully enforced")).WithContext(ctx).Should(Succeed())

			// Remove limitador deployment to simulate enforcement error
			// RLP should transition to enforcement false in this case
			Expect(k8sClient.Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: limitadorDeploymentName, Namespace: kuadrantInstallationNS}})).To(Succeed())

			Eventually(assertAcceptedCondTrueAndEnforcedCond(ctx, policy, metav1.ConditionFalse, string(kuadrant.PolicyReasonUnknown),
				"RateLimitPolicy waiting for the following components to sync: [Limitador]")).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Unknown Reason", func(ctx SpecContext) {
			// Remove limitador deployment to simulate enforcement error
			Expect(k8sClient.Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: limitadorDeploymentName, Namespace: kuadrantInstallationNS}})).To(Succeed())

			// Enforced false as limitador is not ready
			policy := policyFactory()
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(assertAcceptedCondTrueAndEnforcedCond(ctx, policy, metav1.ConditionFalse, string(kuadrant.PolicyReasonUnknown),
				"RateLimitPolicy waiting for the following components to sync: [Limitador]")).WithContext(ctx).Should(Succeed())

			// Enforced true once limitador is ready
			Eventually(assertAcceptedCondTrueAndEnforcedCond(ctx, policy, metav1.ConditionTrue, string(kuadrant.PolicyReasonEnforced),
				"RateLimitPolicy has been successfully enforced")).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})

var _ = Describe("RateLimitPolicy controller", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		routeName     = "toystore-route"
		rlpName       = "toystore-rlp"
		gateway       *gatewayapiv1.Gateway
	)

	policyFactory := func(mutateFns ...func(policy *kuadrantv1.RateLimitPolicy)) *kuadrantv1.RateLimitPolicy {
		policy := &kuadrantv1.RateLimitPolicy{
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
						Name:  gatewayapiv1.ObjectName(routeName),
					},
				},
				Defaults: &kuadrantv1.MergeableRateLimitPolicySpec{
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
			},
		}
		for _, mutateFn := range mutateFns {
			mutateFn(policy)
		}

		return policy
	}

	assertPolicyIsAcceptedAndEnforced := func(ctx context.Context, key client.ObjectKey) func() bool {
		return func() bool {
			return tests.RLPIsAccepted(ctx, testClient(), key)() && tests.RLPIsEnforced(ctx, testClient(), key)()
		}
	}

	assertPolicyIsAcceptedAndNotEnforced := func(ctx context.Context, key client.ObjectKey) func() bool {
		return func() bool {
			return tests.RLPIsAccepted(ctx, testClient(), key)() && !tests.RLPIsEnforced(ctx, testClient(), key)()
		}
	}

	limitadorContainsLimit := func(ctx context.Context, limits ...limitadorv1alpha1.RateLimit) func(g Gomega) {
		return func(g Gomega) {
			limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
			existingLimitador := &limitadorv1alpha1.Limitador{}
			g.Expect(k8sClient.Get(ctx, limitadorKey, existingLimitador)).To(Succeed())
			for i := range limits {
				limit := limits[i]
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limit))
			}
		}
	}

	beforeEachCallback := func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)

		Expect(k8sClient.Create(ctx, gateway)).To(Succeed())
		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
	}

	BeforeEach(beforeEachCallback)
	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("RLP targeting HTTPRoute", func() {
		It("Creates all the resources for a basic HTTPRoute and RateLimitPolicy", func(ctx SpecContext) {
			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory()
			err = k8sClient.Create(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(ctx, limitadorKey, existingLimitador)
				// must exist
				Expect(err).ToNot(HaveOccurred())
				Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  controllers.LimitsNamespaceFromRoute(httpRoute),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, controllers.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
				}))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("RLP targeting Gateway", func() {
		It("Creates all the resources for a basic Gateway and RateLimitPolicy", func(ctx SpecContext) {
			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

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
							Group: gatewayapiv1.Group("gateway.networking.k8s.io"),
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					Defaults: &kuadrantv1.MergeableRateLimitPolicySpec{
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
				},
			}
			err = k8sClient.Create(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1.Gateway{}
			Eventually(func(g Gomega) {
				err = k8sClient.Get(ctx, gwKey, existingGateway)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(ctx, limitadorKey, existingLimitador)
				// must exist
				Expect(err).ToNot(HaveOccurred())
				Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  controllers.LimitsNamespaceFromRoute(httpRoute),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, controllers.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
				}))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Creates all the resources for a basic Gateway and RateLimitPolicy when missing a HTTPRoute attached to the Gateway", func(ctx SpecContext) {
			// create ratelimitpolicy
			rlp := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
			})
			err := k8sClient.Create(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), rlpKey, kuadrant.PolicyReasonUnknown, "RateLimitPolicy is not in the path to any existing routes"))

			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(ctx, limitadorKey, existingLimitador)
				// must exist
				Expect(err).ToNot(HaveOccurred())
				Expect(lo.Filter(existingLimitador.Spec.Limits, func(l limitadorv1alpha1.RateLimit, _ int) bool { // a hack to isolate test namespaces sharing the same limitador cr
					return strings.HasPrefix(l.Namespace, fmt.Sprintf("%s/", testNamespace))
				})).To(BeEmpty())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("RLP Defaults", func() {
		Describe("Route policy defaults taking precedence over Gateway policy defaults", func() {
			var (
				gwRLP    *kuadrantv1.RateLimitPolicy
				routeRLP *kuadrantv1.RateLimitPolicy
			)

			BeforeEach(func(ctx SpecContext) {
				// Common setup
				// GW policy defaults are overridden and not enforced when Route has their own policy attached

				// create httproute
				httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
				Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())
				Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

				// create GW RLP
				gwRLP = policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
					policy.Spec.TargetRef.Kind = "Gateway"
					policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				})
				Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
				gwRLPKey := client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
				Eventually(assertPolicyIsAcceptedAndEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())

				// Create HTTPRoute RLP with new default limits
				routeRLP = policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
					policy.Name = "httproute-rlp"
					policy.Spec.Proper().Limits = map[string]kuadrantv1.Limit{
						"l1": {
							Rates: []kuadrantv1.Rate{
								{
									Limit: 10, Window: kuadrantv1.Duration("5s"),
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
					Namespace:  controllers.LimitsNamespaceFromRoute(httpRoute),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, controllers.LimitNameToLimitadorIdentifier(routeRLPKey, "l1"))},
					Variables:  []string{},
				})).WithContext(ctx).Should(Succeed())
			})

			When("Free route is created", func() {
				It("Gateway policy should now be enforced", func(ctx SpecContext) {
					route2 := tests.BuildBasicHttpRoute("route2", TestGatewayName, testNamespace, []string{"*.car.com"})
					Expect(k8sClient.Create(ctx, route2)).To(Succeed())
					Eventually(tests.RLPIsEnforced(ctx, testClient(), client.ObjectKeyFromObject(gwRLP))).WithContext(ctx).Should(BeTrue())
				}, testTimeOut)
			})

			When("Route policy is deleted", func() {
				It("Gateway policy should now be enforced", func(ctx SpecContext) {
					Expect(k8sClient.Delete(ctx, routeRLP)).To(Succeed())
					Eventually(tests.RLPIsEnforced(ctx, testClient(), client.ObjectKeyFromObject(gwRLP))).WithContext(ctx).Should(BeTrue())
				}, testTimeOut)
			})
		})

		It("Explicit defaults - no underlying routes to enforce policy", func(ctx SpecContext) {
			gwRLP := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
			})

			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), gwRLPKey, kuadrant.PolicyReasonUnknown, "RateLimitPolicy is not in the path to any existing routes"))
		}, testTimeOut)

		It("Implicit defaults - no underlying routes to enforce policy", func(ctx SpecContext) {
			gwRLP := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				policy.Spec.RateLimitPolicySpecProper = *policy.Spec.Defaults.RateLimitPolicySpecProper.DeepCopy()
				policy.Spec.Defaults = nil
			})

			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), gwRLPKey, kuadrant.PolicyReasonUnknown, "RateLimitPolicy is not in the path to any existing routes"))
		}, testTimeOut)
	})

	Context("RLP Overrides", func() {
		var httpRoute *gatewayapiv1.HTTPRoute
		var gwRLP *kuadrantv1.RateLimitPolicy
		var routeRLP *kuadrantv1.RateLimitPolicy

		BeforeEach(func(ctx SpecContext) {
			// create httproute
			httpRoute = tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			gwRLP = policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				policy.Spec.Overrides = policy.Spec.Defaults.DeepCopy()
				policy.Spec.Defaults = nil
			})

			routeRLP = policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Name = "httproute-rlp"
				policy.Spec.Proper().Limits = map[string]kuadrantv1.Limit{
					"route": {
						Rates: []kuadrantv1.Rate{
							{
								Limit: 10, Window: kuadrantv1.Duration("5s"),
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

			limitsNamespace := controllers.LimitsNamespaceFromRoute(httpRoute)

			// check limits - should contain override values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    180,
				Namespace:  limitsNamespace,
				Conditions: []string{fmt.Sprintf(`%s == "1"`, controllers.LimitNameToLimitadorIdentifier(gwRLPKey, "l1"))},
				Variables:  []string{},
			})).WithContext(ctx).Should(Succeed())

			// Delete GW RLP -> Route RLP should be enforced
			Expect(k8sClient.Delete(ctx, gwRLP)).To(Succeed())
			Eventually(tests.RLPIsEnforced(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())
			// check limits - should be route RLP values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   10,
				Seconds:    5,
				Namespace:  limitsNamespace,
				Conditions: []string{fmt.Sprintf(`%s == "1"`, controllers.LimitNameToLimitadorIdentifier(routeRLPKey, "route"))},
				Variables:  []string{},
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
				Namespace:  controllers.LimitsNamespaceFromRoute(httpRoute),
				Conditions: []string{fmt.Sprintf(`%s == "1"`, controllers.LimitNameToLimitadorIdentifier(gwRLPKey, "l1"))},
				Variables:  []string{},
			})).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Gateway atomic override - gateway defaults turned into overrides later on", func(ctx SpecContext) {
			// Create Route RLP
			Expect(k8sClient.Create(ctx, routeRLP)).To(Succeed())
			routeRLPKey := client.ObjectKeyFromObject(routeRLP)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// Create GW RLP with defaults
			gwRLP = policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
			})
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKeyFromObject(gwRLP)
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), gwRLPKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("RateLimitPolicy is overridden by [%s]", routeRLPKey)))

			// Route RLP should still be enforced
			Eventually(tests.RLPIsEnforced(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())

			limitsNamespace := controllers.LimitsNamespaceFromRoute(httpRoute)

			// Should contain Route RLP values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   10,
				Seconds:    5,
				Namespace:  limitsNamespace,
				Conditions: []string{fmt.Sprintf(`%s == "1"`, controllers.LimitNameToLimitadorIdentifier(routeRLPKey, "route"))},
				Variables:  []string{},
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
				Namespace:  limitsNamespace,
				Conditions: []string{fmt.Sprintf(`%s == "1"`, controllers.LimitNameToLimitadorIdentifier(gwRLPKey, "l1"))},
				Variables:  []string{},
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

			limitsNamespace := controllers.LimitsNamespaceFromRoute(httpRoute)

			// Should contain override values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    180,
				Namespace:  limitsNamespace,
				Conditions: []string{fmt.Sprintf(`%s == "1"`, controllers.LimitNameToLimitadorIdentifier(gwRLPKey, "l1"))},
				Variables:  []string{},
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
				Namespace:  limitsNamespace,
				Conditions: []string{fmt.Sprintf(`%s == "1"`, controllers.LimitNameToLimitadorIdentifier(routeRLPKey, "route"))},
				Variables:  []string{},
			})).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Gateway atomic override - no underlying routes to enforce policy", func(ctx SpecContext) {
			// Delete HTTPRoute
			Expect(k8sClient.Delete(ctx, &gatewayapiv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: testNamespace}})).To(Succeed())

			// create GW RLP with overrides
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), gwRLPKey, kuadrant.PolicyReasonUnknown, "RateLimitPolicy is not in the path to any existing routes"))
		}, testTimeOut)
	})

	Context("RLP accepted condition reasons", func() {
		assertAcceptedConditionTrue := func(rlp *kuadrantv1.RateLimitPolicy) func() bool {
			return func() bool {
				rlpKey := client.ObjectKeyFromObject(rlp)
				existingRLP := &kuadrantv1.RateLimitPolicy{}
				err := k8sClient.Get(context.Background(), rlpKey, existingRLP)
				if err != nil {
					return false
				}

				return meta.IsStatusConditionTrue(existingRLP.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
			}
		}

		assertAcceptedConditionFalse := func(ctx context.Context, rlp *kuadrantv1.RateLimitPolicy, reason, message string) func(g Gomega) {
			return func(g Gomega) {
				rlpKey := client.ObjectKeyFromObject(rlp)
				existingRLP := &kuadrantv1.RateLimitPolicy{}
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

		It("Multiple policies can target a same resource", func(ctx SpecContext) {
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			rlp := policyFactory()
			Expect(k8sClient.Create(ctx, rlp)).To(Succeed())
			Eventually(tests.RLPIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(rlp))).WithContext(ctx).Should(BeTrue())

			Eventually(assertAcceptedConditionTrue(rlp), time.Minute, 5*time.Second).Should(BeTrue())

			rlp2 := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Name = "conflicting-rlp"
			})
			Expect(k8sClient.Create(ctx, rlp2)).To(Succeed())

			Eventually(assertAcceptedConditionTrue(rlp), time.Minute, 5*time.Second).Should(BeTrue())
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
					{Name: gatewayapiv1.ObjectName(gatewayAName), Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace))},
					{Name: gatewayapiv1.ObjectName(gatewayBName), Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace))},
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
			rlpGatewayA := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.ObjectMeta.Name = gatewayAName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gatewayAName)
				policy.Spec.Defaults = nil
				policy.Spec.Overrides = &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{
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
			err := k8sClient.Create(ctx, rlpGatewayA)
			Expect(err).ToNot(HaveOccurred())

			rlpGatewayB := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.ObjectMeta.Name = gatewayBName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gatewayBName)
				policy.Spec.Defaults = nil
				policy.Spec.Overrides = &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{
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
			err = k8sClient.Create(ctx, rlpGatewayB)
			Expect(err).ToNot(HaveOccurred())

			rlpTargetedRoute := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.ObjectMeta.Name = targetedRouteName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(targetedRouteName)
				policy.Spec.Proper().Limits = map[string]kuadrantv1.Limit{
					"route-10rps": {
						Rates: []kuadrantv1.Rate{
							{
								Limit: 10, Window: kuadrantv1.Duration("1s"),
							},
						},
					},
				}
			})
			err = k8sClient.Create(ctx, rlpTargetedRoute)
			Expect(err).ToNot(HaveOccurred())

			limitIdentifierGwA := controllers.LimitNameToLimitadorIdentifier(client.ObjectKeyFromObject(rlpGatewayA), "gw-a-1000rps")
			limitIdentifierGwB := controllers.LimitNameToLimitadorIdentifier(client.ObjectKeyFromObject(rlpGatewayB), "gw-b-100rps")

			Eventually(limitadorContainsLimit(
				ctx,
				limitadorv1alpha1.RateLimit{
					MaxValue:   1000,
					Seconds:    1,
					Namespace:  controllers.LimitsNamespaceFromRoute(targetedRoute),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, limitIdentifierGwA)},
					Variables:  []string{},
				},
				limitadorv1alpha1.RateLimit{
					MaxValue:   100,
					Seconds:    1,
					Namespace:  controllers.LimitsNamespaceFromRoute(targetedRoute),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, limitIdentifierGwB)},
					Variables:  []string{},
				},
				limitadorv1alpha1.RateLimit{ // FIXME(@guicassolato): we need to create one limit definition per gateway × route combination, not one per gateway × policy combination
					MaxValue:   1000,
					Seconds:    1,
					Namespace:  controllers.LimitsNamespaceFromRoute(untargetedRoute),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, limitIdentifierGwA)},
					Variables:  []string{},
				},
				limitadorv1alpha1.RateLimit{
					MaxValue:   100,
					Seconds:    1,
					Namespace:  controllers.LimitsNamespaceFromRoute(untargetedRoute),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, limitIdentifierGwB)},
					Variables:  []string{},
				},
			)).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})

var _ = Describe("RateLimitPolicy CEL Validations", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)

	var testNamespace string

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	policyFactory := func(mutateFns ...func(policy *kuadrantv1.RateLimitPolicy)) *kuadrantv1.RateLimitPolicy {
		policy := &kuadrantv1.RateLimitPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-policy",
				Namespace: testNamespace,
			},
			Spec: kuadrantv1.RateLimitPolicySpec{
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
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.Limits = map[string]kuadrantv1.Limit{
					"implicit": {
						Rates: []kuadrantv1.Rate{{Limit: 2, Window: kuadrantv1.Duration("20s")}},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(BeNil())
		}, testTimeOut)

		It("Valid policy targeting Gateway", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.Limits = map[string]kuadrantv1.Limit{
					"implicit": {
						Rates: []kuadrantv1.Rate{{Limit: 2, Window: kuadrantv1.Duration("20s")}},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(BeNil())
		}, testTimeOut)

		It("Invalid Target Ref Group", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.TargetRef.Group = "not-gateway.networking.k8s.io"
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'")).To(BeTrue())
		}, testTimeOut)

		It("Invalid Target Ref Kind", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "TCPRoute"
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'")).To(BeTrue())
		}, testTimeOut)
	})

	Context("Limits missing from configuration", func() {
		It("Missing limits object", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.Limits = nil
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.limits most be defined")).To(BeTrue())
		}, testTimeOut)

		It("Empty limits object created", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.Limits = map[string]kuadrantv1.Limit{}

			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.limits most be defined")).To(BeTrue())
		}, testTimeOut)

		It("Missing defaults.limits object", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.Limits = nil
				policy.Spec.Defaults = &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: nil,
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.defaults.limits most be defined")).To(BeTrue())
		}, testTimeOut)

		It("Empty defaults.limits object created", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.Limits = nil
				policy.Spec.Defaults = &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{},
					},
				}

			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.defaults.limits most be defined")).To(BeTrue())
		}, testTimeOut)

		It("Missing overrides.limits object", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.Limits = nil
				policy.Spec.Overrides = &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: nil,
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.overrides.limits most be defined")).To(BeTrue())
		}, testTimeOut)

		It("Empty overrides.limits object created", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.Limits = nil
				policy.Spec.Overrides = &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{},
					},
				}

			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.overrides.limits most be defined")).To(BeTrue())
		}, testTimeOut)
	})

	Context("Defaults / Override validation", func() {
		It("Valid - only implicit defaults defined", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.Limits = map[string]kuadrantv1.Limit{
					"implicit": {
						Rates: []kuadrantv1.Rate{{Limit: 2, Window: kuadrantv1.Duration("20s")}},
					},
				}
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		}, testTimeOut)

		It("Valid - only explicit defaults defined", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.Defaults = &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{
							"explicit": {
								Rates: []kuadrantv1.Rate{{Limit: 1, Window: kuadrantv1.Duration("10s")}},
							},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		}, testTimeOut)

		It("Invalid - implicit and explicit defaults are mutually exclusive", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.Defaults = &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{
							"explicit": {
								Rates: []kuadrantv1.Rate{{Limit: 1, Window: kuadrantv1.Duration("10s")}},
							},
						},
					},
				}
				policy.Spec.Limits = map[string]kuadrantv1.Limit{
					"implicit": {
						Rates: []kuadrantv1.Rate{{Limit: 2, Window: kuadrantv1.Duration("20s")}},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("Implicit and explicit defaults are mutually exclusive"))
		}, testTimeOut)

		It("Invalid - explicit default and override defined", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.Defaults = &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{
							"implicit": {
								Rates: []kuadrantv1.Rate{{Limit: 2, Window: kuadrantv1.Duration("20s")}},
							},
						},
					},
				}
				policy.Spec.Overrides = &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{
							"explicit": {
								Rates: []kuadrantv1.Rate{{Limit: 1, Window: kuadrantv1.Duration("10s")}},
							},
						},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("Overrides and explicit defaults are mutually exclusive"))
		}, testTimeOut)

		It("Invalid - implicit default and override defined", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.Limits = map[string]kuadrantv1.Limit{
					"implicit": {
						Rates: []kuadrantv1.Rate{{Limit: 2, Window: kuadrantv1.Duration("20s")}},
					},
				}
				policy.Spec.Overrides = &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{
							"overrides": {
								Rates: []kuadrantv1.Rate{{Limit: 1, Window: kuadrantv1.Duration("10s")}},
							},
						},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("Overrides and implicit defaults are mutually exclusive"))
		}, testTimeOut)

		It("Valid - policy override targeting Gateway", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.Overrides = &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{
							"override": {
								Rates: []kuadrantv1.Rate{{Limit: 1, Window: kuadrantv1.Duration("10s")}},
							},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		}, testTimeOut)
	})
})
