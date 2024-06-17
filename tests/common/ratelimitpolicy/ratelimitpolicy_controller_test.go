//go:build integration

package ratelimitpolicy

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
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
		It("policy status is available and backreference is set", func(ctx SpecContext) {
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
		})
	})

	Context("RLP targeting Gateway", func() {
		It("policy status is available and backreference is set", func(ctx SpecContext) {
			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
			})
			err = k8sClient.Create(context.Background(), rlp)
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
		})
	})

	Context("RLP targeting Gateway when there is no HTTPRoute attached to the gateway", func(ctx SpecContext) {
		It("policy status is available and backreference is set", func() {
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
