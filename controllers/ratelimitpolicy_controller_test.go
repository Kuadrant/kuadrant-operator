//go:build integration

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
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
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
)

var _ = Describe("RateLimitPolicy controller", func() {
	var (
		testNamespace string
		routeName     = "toystore-route"
		gwName        = "toystore-gw"
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

	beforeEachCallback := func() {
		CreateNamespace(&testNamespace)
		gateway = testBuildBasicGateway(gwName, testNamespace)
		err := k8sClient.Create(context.Background(), gateway)
		Expect(err).ToNot(HaveOccurred())
		Eventually(testGatewayIsReady(gateway), 30*time.Second, 5*time.Second).Should(BeTrue())
		ApplyKuadrantCR(testNamespace)
	}

	BeforeEach(beforeEachCallback)
	AfterEach(DeleteNamespaceCallback(&testNamespace))

	Context("RLP targeting HTTPRoute", func() {
		It("Creates all the resources for a basic HTTPRoute and RateLimitPolicy", func() {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory()
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check HTTPRoute direct back reference
			routeKey := client.ObjectKey{Name: routeName, Namespace: testNamespace}
			existingRoute := &gatewayapiv1.HTTPRoute{}
			err = k8sClient.Get(context.Background(), routeKey, existingRoute)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			Expect(existingRoute.GetAnnotations()).To(HaveKeyWithValue(
				rlp.DirectReferenceAnnotationName(), client.ObjectKeyFromObject(rlp).String()))

			// check limits
			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
			existingLimitador := &limitadorv1alpha1.Limitador{}
			err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    3 * 60,
				Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
				Conditions: []string{`limit.l1__2804bad6 == "1"`},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(rlp),
			}))

			// Check gateway back references
			gwKey := client.ObjectKey{Name: gwName, Namespace: testNamespace}
			existingGateway := &gatewayapiv1.Gateway{}
			err = k8sClient.Get(context.Background(), gwKey, existingGateway)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			refs := []client.ObjectKey{rlpKey}
			serialized, err := json.Marshal(refs)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
				rlp.BackReferenceAnnotationName(), string(serialized)))
		})
	})

	Context("RLP targeting Gateway", func() {
		It("Creates all the resources for a basic Gateway and RateLimitPolicy", func() {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

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
						Name:  gatewayapiv1.ObjectName(gwName),
					},
					Defaults: &kuadrantv1beta2.RateLimitPolicyCommonSpec{
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
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check Gateway direct back reference
			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1.Gateway{}
			err = k8sClient.Get(context.Background(), gwKey, existingGateway)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
				rlp.DirectReferenceAnnotationName(), client.ObjectKeyFromObject(rlp).String()))

			// check limits
			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
			existingLimitador := &limitadorv1alpha1.Limitador{}
			err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    3 * 60,
				Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
				Conditions: []string{`limit.l1__2804bad6 == "1"`},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(rlp),
			}))

			// Check gateway back references
			err = k8sClient.Get(context.Background(), gwKey, existingGateway)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			refs := []client.ObjectKey{rlpKey}
			serialized, err := json.Marshal(refs)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(rlp.BackReferenceAnnotationName(), string(serialized)))
		})

		It("Creates all the resources for a basic Gateway and RateLimitPolicy when missing a HTTPRoute attached to the Gateway", func() {
			// create ratelimitpolicy
			rlp := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
			})
			err := k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check Gateway direct back reference
			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1.Gateway{}
			err = k8sClient.Get(context.Background(), gwKey, existingGateway)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
				rlp.DirectReferenceAnnotationName(), client.ObjectKeyFromObject(rlp).String()))

			// check limits
			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
			existingLimitador := &limitadorv1alpha1.Limitador{}
			err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    3 * 60,
				Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
				Conditions: []string{`limit.l1__2804bad6 == "1"`},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(rlp),
			}))

			// Check gateway back references
			err = k8sClient.Get(context.Background(), gwKey, existingGateway)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			refs := []client.ObjectKey{rlpKey}
			serialized, err := json.Marshal(refs)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
				rlp.BackReferenceAnnotationName(), string(serialized)))
		})
	})

	Context("RLP Defaults", func() {
		It("HTTPRoute atomic default taking precedence over Gateway defaults", func(ctx SpecContext) {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create GW RLP
			gwRLP := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
			})
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			rlpKey := client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey)).WithContext(ctx).Should(BeTrue())

			// Create HTTPRoute RLP with new default limits
			routeRLP := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Name = "httproute-rlp"
				policy.Spec.CommonSpec().Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 10, Duration: 5, Unit: kuadrantv1beta2.TimeUnit("second"),
							},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, routeRLP)).To(Succeed())
			rlpKey = client.ObjectKey{Name: routeRLP.Name, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey)).WithContext(ctx).Should(BeTrue())

			// Check Gateway direct back reference
			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1.Gateway{}
			Expect(k8sClient.Get(ctx, gwKey, existingGateway)).To(Succeed())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
				gwRLP.DirectReferenceAnnotationName(), client.ObjectKeyFromObject(gwRLP).String()))

			// check limits
			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
			existingLimitador := &limitadorv1alpha1.Limitador{}
			Expect(k8sClient.Get(ctx, limitadorKey, existingLimitador)).To(Succeed())
			Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
				MaxValue:   10,
				Seconds:    5,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{`limit.l1__2804bad6 == "1"`},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			}))

			// Gateway should contain HTTPRoute RLP in backreference
			Expect(k8sClient.Get(ctx, gwKey, existingGateway)).To(Succeed())
			serialized, err := json.Marshal(rlpKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKey(routeRLP.BackReferenceAnnotationName()))
			Expect(existingGateway.GetAnnotations()[routeRLP.BackReferenceAnnotationName()]).To(ContainSubstring(string(serialized)))

		}, SpecTimeout(time.Minute))
	})

	Context("RLP Overrides", func() {
		It("Gateway atomic override - gateway overrides exist and then route policy created", func(ctx SpecContext) {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create GW RLP
			gwRLP := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
				policy.Spec.Defaults = nil
				policy.Spec.Overrides = &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"l1": {
							Rates: []kuadrantv1beta2.Rate{
								{
									Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
								},
							},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			rlpKey := client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey)).WithContext(ctx).Should(BeTrue())

			// Create HTTPRoute RLP with new default limits
			routeRLP := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Name = "httproute-rlp"
				policy.Spec.CommonSpec().Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 10, Duration: 5, Unit: kuadrantv1beta2.TimeUnit("second"),
							},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, routeRLP)).To(Succeed())
			rlpKey = client.ObjectKey{Name: routeRLP.Name, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey)).WithContext(ctx).Should(BeTrue())

			// Check Gateway direct back reference
			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1.Gateway{}
			Expect(k8sClient.Get(ctx, gwKey, existingGateway)).To(Succeed())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
				gwRLP.DirectReferenceAnnotationName(), client.ObjectKeyFromObject(gwRLP).String()))

			// check limits - should contain override values
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				g.Expect(k8sClient.Get(ctx, limitadorKey, existingLimitador)).To(Succeed())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    180,
					Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(routeRLP),
				}))
			}).WithContext(ctx).Should(Succeed())

			// Gateway should contain HTTPRoute RLP in backreference
			Expect(k8sClient.Get(ctx, gwKey, existingGateway)).To(Succeed())
			serialized, err := json.Marshal(rlpKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKey(routeRLP.BackReferenceAnnotationName()))
			Expect(existingGateway.GetAnnotations()[routeRLP.BackReferenceAnnotationName()]).To(ContainSubstring(string(serialized)))

		}, SpecTimeout(time.Minute))

		It("Gateway atomic override - route policy exits and then gateway policy created", func(ctx SpecContext) {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// Create HTTPRoute RLP
			routeRLP := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Name = "httproute-rlp"
				policy.Spec.CommonSpec().Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 10, Duration: 5, Unit: kuadrantv1beta2.TimeUnit("second"),
							},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, routeRLP)).To(Succeed())
			rlpKey := client.ObjectKey{Name: routeRLP.Name, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey)).WithContext(ctx).Should(BeTrue())

			// create GW RLP
			gwRLP := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
				policy.Spec.Defaults = nil
				policy.Spec.Overrides = &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"l1": {
							Rates: []kuadrantv1beta2.Rate{
								{
									Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
								},
							},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			rlpKey = client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey)).WithContext(ctx).Should(BeTrue())

			// Check Gateway direct back reference
			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1.Gateway{}
			Expect(k8sClient.Get(ctx, gwKey, existingGateway)).To(Succeed())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
				gwRLP.DirectReferenceAnnotationName(), client.ObjectKeyFromObject(gwRLP).String()))

			Eventually(func(g Gomega) {
				// check limits - should contain override values
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				Expect(k8sClient.Get(ctx, limitadorKey, existingLimitador)).To(Succeed())
				Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    180,
					Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(routeRLP),
				}))
			})

			// Gateway should contain HTTPRoute RLP in backreference
			Expect(k8sClient.Get(ctx, gwKey, existingGateway)).To(Succeed())
			serialized, err := json.Marshal(rlpKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKey(routeRLP.BackReferenceAnnotationName()))
			Expect(existingGateway.GetAnnotations()[routeRLP.BackReferenceAnnotationName()]).To(ContainSubstring(string(serialized)))

		}, SpecTimeout(time.Minute))

		It("Gateway atomic override - gateway override added later on", func(ctx SpecContext) {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// Create HTTPRoute RLP
			routeRLP := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Name = "httproute-rlp"
				policy.Spec.CommonSpec().Limits = map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit: 10, Duration: 5, Unit: kuadrantv1beta2.TimeUnit("second"),
							},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, routeRLP)).To(Succeed())
			rlpKey := client.ObjectKey{Name: routeRLP.Name, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey)).WithContext(ctx).Should(BeTrue())

			// create GW RLP
			gwRLP := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
				policy.Spec.Defaults = &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"l1": {
							Rates: []kuadrantv1beta2.Rate{
								{
									Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
								},
							},
						},
					},
				}
			})
			Expect(k8sClient.Create(ctx, gwRLP)).To(Succeed())
			rlpKey = client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey)).WithContext(ctx).Should(BeTrue())

			// check limits - should contain override values
			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
			existingLimitador := &limitadorv1alpha1.Limitador{}
			Expect(k8sClient.Get(ctx, limitadorKey, existingLimitador)).To(Succeed())
			Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
				MaxValue:   10,
				Seconds:    5,
				Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
				Conditions: []string{`limit.l1__2804bad6 == "1"`},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(routeRLP),
			}))

			updatedGRLP := &kuadrantv1beta2.RateLimitPolicy{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: gwRLP.Name, Namespace: testNamespace}, updatedGRLP)).To(Succeed())
			Eventually(func(g Gomega) {
				updatedGRLP.Spec.Overrides = updatedGRLP.Spec.Defaults.DeepCopy()
				updatedGRLP.Spec.Defaults = nil
				g.Expect(k8sClient.Update(ctx, updatedGRLP)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				// check limits - should contain override values
				limitadorKey = client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador = &limitadorv1alpha1.Limitador{}
				g.Expect(k8sClient.Get(ctx, limitadorKey, existingLimitador)).To(Succeed())
				g.Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    180,
					Namespace:  rlptools.LimitsNamespaceFromRLP(routeRLP),
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(routeRLP),
				}))
			})
		}, SpecTimeout(time.Minute))
	})

	Context("RLP accepted condition reasons", func() {
		assertAcceptedConditionFalse := func(rlp *kuadrantv1beta2.RateLimitPolicy, reason, message string) func() bool {
			return func() bool {
				rlpKey := client.ObjectKeyFromObject(rlp)
				existingRLP := &kuadrantv1beta2.RateLimitPolicy{}
				err := k8sClient.Get(context.Background(), rlpKey, existingRLP)
				if err != nil {
					return false
				}

				cond := meta.FindStatusCondition(existingRLP.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				if cond == nil {
					return false
				}

				return cond.Status == metav1.ConditionFalse && cond.Reason == reason && cond.Message == message
			}
		}

		// Accepted reason is already tested generally by the existing tests

		It("Target not found reason", func() {
			rlp := policyFactory()
			err := k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			Eventually(assertAcceptedConditionFalse(rlp, string(gatewayapiv1alpha2.PolicyReasonTargetNotFound),
				fmt.Sprintf("RateLimitPolicy target %s was not found", routeName)),
				time.Minute, 5*time.Second).Should(BeTrue())
		})

		It("Conflict reason", func() {
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			rlp := policyFactory()
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			rlp2 := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Name = "conflicting-rlp"
			})
			err = k8sClient.Create(context.Background(), rlp2)
			Expect(err).ToNot(HaveOccurred())

			Eventually(assertAcceptedConditionFalse(rlp2, string(gatewayapiv1alpha2.PolicyReasonConflicted),
				fmt.Sprintf("RateLimitPolicy is conflicted by %[1]v/toystore-rlp: the gateway.networking.k8s.io/v1, Kind=HTTPRoute target %[1]v/toystore-route is already referenced by policy %[1]v/toystore-rlp", testNamespace)),
				time.Minute, 5*time.Second).Should(BeTrue())
		})

		It("Invalid reason", func() {
			var otherNamespace string
			CreateNamespace(&otherNamespace)
			defer DeleteNamespaceCallback(&otherNamespace)

			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Namespace = otherNamespace // create the policy in a different namespace than the target
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gateway.Name)
				policy.Spec.TargetRef.Namespace = ptr.To(gatewayapiv1.Namespace(testNamespace))
			})
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())

			Eventually(assertAcceptedConditionFalse(policy, string(gatewayapiv1alpha2.PolicyReasonInvalid), fmt.Sprintf("RateLimitPolicy target is invalid: invalid targetRef.Namespace %s. Currently only supporting references to the same namespace", testNamespace)), 30*time.Second, 5*time.Second).Should(BeTrue())
		})
	})

	Context("When RLP switches target from one HTTPRoute to another HTTPRoute", func() {
		var (
			routeAName = "route-a"
			routeBName = "route-b"
		)

		It("direct references are updated", func() {
			// Initial state
			// Route A
			// RLP A -> Route A

			// Switch target to another route
			// Route A
			// Route B
			// RLP A -> Route B

			// create httproute A
			httpRouteA := testBuildBasicHttpRoute(routeAName, gwName, testNamespace, []string{"*.a.example.com"})
			err := k8sClient.Create(context.Background(), httpRouteA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRouteA)), time.Minute, 5*time.Second).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeAName)
			})
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check HTTPRoute A direct back reference
			routeAKey := client.ObjectKey{Name: routeAName, Namespace: testNamespace}
			Eventually(
				testHTTPRouteHasDirectBackReference(
					routeAKey, rlp.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlp).String(),
				),
				time.Minute, 5*time.Second).Should(BeTrue())

			// Proceed with the update:
			// From  RLP A -> Route A
			// To RLP A -> Route B

			// create httproute B
			httpRouteB := testBuildBasicHttpRoute(routeBName, gwName, testNamespace, []string{"*.b.example.com"})
			err = k8sClient.Create(context.Background(), httpRouteB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRouteB)), time.Minute, 5*time.Second).Should(BeTrue())

			rlpUpdated := &kuadrantv1beta2.RateLimitPolicy{}
			err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(rlp), rlpUpdated)
			Expect(err).ToNot(HaveOccurred())
			rlpUpdated.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeBName)
			err = k8sClient.Update(context.Background(), rlpUpdated)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check HTTPRoute A direct back reference is gone
			Eventually(
				testHTTPRouteWithoutDirectBackReference(routeAKey, rlp.DirectReferenceAnnotationName()),
				time.Minute, 5*time.Second).Should(BeTrue())

			// Check HTTPRoute B direct back reference
			routeBKey := client.ObjectKey{Name: routeBName, Namespace: testNamespace}
			Eventually(
				testHTTPRouteHasDirectBackReference(
					routeBKey, rlp.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlp).String(),
				),
				time.Minute, 5*time.Second).Should(BeTrue())
		})
	})

	Context("When RLP switches target from one Gateway to another Gateway", func() {
		var (
			gwAName = "gw-a"
			gwBName = "gw-b"
		)

		It("direct references are updated", func() {
			// Initial state
			// Gw A
			// RLP A -> Gw A

			// Switch target to another gw
			// Gw A
			// Gw B
			// RLP A -> Gw B

			// create Gw A
			gatewayA := testBuildBasicGateway(gwAName, testNamespace)
			err := k8sClient.Create(context.Background(), gatewayA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gatewayA), 30*time.Second, 5*time.Second).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwAName)
			})
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check Gateway direct back reference
			gwAKey := client.ObjectKey{Name: gwAName, Namespace: testNamespace}
			Eventually(
				testGatewayHasDirectBackReference(
					gwAKey, rlp.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlp).String(),
				),
				time.Minute, 5*time.Second).Should(BeTrue())

			// Proceed with the update:
			// From  RLP A -> Gw A
			// To RLP A -> Gw B

			// create Gw B
			gatewayB := testBuildBasicGateway(gwBName, testNamespace)
			err = k8sClient.Create(context.Background(), gatewayB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gatewayB), 30*time.Second, 5*time.Second).Should(BeTrue())

			rlpUpdated := &kuadrantv1beta2.RateLimitPolicy{}
			err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(rlp), rlpUpdated)
			Expect(err).ToNot(HaveOccurred())
			rlpUpdated.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwBName)
			err = k8sClient.Update(context.Background(), rlpUpdated)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check Gw A direct back reference is gone
			Eventually(
				testGatewayWithoutDirectBackReference(gwAKey, rlp.DirectReferenceAnnotationName()),
				time.Minute, 5*time.Second).Should(BeTrue())

			// Check Gateway B direct back reference
			gwBKey := client.ObjectKey{Name: gwBName, Namespace: testNamespace}
			Eventually(
				testGatewayHasDirectBackReference(
					gwBKey, rlp.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlp).String(),
				),
				time.Minute, 5*time.Second).Should(BeTrue())
		})
	})

	Context("When RLP switches target from one HTTPRoute to another taken HTTPRoute", func() {
		var (
			routeAName = "route-a"
			routeBName = "route-b"
			rlpAName   = "rlp-a"
			rlpBName   = "rlp-b"
		)

		It("direct references are updated", func() {
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
			httpRouteA := testBuildBasicHttpRoute(routeAName, gwName, testNamespace, []string{"*.a.example.com"})
			err := k8sClient.Create(context.Background(), httpRouteA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRouteA)), time.Minute, 5*time.Second).Should(BeTrue())

			// create httproute B
			httpRouteB := testBuildBasicHttpRoute(routeBName, gwName, testNamespace, []string{"*.b.example.com"})
			err = k8sClient.Create(context.Background(), httpRouteB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRouteB)), time.Minute, 5*time.Second).Should(BeTrue())

			// create rlpA
			rlpA := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.ObjectMeta.Name = rlpAName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeAName)
			})
			err = k8sClient.Create(context.Background(), rlpA)
			Expect(err).ToNot(HaveOccurred())

			rlpAKey := client.ObjectKeyFromObject(rlpA)
			Eventually(testRLPIsAccepted(rlpAKey), time.Minute, 5*time.Second).Should(BeTrue())

			// create rlpB
			rlpB := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.ObjectMeta.Name = rlpBName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeBName)
			})
			err = k8sClient.Create(context.Background(), rlpB)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpBKey := client.ObjectKeyFromObject(rlpB)
			Eventually(testRLPIsAccepted(rlpBKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check HTTPRoute A direct back reference
			routeAKey := client.ObjectKey{Name: routeAName, Namespace: testNamespace}
			Eventually(
				testHTTPRouteHasDirectBackReference(
					routeAKey, rlpA.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlpA).String(),
				),
				time.Minute, 5*time.Second).Should(BeTrue())

			// Check HTTPRoute B direct back reference
			routeBKey := client.ObjectKey{Name: routeBName, Namespace: testNamespace}
			Eventually(
				testHTTPRouteHasDirectBackReference(
					routeBKey, rlpB.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlpB).String(),
				),
				time.Minute, 5*time.Second).Should(BeTrue())

			// Proceed with the update:
			// From  RLP A -> Route A
			// To RLP A -> Route B (already taken)

			rlpUpdated := &kuadrantv1beta2.RateLimitPolicy{}
			err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(rlpA), rlpUpdated)
			Expect(err).ToNot(HaveOccurred())
			rlpUpdated.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeBName)
			err = k8sClient.Update(context.Background(), rlpUpdated)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			Eventually(testRLPIsNotAccepted(rlpAKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check HTTPRoute A direct back reference is gone
			Eventually(
				testHTTPRouteWithoutDirectBackReference(routeAKey, rlpA.DirectReferenceAnnotationName()),
				time.Minute, 5*time.Second).Should(BeTrue())

			// Check HTTPRoute B direct back reference
			Eventually(
				testHTTPRouteHasDirectBackReference(
					routeBKey, rlpB.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlpB).String(),
				),
				time.Minute, 5*time.Second).Should(BeTrue())
		})
	})

	Context("When target is deleted", func() {
		var (
			routeName = "route-a"
		)

		It("policy status reports error", func() {
			// Initial state
			// Route A
			// RLP A -> Route A

			// Delete route
			// RLP A

			// create httproute A
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// create rlp
			rlp := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
			})
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check HTTPRoute A direct back reference
			routeKey := client.ObjectKey{Name: routeName, Namespace: testNamespace}
			Eventually(
				testHTTPRouteHasDirectBackReference(
					routeKey, rlp.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlp).String(),
				),
				time.Minute, 5*time.Second).Should(BeTrue())

			// Proceed with the update:
			// Delete Route A
			err = k8sClient.Delete(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testObjectDoesNotExist(httpRoute), time.Minute, 5*time.Second).Should(BeTrue())

			// Check RLP status is available
			Eventually(testRLPIsNotAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())
		})
	})

	Context("When RLP targets already taken HTTPRoute and the route is being released", func() {
		var (
			routeAName = "route-a"
			routeBName = "route-b"
			rlpAName   = "rlp-a"
			rlpBName   = "rlp-b"
		)

		It("direct references are updated and RLP status is ready", func() {
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
			httpRouteA := testBuildBasicHttpRoute(routeAName, gwName, testNamespace, []string{"*.a.example.com"})
			err := k8sClient.Create(context.Background(), httpRouteA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRouteA)), time.Minute, 5*time.Second).Should(BeTrue())

			// create rlpA
			rlpA := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.ObjectMeta.Name = rlpAName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeAName)
			})
			err = k8sClient.Create(context.Background(), rlpA)
			Expect(err).ToNot(HaveOccurred())

			rlpAKey := client.ObjectKeyFromObject(rlpA)
			Eventually(testRLPIsAccepted(rlpAKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Proceed with the update:
			// new RLP B -> Route A (already taken)

			// create rlpB
			rlpB := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.ObjectMeta.Name = rlpBName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeAName)
			})
			err = k8sClient.Create(context.Background(), rlpB)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is not available
			rlpBKey := client.ObjectKeyFromObject(rlpB)
			Eventually(testRLPIsNotAccepted(rlpBKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check HTTPRoute A direct back reference to RLP A
			routeAKey := client.ObjectKey{Name: routeAName, Namespace: testNamespace}
			Eventually(
				testHTTPRouteHasDirectBackReference(
					routeAKey, rlpA.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlpA).String(),
				),
				time.Minute, 5*time.Second).Should(BeTrue())

			// Proceed with the update:
			// new Route B
			// RLP A -> Route B
			// RLP A was the older owner of route A, and wiil be the new owner of route B

			// create httproute B
			httpRouteB := testBuildBasicHttpRoute(routeBName, gwName, testNamespace, []string{"*.b.example.com"})
			err = k8sClient.Create(context.Background(), httpRouteB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRouteB)), time.Minute, 5*time.Second).Should(BeTrue())

			// RLP A -> Route B
			rlpUpdated := &kuadrantv1beta2.RateLimitPolicy{}
			err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(rlpA), rlpUpdated)
			Expect(err).ToNot(HaveOccurred())
			rlpUpdated.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeBName)
			err = k8sClient.Update(context.Background(), rlpUpdated)
			Expect(err).ToNot(HaveOccurred())

			// Check HTTPRoute A direct back reference to RLP B
			Eventually(
				testHTTPRouteHasDirectBackReference(
					routeAKey, rlpB.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlpB).String(),
				),
				time.Minute, 5*time.Second).Should(BeTrue())

			Eventually(testRLPIsAccepted(rlpBKey), time.Minute, 5*time.Second).Should(BeTrue())

			routeBKey := client.ObjectKey{Name: routeBName, Namespace: testNamespace}
			// Check HTTPRoute B direct back reference to RLP A
			Eventually(
				testHTTPRouteHasDirectBackReference(
					routeBKey, rlpA.DirectReferenceAnnotationName(),
					client.ObjectKeyFromObject(rlpA).String(),
				),
				time.Minute, 5*time.Second).Should(BeTrue())

			Eventually(testRLPIsAccepted(rlpAKey), time.Minute, 5*time.Second).Should(BeTrue())
		})
	})
})

var _ = Describe("RateLimitPolicy CEL Validations", func() {
	var testNamespace string

	BeforeEach(func() {
		CreateNamespace(&testNamespace)
	})

	AfterEach(DeleteNamespaceCallback(&testNamespace))

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
		It("Valid policy targeting HTTPRoute", func() {
			policy := policyFactory()
			err := k8sClient.Create(context.Background(), policy)
			Expect(err).To(BeNil())
		})

		It("Valid policy targeting Gateway", func() {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
			})
			err := k8sClient.Create(context.Background(), policy)
			Expect(err).To(BeNil())
		})

		It("Invalid Target Ref Group", func() {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Group = "not-gateway.networking.k8s.io"
			})
			err := k8sClient.Create(context.Background(), policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'")).To(BeTrue())
		})

		It("Invalid Target Ref Kind", func() {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "TCPRoute"
			})
			err := k8sClient.Create(context.Background(), policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'")).To(BeTrue())
		})
	})

	Context("Defaults validation", func() {
		It("Valid only implicit defaults", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"implicit": {
						Rates: []kuadrantv1beta2.Rate{{Limit: 2, Duration: 20, Unit: "second"}},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(BeNil())
		})

		It("Valid only explicit defaults", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.Defaults = &kuadrantv1beta2.RateLimitPolicyCommonSpec{
					Limits: map[string]kuadrantv1beta2.Limit{
						"explicit": {
							Rates: []kuadrantv1beta2.Rate{{Limit: 1, Duration: 10, Unit: "second"}},
						},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(BeNil())
		})

		It("Invalid implicit and explicit defaults are mutually exclusive", func(ctx SpecContext) {
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
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Implicit and explicit defaults are mutually exclusive")).To(BeTrue())
		})
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
									Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
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
		})
	})
})
