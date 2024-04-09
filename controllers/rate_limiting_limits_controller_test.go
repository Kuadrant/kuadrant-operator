//go:build integration

package controllers

import (
	"context"
	"reflect"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
)

var _ = Describe("Rate Limiting limits controller", func() {
	var (
		testNamespace string
	)

	policyFactory := func(name string, mutateFns ...func(policy *kuadrantv1beta2.RateLimitPolicy)) *kuadrantv1beta2.RateLimitPolicy {
		policy := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RateLimitPolicy",
				APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{},
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
		}

		for _, mutateFn := range mutateFns {
			mutateFn(policy)
		}

		return policy
	}

	BeforeEach(func() { CreateNamespace(&testNamespace) })
	AfterEach(DeleteNamespaceCallback(&testNamespace))

	Context("RLP targeting HTTPRoute", func() {
		var (
			routeName = "toystore-route"
			gwName    = "toystore-gw"
			rlpName   = "toystore-rlp"
			gateway   *gatewayapiv1.Gateway
		)

		BeforeEach(func() {
			gateway = testBuildBasicGateway(gwName, testNamespace)
			err := k8sClient.Create(context.Background(), gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gateway), 30*time.Second, 5*time.Second).Should(BeTrue())
			ApplyKuadrantCR(testNamespace)
		})

		It("limitador captures the policy configuration", func() {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory(rlpName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
			})
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

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
		})
	})

	Context("RLP targeting Gateway", func() {
		var (
			routeName = "toystore-route"
			gwName    = "toystore-gw"
			rlpName   = "toystore-rlp"
			gateway   *gatewayapiv1.Gateway
		)

		BeforeEach(func() {
			gateway = testBuildBasicGateway(gwName, testNamespace)
			err := k8sClient.Create(context.Background(), gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gateway), 30*time.Second, 5*time.Second).Should(BeTrue())
			ApplyKuadrantCR(testNamespace)
		})

		It("limitador captures the policy configuration", func() {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory(rlpName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
			})

			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

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
		})
	})

	Context("RLP targeting Gateway when there is no HTTPRoute attached to the gateway", func() {
		var (
			gwName  = "toystore-gw"
			rlpName = "toystore-rlp"
			gateway *gatewayapiv1.Gateway
		)

		BeforeEach(func() {
			gateway = testBuildBasicGateway(gwName, testNamespace)
			err := k8sClient.Create(context.Background(), gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gateway), 30*time.Second, 5*time.Second).Should(BeTrue())
			ApplyKuadrantCR(testNamespace)
		})

		It("limitador does not capture the policy configuration", func() {
			// create ratelimitpolicy
			rlp := policyFactory(rlpName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
			})
			err := k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// check limits
			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
			existingLimitador := &limitadorv1alpha1.Limitador{}
			err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
			// must NOT exist
			Expect(err).ToNot(HaveOccurred())
			Expect(existingLimitador.Spec.Limits).To(BeEmpty())
		})
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

		BeforeEach(func() {
			ApplyKuadrantCR(testNamespace)
		})

		It("limitador does not capture the policy configuration", func() {
			// gwA
			gatewayA := testBuildBasicGateway(gwAName, testNamespace)
			err := k8sClient.Create(context.Background(), gatewayA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gatewayA), 30*time.Second, 5*time.Second).Should(BeTrue())

			// gwB
			gatewayB := testBuildBasicGateway(gwBName, testNamespace)
			err = k8sClient.Create(context.Background(), gatewayB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gatewayB), 30*time.Second, 5*time.Second).Should(BeTrue())

			// RLP
			rlp := policyFactory(rlpName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwAName)
			})
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// HTTPRoute
			httpRoute := testBuildBasicHttpRoute(routeName, gwAName, testNamespace, []string{"*.example.com"})
			err = k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// Initial state set.
			// check limits
			Eventually(func() bool {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
				if err != nil {
					logf.Log.V(1).Info("limitador not read", "key", limitadorKey, "error", err)
					return false
				}

				if len(existingLimitador.Spec.Limits) == 0 {
					logf.Log.V(1).Info("limits is empty")
					return false
				}

				expectedLimit := limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}

				if !reflect.DeepEqual(existingLimitador.Spec.Limits[0], expectedLimit) {
					diff := cmp.Diff(existingLimitador.Spec.Limits[0], expectedLimit)
					logf.Log.V(1).Info("limits do not match", "diff", diff)
					return false
				}

				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			// Proceed with the update:
			// From Route A -> Gw A
			// To Route A -> Gw B
			httpRouteUpdated := &gatewayapiv1.HTTPRoute{}
			err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(httpRoute), httpRouteUpdated)
			Expect(err).ToNot(HaveOccurred())
			httpRouteUpdated.Spec.CommonRouteSpec.ParentRefs[0].Name = gatewayapiv1.ObjectName(gwBName)
			err = k8sClient.Update(context.Background(), httpRouteUpdated)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// The RLP targeting the gateway no longer is effective, as the gateway does not have any route
			// check limits
			Eventually(func() bool {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
				if err != nil {
					logf.Log.V(1).Info("limitador not read", "key", limitadorKey, "error", err)
					return false
				}

				if len(existingLimitador.Spec.Limits) > 0 {
					logf.Log.V(1).Info("limits is not empty")
					return false
				}

				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})
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

		BeforeEach(func() {
			ApplyKuadrantCR(testNamespace)
		})

		It("limitador captures the policy configuration", func() {
			// gwA
			gatewayA := testBuildBasicGateway(gwAName, testNamespace)
			err := k8sClient.Create(context.Background(), gatewayA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gatewayA), 30*time.Second, 5*time.Second).Should(BeTrue())

			// gwB
			gatewayB := testBuildBasicGateway(gwBName, testNamespace)
			err = k8sClient.Create(context.Background(), gatewayB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gatewayB), 30*time.Second, 5*time.Second).Should(BeTrue())

			// HTTPRoute
			httpRoute := testBuildBasicHttpRoute(routeName, gwAName, testNamespace, []string{"*.example.com"})
			err = k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// RLP
			rlp := policyFactory(rlpName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
			})
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Initial state set.
			// check limits
			Eventually(func() bool {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
				if err != nil {
					logf.Log.V(1).Info("limitador not read", "key", limitadorKey, "error", err)
					return false
				}

				if len(existingLimitador.Spec.Limits) == 0 {
					logf.Log.V(1).Info("limits is empty")
					return false
				}

				expectedLimit := limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}

				if !reflect.DeepEqual(existingLimitador.Spec.Limits[0], expectedLimit) {
					diff := cmp.Diff(existingLimitador.Spec.Limits[0], expectedLimit)
					logf.Log.V(1).Info("limits do not match", "diff", diff)
					return false
				}

				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			// Proceed with the update:
			// From Route A -> Gw A
			// To Route A -> Gw B
			httpRouteUpdated := &gatewayapiv1.HTTPRoute{}
			err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(httpRoute), httpRouteUpdated)
			Expect(err).ToNot(HaveOccurred())
			httpRouteUpdated.Spec.CommonRouteSpec.ParentRefs[0].Name = gatewayapiv1.ObjectName(gwBName)
			err = k8sClient.Update(context.Background(), httpRouteUpdated)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// The RLP targeting the new route is still effective
			// check limits
			Eventually(func() bool {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
				if err != nil {
					logf.Log.V(1).Info("limitador not read", "key", limitadorKey, "error", err)
					return false
				}

				if len(existingLimitador.Spec.Limits) == 0 {
					logf.Log.V(1).Info("limits is empty")
					return false
				}

				expectedLimit := limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}

				if !reflect.DeepEqual(existingLimitador.Spec.Limits[0], expectedLimit) {
					diff := cmp.Diff(existingLimitador.Spec.Limits[0], expectedLimit)
					logf.Log.V(1).Info("limits do not match", "diff", diff)
					return false
				}

				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})
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

		BeforeEach(func() {
			ApplyKuadrantCR(testNamespace)
		})

		It("limitador captures the policy configuration", func() {
			// gwA
			gateway := testBuildBasicGateway(gwName, testNamespace)
			err := k8sClient.Create(context.Background(), gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gateway), 30*time.Second, 5*time.Second).Should(BeTrue())

			// HTTPRoute A
			httpRouteA := testBuildBasicHttpRoute(routeAName, gwName, testNamespace, []string{"*.a.example.com"})
			err = k8sClient.Create(context.Background(), httpRouteA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRouteA)), time.Minute, 5*time.Second).Should(BeTrue())

			// HTTPRoute B
			httpRouteB := testBuildBasicHttpRoute(routeBName, gwName, testNamespace, []string{"*.b.example.com"})
			err = k8sClient.Create(context.Background(), httpRouteB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRouteB)), time.Minute, 5*time.Second).Should(BeTrue())

			// RLP
			rlp := policyFactory(rlpName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeAName)
			})
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Initial state set.
			// check limits
			Eventually(func() bool {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
				if err != nil {
					logf.Log.V(1).Info("limitador not read", "key", limitadorKey, "error", err)
					return false
				}

				if len(existingLimitador.Spec.Limits) == 0 {
					logf.Log.V(1).Info("limits is empty")
					return false
				}

				expectedLimit := limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}

				if !reflect.DeepEqual(existingLimitador.Spec.Limits[0], expectedLimit) {
					diff := cmp.Diff(existingLimitador.Spec.Limits[0], expectedLimit)
					logf.Log.V(1).Info("limits do not match", "diff", diff)
					return false
				}

				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			// Proceed with the update:
			// From RLP -> Route A
			// To RLP -> Route B
			rlpUpdated := &kuadrantv1beta2.RateLimitPolicy{}
			err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(rlp), rlpUpdated)
			Expect(err).ToNot(HaveOccurred())
			rlpUpdated.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeBName)
			err = k8sClient.Update(context.Background(), rlpUpdated)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// The RLP targeting the new route is still effective
			// check limits
			Eventually(func() bool {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
				if err != nil {
					logf.Log.V(1).Info("limitador not read", "key", limitadorKey, "error", err)
					return false
				}

				if len(existingLimitador.Spec.Limits) == 0 {
					logf.Log.V(1).Info("limits is empty")
					return false
				}

				expectedLimit := limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlp),
				}

				if !reflect.DeepEqual(existingLimitador.Spec.Limits[0], expectedLimit) {
					diff := cmp.Diff(existingLimitador.Spec.Limits[0], expectedLimit)
					logf.Log.V(1).Info("limits do not match", "diff", diff)
					return false
				}

				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})
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

		BeforeEach(func() {
			ApplyKuadrantCR(testNamespace)
		})

		It("limitador only captures the policy configuration that targets the route", func() {
			// gwA
			gateway := testBuildBasicGateway(gwName, testNamespace)
			err := k8sClient.Create(context.Background(), gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gateway), 30*time.Second, 5*time.Second).Should(BeTrue())

			// HTTPRoute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err = k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// RLP A
			rlpA := policyFactory(rlpAName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
			})
			err = k8sClient.Create(context.Background(), rlpA)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpAKey := client.ObjectKey{Name: rlpAName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpAKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Initial state set.
			// check limits
			Eventually(func() bool {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
				if err != nil {
					logf.Log.V(1).Info("limitador not read", "key", limitadorKey, "error", err)
					return false
				}

				if len(existingLimitador.Spec.Limits) == 0 {
					logf.Log.V(1).Info("limits is empty")
					return false
				}

				expectedLimit := limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    3 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpA),
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpA),
				}

				if !reflect.DeepEqual(existingLimitador.Spec.Limits[0], expectedLimit) {
					diff := cmp.Diff(existingLimitador.Spec.Limits[0], expectedLimit)
					logf.Log.V(1).Info("limits do not match", "diff", diff)
					return false
				}

				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

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
			err = k8sClient.Create(context.Background(), rlpB)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpBKey := client.ObjectKey{Name: rlpBName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpBKey), time.Minute, 5*time.Second).Should(BeTrue())

			// The RLP targeting the new route is effective
			// The RLP targeting the gateway is no longer effective
			// check limits
			Eventually(func() bool {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
				if err != nil {
					logf.Log.V(1).Info("limitador not read", "key", limitadorKey, "error", err)
					return false
				}

				if len(existingLimitador.Spec.Limits) == 0 {
					logf.Log.V(1).Info("limits is empty")
					return false
				}

				expectedLimit := limitadorv1alpha1.RateLimit{
					MaxValue:   15,
					Seconds:    5 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpB),
					Conditions: []string{`limit.l2__8a1cee43 == "1"`},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpB),
				}

				if !reflect.DeepEqual(existingLimitador.Spec.Limits[0], expectedLimit) {
					diff := cmp.Diff(existingLimitador.Spec.Limits[0], expectedLimit)
					logf.Log.V(1).Info("limits do not match", "diff", diff)
					return false
				}

				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})
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

		BeforeEach(func() {
			ApplyKuadrantCR(testNamespace)
		})

		It("limitador only captures the policy configuration that targets the route", func() {
			// gw
			gateway := testBuildBasicGateway(gwName, testNamespace)
			err := k8sClient.Create(context.Background(), gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gateway), 30*time.Second, 5*time.Second).Should(BeTrue())

			// HTTPRoute A
			httpRouteA := testBuildBasicHttpRoute(routeAName, gwName, testNamespace, []string{"*.a.example.com"})
			err = k8sClient.Create(context.Background(), httpRouteA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRouteA)), time.Minute, 5*time.Second).Should(BeTrue())

			// RLP A
			rlpA := policyFactory(rlpAName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
			})
			err = k8sClient.Create(context.Background(), rlpA)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpAKey := client.ObjectKey{Name: rlpAName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpAKey), time.Minute, 5*time.Second).Should(BeTrue())

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
			err = k8sClient.Create(context.Background(), rlpB)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpBKey := client.ObjectKey{Name: rlpBName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpBKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Initial state set.
			// check limits -> only RLP targeting the route adds configuration
			Eventually(func() bool {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
				if err != nil {
					logf.Log.V(1).Info("limitador not read", "key", limitadorKey, "error", err)
					return false
				}

				if len(existingLimitador.Spec.Limits) == 0 {
					logf.Log.V(1).Info("limits is empty")
					return false
				}

				expectedLimit := limitadorv1alpha1.RateLimit{
					MaxValue:   15,
					Seconds:    5 * 60,
					Namespace:  rlptools.LimitsNamespaceFromRLP(rlpB),
					Conditions: []string{`limit.l2__8a1cee43 == "1"`},
					Variables:  []string{},
					Name:       rlptools.LimitsNameFromRLP(rlpB),
				}

				if !reflect.DeepEqual(existingLimitador.Spec.Limits[0], expectedLimit) {
					diff := cmp.Diff(existingLimitador.Spec.Limits[0], expectedLimit)
					logf.Log.V(1).Info("limits do not match", "diff", diff)
					return false
				}

				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			// Proceed with the update:
			// New Route B -> Gw A
			// the gateway has a new untargeted route, thus RLP targeting the gateway takes effect
			// HTTPRoute B
			httpRouteB := testBuildBasicHttpRoute(routeBName, gwName, testNamespace, []string{"*.b.example.com"})
			err = k8sClient.Create(context.Background(), httpRouteB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRouteB)), time.Minute, 5*time.Second).Should(BeTrue())

			// The RLP targeting the gateway is now effective
			// check limits
			Eventually(func() bool {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
				if err != nil {
					logf.Log.V(1).Info("limitador not read", "key", limitadorKey, "error", err)
					return false
				}

				if len(existingLimitador.Spec.Limits) == 0 {
					logf.Log.V(1).Info("limits is empty")
					return false
				}

				if len(existingLimitador.Spec.Limits) == 1 {
					logf.Log.V(1).Info("limits has only one element")
					return false
				}

				_, found := utils.Find(existingLimitador.Spec.Limits, func(limit limitadorv1alpha1.RateLimit) bool {
					return reflect.DeepEqual(limit, limitadorv1alpha1.RateLimit{
						MaxValue:   15,
						Seconds:    5 * 60,
						Namespace:  rlptools.LimitsNamespaceFromRLP(rlpB),
						Conditions: []string{`limit.l2__8a1cee43 == "1"`},
						Variables:  []string{},
						Name:       rlptools.LimitsNameFromRLP(rlpB),
					})
				})

				if !found {
					logf.Log.V(1).Info("RLP B limit not found")
					return false
				}

				_, found = utils.Find(existingLimitador.Spec.Limits, func(limit limitadorv1alpha1.RateLimit) bool {
					return reflect.DeepEqual(limit, limitadorv1alpha1.RateLimit{
						MaxValue:   1,
						Seconds:    3 * 60,
						Namespace:  rlptools.LimitsNamespaceFromRLP(rlpA),
						Conditions: []string{`limit.l1__2804bad6 == "1"`},
						Variables:  []string{},
						Name:       rlptools.LimitsNameFromRLP(rlpA),
					})
				})

				if !found {
					logf.Log.V(1).Info("RLP A limit not found")
					return false
				}

				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})
	})

	Context("When kuadrant CR does not exist", func() {
		var (
			routeName = "toystore-route"
			gwName    = "toystore-gw"
			rlpName   = "toystore-rlp"
		)

		It("limitador does not capture policy configuration", func() {
			// Gw
			gateway := testBuildBasicGateway(gwName, testNamespace)
			err := k8sClient.Create(context.Background(), gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gateway), 30*time.Second, 5*time.Second).Should(BeTrue())

			// HTTPRoute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err = k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory(rlpName, func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
			})
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Since Kuadrant CR does not exist, Limitador CR is not deployed by kuadrant
			// Thus, create it manually
			limitador := &limitadorv1alpha1.Limitador{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Limitador",
					APIVersion: "limitador.kuadrant.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.LimitadorName,
					Namespace: testNamespace,
				},
				Spec: limitadorv1alpha1.LimitadorSpec{
					// fake limit that should be gone
					Limits: []limitadorv1alpha1.RateLimit{
						{
							MaxValue:   1,
							Seconds:    1,
							Namespace:  "test",
							Conditions: []string{},
							Variables:  []string{},
							Name:       "test",
						},
					},
				},
			}
			err = k8sClient.Create(context.Background(), limitador)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
				existingLimitador := &limitadorv1alpha1.Limitador{}
				err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
				if err != nil {
					logf.Log.V(1).Info("limitador not read", "key", limitadorKey, "error", err)
					return false
				}

				if len(existingLimitador.Spec.Limits) != 1 {
					logf.Log.V(1).Info("limits size is not 1")
					return false
				}

				expectedLimit := limitadorv1alpha1.RateLimit{
					MaxValue:   1,
					Seconds:    1,
					Namespace:  "test",
					Conditions: []string{},
					Variables:  []string{},
					Name:       "test",
				}

				if !reflect.DeepEqual(existingLimitador.Spec.Limits[0], expectedLimit) {
					diff := cmp.Diff(existingLimitador.Spec.Limits[0], expectedLimit)
					logf.Log.V(1).Info("limits do not match", "diff", diff)
					return false
				}

				return true
			}, 70*time.Second, 5*time.Second).MustPassRepeatedly(5).Should(BeTrue())
		})
	})
})
