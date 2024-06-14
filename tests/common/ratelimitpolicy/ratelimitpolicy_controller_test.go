//go:build integration

package ratelimitpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
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

	policyFactory := func(mutateFns ...func(policy *kuadrantv1beta2.RateLimitPolicy)) *kuadrantv1beta2.RateLimitPolicy {
		policy := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RateLimitPolicy",
				APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      rlpName,
				Namespace: testNamespace,
			},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "HTTPRoute",
					Name:  gatewayapiv1.ObjectName(routeName),
				},
				Defaults: &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"l1": {
							Rates: []kuadrantv1beta2.Rate{
								{
									Limit: 1, Duration: 3, Unit: "minute",
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

		assertAcceptedCondTrueAndEnforcedCond := func(ctx context.Context, policy *kuadrantv1beta2.RateLimitPolicy, conditionStatus metav1.ConditionStatus, reason, message string) func(g Gomega) {
			return func(g Gomega) {
				existingPolicy := &kuadrantv1beta2.RateLimitPolicy{}
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
				"RateLimitPolicy has encountered some issues: limitador is not ready")).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Unknown Reason", func(ctx SpecContext) {
			// Remove limitador deployment to simulate enforcement error
			Expect(k8sClient.Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: limitadorDeploymentName, Namespace: kuadrantInstallationNS}})).To(Succeed())

			// Enforced false as limitador is not ready
			policy := policyFactory()
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(assertAcceptedCondTrueAndEnforcedCond(ctx, policy, metav1.ConditionFalse, string(kuadrant.PolicyReasonUnknown),
				"RateLimitPolicy has encountered some issues: limitador is not ready")).WithContext(ctx).Should(Succeed())

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

	policyFactory := func(mutateFns ...func(policy *kuadrantv1beta2.RateLimitPolicy)) *kuadrantv1beta2.RateLimitPolicy {
		policy := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RateLimitPolicy",
				APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      rlpName,
				Namespace: testNamespace,
			},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "HTTPRoute",
					Name:  gatewayapiv1.ObjectName(routeName),
				},
				Defaults: &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"l1": {
							Rates: []kuadrantv1beta2.Rate{
								{
									Limit: 1, Duration: 3, Unit: "minute",
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
			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantInstallationNS}
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

			// Check HTTPRoute direct back reference
			routeKey := client.ObjectKey{Name: routeName, Namespace: testNamespace}
			existingRoute := &gatewayapiv1.HTTPRoute{}
			Eventually(func(g Gomega) {
				err = k8sClient.Get(ctx, routeKey, existingRoute)
				// must exist
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingRoute.GetAnnotations()).To(HaveKeyWithValue(
					rlp.DirectReferenceAnnotationName(), client.ObjectKeyFromObject(rlp).String()))
			}).WithContext(ctx).Should(Succeed())

			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantInstallationNS}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(ctx, limitadorKey, existingLimitador)
				// must exist
				Expect(err).ToNot(HaveOccurred())
				Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}))
			}).WithContext(ctx).Should(Succeed())

			// Check gateway back references
			gwKey := client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}
			existingGateway := &gatewayapiv1.Gateway{}
			Eventually(func(g Gomega) {
				err = k8sClient.Get(ctx, gwKey, existingGateway)
				// must exist
				Expect(err).ToNot(HaveOccurred())
				refs := []client.ObjectKey{rlpKey}
				serialized, err := json.Marshal(refs)
				Expect(err).ToNot(HaveOccurred())
				Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
					rlp.BackReferenceAnnotationName(), string(serialized)))
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
			rlp := &kuadrantv1beta2.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RateLimitPolicy",
					APIVersion: kuadrantv1beta2.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      rlpName,
					Namespace: testNamespace,
				},
				Spec: kuadrantv1beta2.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: gatewayapiv1.Group("gateway.networking.k8s.io"),
						Kind:  "Gateway",
						Name:  gatewayapiv1.ObjectName(TestGatewayName),
					},
					Defaults: &kuadrantv1beta2.RateLimitPolicyCommonSpec{
						Limits: map[string]kuadrantv1beta2.Limit{
							"l1": {
								Rates: []kuadrantv1beta2.Rate{
									{
										Limit: 1, Duration: 3, Unit: "minute",
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

			// Check Gateway direct back reference
			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1.Gateway{}
			Eventually(func(g Gomega) {
				err = k8sClient.Get(ctx, gwKey, existingGateway)
				// must exist
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
					rlp.DirectReferenceAnnotationName(), client.ObjectKeyFromObject(rlp).String()))
			}).WithContext(ctx).Should(Succeed())

			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantInstallationNS}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(ctx, limitadorKey, existingLimitador)
				// must exist
				Expect(err).ToNot(HaveOccurred())
				Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}))
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				// Check gateway back references
				err = k8sClient.Get(ctx, gwKey, existingGateway)
				// must exist
				g.Expect(err).ToNot(HaveOccurred())
				refs := []client.ObjectKey{rlpKey}
				serialized, err := json.Marshal(refs)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(rlp.BackReferenceAnnotationName(), string(serialized)))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Creates all the resources for a basic Gateway and RateLimitPolicy when missing a HTTPRoute attached to the Gateway", func(ctx SpecContext) {
			// create ratelimitpolicy
			rlp := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
			})
			err := k8sClient.Create(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), rlpKey, kuadrant.PolicyReasonUnknown, "RateLimitPolicy has encountered some issues: no free routes to enforce policy"))

			// Check Gateway direct back reference
			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1.Gateway{}
			err = k8sClient.Get(ctx, gwKey, existingGateway)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
				rlp.DirectReferenceAnnotationName(), client.ObjectKeyFromObject(rlp).String()))

			// check limits
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantInstallationNS}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(ctx, limitadorKey, existingLimitador)
				// must exist
				Expect(err).ToNot(HaveOccurred())
				Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(rlpKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}))
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				// Check gateway back references
				err = k8sClient.Get(ctx, gwKey, existingGateway)
				// must exist
				g.Expect(err).ToNot(HaveOccurred())
				refs := []client.ObjectKey{rlpKey}
				serialized, err := json.Marshal(refs)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(rlp.BackReferenceAnnotationName(), string(serialized)))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("RLP Defaults", func() {
		Describe("Route policy defaults taking precedence over Gateway policy defaults", func() {
			var (
				gwRLP    *kuadrantv1beta2.RateLimitPolicy
				routeRLP *kuadrantv1beta2.RateLimitPolicy
			)

			BeforeEach(func(ctx SpecContext) {
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

				// Check Gateway direct back reference
				gwKey := client.ObjectKeyFromObject(gateway)
				existingGateway := &gatewayapiv1.Gateway{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, gwKey, existingGateway)).To(Succeed())
					g.Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
						gwRLP.DirectReferenceAnnotationName(), client.ObjectKeyFromObject(gwRLP).String()))
				}).WithContext(ctx).Should(Succeed())

				// check limits
				Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
					MaxValue:   10,
					Seconds:    5,
					Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
					Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(routeRLPKey, "l1"))},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(routeRLP),
				})).WithContext(ctx).Should(Succeed())

				// Gateway should contain HTTPRoute RLP in backreference
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, gwKey, existingGateway)).To(Succeed())
					serialized, err := json.Marshal(routeRLPKey)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(existingGateway.GetAnnotations()).To(HaveKey(routeRLP.BackReferenceAnnotationName()))
					g.Expect(existingGateway.GetAnnotations()[routeRLP.BackReferenceAnnotationName()]).To(ContainSubstring(string(serialized)))
				}).WithContext(ctx).Should(Succeed())
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
			gwRLP := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
			})

			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), gwRLPKey, kuadrant.PolicyReasonUnknown, "RateLimitPolicy has encountered some issues: no free routes to enforce policy"))
		}, testTimeOut)

		It("Implicit defaults - no underlying routes to enforce policy", func(ctx SpecContext) {
			gwRLP := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				policy.Spec.RateLimitPolicyCommonSpec = *policy.Spec.Defaults.DeepCopy()
				policy.Spec.Defaults = nil
			})

			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), gwRLPKey, kuadrant.PolicyReasonUnknown, "RateLimitPolicy has encountered some issues: no free routes to enforce policy"))
		}, testTimeOut)
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

			// Check Gateway direct back reference
			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1.Gateway{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, gwKey, existingGateway)).To(Succeed())
				g.Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
					gwRLP.DirectReferenceAnnotationName(), client.ObjectKeyFromObject(gwRLP).String()))
			}).WithContext(ctx).Should(Succeed())

			// check limits - should contain override values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    180,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(routeRLPKey, "l1"))},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			})).WithContext(ctx).Should(Succeed())

			// Gateway should contain HTTPRoute RLP in backreference
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, gwKey, existingGateway)).To(Succeed())
				serialized, err := json.Marshal(gwRLPKey)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingGateway.GetAnnotations()).To(HaveKey(routeRLP.BackReferenceAnnotationName()))
				g.Expect(existingGateway.GetAnnotations()[routeRLP.BackReferenceAnnotationName()]).To(ContainSubstring(string(serialized)))
			}).WithContext(ctx).Should(Succeed())

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

			// Check Gateway direct back reference
			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1.Gateway{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, gwKey, existingGateway)).To(Succeed())
				g.Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
					gwRLP.DirectReferenceAnnotationName(), client.ObjectKeyFromObject(gwRLP).String()))
			}).WithContext(ctx).Should(Succeed())

			// Should contain override values
			Eventually(limitadorContainsLimit(ctx, limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    180,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{fmt.Sprintf(`%s == "1"`, wasm.LimitNameToLimitadorIdentifier(routeRLPKey, "l1"))},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			})).WithContext(ctx).Should(Succeed())

			// Gateway should contain HTTPRoute RLP in backreference
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, gwKey, existingGateway)).To(Succeed())
				serialized, err := json.Marshal(routeRLPKey)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingGateway.GetAnnotations()).To(HaveKey(routeRLP.BackReferenceAnnotationName()))
				g.Expect(existingGateway.GetAnnotations()[routeRLP.BackReferenceAnnotationName()]).To(ContainSubstring(string(serialized)))
			}).WithContext(ctx).Should(Succeed())
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

		It("Gateway atomic override - no underlying routes to enforce policy", func(ctx SpecContext) {
			// Delete HTTPRoute
			Expect(k8sClient.Delete(ctx, &gatewayapiv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: testNamespace}})).To(Succeed())

			// create GW RLP with overrides
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), gwRLPKey, kuadrant.PolicyReasonUnknown, "RateLimitPolicy has encountered some issues: no free routes to enforce policy"))
		}, testTimeOut)
	})

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

	policyFactory := func(mutateFns ...func(policy *kuadrantv1beta2.RateLimitPolicy)) *kuadrantv1beta2.RateLimitPolicy {
		policy := &kuadrantv1beta2.RateLimitPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-policy",
				Namespace: testNamespace,
			},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "HTTPRoute",
					Name:  "my-target",
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
			policy := policyFactory()
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(BeNil())
		}, testTimeOut)

		It("Valid policy targeting Gateway", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(BeNil())
		}, testTimeOut)

		It("Invalid Target Ref Group", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Group = "not-gateway.networking.k8s.io"
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'")).To(BeTrue())
		}, testTimeOut)

		It("Invalid Target Ref Kind", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "TCPRoute"
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'")).To(BeTrue())
		}, testTimeOut)
	})

	Context("Defaults / Override validation", func() {
		It("Valid - only implicit defaults defined", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"implicit": {
						Rates: []kuadrantv1beta2.Rate{{Limit: 2, Duration: 20, Unit: "second"}},
					},
				}
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		}, testTimeOut)

		It("Valid - only explicit defaults defined", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.Defaults = &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"explicit": {
							Rates: []kuadrantv1beta2.Rate{{Limit: 1, Duration: 10, Unit: "second"}},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		}, testTimeOut)

		It("Invalid - implicit and explicit defaults are mutually exclusive", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.Defaults = &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"explicit": {
							Rates: []kuadrantv1beta2.Rate{{Limit: 1, Duration: 10, Unit: "second"}},
						},
					},
				}
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"implicit": {
						Rates: []kuadrantv1beta2.Rate{{Limit: 2, Duration: 20, Unit: "second"}},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("Implicit and explicit defaults are mutually exclusive"))
		}, testTimeOut)

		It("Invalid - explicit default and override defined", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.Defaults = &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"implicit": {
							Rates: []kuadrantv1beta2.Rate{{Limit: 2, Duration: 20, Unit: "second"}},
						},
					},
				}
				policy.Spec.Overrides = &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"explicit": {
							Rates: []kuadrantv1beta2.Rate{{Limit: 1, Duration: 10, Unit: "second"}},
						},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("Overrides and explicit defaults are mutually exclusive"))
		}, testTimeOut)

		It("Invalid - implicit default and override defined", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"implicit": {
						Rates: []kuadrantv1beta2.Rate{{Limit: 2, Duration: 20, Unit: "second"}},
					},
				}
				policy.Spec.Overrides = &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"overrides": {
							Rates: []kuadrantv1beta2.Rate{{Limit: 1, Duration: 10, Unit: "second"}},
						},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("Overrides and implicit defaults are mutually exclusive"))
		}, testTimeOut)

		It("Invalid - policy override targeting resource other than Gateway", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.Overrides = &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"implicit": {
							Rates: []kuadrantv1beta2.Rate{{Limit: 1, Duration: 10, Unit: "second"}},
						},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("Overrides are only allowed for policies targeting a Gateway resource"))
		}, testTimeOut)

		It("Valid - policy override targeting Gateway", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.Overrides = &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"override": {
							Rates: []kuadrantv1beta2.Rate{{Limit: 1, Duration: 10, Unit: "second"}},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		}, testTimeOut)
	})

	Context("Route Selector Validation", func() {
		const (
			gateWayRouteSelectorErrorMessage = "route selectors not supported when targeting a Gateway"
		)

		It("invalid usage of limit route selectors with a gateway targetRef", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = "my-gw"
				policy.Spec.RateLimitPolicyCommonSpec = kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"l1": {
							Rates: []kuadrantv1beta2.Rate{
								{
									Limit: 1, Duration: 3, Unit: "minute",
								},
							},
							RouteSelectors: []kuadrantv1beta2.RouteSelector{
								{
									Hostnames: []gatewayapiv1.Hostname{"*.foo.io"},
									Matches: []gatewayapiv1.HTTPRouteMatch{
										{
											Path: &gatewayapiv1.HTTPPathMatch{
												Value: ptr.To("/foo"),
											},
										},
									},
								},
							},
						},
					},
				}
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), gateWayRouteSelectorErrorMessage)).To(BeTrue())
		}, testTimeOut)
	})
})
