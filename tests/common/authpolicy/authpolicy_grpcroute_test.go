//go:build integration

package authpolicy

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/machinery"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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

var _ = Describe("AuthPolicy controller (GRPCRoute)", func() {
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

	authConfigKeyForGRPCPath := func(grpcRoute *gatewayapiv1.GRPCRoute, grpcRouteRuleIndex int) types.NamespacedName {
		mGateway := &machinery.Gateway{Gateway: gateway}
		mGRPCRoute := &machinery.GRPCRoute{GRPCRoute: grpcRoute}
		authConfigName := controllers.AuthConfigNameForPath(kuadrantv1.PathID([]machinery.Targetable{
			&machinery.GatewayClass{GatewayClass: gatewayClass},
			mGateway,
			&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
			mGRPCRoute,
			&machinery.GRPCRouteRule{GRPCRoute: mGRPCRoute, GRPCRouteRule: &grpcRoute.Spec.Rules[grpcRouteRuleIndex], Name: gatewayapiv1.SectionName(fmt.Sprintf("rule-%d", grpcRouteRuleIndex+1))},
		}))
		return types.NamespacedName{Name: authConfigName, Namespace: kuadrantInstallationNS}
	}

	fetchReadyAuthConfigForGRPCRoute := func(ctx context.Context, grpcRoute *gatewayapiv1.GRPCRoute, grpcRouteRuleIndex int, authConfig *authorinov1beta3.AuthConfig) func() bool {
		authConfigKey := authConfigKeyForGRPCPath(grpcRoute, grpcRouteRuleIndex)
		return func() bool {
			err := k8sClient.Get(ctx, authConfigKey, authConfig)
			logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
			return err == nil && authConfig.Status.Ready()
		}
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

	policyFactory := func(mutateFns ...func(policy *kuadrantv1.AuthPolicy)) *kuadrantv1.AuthPolicy {
		policy := &kuadrantv1.AuthPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AuthPolicy",
				APIVersion: kuadrantv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "grpcbin",
				Namespace: testNamespace,
			},
			Spec: kuadrantv1.AuthPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "GRPCRoute",
						Name:  TestGRPCRouteName,
					},
				},
				Defaults: &kuadrantv1.MergeableAuthPolicySpec{
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: tests.BuildBasicAuthScheme(),
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
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				policy.Spec.Proper().AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfig := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfigForGRPCRoute(ctx, grpcRoute, 0, authConfig)).WithContext(ctx).Should(BeTrue())
			Expect(authConfig.Spec.Authentication).To(HaveLen(1))
			Expect(authConfig.Spec.Authentication).To(HaveKeyWithValue("apiKey", policy.Spec.Proper().AuthScheme.Authentication["apiKey"].AuthenticationSpec))

			// create other route
			otherGRPCRoute := tests.BuildBasicGrpcRoute("other-route", TestGatewayName, testNamespace, []string{routeHost})
			err = k8sClient.Create(ctx, otherGRPCRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GRPCRouteIsAccepted(ctx, k8sClient, client.ObjectKeyFromObject(otherGRPCRoute))).WithContext(ctx).Should(BeTrue())

			// check authorino other authconfig
			otherAuthConfig := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfigForGRPCRoute(ctx, otherGRPCRoute, 0, otherAuthConfig)).WithContext(ctx).Should(BeTrue())
			Expect(otherAuthConfig.Spec.Authentication).To(HaveLen(1))
			Expect(otherAuthConfig.Spec.Authentication).To(HaveKeyWithValue("apiKey", policy.Spec.Proper().AuthScheme.Authentication["apiKey"].AuthenticationSpec))
		}, testTimeOut)

		It("Attaches policy to the GRPCRoute", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfig := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfigForGRPCRoute(ctx, grpcRoute, 0, authConfig)).WithContext(ctx).Should(BeTrue())
			Expect(authConfig.Spec.Authentication).To(HaveLen(1))
			Expect(authConfig.Spec.Authentication).To(HaveKeyWithValue("apiKey", policy.Spec.Proper().AuthScheme.Authentication["apiKey"].AuthenticationSpec))
		}, testTimeOut)

		It("Attaches policy to the Gateway while having other policies attached to some GRPCRoutes", func(ctx SpecContext) {
			routePolicy := policyFactory()

			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			// create second (policyless) grpcroute
			otherRoute := tests.BuildBasicGrpcRoute("policyless-route", TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err = k8sClient.Create(ctx, otherRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GRPCRouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(otherRoute))).WithContext(ctx).Should(BeTrue())

			// attach policy to the gateway
			gwPolicy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.Proper().AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["gateway"] = "yes"
			})

			err = k8sClient.Create(ctx, gwPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(gwPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gwPolicy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig for route with its own policy (should not be affected by gw policy)
			authConfig := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfigForGRPCRoute(ctx, grpcRoute, 0, authConfig)).WithContext(ctx).Should(BeTrue())
			Expect(authConfig.Spec.Authentication["apiKey"].ApiKey.Selector.MatchLabels).ToNot(HaveKeyWithValue("gateway", "yes"))

			// check authorino authconfig for policyless route (should get gw policy)
			otherAuthConfig := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfigForGRPCRoute(ctx, otherRoute, 0, otherAuthConfig)).WithContext(ctx).Should(BeTrue())
			Expect(otherAuthConfig.Spec.Authentication).To(HaveLen(1))
			Expect(otherAuthConfig.Spec.Authentication["apiKey"].ApiKey.Selector.MatchLabels).To(HaveKeyWithValue("gateway", "yes"))
		}, testTimeOut)

		It("Deletes resources when the policy is deleted", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfig := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfigForGRPCRoute(ctx, grpcRoute, 0, authConfig)).WithContext(ctx).Should(BeTrue())

			// delete policy
			err = k8sClient.Delete(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check authconfig is deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(authConfig), authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", client.ObjectKeyFromObject(authConfig).String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

	})

	Context("Complex GRPCRoute with multiple rules", func() {
		var (
			grpcRoute *gatewayapiv1.GRPCRoute
			host1     = randomHostFromGWHost()
			host2     = randomHostFromGWHost()
		)

		BeforeEach(func(ctx SpecContext) {
			grpcRoute = tests.BuildMultipleRulesGrpcRoute(TestGRPCRouteName, TestGatewayName, testNamespace, []string{host1, host2})
			err := k8sClient.Create(ctx, grpcRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GRPCRouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(grpcRoute))).WithContext(ctx).Should(BeTrue())
		})

		It("Attaches simple policy to the GRPCRoute", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfigs for both rules
			authConfigRule1 := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfigForGRPCRoute(ctx, grpcRoute, 0, authConfigRule1)).WithContext(ctx).Should(BeTrue())
			Expect(authConfigRule1.Spec.Authentication).To(HaveLen(1))

			authConfigRule2 := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfigForGRPCRoute(ctx, grpcRoute, 1, authConfigRule2)).WithContext(ctx).Should(BeTrue())
			Expect(authConfigRule2.Spec.Authentication).To(HaveLen(1))
		}, testTimeOut)

	})

	Context("AuthPolicy accepted condition reasons", func() {
		assertAcceptedCondFalseAndEnforcedCondNil := func(ctx context.Context, policy *kuadrantv1.AuthPolicy, reason, message string) func() bool {
			return func() bool {
				existingPolicy := &kuadrantv1.AuthPolicy{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), existingPolicy)
				if err != nil {
					return false
				}
				acceptedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				if acceptedCond == nil {
					return false
				}

				acceptedCondMatch := acceptedCond.Status == metav1.ConditionFalse && acceptedCond.Reason == reason && acceptedCond.Message == message

				enforcedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(kuadrant.PolicyConditionEnforced))
				enforcedCondMatch := enforcedCond == nil

				return acceptedCondMatch && enforcedCondMatch
			}
		}

		// Accepted reason is already tested generally by the existing tests

		It("Target not found reason", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(assertAcceptedCondFalseAndEnforcedCondNil(ctx, policy, string(gatewayapiv1alpha2.PolicyReasonTargetNotFound),
				fmt.Sprintf("AuthPolicy target %s was not found", TestGRPCRouteName))).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("AuthPolicy enforced condition reasons", func() {
		var grpcRoute *gatewayapiv1.GRPCRoute
		var routeHost = randomHostFromGWHost()

		BeforeEach(func(ctx SpecContext) {
			grpcRoute = tests.BuildBasicGrpcRoute(TestGRPCRouteName, TestGatewayName, testNamespace, []string{routeHost})
			err := k8sClient.Create(ctx, grpcRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GRPCRouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(grpcRoute))).WithContext(ctx).Should(BeTrue())
		})

		It("Enforced reason", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Overridden reason - Attaches policy to the Gateway while having other policies attached to all GRPCRoutes", func(ctx SpecContext) {
			assertAcceptedCondTrueAndEnforcedCond := func(ctx context.Context, policy *kuadrantv1.AuthPolicy, conditionStatus metav1.ConditionStatus, reason, message string) func() bool {
				return func() bool {
					existingPolicy := &kuadrantv1.AuthPolicy{}
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), existingPolicy)
					if err != nil {
						return false
					}
					acceptedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
					if acceptedCond == nil {
						return false
					}

					acceptedCondMatch := acceptedCond.Status == metav1.ConditionTrue

					enforcedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(kuadrant.PolicyConditionEnforced))
					if enforcedCond == nil {
						return false
					}

					enforcedCondMatch := enforcedCond.Status == conditionStatus && enforcedCond.Reason == reason
					if message != "" {
						enforcedCondMatch = enforcedCondMatch && strings.Contains(enforcedCond.Message, message)
					}

					return acceptedCondMatch && enforcedCondMatch
				}
			}

			// Attach policy to GRPCRoute
			routePolicy := policyFactory()
			err := k8sClient.Create(ctx, routePolicy)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			// Attach policy to Gateway
			gwPolicy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
			})
			err = k8sClient.Create(ctx, gwPolicy)
			Expect(err).ToNot(HaveOccurred())

			// GW Policy should be accepted but overridden
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), gwPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(assertAcceptedCondTrueAndEnforcedCond(ctx, gwPolicy, metav1.ConditionFalse, string(kuadrant.PolicyReasonOverridden), "")).WithContext(ctx).Should(BeTrue())

			// GW Policy should go back to being enforced when a GRPCRoute with no AP attached becomes available
			By("creating a new GRPCRoute without a policy attached")
			otherRoute := tests.BuildBasicGrpcRoute("other-route", TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err = k8sClient.Create(ctx, otherRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GRPCRouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(otherRoute))).WithContext(ctx).Should(BeTrue())

			// GW Policy should now be enforced
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), gwPolicy)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("AuthPolicy with GRPCRoute method matching", func() {
		var (
			grpcRoute *gatewayapiv1.GRPCRoute
			routeHost = randomHostFromGWHost()
		)

		BeforeEach(func(ctx SpecContext) {
			// Create a GRPCRoute with method-specific rules
			grpcRoute = tests.BuildBasicGrpcRoute(TestGRPCRouteName, TestGatewayName, testNamespace, []string{routeHost}, func(route *gatewayapiv1.GRPCRoute) {
				route.Spec.Rules = []gatewayapiv1.GRPCRouteRule{
					{
						Matches: []gatewayapiv1.GRPCRouteMatch{
							{
								Method: &gatewayapiv1.GRPCMethodMatch{
									Type:    ptr.To(gatewayapiv1.GRPCMethodMatchExact),
									Service: ptr.To("grpcbin.GRPCBin"),
									Method:  ptr.To("SayHello"),
								},
							},
						},
					},
					{
						Matches: []gatewayapiv1.GRPCRouteMatch{
							{
								Method: &gatewayapiv1.GRPCMethodMatch{
									Type:    ptr.To(gatewayapiv1.GRPCMethodMatchExact),
									Service: ptr.To("grpcbin.GRPCBin"),
									Method:  ptr.To("StreamMessages"),
								},
							},
						},
					},
				}
			})
			err := k8sClient.Create(ctx, grpcRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GRPCRouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(grpcRoute))).WithContext(ctx).Should(BeTrue())
		})

		It("Creates separate AuthConfigs for different method matches", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig for SayHello method (rule 0)
			authConfigRule1 := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfigForGRPCRoute(ctx, grpcRoute, 0, authConfigRule1)).WithContext(ctx).Should(BeTrue())
			Expect(authConfigRule1.Spec.Authentication).To(HaveLen(1))

			// check authorino authconfig for StreamMessages method (rule 1)
			authConfigRule2 := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfigForGRPCRoute(ctx, grpcRoute, 1, authConfigRule2)).WithContext(ctx).Should(BeTrue())
			Expect(authConfigRule2.Spec.Authentication).To(HaveLen(1))
		}, testTimeOut)
	})

	Context("GRPCRoute with varying method specificity", func() {
		var (
			grpcRoute *gatewayapiv1.GRPCRoute
			routeHost = randomHostFromGWHost()
		)

		BeforeEach(func(ctx SpecContext) {
			// Create a GRPCRoute with rules of varying specificity
			// to verify correct sorting and policy application
			grpcRoute = tests.BuildBasicGrpcRoute(TestGRPCRouteName, TestGatewayName, testNamespace, []string{routeHost}, func(route *gatewayapiv1.GRPCRoute) {
				route.Spec.Rules = []gatewayapiv1.GRPCRouteRule{
					{
						// Rule 0: Most specific - both service and method
						Matches: []gatewayapiv1.GRPCRouteMatch{
							{
								Method: &gatewayapiv1.GRPCMethodMatch{
									Type:    ptr.To(gatewayapiv1.GRPCMethodMatchExact),
									Service: ptr.To("grpcbin.GRPCBin"),
									Method:  ptr.To("SayHello"),
								},
							},
						},
					},
					{
						// Rule 1: Less specific - service only (matches all methods in service)
						Matches: []gatewayapiv1.GRPCRouteMatch{
							{
								Method: &gatewayapiv1.GRPCMethodMatch{
									Type:    ptr.To(gatewayapiv1.GRPCMethodMatchExact),
									Service: ptr.To("grpcbin.GRPCBin"),
								},
							},
						},
					},
					{
						// Rule 2: Least specific - method only (matches method across all services)
						Matches: []gatewayapiv1.GRPCRouteMatch{
							{
								Method: &gatewayapiv1.GRPCMethodMatch{
									Type:   ptr.To(gatewayapiv1.GRPCMethodMatchExact),
									Method: ptr.To("SayHello"),
								},
							},
						},
					},
				}
			})
			err := k8sClient.Create(ctx, grpcRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GRPCRouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(grpcRoute))).WithContext(ctx).Should(BeTrue())
		})

		It("Creates AuthConfigs for all specificity levels", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// Verify AuthConfig created for most specific rule (service + method)
			authConfigRule0 := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfigForGRPCRoute(ctx, grpcRoute, 0, authConfigRule0)).WithContext(ctx).Should(BeTrue())
			Expect(authConfigRule0.Spec.Authentication).To(HaveLen(1))

			// Verify AuthConfig created for medium specific rule (service only)
			authConfigRule1 := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfigForGRPCRoute(ctx, grpcRoute, 1, authConfigRule1)).WithContext(ctx).Should(BeTrue())
			Expect(authConfigRule1.Spec.Authentication).To(HaveLen(1))

			// Verify AuthConfig created for least specific rule (method only)
			authConfigRule2 := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfigForGRPCRoute(ctx, grpcRoute, 2, authConfigRule2)).WithContext(ctx).Should(BeTrue())
			Expect(authConfigRule2.Spec.Authentication).To(HaveLen(1))
		}, testTimeOut)
	})
})
