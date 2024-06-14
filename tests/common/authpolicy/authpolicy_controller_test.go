//go:build integration

package authpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/controllers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("AuthPolicy controller (Serial)", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gwHost        = fmt.Sprintf("*.toystore-%s.com", rand.String(6))
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())

		gateway := tests.BuildBasicGateway(TestGatewayName, testNamespace, func(gateway *gatewayapiv1.Gateway) {
			gateway.Spec.Listeners[0].Hostname = ptr.To(gatewayapiv1.Hostname(gwHost))
		})
		err := k8sClient.Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("AuthPolicy enforced condition reasons", func() {
		assertAcceptedCondTrueAndEnforcedCond := func(ctx context.Context, policy *api.AuthPolicy, conditionStatus metav1.ConditionStatus, reason, message string) func() bool {
			return func() bool {
				existingPolicy := &api.AuthPolicy{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), existingPolicy)
				if err != nil {
					return false
				}
				acceptedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				if acceptedCond == nil {
					return false
				}

				acceptedCondMatch := acceptedCond.Status == metav1.ConditionTrue && acceptedCond.Reason == string(gatewayapiv1alpha2.PolicyReasonAccepted)

				enforcedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(kuadrant.PolicyReasonEnforced))
				if enforcedCond == nil {
					return false
				}
				enforcedCondMatch := enforcedCond.Status == conditionStatus && enforcedCond.Reason == reason && enforcedCond.Message == message

				return acceptedCondMatch && enforcedCondMatch
			}
		}

		policyFactory := func(mutateFns ...func(policy *api.AuthPolicy)) *api.AuthPolicy {
			policy := &api.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: api.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     gatewayapiv1.GroupName,
						Kind:      "HTTPRoute",
						Name:      TestHTTPRouteName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					Defaults: &api.AuthPolicyCommonSpec{
						AuthScheme: testBasicAuthScheme(),
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

		BeforeEach(func(ctx SpecContext) {
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err := k8sClient.Create(ctx, route)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())
		})

		It("Unknown reason", func(ctx SpecContext) {
			// Remove kuadrant to simulate AuthPolicy enforcement error
			defer tests.ApplyKuadrantCR(ctx, testClient(), kuadrantInstallationNS)
			tests.DeleteKuadrantCR(ctx, testClient(), kuadrantInstallationNS)

			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			Eventually(assertAcceptedCondTrueAndEnforcedCond(ctx, policy, metav1.ConditionFalse, string(kuadrant.PolicyReasonUnknown),
				"AuthPolicy has encountered some issues: AuthScheme is not ready yet")).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})
})

var _ = Describe("AuthPolicy controller", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gwHost        = fmt.Sprintf("*.toystore-%s.com", rand.String(6))
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())

		gateway := tests.BuildBasicGateway(TestGatewayName, testNamespace, func(gateway *gatewayapiv1.Gateway) {
			gateway.Spec.Listeners[0].Hostname = ptr.To(gatewayapiv1.Hostname(gwHost))
		})
		err := k8sClient.Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	policyFactory := func(mutateFns ...func(policy *api.AuthPolicy)) *api.AuthPolicy {
		policy := &api.AuthPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AuthPolicy",
				APIVersion: api.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "toystore",
				Namespace: testNamespace,
			},
			Spec: api.AuthPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group:     gatewayapiv1.GroupName,
					Kind:      "HTTPRoute",
					Name:      TestHTTPRouteName,
					Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
				},
				Defaults: &api.AuthPolicyCommonSpec{
					AuthScheme: testBasicAuthScheme(),
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

	Context("Basic HTTPRoute", func() {
		routeHost := randomHostFromGWHost()

		BeforeEach(func(ctx SpecContext) {
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{routeHost})
			err := k8sClient.Create(ctx, route)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())
		})

		It("Attaches policy to the Gateway (without hostname defined in listener)", func(ctx SpecContext) {
			// Create GW with no hostname defined in listener
			gwName := "no-defined-hostname"
			gateway := tests.BuildBasicGateway(gwName, testNamespace)
			err := k8sClient.Create(ctx, gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, k8sClient, gateway)).WithContext(ctx).Should(BeTrue())

			// Create route with this GW as parent
			route := tests.BuildBasicHttpRoute("other-route", gwName, testNamespace, []string{routeHost})
			err = k8sClient.Create(ctx, route)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, k8sClient, client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
				policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})

			err = k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: controllers.AuthConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}).WithContext(ctx).Should(BeTrue())
			logf.Log.V(1).Info("authConfig.Spec", "hosts", authConfig.Spec.Hosts, "conditions", authConfig.Spec.Conditions)
			Expect(authConfig.Spec.Hosts).To(Equal([]string{"*"}))
			Expect(authConfig.Spec.Conditions).To(HaveLen(1))
			Expect(authConfig.Spec.Conditions[0].Any).To(HaveLen(1))        // 1 HTTPRouteRule in the HTTPRoute
			Expect(authConfig.Spec.Conditions[0].Any[0].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal("GET"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("/toy.*"))
		}, testTimeOut)

		It("Attaches policy to a Gateway with hostname in listeners", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
			})

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig hosts
			authConfigKey := types.NamespacedName{Name: controllers.AuthConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}).WithContext(ctx).Should(BeTrue())

			Expect(authConfig.Spec.Hosts).To(ConsistOf(gwHost))
		}, testTimeOut)

		It("Attaches policy to the HTTPRoute", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: controllers.AuthConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}).WithContext(ctx).Should(BeTrue())
			logf.Log.V(1).Info("authConfig.Spec", "hosts", authConfig.Spec.Hosts, "conditions", authConfig.Spec.Conditions)
			Expect(authConfig.Spec.Hosts).To(Equal([]string{routeHost}))
			Expect(authConfig.Spec.Conditions[0].Any).To(HaveLen(1))        // 1 HTTPRouteRule in the HTTPRoute
			Expect(authConfig.Spec.Conditions[0].Any[0].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal("GET"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("/toy.*"))
		}, testTimeOut)

		It("Attaches policy to the Gateway while having other policies attached to some HTTPRoutes", func(ctx SpecContext) {
			routePolicy := policyFactory()

			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			// create second (policyless) httproute
			otherRoute := tests.BuildBasicHttpRoute("policyless-route", TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			otherRoute.Spec.Rules = []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Method: ptr.To(gatewayapiv1.HTTPMethod("POST")),
						},
					},
				},
			}
			err = k8sClient.Create(ctx, otherRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(otherRoute))).WithContext(ctx).Should(BeTrue())

			// attach policy to the gatewaay
			gwPolicy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
			})

			err = k8sClient.Create(ctx, gwPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(gwPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gwPolicy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: controllers.AuthConfigName(client.ObjectKeyFromObject(gwPolicy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}).WithContext(ctx).Should(BeTrue())
			logf.Log.V(1).Info("authConfig.Spec", "hosts", authConfig.Spec.Hosts, "conditions", authConfig.Spec.Conditions)
			Expect(authConfig.Spec.Hosts).To(Equal([]string{gwHost}))
			Expect(authConfig.Spec.Conditions).To(HaveLen(1))
			Expect(authConfig.Spec.Conditions[0].Any).To(HaveLen(1))        // 1 HTTPRouteRule in the policyless HTTPRoute
			Expect(authConfig.Spec.Conditions[0].Any[0].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal("POST"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("/.*"))
		}, testTimeOut)

		It("Rejects policy with only unmatching top-level route selectors while trying to configure the gateway", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.CommonSpec().RouteSelectors = []api.RouteSelector{
					{ // does not select any HTTPRouteRule
						Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
							{
								Method: ptr.To(gatewayapiv1alpha2.HTTPMethod("DELETE")),
							},
						},
					},
				}
			})

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(func() bool {
				existingPolicy := &api.AuthPolicy{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), existingPolicy)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				return condition != nil && condition.Reason == string(kuadrant.PolicyReasonUnknown) && strings.Contains(condition.Message, "cannot match any route rules, check for invalid route selectors in the policy")
			}).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: controllers.AuthConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, &authorinoapi.AuthConfig{})
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Rejects policy with only unmatching config-level route selectors post-configuring the gateway", func(ctx SpecContext) {
			policy := policyFactory()
			config := policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"]
			config.RouteSelectors = []api.RouteSelector{
				{ // does not select any HTTPRouteRule
					Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
						{
							Method: ptr.To(gatewayapiv1alpha2.HTTPMethod("DELETE")),
						},
					},
				},
			}
			policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"] = config

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(func() bool {
				existingPolicy := &api.AuthPolicy{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), existingPolicy)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				return condition != nil && condition.Reason == string(kuadrant.PolicyReasonUnknown) && strings.Contains(condition.Message, "cannot match any route rules, check for invalid route selectors in the policy")
			}).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: controllers.AuthConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, &authorinoapi.AuthConfig{})
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Deletes resources when the policy is deleted", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// delete policy
			err = k8sClient.Delete(ctx, policy)
			logf.Log.V(1).Info("Deleting AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: controllers.AuthConfigName(client.ObjectKey{Name: "toystore", Namespace: testNamespace}), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, &authorinoapi.AuthConfig{})
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Maps to all fields of the AuthConfig", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.CommonSpec().NamedPatterns = map[string]authorinoapi.PatternExpressions{
					"internal-source": []authorinoapi.PatternExpression{
						{
							Selector: "source.ip",
							Operator: authorinoapi.PatternExpressionOperator("matches"),
							Value:    `192\.168\..*`,
						},
					},
					"authz-and-rl-required": []authorinoapi.PatternExpression{
						{
							Selector: "source.ip",
							Operator: authorinoapi.PatternExpressionOperator("neq"),
							Value:    "192.168.0.10",
						},
					},
				}
				policy.Spec.CommonSpec().Conditions = []authorinoapi.PatternExpressionOrRef{
					{
						PatternRef: authorinoapi.PatternRef{
							Name: "internal-source",
						},
					},
				}
				policy.Spec.CommonSpec().AuthScheme = &api.AuthSchemeSpec{
					Authentication: map[string]api.AuthenticationSpec{
						"jwt": {
							AuthenticationSpec: authorinoapi.AuthenticationSpec{
								CommonEvaluatorSpec: authorinoapi.CommonEvaluatorSpec{
									Conditions: []authorinoapi.PatternExpressionOrRef{
										{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: `filter_metadata.envoy\.filters\.http\.jwt_authn|verified_jwt`,
												Operator: "neq",
												Value:    "",
											},
										},
									},
								},
								AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
									Plain: &authorinoapi.PlainIdentitySpec{
										Selector: `filter_metadata.envoy\.filters\.http\.jwt_authn|verified_jwt`,
									},
								},
							},
						},
					},
					Metadata: map[string]api.MetadataSpec{
						"user-groups": {
							MetadataSpec: authorinoapi.MetadataSpec{
								CommonEvaluatorSpec: authorinoapi.CommonEvaluatorSpec{
									Conditions: []authorinoapi.PatternExpressionOrRef{
										{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: "auth.identity.admin",
												Operator: authorinoapi.PatternExpressionOperator("neq"),
												Value:    "true",
											},
										},
									},
								},
								MetadataMethodSpec: authorinoapi.MetadataMethodSpec{
									Http: &authorinoapi.HttpEndpointSpec{
										Url: "http://user-groups/username={auth.identity.username}",
									},
								},
							},
						},
					},
					Authorization: map[string]api.AuthorizationSpec{
						"admin-or-privileged": {
							AuthorizationSpec: authorinoapi.AuthorizationSpec{
								CommonEvaluatorSpec: authorinoapi.CommonEvaluatorSpec{
									Conditions: []authorinoapi.PatternExpressionOrRef{
										{
											PatternRef: authorinoapi.PatternRef{
												Name: "authz-and-rl-required",
											},
										},
									},
								},
								AuthorizationMethodSpec: authorinoapi.AuthorizationMethodSpec{
									PatternMatching: &authorinoapi.PatternMatchingAuthorizationSpec{
										Patterns: []authorinoapi.PatternExpressionOrRef{
											{
												Any: []authorinoapi.UnstructuredPatternExpressionOrRef{
													{
														PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
															PatternExpression: authorinoapi.PatternExpression{
																Selector: "auth.identity.admin",
																Operator: authorinoapi.PatternExpressionOperator("eq"),
																Value:    "true",
															},
														},
													},
													{
														PatternExpressionOrRef: authorinoapi.PatternExpressionOrRef{
															PatternExpression: authorinoapi.PatternExpression{
																Selector: "auth.metadata.user-groups",
																Operator: authorinoapi.PatternExpressionOperator("incl"),
																Value:    "privileged",
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
					Response: &api.ResponseSpec{
						Unauthenticated: &authorinoapi.DenyWithSpec{
							Message: &authorinoapi.ValueOrSelector{
								Value: k8sruntime.RawExtension{Raw: []byte(`"Missing verified JWT injected by the gateway"`)},
							},
						},
						Unauthorized: &authorinoapi.DenyWithSpec{
							Message: &authorinoapi.ValueOrSelector{
								Value: k8sruntime.RawExtension{Raw: []byte(`"User must be admin or member of privileged group"`)},
							},
						},
						Success: api.WrappedSuccessResponseSpec{
							Headers: map[string]api.HeaderSuccessResponseSpec{
								"x-username": {
									SuccessResponseSpec: api.SuccessResponseSpec{
										SuccessResponseSpec: authorinoapi.SuccessResponseSpec{
											CommonEvaluatorSpec: authorinoapi.CommonEvaluatorSpec{
												Conditions: []authorinoapi.PatternExpressionOrRef{
													{
														PatternExpression: authorinoapi.PatternExpression{
															Selector: "request.headers.x-propagate-username.@case:lower",
															Operator: authorinoapi.PatternExpressionOperator("matches"),
															Value:    "1|yes|true",
														},
													},
												},
											},
											AuthResponseMethodSpec: authorinoapi.AuthResponseMethodSpec{
												Plain: &authorinoapi.PlainAuthResponseSpec{
													Selector: "auth.identity.username",
												},
											},
										},
									},
								},
							},
							DynamicMetadata: map[string]api.SuccessResponseSpec{
								"x-auth-data": {
									SuccessResponseSpec: authorinoapi.SuccessResponseSpec{
										CommonEvaluatorSpec: authorinoapi.CommonEvaluatorSpec{
											Conditions: []authorinoapi.PatternExpressionOrRef{
												{
													PatternRef: authorinoapi.PatternRef{
														Name: "authz-and-rl-required",
													},
												},
											},
										},
										AuthResponseMethodSpec: authorinoapi.AuthResponseMethodSpec{
											Json: &authorinoapi.JsonAuthResponseSpec{
												Properties: authorinoapi.NamedValuesOrSelectors{
													"username": {
														Selector: "auth.identity.username",
													},
													"groups": {
														Selector: "auth.metadata.user-groups",
													},
												},
											},
										},
									},
								},
							},
						},
					},
					Callbacks: map[string]api.CallbackSpec{
						"unauthorized-attempt": {
							CallbackSpec: authorinoapi.CallbackSpec{
								CommonEvaluatorSpec: authorinoapi.CommonEvaluatorSpec{
									Conditions: []authorinoapi.PatternExpressionOrRef{
										{
											PatternRef: authorinoapi.PatternRef{
												Name: "authz-and-rl-required",
											},
										},
										{
											PatternExpression: authorinoapi.PatternExpression{
												Selector: "auth.authorization.admin-or-privileged",
												Operator: authorinoapi.PatternExpressionOperator("neq"),
												Value:    "true",
											},
										},
									},
								},
								CallbackMethodSpec: authorinoapi.CallbackMethodSpec{
									Http: &authorinoapi.HttpEndpointSpec{
										Url:         "http://events/unauthorized",
										Method:      ptr.To(authorinoapi.HttpMethod("POST")),
										ContentType: authorinoapi.HttpContentType("application/json"),
										Body: &authorinoapi.ValueOrSelector{
											Selector: `\{"identity":{auth.identity},"request-id":{request.id}\}`,
										},
									},
								},
							},
						},
					},
				}
			})

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: controllers.AuthConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}).WithContext(ctx).Should(BeTrue())
			authConfigSpecAsJSON, _ := json.Marshal(authConfig.Spec)
			Expect(string(authConfigSpecAsJSON)).To(Equal(fmt.Sprintf(`{"hosts":["%s"],"patterns":{"authz-and-rl-required":[{"selector":"source.ip","operator":"neq","value":"192.168.0.10"}],"internal-source":[{"selector":"source.ip","operator":"matches","value":"192\\.168\\..*"}]},"when":[{"patternRef":"internal-source"},{"any":[{"any":[{"all":[{"selector":"request.method","operator":"eq","value":"GET"},{"selector":"request.url_path","operator":"matches","value":"/toy.*"}]}]}]}],"authentication":{"jwt":{"when":[{"selector":"filter_metadata.envoy\\.filters\\.http\\.jwt_authn|verified_jwt","operator":"neq"}],"credentials":{"authorizationHeader":{}},"plain":{"selector":"filter_metadata.envoy\\.filters\\.http\\.jwt_authn|verified_jwt"}}},"metadata":{"user-groups":{"when":[{"selector":"auth.identity.admin","operator":"neq","value":"true"}],"http":{"url":"http://user-groups/username={auth.identity.username}","method":"GET","contentType":"application/x-www-form-urlencoded","credentials":{"authorizationHeader":{}}}}},"authorization":{"admin-or-privileged":{"when":[{"patternRef":"authz-and-rl-required"}],"patternMatching":{"patterns":[{"any":[{"selector":"auth.identity.admin","operator":"eq","value":"true"},{"selector":"auth.metadata.user-groups","operator":"incl","value":"privileged"}]}]}}},"response":{"unauthenticated":{"message":{"value":"Missing verified JWT injected by the gateway"}},"unauthorized":{"message":{"value":"User must be admin or member of privileged group"}},"success":{"headers":{"x-username":{"when":[{"selector":"request.headers.x-propagate-username.@case:lower","operator":"matches","value":"1|yes|true"}],"plain":{"value":null,"selector":"auth.identity.username"}}},"dynamicMetadata":{"x-auth-data":{"when":[{"patternRef":"authz-and-rl-required"}],"json":{"properties":{"groups":{"value":null,"selector":"auth.metadata.user-groups"},"username":{"value":null,"selector":"auth.identity.username"}}}}}}},"callbacks":{"unauthorized-attempt":{"when":[{"patternRef":"authz-and-rl-required"},{"selector":"auth.authorization.admin-or-privileged","operator":"neq","value":"true"}],"http":{"url":"http://events/unauthorized","method":"POST","body":{"value":null,"selector":"\\{\"identity\":{auth.identity},\"request-id\":{request.id}\\}"},"contentType":"application/json","credentials":{"authorizationHeader":{}}}}}}`, routeHost)))
		}, testTimeOut)

		It("Succeeds when AuthScheme is not defined", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.CommonSpec().AuthScheme = nil
			})

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("Complex HTTPRoute with multiple rules and hostnames", func() {
		var (
			host1 = randomHostFromGWHost()
			host2 = randomHostFromGWHost()
		)

		BeforeEach(func(ctx SpecContext) {
			route := tests.BuildMultipleRulesHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{host1, host2})
			err := k8sClient.Create(ctx, route)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())
		})

		It("Attaches simple policy to the HTTPRoute", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: controllers.AuthConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}).WithContext(ctx).Should(BeTrue())
			logf.Log.V(1).Info("authConfig.Spec", "hosts", authConfig.Spec.Hosts, "conditions", authConfig.Spec.Conditions)
			Expect(authConfig.Spec.Hosts).To(Equal([]string{host1, host2}))
			Expect(authConfig.Spec.Conditions).To(HaveLen(1))
			Expect(authConfig.Spec.Conditions[0].Any).To(HaveLen(2))        // 2 HTTPRouteRules in the HTTPRoute
			Expect(authConfig.Spec.Conditions[0].Any[0].Any).To(HaveLen(2)) // 2 HTTPRouteMatches in the 1st HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal("POST"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Value).To(Equal("DELETE"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the 2nd HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Value).To(Equal("GET"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Value).To(Equal("/private.*"))
		}, testTimeOut)

		It("Attaches policy with top-level route selectors to the HTTPRoute", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.CommonSpec().RouteSelectors = []api.RouteSelector{
					{ // Selects: POST|DELETE *.admin.toystore.com/admin*
						Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
							{
								Path: &gatewayapiv1alpha2.HTTPPathMatch{
									Type:  ptr.To(gatewayapiv1alpha2.PathMatchType("PathPrefix")),
									Value: ptr.To("/admin"),
								},
							},
						},
						Hostnames: []gatewayapiv1.Hostname{gatewayapiv1.Hostname(host2)},
					},
					{ // Selects: GET /private*
						Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
							{
								Path: &gatewayapiv1alpha2.HTTPPathMatch{
									Type:  ptr.To(gatewayapiv1alpha2.PathMatchType("PathPrefix")),
									Value: ptr.To("/private"),
								},
							},
						},
					},
				}
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: controllers.AuthConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}).WithContext(ctx).Should(BeTrue())
			logf.Log.V(1).Info("authConfig.Spec", "hosts", authConfig.Spec.Hosts, "conditions", authConfig.Spec.Conditions)
			Expect(authConfig.Spec.Hosts).To(Equal([]string{host1, host2}))
			Expect(authConfig.Spec.Conditions).To(HaveLen(1))
			Expect(authConfig.Spec.Conditions[0].Any).To(HaveLen(2))        // 2 HTTPRouteRules in the HTTPRoute
			Expect(authConfig.Spec.Conditions[0].Any[0].Any).To(HaveLen(2)) // 2 HTTPRouteMatches in the 1st HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All).To(HaveLen(3))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("request.host"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal(strings.Replace(host2, ".", `\.`, -1)))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("POST"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[2].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[2].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[2].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All).To(HaveLen(3))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Selector).To(Equal("request.host"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Value).To(Equal(strings.Replace(host2, ".", `\.`, -1)))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Value).To(Equal("DELETE"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[2].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[2].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[2].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the 2nd HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Value).To(Equal("GET"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Value).To(Equal("/private.*"))
		}, testTimeOut)

		It("Attaches policy with config-level route selectors to the HTTPRoute", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				config := policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"]
				config.RouteSelectors = []api.RouteSelector{
					{ // Selects: POST|DELETE *.admin.toystore.com/admin*
						Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
							{
								Path: &gatewayapiv1alpha2.HTTPPathMatch{
									Type:  ptr.To(gatewayapiv1alpha2.PathMatchType("PathPrefix")),
									Value: ptr.To("/admin"),
								},
							},
						},
						Hostnames: []gatewayapiv1.Hostname{gatewayapiv1.Hostname(host2)},
					},
				}
				policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"] = config
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: controllers.AuthConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}).WithContext(ctx).Should(BeTrue())
			apiKeyConditions := authConfig.Spec.Authentication["apiKey"].Conditions
			logf.Log.V(1).Info("authConfig.Spec", "hosts", authConfig.Spec.Hosts, "conditions", authConfig.Spec.Conditions, "apiKey conditions", apiKeyConditions)
			Expect(authConfig.Spec.Hosts).To(Equal([]string{host1, host2}))
			Expect(authConfig.Spec.Conditions).To(HaveLen(1))
			Expect(authConfig.Spec.Conditions[0].Any).To(HaveLen(2))        // 2 HTTPRouteRules in the HTTPRoute
			Expect(authConfig.Spec.Conditions[0].Any[0].Any).To(HaveLen(2)) // 2 HTTPRouteMatches in the 1st HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal("POST"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Value).To(Equal("DELETE"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the 2nd HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Value).To(Equal("GET"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Value).To(Equal("/private.*"))
			Expect(apiKeyConditions).To(HaveLen(1))
			Expect(apiKeyConditions[0].Any).To(HaveLen(1))        // 1 HTTPRouteRule selected from the HTTPRoute
			Expect(apiKeyConditions[0].Any[0].Any).To(HaveLen(2)) // 2 HTTPRouteMatches in the HTTPRouteRule
			Expect(apiKeyConditions[0].Any[0].Any[0].All).To(HaveLen(3))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[0].Selector).To(Equal("request.host"))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[0].Value).To(Equal(strings.Replace(host2, ".", `\.`, -1)))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[1].Selector).To(Equal("request.method"))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[1].Value).To(Equal("POST"))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[2].Selector).To(Equal(`request.url_path`))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[2].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[2].Value).To(Equal("/admin.*"))
			Expect(apiKeyConditions[0].Any[0].Any[1].All).To(HaveLen(3))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[0].Selector).To(Equal("request.host"))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[0].Value).To(Equal(strings.Replace(host2, ".", `\.`, -1)))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[1].Selector).To(Equal("request.method"))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[1].Value).To(Equal("DELETE"))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[2].Selector).To(Equal(`request.url_path`))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[2].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[2].Value).To(Equal("/admin.*"))
		}, testTimeOut)

		It("Mixes route selectors into other conditions", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				config := policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"]
				config.RouteSelectors = []api.RouteSelector{
					{ // Selects: GET /private*
						Matches: []gatewayapiv1.HTTPRouteMatch{
							{
								Path: &gatewayapiv1.HTTPPathMatch{
									Type:  ptr.To(gatewayapiv1.PathMatchType("PathPrefix")),
									Value: ptr.To("/private"),
								},
								Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
							},
						},
					},
				}
				config.Conditions = []authorinoapi.PatternExpressionOrRef{
					{
						PatternExpression: authorinoapi.PatternExpression{
							Selector: "context.source.address.Address.SocketAddress.address",
							Operator: authorinoapi.PatternExpressionOperator("matches"),
							Value:    `192\.168\.0\..*`,
						},
					},
				}
				policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"] = config
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: controllers.AuthConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}).WithContext(ctx).Should(BeTrue())
			apiKeyConditions := authConfig.Spec.Authentication["apiKey"].Conditions
			logf.Log.V(1).Info("authConfig.Spec", "hosts", authConfig.Spec.Hosts, "conditions", authConfig.Spec.Conditions, "apiKey conditions", apiKeyConditions)
			Expect(authConfig.Spec.Hosts).To(Equal([]string{host1, host2}))
			Expect(authConfig.Spec.Conditions).To(HaveLen(1))
			Expect(authConfig.Spec.Conditions[0].Any).To(HaveLen(2))        // 2 HTTPRouteRules in the HTTPRoute
			Expect(authConfig.Spec.Conditions[0].Any[0].Any).To(HaveLen(2)) // 2 HTTPRouteMatches in the 1st HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal("POST"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Value).To(Equal("DELETE"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the 2nd HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Value).To(Equal("GET"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Value).To(Equal("/private.*"))
			Expect(apiKeyConditions).To(HaveLen(2)) // 1 existed condition + 1 HTTPRouteRule selected from the HTTPRoute
			Expect(apiKeyConditions[0].Selector).To(Equal("context.source.address.Address.SocketAddress.address"))
			Expect(apiKeyConditions[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(apiKeyConditions[0].Value).To(Equal(`192\.168\.0\..*`))
			Expect(apiKeyConditions[1].Any).To(HaveLen(1))        // 1 HTTPRouteRule selected from the HTTPRoute
			Expect(apiKeyConditions[1].Any[0].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the HTTPRouteRule
			Expect(apiKeyConditions[1].Any[0].Any[0].All).To(HaveLen(2))
			Expect(apiKeyConditions[1].Any[0].Any[0].All[0].Selector).To(Equal("request.method"))
			Expect(apiKeyConditions[1].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(apiKeyConditions[1].Any[0].Any[0].All[0].Value).To(Equal("GET"))
			Expect(apiKeyConditions[1].Any[0].Any[0].All[1].Selector).To(Equal(`request.url_path`))
			Expect(apiKeyConditions[1].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(apiKeyConditions[1].Any[0].Any[0].All[1].Value).To(Equal("/private.*"))
		}, testTimeOut)
	})

	Context("AuthPolicy accepted condition reasons", func() {
		assertAcceptedCondFalseAndEnforcedCondNil := func(ctx context.Context, policy *api.AuthPolicy, reason, message string) func() bool {
			return func() bool {
				existingPolicy := &api.AuthPolicy{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), existingPolicy)
				if err != nil {
					return false
				}
				acceptedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				if acceptedCond == nil {
					return false
				}

				acceptedCondMatch := acceptedCond.Status == metav1.ConditionFalse && acceptedCond.Reason == reason && acceptedCond.Message == message

				enforcedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(kuadrant.PolicyReasonEnforced))
				enforcedCondMatch := enforcedCond == nil

				return acceptedCondMatch && enforcedCondMatch
			}
		}

		// Accepted reason is already tested generally by the existing tests

		It("Target not found reason", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(assertAcceptedCondFalseAndEnforcedCondNil(ctx, policy, string(gatewayapiv1alpha2.PolicyReasonTargetNotFound),
				fmt.Sprintf("AuthPolicy target %s was not found", TestHTTPRouteName))).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Conflict reason", func(ctx SpecContext) {
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err := k8sClient.Create(ctx, route)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			policy := policyFactory()
			err = k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			policy2 := policyFactory(func(policy *api.AuthPolicy) {
				policy.Name = "conflicting-ap"
			})
			err = k8sClient.Create(ctx, policy2)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy2).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(assertAcceptedCondFalseAndEnforcedCondNil(ctx, policy2, string(gatewayapiv1alpha2.PolicyReasonConflicted),
				fmt.Sprintf("AuthPolicy is conflicted by %[1]v/toystore: the gateway.networking.k8s.io/v1, Kind=HTTPRoute target %[1]v/toystore-route is already referenced by policy %[1]v/toystore", testNamespace),
			)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Invalid reason", func(ctx SpecContext) {
			otherNamespace := tests.CreateNamespace(ctx, testClient())
			defer tests.DeleteNamespaceCallback(ctx, testClient(), otherNamespace)()

			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Namespace = otherNamespace // create the policy in a different namespace than the target
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
				policy.Spec.TargetRef.Namespace = ptr.To(gatewayapiv1.Namespace(testNamespace))
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())

			Eventually(assertAcceptedCondFalseAndEnforcedCondNil(ctx, policy, string(gatewayapiv1alpha2.PolicyReasonInvalid), fmt.Sprintf("AuthPolicy target is invalid: invalid targetRef.Namespace %s. Currently only supporting references to the same namespace", testNamespace))).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("AuthPolicy enforced condition reasons", func() {
		assertAcceptedCondTrueAndEnforcedCond := func(ctx context.Context, policy *api.AuthPolicy, conditionStatus metav1.ConditionStatus, reason, message string) func() bool {
			return func() bool {
				existingPolicy := &api.AuthPolicy{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), existingPolicy)
				if err != nil {
					return false
				}
				acceptedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				if acceptedCond == nil {
					return false
				}

				acceptedCondMatch := acceptedCond.Status == metav1.ConditionTrue && acceptedCond.Reason == string(gatewayapiv1alpha2.PolicyReasonAccepted)

				enforcedCond := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(kuadrant.PolicyReasonEnforced))
				if enforcedCond == nil {
					return false
				}
				enforcedCondMatch := enforcedCond.Status == conditionStatus && enforcedCond.Reason == reason && enforcedCond.Message == message

				return acceptedCondMatch && enforcedCondMatch
			}
		}

		BeforeEach(func(ctx SpecContext) {
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err := k8sClient.Create(ctx, route)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())
		})

		It("Enforced reason", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			Eventually(assertAcceptedCondTrueAndEnforcedCond(ctx, policy, metav1.ConditionTrue, string(kuadrant.PolicyReasonEnforced),
				"AuthPolicy has been successfully enforced")).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Overridden reason - Attaches policy to the Gateway while having other policies attached to all HTTPRoutes", func(ctx SpecContext) {
			routePolicy := policyFactory()

			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check route policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			// attach policy to the gatewaay
			gwPolicy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
			})

			err = k8sClient.Create(ctx, gwPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(gwPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), gwPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(
				assertAcceptedCondTrueAndEnforcedCond(ctx, gwPolicy, metav1.ConditionFalse, string(kuadrant.PolicyReasonOverridden),
					fmt.Sprintf("AuthPolicy is overridden by [%s/%s]", testNamespace, routePolicy.Name))).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: controllers.AuthConfigName(client.ObjectKeyFromObject(gwPolicy)), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, &authorinoapi.AuthConfig{})
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())

			// GW Policy should go back to being enforced when a HTTPRoute with no AP attached becomes available
			route2 := tests.BuildBasicHttpRoute("route2", TestGatewayName, testNamespace, []string{randomHostFromGWHost()})

			err = k8sClient.Create(ctx, route2)
			Expect(err).ToNot(HaveOccurred())

			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gwPolicy)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("AuthPolicies configured with overrides", func() {
		BeforeEach(func(ctx SpecContext) {
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err := k8sClient.Create(ctx, route)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())
		})

		It("Gateway AuthPolicy has overrides and Route AuthPolicy is added.", func(ctx SpecContext) {
			gatewayPolicy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.Overrides = &api.AuthPolicyCommonSpec{}
				policy.Spec.Defaults = nil
				policy.Spec.Overrides.AuthScheme = testBasicAuthScheme()
				policy.Spec.Overrides.AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})

			err := k8sClient.Create(ctx, gatewayPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(gatewayPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())

			routePolicy := policyFactory()
			err = k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndNotEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), client.ObjectKeyFromObject(routePolicy), kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", client.ObjectKeyFromObject(gatewayPolicy)))).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Route AuthPolicy exists and Gateway AuthPolicy with overrides is added.", func(ctx SpecContext) {
			routePolicy := policyFactory()
			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			gatewayPolicy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.Overrides = &api.AuthPolicyCommonSpec{}
				policy.Spec.Defaults = nil
				policy.Spec.Overrides.AuthScheme = testBasicAuthScheme()
				policy.Spec.Overrides.AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})

			err = k8sClient.Create(ctx, gatewayPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(gatewayPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeFalse())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), client.ObjectKeyFromObject(routePolicy), kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", client.ObjectKeyFromObject(gatewayPolicy)))).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Route AuthPolicy exists and Gateway AuthPolicy with overrides is removed.", func(ctx SpecContext) {
			routePolicy := policyFactory()
			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			gatewayPolicy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.Overrides = &api.AuthPolicyCommonSpec{}
				policy.Spec.Defaults = nil
				policy.Spec.Overrides.AuthScheme = testBasicAuthScheme()
				policy.Spec.Overrides.AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})

			err = k8sClient.Create(ctx, gatewayPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(gatewayPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeFalse())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), client.ObjectKeyFromObject(routePolicy), kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", client.ObjectKeyFromObject(gatewayPolicy)))).WithContext(ctx).Should(BeTrue())

			err = k8sClient.Delete(ctx, gatewayPolicy)
			logf.Log.V(1).Info("Deleting AuthPolicy", "key", client.ObjectKeyFromObject(gatewayPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Route and Gateway AuthPolicies exist. Gateway AuthPolicy updated to include overrides.", func(ctx SpecContext) {
			routePolicy := policyFactory()
			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			gatewayPolicy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})

			err = k8sClient.Create(ctx, gatewayPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(gatewayPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndNotEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), client.ObjectKeyFromObject(gatewayPolicy), kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", client.ObjectKeyFromObject(routePolicy)))).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			Eventually(func() bool {
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(gatewayPolicy), gatewayPolicy)
				if err != nil {
					return false
				}
				gatewayPolicy.Spec.Overrides = &api.AuthPolicyCommonSpec{}
				gatewayPolicy.Spec.Defaults = nil
				gatewayPolicy.Spec.Overrides.AuthScheme = testBasicAuthScheme()
				gatewayPolicy.Spec.Overrides.AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
				err = k8sClient.Update(ctx, gatewayPolicy)
				logf.Log.V(1).Info("Updating AuthPolicy", "key", client.ObjectKeyFromObject(gatewayPolicy).String(), "error", err)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeFalse())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), client.ObjectKeyFromObject(routePolicy), kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", client.ObjectKeyFromObject(gatewayPolicy)))).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Route and Gateway AuthPolicies exist. Gateway AuthPolicy updated to remove overrides.", func(ctx SpecContext) {
			routePolicy := policyFactory()
			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			gatewayPolicy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.Overrides = &api.AuthPolicyCommonSpec{}
				policy.Spec.Defaults = nil
				policy.Spec.Overrides.AuthScheme = testBasicAuthScheme()
				policy.Spec.Overrides.AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})

			err = k8sClient.Create(ctx, gatewayPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(gatewayPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeFalse())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), client.ObjectKeyFromObject(routePolicy), kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", client.ObjectKeyFromObject(gatewayPolicy)))).WithContext(ctx).Should(BeTrue())

			Eventually(func() bool {
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(gatewayPolicy), gatewayPolicy)
				if err != nil {
					return false
				}
				gatewayPolicy.Spec.Overrides = nil
				gatewayPolicy.Spec.CommonSpec().AuthScheme = testBasicAuthScheme()
				gatewayPolicy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
				err = k8sClient.Update(ctx, gatewayPolicy)
				logf.Log.V(1).Info("Updating AuthPolicy", "key", client.ObjectKeyFromObject(gatewayPolicy).String(), "error", err)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndNotEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), client.ObjectKeyFromObject(gatewayPolicy), kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", client.ObjectKeyFromObject(routePolicy)))).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Blocks creation of AuthPolicies with overrides targeting HTTPRoutes", func(ctx SpecContext) {
			routePolicy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Overrides = &api.AuthPolicyCommonSpec{}
				policy.Spec.Defaults = nil
				policy.Spec.Overrides.AuthScheme = testBasicAuthScheme()
			})
			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Overrides are not allowed for policies targeting a HTTPRoute resource"))
		}, testTimeOut)
	})
})

var _ = Describe("AuthPolicy CEL Validations", func() {
	const (
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)

	var testNamespace string

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	policyFactory := func(mutateFns ...func(policy *api.AuthPolicy)) *api.AuthPolicy {
		policy := &api.AuthPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-policy",
				Namespace: testNamespace,
			},
			Spec: api.AuthPolicySpec{
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
		})

		It("Valid policy targeting Gateway", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(BeNil())
		})

		It("Invalid Target Ref Group", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.TargetRef.Group = "not-gateway.networking.k8s.io"
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'")).To(BeTrue())
		})

		It("Invalid Target Ref Kind", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.TargetRef.Kind = "TCPRoute"
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'")).To(BeTrue())
		})
	})

	Context("Defaults mutual exclusivity validation", func() {
		It("Valid when only implicit defaults are used", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.AuthScheme = testBasicAuthScheme()
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		})

		It("Valid when only explicit defaults are used", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{
					AuthScheme: testBasicAuthScheme(),
				}
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		})

		It("Invalid when both implicit and explicit defaults are used - authScheme", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.AuthScheme = testBasicAuthScheme()
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Implicit and explicit defaults are mutually exclusive")).To(BeTrue())
		})

		It("Invalid when both implicit and explicit defaults are used - routeSelectors", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.RouteSelectors = []api.RouteSelector{
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
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Implicit and explicit defaults are mutually exclusive")).To(BeTrue())
		})

		It("Invalid when both implicit and explicit defaults are used - namedPatterns", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.NamedPatterns = map[string]authorinoapi.PatternExpressions{
					"internal-source": []authorinoapi.PatternExpression{
						{
							Selector: "source.ip",
							Operator: authorinoapi.PatternExpressionOperator("matches"),
							Value:    `192\.168\..*`,
						},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Implicit and explicit defaults are mutually exclusive")).To(BeTrue())
		})

		It("Invalid when both implicit and explicit defaults are used - conditions", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.Conditions = []authorinoapi.PatternExpressionOrRef{
					{
						PatternRef: authorinoapi.PatternRef{
							Name: "internal-source",
						},
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

		var (
			routeSelector = api.RouteSelector{
				Hostnames: []gatewayapiv1.Hostname{"*.foo.io"},
				Matches: []gatewayapiv1.HTTPRouteMatch{
					{
						Path: &gatewayapiv1.HTTPPathMatch{
							Value: ptr.To("/foo"),
						},
					},
				},
			}
			routeSelectors     = []api.RouteSelector{routeSelector}
			commonAuthRuleSpec = api.CommonAuthRuleSpec{RouteSelectors: routeSelectors}
		)

		policyFactory := func(mutateFn func(policy *api.AuthPolicy)) *api.AuthPolicy {
			policy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-policy",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "Gateway",
						Name:  "my-gw",
					},
				},
			}

			if mutateFn != nil {
				mutateFn(policy)
			}

			return policy
		}
		It("invalid usage of top-level route selectors with a gateway targetRef", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.RouteSelectors = routeSelectors
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), gateWayRouteSelectorErrorMessage)).To(BeTrue())
		})

		It("invalid usage of top-level route selectors with a gateway targetRef - defaults", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.CommonSpec().RouteSelectors = routeSelectors
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), gateWayRouteSelectorErrorMessage)).To(BeTrue())
		})

		It("invalid usage of config-level route selectors with a gateway targetRef - authentication", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.AuthScheme = &api.AuthSchemeSpec{
					Authentication: map[string]api.AuthenticationSpec{
						"my-rule": {
							AuthenticationSpec: authorinoapi.AuthenticationSpec{
								AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
									AnonymousAccess: &authorinoapi.AnonymousAccessSpec{},
								},
							},
							CommonAuthRuleSpec: commonAuthRuleSpec,
						},
					},
				}
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), gateWayRouteSelectorErrorMessage)).To(BeTrue())
		})

		It("invalid usage of config-level route selectors with a gateway targetRef - authentication - defaults", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.CommonSpec().AuthScheme = &api.AuthSchemeSpec{
					Authentication: map[string]api.AuthenticationSpec{
						"my-rule": {
							AuthenticationSpec: authorinoapi.AuthenticationSpec{
								AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
									AnonymousAccess: &authorinoapi.AnonymousAccessSpec{},
								},
							},
							CommonAuthRuleSpec: commonAuthRuleSpec,
						},
					},
				}
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), gateWayRouteSelectorErrorMessage)).To(BeTrue())
		})

		It("invalid usage of config-level route selectors with a gateway targetRef - metadata", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.AuthScheme = &api.AuthSchemeSpec{
					Metadata: map[string]api.MetadataSpec{
						"my-metadata": {
							CommonAuthRuleSpec: commonAuthRuleSpec,
						},
					},
				}
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), gateWayRouteSelectorErrorMessage)).To(BeTrue())
		})

		It("invalid usage of config-level route selectors with a gateway targetRef - metadata - defaults", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.CommonSpec().AuthScheme = &api.AuthSchemeSpec{
					Metadata: map[string]api.MetadataSpec{
						"my-metadata": {
							CommonAuthRuleSpec: commonAuthRuleSpec,
						},
					},
				}
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), gateWayRouteSelectorErrorMessage)).To(BeTrue())
		})

		It("invalid usage of config-level route selectors with a gateway targetRef - authorization", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.AuthScheme = &api.AuthSchemeSpec{
					Authorization: map[string]api.AuthorizationSpec{
						"my-authZ": {
							CommonAuthRuleSpec: commonAuthRuleSpec,
						},
					},
				}
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), gateWayRouteSelectorErrorMessage)).To(BeTrue())
		})

		It("invalid usage of config-level route selectors with a gateway targetRef - authorization - defaults", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.CommonSpec().AuthScheme = &api.AuthSchemeSpec{
					Authorization: map[string]api.AuthorizationSpec{
						"my-authZ": {
							CommonAuthRuleSpec: commonAuthRuleSpec,
						},
					},
				}
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), gateWayRouteSelectorErrorMessage)).To(BeTrue())
		})

		It("invalid usage of config-level route selectors with a gateway targetRef - response success headers", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.AuthScheme = &api.AuthSchemeSpec{
					Response: &api.ResponseSpec{
						Success: api.WrappedSuccessResponseSpec{
							Headers: map[string]api.HeaderSuccessResponseSpec{
								"header": {
									SuccessResponseSpec: api.SuccessResponseSpec{
										CommonAuthRuleSpec: commonAuthRuleSpec,
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

		It("invalid usage of config-level route selectors with a gateway targetRef - response success headers - defaults", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.CommonSpec().AuthScheme = &api.AuthSchemeSpec{
					Response: &api.ResponseSpec{
						Success: api.WrappedSuccessResponseSpec{
							Headers: map[string]api.HeaderSuccessResponseSpec{
								"header": {
									SuccessResponseSpec: api.SuccessResponseSpec{
										CommonAuthRuleSpec: commonAuthRuleSpec,
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

		It("invalid usage of config-level route selectors with a gateway targetRef - response success dynamic metadata", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.CommonSpec().AuthScheme = &api.AuthSchemeSpec{
					Response: &api.ResponseSpec{
						Success: api.WrappedSuccessResponseSpec{
							DynamicMetadata: map[string]api.SuccessResponseSpec{
								"header": {
									CommonAuthRuleSpec: commonAuthRuleSpec,
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

		It("invalid usage of config-level route selectors with a gateway targetRef - response success dynamic metadata - defaults", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.CommonSpec().AuthScheme = &api.AuthSchemeSpec{
					Response: &api.ResponseSpec{
						Success: api.WrappedSuccessResponseSpec{
							DynamicMetadata: map[string]api.SuccessResponseSpec{
								"header": {
									CommonAuthRuleSpec: commonAuthRuleSpec,
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

		It("invalid usage of config-level route selectors with a gateway targetRef - callbacks", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.AuthScheme = &api.AuthSchemeSpec{
					Callbacks: map[string]api.CallbackSpec{
						"callback": {
							CallbackSpec: authorinoapi.CallbackSpec{
								CallbackMethodSpec: authorinoapi.CallbackMethodSpec{
									Http: &authorinoapi.HttpEndpointSpec{
										Url: "test.com",
									},
								},
							},
							CommonAuthRuleSpec: commonAuthRuleSpec,
						},
					},
				}
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), gateWayRouteSelectorErrorMessage)).To(BeTrue())
		})

		It("invalid usage of config-level route selectors with a gateway targetRef - callbacks - defaults", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.CommonSpec().AuthScheme = &api.AuthSchemeSpec{
					Callbacks: map[string]api.CallbackSpec{
						"callback": {
							CallbackSpec: authorinoapi.CallbackSpec{
								CallbackMethodSpec: authorinoapi.CallbackMethodSpec{
									Http: &authorinoapi.HttpEndpointSpec{
										Url: "test.com",
									},
								},
							},
							CommonAuthRuleSpec: commonAuthRuleSpec,
						},
					},
				}
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), gateWayRouteSelectorErrorMessage)).To(BeTrue())
		})

		It("invalid usage of root level route selectors for HTTPRoute - max number is 15", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = "my-route"
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.CommonSpec().RouteSelectors = []api.RouteSelector{
					routeSelector,
					routeSelector,
					routeSelector,
					routeSelector,
					routeSelector,
					routeSelector,
					routeSelector,
					routeSelector,
					routeSelector,
					routeSelector,
					routeSelector,
					routeSelector,
					routeSelector,
					routeSelector,
					routeSelector,
					routeSelector,
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error(), ContainSubstring("Too many: 16: must have at most 15 items"))
		})

		It("invalid usage of config level route selectors for HTTPRoute - max number is 8", func(ctx SpecContext) {
			policy := policyFactory(func(policy *api.AuthPolicy) {
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = "my-route"
				policy.Spec.Defaults = &api.AuthPolicyCommonSpec{}
				policy.Spec.CommonSpec().AuthScheme = &api.AuthSchemeSpec{
					Callbacks: map[string]api.CallbackSpec{
						"callback": {
							CallbackSpec: authorinoapi.CallbackSpec{
								CallbackMethodSpec: authorinoapi.CallbackMethodSpec{
									Http: &authorinoapi.HttpEndpointSpec{
										Url: "test.com",
									},
								},
							},
							CommonAuthRuleSpec: api.CommonAuthRuleSpec{
								RouteSelectors: []api.RouteSelector{
									routeSelector,
									routeSelector,
									routeSelector,
									routeSelector,
									routeSelector,
									routeSelector,
									routeSelector,
									routeSelector,
									routeSelector,
								},
							},
						},
					},
				}
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(BeNil())
			Expect(err.Error(), ContainSubstring("Too many: 9: must have at most 8 items"))
		})
	})
})

func testBasicAuthScheme() *api.AuthSchemeSpec {
	return &api.AuthSchemeSpec{
		Authentication: map[string]api.AuthenticationSpec{
			"apiKey": {
				AuthenticationSpec: authorinoapi.AuthenticationSpec{
					AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
						ApiKey: &authorinoapi.ApiKeyAuthenticationSpec{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "toystore",
								},
							},
						},
					},
					Credentials: authorinoapi.Credentials{
						AuthorizationHeader: &authorinoapi.Prefixed{
							Prefix: "APIKEY",
						},
					},
				},
			},
		},
	}
}
