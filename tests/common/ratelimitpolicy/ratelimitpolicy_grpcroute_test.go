//go:build integration

package ratelimitpolicy

import (
	"fmt"
	"strings"
	"time"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("RateLimitPolicy controller (GRPCRoute)", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gateway       *gatewayapiv1.Gateway
		gatewayClass  *gatewayapiv1.GatewayClass
		gwHost        = fmt.Sprintf("*.grpcbin-%s.com", rand.String(6))
	)

	limitsNamespaceForGRPCRoute := func(grpcRoute *gatewayapiv1.GRPCRoute) string {
		return types.NamespacedName{Name: grpcRoute.GetName(), Namespace: grpcRoute.GetNamespace()}.String()
	}

	limitKeyForGRPCPath := func(policyKey types.NamespacedName, limitName string) string {
		return controllers.LimitNameToLimitadorIdentifier(policyKey, limitName)
	}

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gatewayClass = &gatewayapiv1.GatewayClass{}
		err := testClient().Get(ctx, types.NamespacedName{Name: tests.GatewayClassName}, gatewayClass)
		Expect(err).ToNot(HaveOccurred())
		gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace, func(gateway *gatewayapiv1.Gateway) {
			gateway.Spec.Listeners[0].Hostname = ptr.To(gatewayapiv1.Hostname(gwHost))
		})
		err = k8sClient.Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	policyFactory := func(mutateFns ...func(policy *kuadrantv1.RateLimitPolicy)) *kuadrantv1.RateLimitPolicy {
		policy := &kuadrantv1.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RateLimitPolicy",
				APIVersion: kuadrantv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "grpcbin",
				Namespace: testNamespace,
			},
			Spec: kuadrantv1.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "GRPCRoute",
						Name:  TestGRPCRouteName,
					},
				},
				Defaults: &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{
							"l1": {
								Rates: []kuadrantv1.Rate{
									{
										Limit: 10, Window: kuadrantv1.Duration("1m"),
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

	randomHostFromGWHost := func() string {
		return strings.Replace(gwHost, "*", rand.String(3), 1)
	}

	Context("Basic GRPCRoute", func() {
		var (
			grpcRoute *gatewayapiv1.GRPCRoute
			routeHost = randomHostFromGWHost()
		)

		BeforeEach(func(ctx SpecContext) {
			grpcRoute = tests.BuildBasicGrpcRoute(TestGRPCRouteName, TestGatewayName, testNamespace, []string{routeHost})
			err := k8sClient.Create(ctx, grpcRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GRPCRouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(grpcRoute))).WithContext(ctx).Should(BeTrue())
		})

		It("Attaches policy to the Gateway", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Name = "gw-rlp"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
			})

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating RateLimitPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(func(g Gomega) {
				tests.IsRLPAcceptedAndEnforced(g, ctx, testClient(), client.ObjectKeyFromObject(policy))
			}).WithContext(ctx).Should(Succeed())

			// check limitador limits
			limitadorKey := types.NamespacedName{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
			limitador := &limitadorv1alpha1.Limitador{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, limitadorKey, limitador)
				if err != nil {
					logf.Log.V(1).Info("Failed to get limitador", "error", err)
					return false
				}
				limitsNamespace := limitsNamespaceForGRPCRoute(grpcRoute)
				limitKey := limitKeyForGRPCPath(client.ObjectKeyFromObject(policy), "l1")
				return limitadorLimitsContain(*limitador, limitsNamespace, limitKey)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Attaches policy to the GRPCRoute", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			logf.Log.Info("Creating RateLimitPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			logf.Log.Info("Expected limits namespace", "namespace", limitsNamespaceForGRPCRoute(grpcRoute))
			logf.Log.Info("Expected limit key", "key", limitKeyForGRPCPath(client.ObjectKeyFromObject(policy), "l1"))
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(func(g Gomega) {
				tests.IsRLPAcceptedAndEnforced(g, ctx, testClient(), client.ObjectKeyFromObject(policy))
			}).WithContext(ctx).Should(Succeed())

			// check limitador limits
			limitadorKey := types.NamespacedName{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
			limitador := &limitadorv1alpha1.Limitador{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, limitadorKey, limitador)
				if err != nil {
					logf.Log.V(1).Info("Failed to get limitador", "error", err)
					return false
				}
				limitsNamespace := limitsNamespaceForGRPCRoute(grpcRoute)
				limitKey := limitKeyForGRPCPath(client.ObjectKeyFromObject(policy), "l1")
				return limitadorLimitsContain(*limitador, limitsNamespace, limitKey)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Attaches policy to the Gateway while having other policies attached to some GRPCRoutes", func(ctx SpecContext) {
			routePolicy := policyFactory()

			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating RateLimitPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(func(g Gomega) {
				tests.IsRLPAcceptedAndEnforced(g, ctx, testClient(), client.ObjectKeyFromObject(routePolicy))
			}).WithContext(ctx).Should(Succeed())

			// create second (policyless) grpcroute
			otherRoute := tests.BuildBasicGrpcRoute("policyless-route", TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err = k8sClient.Create(ctx, otherRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GRPCRouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(otherRoute))).WithContext(ctx).Should(BeTrue())

			// create gateway policy
			gwPolicy := policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Name = "gw-rlp"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				policy.Spec.Defaults.Limits = map[string]kuadrantv1.Limit{
					"gw-limit": {
						Rates: []kuadrantv1.Rate{
							{
								Limit: 100, Window: kuadrantv1.Duration("1m"),
							},
						},
					},
				}
			})
			err = k8sClient.Create(ctx, gwPolicy)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func(g Gomega) {
				tests.IsRLPAcceptedAndEnforced(g, ctx, testClient(), client.ObjectKeyFromObject(gwPolicy))
			}).WithContext(ctx).Should(Succeed())

			// check limitador limits include both policies
			limitadorKey := types.NamespacedName{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
			limitador := &limitadorv1alpha1.Limitador{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, limitadorKey, limitador)
				if err != nil {
					return false
				}
				// Route policy limit for grpcRoute
				routeLimitsNamespace := limitsNamespaceForGRPCRoute(grpcRoute)
				routeLimitKey := limitKeyForGRPCPath(client.ObjectKeyFromObject(routePolicy), "l1")
				hasRouteLimit := limitadorLimitsContain(*limitador, routeLimitsNamespace, routeLimitKey)

				// Gateway policy limit for policyless route
				gwLimitsNamespace := limitsNamespaceForGRPCRoute(otherRoute)
				gwLimitKey := limitKeyForGRPCPath(client.ObjectKeyFromObject(gwPolicy), "gw-limit")
				hasGWLimit := limitadorLimitsContain(*limitador, gwLimitsNamespace, gwLimitKey)

				return hasRouteLimit && hasGWLimit
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Deletes policy and removes limits", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating RateLimitPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(func(g Gomega) {
				tests.IsRLPAcceptedAndEnforced(g, ctx, testClient(), client.ObjectKeyFromObject(policy))
			}).WithContext(ctx).Should(Succeed())

			// check limitador limits
			limitadorKey := types.NamespacedName{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
			limitador := &limitadorv1alpha1.Limitador{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, limitadorKey, limitador)
				if err != nil {
					return false
				}
				limitsNamespace := limitsNamespaceForGRPCRoute(grpcRoute)
				limitKey := limitKeyForGRPCPath(client.ObjectKeyFromObject(policy), "l1")
				return limitadorLimitsContain(*limitador, limitsNamespace, limitKey)
			}).WithContext(ctx).Should(BeTrue())

			// delete policy
			err = k8sClient.Delete(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check limits are removed
			Eventually(func() bool {
				err := k8sClient.Get(ctx, limitadorKey, limitador)
				if err != nil {
					return false
				}
				limitsNamespace := limitsNamespaceForGRPCRoute(grpcRoute)
				limitKey := limitKeyForGRPCPath(client.ObjectKeyFromObject(policy), "l1")
				return !limitadorLimitsContain(*limitador, limitsNamespace, limitKey)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

})

// limitadorLimitsContain checks if limitador contains a limit with the given namespace and identifier
func limitadorLimitsContain(limitador limitadorv1alpha1.Limitador, namespace, identifier string) bool {
	for _, limit := range limitador.Spec.Limits {
		if limit.Namespace == namespace {
			for _, condition := range limit.Conditions {
				if strings.Contains(condition, identifier) {
					return true
				}
			}
		}
	}
	return false
}
