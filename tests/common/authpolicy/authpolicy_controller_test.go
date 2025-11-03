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

	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/machinery"
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

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("AuthPolicy controller", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gateway       *gatewayapiv1.Gateway
		gatewayClass  *gatewayapiv1.GatewayClass
		gwHost        = fmt.Sprintf("*.toystore-%s.com", rand.String(6))
	)

	authConfigKeyForPath := func(httpRoute *gatewayapiv1.HTTPRoute, httpRouteRuleIndex int) types.NamespacedName {
		mGateway := &machinery.Gateway{Gateway: gateway}
		mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: httpRoute}
		authConfigName := controllers.AuthConfigNameForPath(kuadrantv1.PathID([]machinery.Targetable{
			&machinery.GatewayClass{GatewayClass: gatewayClass},
			mGateway,
			&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
			mHTTPRoute,
			&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRoute.Spec.Rules[httpRouteRuleIndex], Name: "rule-1"},
		}))
		return types.NamespacedName{Name: authConfigName, Namespace: kuadrantInstallationNS}
	}

	fetchReadyAuthConfig := func(ctx context.Context, httpRoute *gatewayapiv1.HTTPRoute, httpRouteRuleIndex int, authConfig *authorinov1beta3.AuthConfig) func() bool {
		authConfigKey := authConfigKeyForPath(httpRoute, httpRouteRuleIndex)
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
				Name:      "toystore",
				Namespace: testNamespace,
			},
			Spec: kuadrantv1.AuthPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "HTTPRoute",
						Name:  TestHTTPRouteName,
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

	Context("Basic HTTPRoute", func() {
		var (
			httpRoute *gatewayapiv1.HTTPRoute
			routeHost = randomHostFromGWHost()
		)

		BeforeEach(func(ctx SpecContext) {
			httpRoute = tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{routeHost})
			err := k8sClient.Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())
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
			Eventually(fetchReadyAuthConfig(ctx, httpRoute, 0, authConfig)).WithContext(ctx).Should(BeTrue())
			Expect(authConfig.Spec.Authentication).To(HaveLen(1))
			Expect(authConfig.Spec.Authentication).To(HaveKeyWithValue("apiKey", policy.Spec.Proper().AuthScheme.Authentication["apiKey"].AuthenticationSpec))

			// create other route
			otherHTTPRoute := tests.BuildBasicHttpRoute("other-route", TestGatewayName, testNamespace, []string{routeHost})
			err = k8sClient.Create(ctx, otherHTTPRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, k8sClient, client.ObjectKeyFromObject(otherHTTPRoute))).WithContext(ctx).Should(BeTrue())

			// check authorino other authconfig
			otherAuthConfig := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfig(ctx, otherHTTPRoute, 0, otherAuthConfig)).WithContext(ctx).Should(BeTrue())
			Expect(otherAuthConfig.Spec.Authentication).To(HaveLen(1))
			Expect(otherAuthConfig.Spec.Authentication).To(HaveKeyWithValue("apiKey", policy.Spec.Proper().AuthScheme.Authentication["apiKey"].AuthenticationSpec))
		}, testTimeOut)

		It("Attaches policy to the HTTPRoute", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfig := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfig(ctx, httpRoute, 0, authConfig)).WithContext(ctx).Should(BeTrue())
			Expect(authConfig.Spec.Authentication).To(HaveLen(1))
			Expect(authConfig.Spec.Authentication).To(HaveKeyWithValue("apiKey", policy.Spec.Proper().AuthScheme.Authentication["apiKey"].AuthenticationSpec))
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
			err = k8sClient.Create(ctx, otherRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(otherRoute))).WithContext(ctx).Should(BeTrue())

			// attach policy to the gatewaay
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

			// check authorino authconfig
			authConfig := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfig(ctx, httpRoute, 0, authConfig)).WithContext(ctx).Should(BeTrue())
			Expect(authConfig.Spec.Authentication).To(HaveLen(1))
			Expect(authConfig.Spec.Authentication).To(HaveKeyWithValue("apiKey", routePolicy.Spec.Proper().AuthScheme.Authentication["apiKey"].AuthenticationSpec))

			otherAuthConfig := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfig(ctx, otherRoute, 0, otherAuthConfig)).WithContext(ctx).Should(BeTrue())
			Expect(otherAuthConfig.Spec.Authentication).To(HaveLen(1))
			Expect(otherAuthConfig.Spec.Authentication).To(HaveKeyWithValue("apiKey", gwPolicy.Spec.Proper().AuthScheme.Authentication["apiKey"].AuthenticationSpec))
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
			authConfigKey := authConfigKeyForPath(httpRoute, 0)
			Eventually(func() bool {
				err := k8sClient.Get(ctx, authConfigKey, &authorinov1beta3.AuthConfig{})
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Maps to all fields of the AuthConfig", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.Proper().NamedPatterns = map[string]kuadrantv1.MergeablePatternExpressions{
					"internal-source": {
						PatternExpressions: []authorinov1beta3.PatternExpression{
							{
								Selector: "source.ip",
								Operator: authorinov1beta3.PatternExpressionOperator("matches"),
								Value:    `192\.168\..*`,
							},
						},
					},
					"authz-and-rl-required": {
						PatternExpressions: []authorinov1beta3.PatternExpression{
							{
								Selector: "source.ip",
								Operator: authorinov1beta3.PatternExpressionOperator("neq"),
								Value:    "192.168.0.10",
							},
						},
					},
				}
				policy.Spec.Proper().MergeableWhenPredicates = kuadrantv1.MergeableWhenPredicates{
					Predicates: kuadrantv1.WhenPredicates{
						{Predicate: `source.ip.matches("^192\\.168\\..*")`},
					},
				}
				policy.Spec.Proper().AuthScheme = &kuadrantv1.AuthSchemeSpec{
					Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
						"jwt": {
							AuthenticationSpec: authorinov1beta3.AuthenticationSpec{
								CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
									Conditions: []authorinov1beta3.PatternExpressionOrRef{
										{
											PatternExpression: authorinov1beta3.PatternExpression{
												Selector: `filter_metadata.envoy\.filters\.http\.jwt_authn|verified_jwt`,
												Operator: "neq",
												Value:    "",
											},
										},
									},
								},
								AuthenticationMethodSpec: authorinov1beta3.AuthenticationMethodSpec{
									Plain: &authorinov1beta3.PlainIdentitySpec{
										Selector: `filter_metadata.envoy\.filters\.http\.jwt_authn|verified_jwt`,
									},
								},
							},
						},
					},
					Metadata: map[string]kuadrantv1.MergeableMetadataSpec{
						"user-groups": {
							MetadataSpec: authorinov1beta3.MetadataSpec{
								CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
									Conditions: []authorinov1beta3.PatternExpressionOrRef{
										{
											PatternExpression: authorinov1beta3.PatternExpression{
												Selector: "auth.identity.admin",
												Operator: authorinov1beta3.PatternExpressionOperator("neq"),
												Value:    "true",
											},
										},
									},
								},
								MetadataMethodSpec: authorinov1beta3.MetadataMethodSpec{
									Http: &authorinov1beta3.HttpEndpointSpec{
										Url: "http://user-groups/username={auth.identity.username}",
									},
								},
							},
						},
					},
					Authorization: map[string]kuadrantv1.MergeableAuthorizationSpec{
						"admin-or-privileged": {
							AuthorizationSpec: authorinov1beta3.AuthorizationSpec{
								CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
									Conditions: []authorinov1beta3.PatternExpressionOrRef{
										{
											PatternRef: authorinov1beta3.PatternRef{
												Name: "authz-and-rl-required",
											},
										},
									},
								},
								AuthorizationMethodSpec: authorinov1beta3.AuthorizationMethodSpec{
									PatternMatching: &authorinov1beta3.PatternMatchingAuthorizationSpec{
										Patterns: []authorinov1beta3.PatternExpressionOrRef{
											{
												Any: []authorinov1beta3.UnstructuredPatternExpressionOrRef{
													{
														PatternExpressionOrRef: authorinov1beta3.PatternExpressionOrRef{
															PatternExpression: authorinov1beta3.PatternExpression{
																Selector: "auth.identity.admin",
																Operator: authorinov1beta3.PatternExpressionOperator("eq"),
																Value:    "true",
															},
														},
													},
													{
														PatternExpressionOrRef: authorinov1beta3.PatternExpressionOrRef{
															PatternExpression: authorinov1beta3.PatternExpression{
																Selector: "auth.metadata.user-groups",
																Operator: authorinov1beta3.PatternExpressionOperator("incl"),
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
					Response: &kuadrantv1.MergeableResponseSpec{
						Unauthenticated: &kuadrantv1.MergeableDenyWithSpec{
							DenyWithSpec: authorinov1beta3.DenyWithSpec{
								Message: &authorinov1beta3.ValueOrSelector{
									Value: k8sruntime.RawExtension{Raw: []byte(`"Missing verified JWT injected by the gateway"`)},
								},
							},
						},
						Unauthorized: &kuadrantv1.MergeableDenyWithSpec{
							DenyWithSpec: authorinov1beta3.DenyWithSpec{
								Message: &authorinov1beta3.ValueOrSelector{
									Value: k8sruntime.RawExtension{Raw: []byte(`"User must be admin or member of privileged group"`)},
								},
							},
						},
						Success: kuadrantv1.MergeableWrappedSuccessResponseSpec{
							Headers: map[string]kuadrantv1.MergeableHeaderSuccessResponseSpec{
								"x-username": {
									HeaderSuccessResponseSpec: authorinov1beta3.HeaderSuccessResponseSpec{
										SuccessResponseSpec: authorinov1beta3.SuccessResponseSpec{
											CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
												Conditions: []authorinov1beta3.PatternExpressionOrRef{
													{
														PatternExpression: authorinov1beta3.PatternExpression{
															Selector: "request.headers.x-propagate-username.@case:lower",
															Operator: authorinov1beta3.PatternExpressionOperator("matches"),
															Value:    "1|yes|true",
														},
													},
												},
											},
											AuthResponseMethodSpec: authorinov1beta3.AuthResponseMethodSpec{
												Plain: &authorinov1beta3.PlainAuthResponseSpec{
													Selector: "auth.identity.username",
												},
											},
										},
									},
								},
							},
							DynamicMetadata: map[string]kuadrantv1.MergeableSuccessResponseSpec{
								"x-auth-data": {
									SuccessResponseSpec: authorinov1beta3.SuccessResponseSpec{
										CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
											Conditions: []authorinov1beta3.PatternExpressionOrRef{
												{
													PatternRef: authorinov1beta3.PatternRef{
														Name: "authz-and-rl-required",
													},
												},
											},
										},
										AuthResponseMethodSpec: authorinov1beta3.AuthResponseMethodSpec{
											Json: &authorinov1beta3.JsonAuthResponseSpec{
												Properties: authorinov1beta3.NamedValuesOrSelectors{
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
					Callbacks: map[string]kuadrantv1.MergeableCallbackSpec{
						"unauthorized-attempt": {
							CallbackSpec: authorinov1beta3.CallbackSpec{
								CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
									Conditions: []authorinov1beta3.PatternExpressionOrRef{
										{
											PatternRef: authorinov1beta3.PatternRef{
												Name: "authz-and-rl-required",
											},
										},
										{
											PatternExpression: authorinov1beta3.PatternExpression{
												Selector: "auth.authorization.admin-or-privileged",
												Operator: authorinov1beta3.PatternExpressionOperator("neq"),
												Value:    "true",
											},
										},
									},
								},
								CallbackMethodSpec: authorinov1beta3.CallbackMethodSpec{
									Http: &authorinov1beta3.HttpEndpointSpec{
										Url:         "http://events/unauthorized",
										Method:      ptr.To(authorinov1beta3.HttpMethod("POST")),
										ContentType: authorinov1beta3.HttpContentType("application/json"),
										Body: &authorinov1beta3.ValueOrSelector{
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
			authConfig := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfig(ctx, httpRoute, 0, authConfig)).WithContext(ctx).Should(BeTrue())
			authConfigSpecAsJSON, _ := json.Marshal(authConfig.Spec)
			Expect(string(authConfigSpecAsJSON)).To(Equal(fmt.Sprintf(`{"hosts":["%s"],"patterns":{"authz-and-rl-required":[{"selector":"source.ip","operator":"neq","value":"192.168.0.10"}],"internal-source":[{"selector":"source.ip","operator":"matches","value":"192\\.168\\..*"}]},"authentication":{"jwt":{"when":[{"selector":"filter_metadata.envoy\\.filters\\.http\\.jwt_authn|verified_jwt","operator":"neq"}],"credentials":{},"plain":{"selector":"filter_metadata.envoy\\.filters\\.http\\.jwt_authn|verified_jwt"}}},"metadata":{"user-groups":{"when":[{"selector":"auth.identity.admin","operator":"neq","value":"true"}],"http":{"url":"http://user-groups/username={auth.identity.username}","method":"GET","contentType":"application/x-www-form-urlencoded","credentials":{}}}},"authorization":{"admin-or-privileged":{"when":[{"patternRef":"authz-and-rl-required"}],"patternMatching":{"patterns":[{"any":[{"selector":"auth.identity.admin","operator":"eq","value":"true"},{"selector":"auth.metadata.user-groups","operator":"incl","value":"privileged"}]}]}}},"response":{"unauthenticated":{"message":{"value":"Missing verified JWT injected by the gateway"}},"unauthorized":{"message":{"value":"User must be admin or member of privileged group"}},"success":{"headers":{"x-username":{"when":[{"selector":"request.headers.x-propagate-username.@case:lower","operator":"matches","value":"1|yes|true"}],"plain":{"value":null,"selector":"auth.identity.username"}}},"dynamicMetadata":{"x-auth-data":{"when":[{"patternRef":"authz-and-rl-required"}],"json":{"properties":{"groups":{"value":null,"selector":"auth.metadata.user-groups"},"username":{"value":null,"selector":"auth.identity.username"}}}}}}},"callbacks":{"unauthorized-attempt":{"when":[{"patternRef":"authz-and-rl-required"},{"selector":"auth.authorization.admin-or-privileged","operator":"neq","value":"true"}],"http":{"url":"http://events/unauthorized","method":"POST","body":{"value":null,"selector":"\\{\"identity\":{auth.identity},\"request-id\":{request.id}\\}"},"contentType":"application/json","credentials":{}}}}}`, authConfig.GetName())))
		}, testTimeOut)
	})

	Context("Complex HTTPRoute with multiple rules and hostnames", func() {
		var (
			httpRoute *gatewayapiv1.HTTPRoute
			host1     = randomHostFromGWHost()
			host2     = randomHostFromGWHost()
		)

		BeforeEach(func(ctx SpecContext) {
			httpRoute = tests.BuildMultipleRulesHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{host1, host2})
			err := k8sClient.Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())
		})

		It("Attaches simple policy to the HTTPRoute", func(ctx SpecContext) {
			policy := policyFactory()

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfigs
			authConfigPOST_DELETE_admin := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfig(ctx, httpRoute, 0, authConfigPOST_DELETE_admin)).WithContext(ctx).Should(BeTrue())

			authConfigGET_private := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfig(ctx, httpRoute, 1, authConfigGET_private)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("Invalid paths are ignored", func() {
		It("Should only create auth configs for valid paths", func(ctx SpecContext) {
			host1 := randomHostFromGWHost()
			host2 := randomHostFromGWHost()

			// Update GW with 2 listeners with direct hostnames
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)).To(Succeed())
				patch := client.MergeFrom(gateway.DeepCopy())
				gateway.Spec.Listeners = []gatewayapiv1.Listener{
					{
						Name:     "l1",
						Port:     gatewayapiv1.PortNumber(80),
						Protocol: "HTTP",
						Hostname: ptr.To(gatewayapiv1.Hostname(host1)),
					},
					{
						Name:     "l2",
						Port:     gatewayapiv1.PortNumber(80),
						Protocol: "HTTP",
						Hostname: ptr.To(gatewayapiv1.Hostname(host2)),
					},
				}
				g.Expect(k8sClient.Patch(ctx, gateway, patch)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			// Link HTTPRoute to gateway with only 1 hostname
			httpRoute := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{host1})
			httpRoute.Spec.Hostnames = []gatewayapiv1.Hostname{gatewayapiv1.Hostname(host1)}
			Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())

			// Create policy targeting gateway
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())
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
	})

	Context("AuthPolicy enforced condition reasons", func() {
		var httpRoute *gatewayapiv1.HTTPRoute

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
			httpRoute = tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err := k8sClient.Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())
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
			routePolicyKey := client.ObjectKeyFromObject(routePolicy)

			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", routePolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check route policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfig := &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfig(ctx, httpRoute, 0, authConfig)).WithContext(ctx).Should(BeTrue())
			Expect(authConfig.Spec.Authentication).To(HaveLen(1))
			Expect(authConfig.Spec.Authentication).To(HaveKeyWithValue("apiKey", routePolicy.Spec.Proper().AuthScheme.Authentication["apiKey"].AuthenticationSpec))

			// attach policy to the gatewaay
			gwPolicy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.Proper().AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["gateway"] = "yes"
			})
			gatewayPolicyKey := client.ObjectKeyFromObject(gwPolicy)
			err = k8sClient.Create(ctx, gwPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", gatewayPolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), gwPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(assertAcceptedCondTrueAndEnforcedCond(ctx, gwPolicy, metav1.ConditionFalse, string(kuadrant.PolicyReasonOverridden), fmt.Sprintf("AuthPolicy is overridden by [%s]", routePolicyKey.String()))).WithContext(ctx).Should(BeTrue())

			// check authorino authconfig
			authConfig = &authorinov1beta3.AuthConfig{}
			Eventually(fetchReadyAuthConfig(ctx, httpRoute, 0, authConfig)).WithContext(ctx).Should(BeTrue())
			Expect(authConfig.Spec.Authentication).To(HaveLen(1))
			Expect(authConfig.Spec.Authentication).To(HaveKeyWithValue("apiKey", routePolicy.Spec.Proper().AuthScheme.Authentication["apiKey"].AuthenticationSpec))

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
			gatewayPolicy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.Overrides = &kuadrantv1.MergeableAuthPolicySpec{}
				policy.Spec.Defaults = nil
				policy.Spec.Overrides.AuthScheme = tests.BuildBasicAuthScheme()
				policy.Spec.Overrides.AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})
			gatewayPolicyKey := client.ObjectKeyFromObject(gatewayPolicy)
			err := k8sClient.Create(ctx, gatewayPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", gatewayPolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())

			routePolicy := policyFactory()
			routePolicyKey := client.ObjectKeyFromObject(routePolicy)
			err = k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", routePolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndNotEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), routePolicyKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", gatewayPolicyKey.String()))).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Route AuthPolicy exists and Gateway AuthPolicy with overrides is added.", func(ctx SpecContext) {
			routePolicy := policyFactory()
			routePolicyKey := client.ObjectKeyFromObject(routePolicy)
			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", routePolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			gatewayPolicy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.Overrides = &kuadrantv1.MergeableAuthPolicySpec{}
				policy.Spec.Defaults = nil
				policy.Spec.Overrides.AuthScheme = tests.BuildBasicAuthScheme()
				policy.Spec.Overrides.AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})
			gatewayPolicyKey := client.ObjectKeyFromObject(gatewayPolicy)
			err = k8sClient.Create(ctx, gatewayPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", gatewayPolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeFalse())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), routePolicyKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", gatewayPolicyKey.String()))).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Route AuthPolicy exists and Gateway AuthPolicy with overrides is removed.", func(ctx SpecContext) {
			routePolicy := policyFactory()
			routePolicyKey := client.ObjectKeyFromObject(routePolicy)
			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", routePolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			gatewayPolicy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.Overrides = &kuadrantv1.MergeableAuthPolicySpec{}
				policy.Spec.Defaults = nil
				policy.Spec.Overrides.AuthScheme = tests.BuildBasicAuthScheme()
				policy.Spec.Overrides.AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})
			gatewayPolicyKey := client.ObjectKeyFromObject(gatewayPolicy)
			err = k8sClient.Create(ctx, gatewayPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", gatewayPolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeFalse())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), routePolicyKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", gatewayPolicyKey.String()))).WithContext(ctx).Should(BeTrue())

			err = k8sClient.Delete(ctx, gatewayPolicy)
			logf.Log.V(1).Info("Deleting AuthPolicy", "key", gatewayPolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Route and Gateway AuthPolicies exist. Gateway AuthPolicy updated to include overrides.", func(ctx SpecContext) {
			routePolicy := policyFactory()
			routePolicyKey := client.ObjectKeyFromObject(routePolicy)
			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", routePolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			gatewayPolicy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.Proper().AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})
			gatewayPolicyKey := client.ObjectKeyFromObject(gatewayPolicy)
			err = k8sClient.Create(ctx, gatewayPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", gatewayPolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndNotEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), gatewayPolicyKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", routePolicyKey.String()))).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			Eventually(func() bool {
				err = k8sClient.Get(ctx, gatewayPolicyKey, gatewayPolicy)
				if err != nil {
					return false
				}
				patch := client.MergeFrom(gatewayPolicy.DeepCopy())
				gatewayPolicy.Spec.Overrides = &kuadrantv1.MergeableAuthPolicySpec{}
				gatewayPolicy.Spec.Defaults = nil
				gatewayPolicy.Spec.Overrides.AuthScheme = tests.BuildBasicAuthScheme()
				gatewayPolicy.Spec.Overrides.AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
				err = k8sClient.Patch(ctx, gatewayPolicy, patch)
				logf.Log.V(1).Info("Updating AuthPolicy", "key", client.ObjectKeyFromObject(gatewayPolicy).String(), "error", err)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeFalse())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), routePolicyKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", gatewayPolicyKey.String()))).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Route and Gateway AuthPolicies exist. Gateway AuthPolicy updated to remove overrides.", func(ctx SpecContext) {
			routePolicy := policyFactory()
			routePolicyKey := client.ObjectKeyFromObject(routePolicy)
			err := k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", routePolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())

			gatewayPolicy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.Overrides = &kuadrantv1.MergeableAuthPolicySpec{}
				policy.Spec.Defaults = nil
				policy.Spec.Overrides.AuthScheme = tests.BuildBasicAuthScheme()
				policy.Spec.Overrides.AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})
			gatewayPolicyKey := client.ObjectKeyFromObject(gatewayPolicy)
			err = k8sClient.Create(ctx, gatewayPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", gatewayPolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeFalse())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), routePolicyKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", gatewayPolicyKey.String()))).WithContext(ctx).Should(BeTrue())

			Eventually(func() bool {
				err = k8sClient.Get(ctx, gatewayPolicyKey, gatewayPolicy)
				if err != nil {
					return false
				}
				patch := client.MergeFrom(gatewayPolicy.DeepCopy())
				gatewayPolicy.Spec.Overrides = nil
				gatewayPolicy.Spec.Proper().AuthScheme = tests.BuildBasicAuthScheme()
				gatewayPolicy.Spec.Proper().AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
				err = k8sClient.Patch(ctx, gatewayPolicy, patch)
				logf.Log.V(1).Info("Updating AuthPolicy", "key", gatewayPolicyKey.String(), "error", err)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndNotEnforced(ctx, testClient(), gatewayPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), gatewayPolicyKey, kuadrant.PolicyReasonOverridden, fmt.Sprintf("AuthPolicy is overridden by [%s]", routePolicyKey.String()))).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())
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

	policyFactory := func(mutateFns ...func(policy *kuadrantv1.AuthPolicy)) *kuadrantv1.AuthPolicy {
		policy := &kuadrantv1.AuthPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-policy",
				Namespace: testNamespace,
			},
			Spec: kuadrantv1.AuthPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "HTTPRoute",
						Name:  "my-target",
					},
				},
				AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
					AuthScheme: &kuadrantv1.AuthSchemeSpec{
						Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
							"anonymous": {
								AuthenticationSpec: authorinov1beta3.AuthenticationSpec{
									AuthenticationMethodSpec: authorinov1beta3.AuthenticationMethodSpec{
										AnonymousAccess: &authorinov1beta3.AnonymousAccessSpec{},
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

	Context("Spec TargetRef Validations", func() {
		It("Valid policy targeting HTTPRoute", func(ctx SpecContext) {
			policy := policyFactory()
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(BeNil())
		})

		It("Valid policy targeting Gateway", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(BeNil())
		})

		It("Invalid Target Ref Group", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.TargetRef.Group = "not-gateway.networking.k8s.io"
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'")).To(BeTrue())
		})

		It("Invalid Target Ref Kind", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.TargetRef.Kind = "TCPRoute"
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'")).To(BeTrue())
		})
	})

	Context("Rules missing from configuration", func() {
		It("Missing rules object", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.AuthScheme = nil
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.rules must be defined")).To(BeTrue())
		})

		It("Empty rules object created", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.AuthScheme = &kuadrantv1.AuthSchemeSpec{}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.rules must be defined")).To(BeTrue())
		})

		It("Empty rules.authentication object created", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.AuthScheme = &kuadrantv1.AuthSchemeSpec{
					Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.rules must be defined")).To(BeTrue())
		})

		It("Missing defaults.rules.authentication object", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.AuthScheme = nil
				policy.Spec.Defaults = &kuadrantv1.MergeableAuthPolicySpec{
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: nil,
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.defaults.rules must be defined")).To(BeTrue())
		})

		It("Empty defaults.rules.authentication object created", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.AuthScheme = nil
				policy.Spec.Defaults = &kuadrantv1.MergeableAuthPolicySpec{
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: &kuadrantv1.AuthSchemeSpec{
							Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{},
						},
					},
				}

			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.defaults.rules must be defined")).To(BeTrue())
		})

		It("Missing overrides.rules.authentication object", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.AuthScheme = nil
				policy.Spec.Overrides = &kuadrantv1.MergeableAuthPolicySpec{
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: nil,
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.overrides.rules must be defined")).To(BeTrue())
		})

		It("Empty overrides.rules.authentication object created", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.AuthScheme = nil
				policy.Spec.Overrides = &kuadrantv1.MergeableAuthPolicySpec{
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: &kuadrantv1.AuthSchemeSpec{
							Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{},
						},
					},
				}

			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "At least one spec.overrides.rules must be defined")).To(BeTrue())
		})
	})

	Context("Defaults mutual exclusivity validation", func() {
		It("Valid when only implicit defaults are used", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.AuthScheme = tests.BuildBasicAuthScheme()
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		})

		It("Valid when only explicit defaults are used", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.AuthScheme = nil
				policy.Spec.Defaults = &kuadrantv1.MergeableAuthPolicySpec{
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: tests.BuildBasicAuthScheme(),
					},
				}
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		})

		It("Invalid when both implicit and explicit defaults are used - authScheme", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.Defaults = &kuadrantv1.MergeableAuthPolicySpec{}
				policy.Spec.AuthScheme = tests.BuildBasicAuthScheme()
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Implicit and explicit defaults are mutually exclusive")).To(BeTrue())
		})

		It("Invalid when both implicit and explicit defaults are used - namedPatterns", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.Defaults = &kuadrantv1.MergeableAuthPolicySpec{}
				policy.Spec.NamedPatterns = map[string]kuadrantv1.MergeablePatternExpressions{
					"internal-source": {
						PatternExpressions: []authorinov1beta3.PatternExpression{
							{
								Selector: "source.ip",
								Operator: authorinov1beta3.PatternExpressionOperator("matches"),
								Value:    `192\.168\..*`,
							},
						},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Implicit and explicit defaults are mutually exclusive")).To(BeTrue())
		})

		It("Invalid when both implicit and explicit defaults are used - conditions", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1.AuthPolicy) {
				policy.Spec.Defaults = &kuadrantv1.MergeableAuthPolicySpec{}
				policy.Spec.MergeableWhenPredicates = kuadrantv1.MergeableWhenPredicates{
					Predicates: kuadrantv1.WhenPredicates{
						{Predicate: `source.ip.matches("^192\.168\..*")`},
					},
				}
			})
			err := k8sClient.Create(ctx, policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Implicit and explicit defaults are mutually exclusive")).To(BeTrue())
		})
	})
})
