//go:build integration

package envoygateway_test

import (
	"fmt"
	"strings"
	"time"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/go-logr/logr"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("wasm controller", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gwHost        = fmt.Sprintf("*.toystore-%s.com", rand.String(4))
		gatewayClass  *gatewayapiv1.GatewayClass
		gateway       *gatewayapiv1.Gateway
		logger        logr.Logger
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gatewayClass = &gatewayapiv1.GatewayClass{}
		err := testClient().Get(ctx, types.NamespacedName{Name: tests.GatewayClassName}, gatewayClass)
		Expect(err).ToNot(HaveOccurred())
		gateway = tests.NewGatewayBuilder(TestGatewayName, tests.GatewayClassName, testNamespace).
			WithHTTPListener("test-listener", gwHost).
			Gateway
		err = testClient().Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())
		logger = controller.LoggerFromContext(ctx).WithName("EnvoyExtensionReconcilerTest")

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
				Name:      "rlp",
				Namespace: testNamespace,
			},
			Spec: kuadrantv1.RateLimitPolicySpec{},
		}

		for _, mutateFn := range mutateFns {
			mutateFn(policy)
		}

		return policy
	}

	randomHostFromGWHost := func() string {
		return strings.Replace(gwHost, "*", rand.String(4), 1)
	}

	Context("RateLimitPolicy attached to the gateway", func() {

		var (
			gwPolicy      *kuadrantv1.RateLimitPolicy
			gwRoute       *gatewayapiv1.HTTPRoute
			actionSetName string
		)

		BeforeEach(func(ctx SpecContext) {
			gwRoute = tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err := testClient().Create(ctx, gwRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(gwRoute))).WithContext(ctx).Should(BeTrue())

			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: gwRoute}
			pathID := kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &gwRoute.Spec.Rules[0], Name: "rule-1"},
			})
			actionSetName = wasm.ActionSetNameForPath(pathID, 0, string(gwRoute.Spec.Hostnames[0]))

			gwPolicy = policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Name = "gw"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.Defaults = &kuadrantv1.MergeableRateLimitPolicySpec{
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
				}
			})

			gwPolicyKey := client.ObjectKeyFromObject(gwPolicy)

			err = testClient().Create(ctx, gwPolicy)
			logf.Log.V(1).Info("Creating RateLimitPolicy", "key", gwPolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsRLPAcceptedAndEnforced).
				WithContext(ctx).
				WithArguments(testClient(), gwPolicyKey).Should(Succeed())
		})

		It("Creates envoyextensionpolicy", func(ctx SpecContext) {
			extKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}

			gwPolicyKey := client.ObjectKeyFromObject(gwPolicy)

			Eventually(IsEnvoyExtensionPolicyAccepted).
				WithContext(ctx).
				WithArguments(testClient(), extKey, client.ObjectKeyFromObject(gateway)).
				Should(Succeed())

			ext := &egv1alpha1.EnvoyExtensionPolicy{}
			err := testClient().Get(ctx, extKey, ext)
			// must exist
			Expect(err).ToNot(HaveOccurred())

			Expect(ext.Spec.PolicyTargetReferences.TargetRefs).To(HaveLen(1))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs[0].LocalPolicyTargetReference.Group).To(Equal(gatewayapiv1.Group("gateway.networking.k8s.io")))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs[0].LocalPolicyTargetReference.Kind).To(Equal(gatewayapiv1.Kind("Gateway")))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs[0].LocalPolicyTargetReference.Name).To(Equal(gatewayapiv1.ObjectName(gateway.Name)))
			Expect(ext.Spec.Wasm).To(HaveLen(1))
			Expect(ext.Spec.Wasm[0].Code.Type).To(Equal(egv1alpha1.ImageWasmCodeSourceType))
			Expect(ext.Spec.Wasm[0].Code.Image).To(Not(BeNil()))
			Expect(ext.Spec.Wasm[0].Code.Image.URL).To(Equal(controllers.WASMFilterImageURL))
			existingWASMConfig, err := wasm.ConfigFromJSON(ext.Spec.Wasm[0].Config)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig).To(Equal(&wasm.Config{
				Services: map[string]wasm.Service{
					wasm.AuthServiceName: {
						Type:        wasm.AuthServiceType,
						Endpoint:    kuadrant.KuadrantAuthClusterName,
						FailureMode: wasm.AuthServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.AuthServiceTimeout()),
					},
					wasm.RateLimitCheckServiceName: {
						Type:        wasm.RateLimitCheckServiceType,
						Endpoint:    kuadrant.KuadrantRateLimitClusterName,
						FailureMode: wasm.RatelimitCheckServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.RatelimitCheckServiceTimeout()),
					},
					wasm.RateLimitServiceName: {
						Type:        wasm.RateLimitServiceType,
						Endpoint:    kuadrant.KuadrantRateLimitClusterName,
						FailureMode: wasm.RatelimitServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.RatelimitServiceTimeout()),
					},
					wasm.RateLimitReportServiceName: {
						Type:        wasm.RateLimitReportServiceType,
						Endpoint:    kuadrant.KuadrantRateLimitClusterName,
						FailureMode: wasm.RatelimitReportServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.RatelimitReportServiceTimeout()),
					},
				},
				ActionSets: []wasm.ActionSet{
					{
						Name: actionSetName,
						RouteRuleConditions: wasm.RouteRuleConditions{
							Hostnames: []string{string(gwRoute.Spec.Hostnames[0])},
							Predicates: []string{
								"request.method == 'GET'",
								"request.url_path.startsWith('/toy')",
							},
						},
						Actions: []wasm.Action{
							{
								ServiceName:          wasm.RateLimitServiceName,
								Scope:                controllers.LimitsNamespaceFromRoute(gwRoute),
								SourcePolicyLocators: []string{"ratelimitpolicy.kuadrant.io:" + gwPolicyKey.String()},
								ConditionalData: []wasm.ConditionalData{
									{
										Data: []wasm.DataType{
											{
												Value: &wasm.Expression{
													ExpressionItem: wasm.ExpressionItem{
														Key:   controllers.LimitNameToLimitadorIdentifier(gwPolicyKey, "l1"),
														Value: "1",
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
			}))
		}, testTimeOut)

		It("Deletes envoyextensionpolicy when rate limit policy is deleted", func(ctx SpecContext) {
			gwPolicyKey := client.ObjectKeyFromObject(gwPolicy)
			err := testClient().Delete(ctx, gwPolicy)
			logf.Log.V(1).Info("Deleting RateLimitPolicy", "key", gwPolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			extKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(func() bool {
				err := testClient().Get(ctx, extKey, &egv1alpha1.EnvoyExtensionPolicy{})
				logf.Log.V(1).Info("Fetching EnvoyExtensionPolicy", "key", extKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Deletes envoyextensionpolicy if gateway is deleted", func(ctx SpecContext) {
			err := testClient().Delete(ctx, gateway)
			logf.Log.V(1).Info("Deleting Gateway", "key", client.ObjectKeyFromObject(gateway).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			extKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(func() bool {
				err := testClient().Get(ctx, extKey, &egv1alpha1.EnvoyExtensionPolicy{})
				logf.Log.V(1).Info("Fetching EnvoyExtensionPolicy", "key", extKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("RateLimitPolicy attached to the route", func() {

		var (
			routePolicy   *kuadrantv1.RateLimitPolicy
			gwRoute       *gatewayapiv1.HTTPRoute
			actionSetName string
		)

		BeforeEach(func(ctx SpecContext) {
			gwRoute = tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err := testClient().Create(ctx, gwRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(gwRoute))).WithContext(ctx).Should(BeTrue())

			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: gwRoute}
			pathID := kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &gwRoute.Spec.Rules[0], Name: "rule-1"},
			})
			actionSetName = wasm.ActionSetNameForPath(pathID, 0, string(gwRoute.Spec.Hostnames[0]))

			routePolicy = policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Name = "route"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = TestHTTPRouteName
				policy.Spec.Defaults = &kuadrantv1.MergeableRateLimitPolicySpec{
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
				}
			})

			err = testClient().Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating RateLimitPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsRLPAcceptedAndEnforced).
				WithContext(ctx).
				WithArguments(testClient(), client.ObjectKeyFromObject(routePolicy)).Should(Succeed())
		})

		It("Creates envoyextensionpolicy", func(ctx SpecContext) {
			extKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}

			routePolicyKey := client.ObjectKeyFromObject(routePolicy)

			Eventually(IsEnvoyExtensionPolicyAccepted).
				WithContext(ctx).
				WithArguments(testClient(), extKey, client.ObjectKeyFromObject(gateway)).
				Should(Succeed())

			ext := &egv1alpha1.EnvoyExtensionPolicy{}
			err := testClient().Get(ctx, extKey, ext)
			// must exist
			Expect(err).ToNot(HaveOccurred())

			Expect(gwRoute.Spec.Hostnames).To(Not(BeEmpty()))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs).To(HaveLen(1))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs[0].LocalPolicyTargetReference.Group).To(Equal(gatewayapiv1.Group("gateway.networking.k8s.io")))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs[0].LocalPolicyTargetReference.Kind).To(Equal(gatewayapiv1.Kind("Gateway")))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs[0].LocalPolicyTargetReference.Name).To(Equal(gatewayapiv1.ObjectName(gateway.Name)))
			Expect(ext.Spec.Wasm).To(HaveLen(1))
			Expect(ext.Spec.Wasm[0].Code.Type).To(Equal(egv1alpha1.ImageWasmCodeSourceType))
			Expect(ext.Spec.Wasm[0].Code.Image).To(Not(BeNil()))
			Expect(ext.Spec.Wasm[0].Code.Image.URL).To(Equal(controllers.WASMFilterImageURL))
			existingWASMConfig, err := wasm.ConfigFromJSON(ext.Spec.Wasm[0].Config)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig).To(Equal(&wasm.Config{
				Services: map[string]wasm.Service{
					wasm.AuthServiceName: {
						Type:        wasm.AuthServiceType,
						Endpoint:    kuadrant.KuadrantAuthClusterName,
						FailureMode: wasm.AuthServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.AuthServiceTimeout()),
					},
					wasm.RateLimitCheckServiceName: {
						Type:        wasm.RateLimitCheckServiceType,
						Endpoint:    kuadrant.KuadrantRateLimitClusterName,
						FailureMode: wasm.RatelimitCheckServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.RatelimitCheckServiceTimeout()),
					},
					wasm.RateLimitServiceName: {
						Type:        wasm.RateLimitServiceType,
						Endpoint:    kuadrant.KuadrantRateLimitClusterName,
						FailureMode: wasm.RatelimitServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.RatelimitServiceTimeout()),
					},
					wasm.RateLimitReportServiceName: {
						Type:        wasm.RateLimitReportServiceType,
						Endpoint:    kuadrant.KuadrantRateLimitClusterName,
						FailureMode: wasm.RatelimitReportServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.RatelimitReportServiceTimeout()),
					},
				},
				ActionSets: []wasm.ActionSet{
					{
						Name: actionSetName,
						RouteRuleConditions: wasm.RouteRuleConditions{
							Hostnames: []string{string(gwRoute.Spec.Hostnames[0])},
							Predicates: []string{
								"request.method == 'GET'",
								"request.url_path.startsWith('/toy')",
							},
						},
						Actions: []wasm.Action{
							{
								ServiceName:          wasm.RateLimitServiceName,
								Scope:                controllers.LimitsNamespaceFromRoute(gwRoute),
								SourcePolicyLocators: []string{"ratelimitpolicy.kuadrant.io:" + routePolicyKey.String()},
								ConditionalData: []wasm.ConditionalData{
									{
										Data: []wasm.DataType{
											{
												Value: &wasm.Expression{
													ExpressionItem: wasm.ExpressionItem{
														Key:   controllers.LimitNameToLimitadorIdentifier(routePolicyKey, "l1"),
														Value: "1",
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
			}))
		}, testTimeOut)

		It("Deletes envoyextensionpolicy when rate limit policy is deleted", func(ctx SpecContext) {
			routePolicyKey := client.ObjectKeyFromObject(routePolicy)
			err := testClient().Delete(ctx, routePolicy)
			logf.Log.V(1).Info("Deleting RateLimitPolicy", "key", routePolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			extKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(func() bool {
				err := testClient().Get(ctx, extKey, &egv1alpha1.EnvoyExtensionPolicy{})
				logf.Log.V(1).Info("Fetching EnvoyExtensionPolicy", "key", extKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Deletes envoyextensionpolicy if route is deleted", func(ctx SpecContext) {
			gwRouteKey := client.ObjectKeyFromObject(gwRoute)
			err := testClient().Delete(ctx, gwRoute)
			logf.Log.V(1).Info("Deleting Route", "key", gwRouteKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			extKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(func() bool {
				err := testClient().Get(ctx, extKey, &egv1alpha1.EnvoyExtensionPolicy{})
				logf.Log.V(1).Info("Fetching EnvoyExtensionPolicy", "key", extKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("Source Policy Locators", func() {
		It("EnvoyExtensionPolicy config includes source policy locators for AuthPolicy with merge strategy", func(ctx SpecContext) {
			routeName := "test-route"
			gwAuthPolicyName := "gw-auth"
			routeAuthPolicyName := "route-auth"

			// Create HTTPRoute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			Expect(testClient().Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// Create Gateway AuthPolicy with defaults and merge strategy
			gwAuthPolicy := &kuadrantv1.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: kuadrantv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: gwAuthPolicyName, Namespace: testNamespace},
				Spec: kuadrantv1.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					Defaults: &kuadrantv1.MergeableAuthPolicySpec{
						Strategy: kuadrantv1.PolicyRuleMergeStrategy, // Merge strategy
						AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
							AuthScheme: &kuadrantv1.AuthSchemeSpec{
								Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
									"gateway-auth": {
										AuthenticationSpec: authorinoapi.AuthenticationSpec{
											AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
												ApiKey: &authorinoapi.ApiKeyAuthenticationSpec{
													Selector: &metav1.LabelSelector{
														MatchLabels: map[string]string{"app": "gateway"},
													},
												},
											},
											Credentials: authorinoapi.Credentials{
												AuthorizationHeader: &authorinoapi.Prefixed{Prefix: "GATEWAY-KEY"},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient().Create(ctx, gwAuthPolicy)).To(Succeed())
			gwAuthPolicyKey := client.ObjectKeyFromObject(gwAuthPolicy)
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), gwAuthPolicy)).WithContext(ctx).Should(BeTrue())

			// Check EnvoyExtensionPolicy exists and has correct source for gateway policy
			extKey := client.ObjectKey{Name: wasm.ExtensionName(TestGatewayName), Namespace: testNamespace}
			existingExt := &egv1alpha1.EnvoyExtensionPolicy{}
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())
				g.Expect(existingWASMConfig.ActionSets[0].Actions).ToNot(BeEmpty())
				// Single policy source for auth action
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(Equal([]string{
					"authpolicy.kuadrant.io:" + gwAuthPolicyKey.String(),
				}))
			}).WithContext(ctx).Should(Succeed())

			// Create Route AuthPolicy with merge strategy
			routeAuthPolicy := &kuadrantv1.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: kuadrantv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: routeAuthPolicyName, Namespace: testNamespace},
				Spec: kuadrantv1.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(routeName),
						},
					},
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: &kuadrantv1.AuthSchemeSpec{
							Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
								"route-auth": {
									AuthenticationSpec: authorinoapi.AuthenticationSpec{
										AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
											ApiKey: &authorinoapi.ApiKeyAuthenticationSpec{
												Selector: &metav1.LabelSelector{
													MatchLabels: map[string]string{"app": "route"},
												},
											},
										},
										Credentials: authorinoapi.Credentials{
											AuthorizationHeader: &authorinoapi.Prefixed{Prefix: "ROUTE-KEY"},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient().Create(ctx, routeAuthPolicy)).To(Succeed())
			routeAuthPolicyKey := client.ObjectKeyFromObject(routeAuthPolicy)
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), routeAuthPolicy)).WithContext(ctx).Should(BeTrue())

			// Check EnvoyExtensionPolicy config now has BOTH policies in sources (merged)
			// Note: AuthPolicy creates a single auth action that includes all merged policies
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())

				// Should still have 1 auth action (auth actions are not split per policy)
				g.Expect(existingWASMConfig.ActionSets[0].Actions).ToNot(BeEmpty())

				// The single auth action should list BOTH policy sources
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].ServiceName).To(Equal(wasm.AuthServiceName))
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(ConsistOf(
					"authpolicy.kuadrant.io:"+gwAuthPolicyKey.String(),
					"authpolicy.kuadrant.io:"+routeAuthPolicyKey.String(),
				))
			}).WithContext(ctx).Should(Succeed())

		}, testTimeOut)

		It("EnvoyExtensionPolicy config includes source policy locators for AuthPolicy with atomic strategy", func(ctx SpecContext) {
			routeName := "test-route"
			gwAuthPolicyName := "gw-auth"
			routeAuthPolicyName := "route-auth"

			// Create HTTPRoute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			Expect(testClient().Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// Create Gateway AuthPolicy with defaults and atomic strategy
			gwAuthPolicy := &kuadrantv1.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: kuadrantv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: gwAuthPolicyName, Namespace: testNamespace},
				Spec: kuadrantv1.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					Defaults: &kuadrantv1.MergeableAuthPolicySpec{
						Strategy: kuadrantv1.AtomicMergeStrategy, // Atomic strategy
						AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
							AuthScheme: &kuadrantv1.AuthSchemeSpec{
								Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
									"gateway-auth": {
										AuthenticationSpec: authorinoapi.AuthenticationSpec{
											AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
												ApiKey: &authorinoapi.ApiKeyAuthenticationSpec{
													Selector: &metav1.LabelSelector{
														MatchLabels: map[string]string{"app": "gateway"},
													},
												},
											},
											Credentials: authorinoapi.Credentials{
												AuthorizationHeader: &authorinoapi.Prefixed{Prefix: "GATEWAY-KEY"},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient().Create(ctx, gwAuthPolicy)).To(Succeed())
			gwAuthPolicyKey := client.ObjectKeyFromObject(gwAuthPolicy)
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), gwAuthPolicy)).WithContext(ctx).Should(BeTrue())

			// Check EnvoyExtensionPolicy exists and has correct source for gateway policy
			extKey := client.ObjectKey{Name: wasm.ExtensionName(TestGatewayName), Namespace: testNamespace}
			existingExt := &egv1alpha1.EnvoyExtensionPolicy{}
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())
				g.Expect(existingWASMConfig.ActionSets[0].Actions).ToNot(BeEmpty())
				// Single policy source for auth action
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(Equal([]string{
					"authpolicy.kuadrant.io:" + gwAuthPolicyKey.String(),
				}))
			}).WithContext(ctx).Should(Succeed())

			// Create Route AuthPolicy - with atomic strategy, route policy should replace gateway defaults entirely
			routeAuthPolicy := &kuadrantv1.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: kuadrantv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: routeAuthPolicyName, Namespace: testNamespace},
				Spec: kuadrantv1.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(routeName),
						},
					},
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: &kuadrantv1.AuthSchemeSpec{
							Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
								"route-auth": {
									AuthenticationSpec: authorinoapi.AuthenticationSpec{
										AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
											ApiKey: &authorinoapi.ApiKeyAuthenticationSpec{
												Selector: &metav1.LabelSelector{
													MatchLabels: map[string]string{"app": "route"},
												},
											},
										},
										Credentials: authorinoapi.Credentials{
											AuthorizationHeader: &authorinoapi.Prefixed{Prefix: "ROUTE-KEY"},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient().Create(ctx, routeAuthPolicy)).To(Succeed())
			routeAuthPolicyKey := client.ObjectKeyFromObject(routeAuthPolicy)
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), routeAuthPolicy)).WithContext(ctx).Should(BeTrue())

			// With atomic strategy, route policy replaces gateway defaults - should have only route policy source
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())

				// Should have 1 auth action with only route policy (atomic replacement)
				g.Expect(existingWASMConfig.ActionSets[0].Actions).ToNot(BeEmpty())

				// The action should list ONLY the route policy source (atomic replaces gateway defaults)
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].ServiceName).To(Equal(wasm.AuthServiceName))
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(Equal([]string{
					"authpolicy.kuadrant.io:" + routeAuthPolicyKey.String(),
				}))
			}).WithContext(ctx).Should(Succeed())

		}, testTimeOut)

		It("EnvoyExtensionPolicy config includes source policy locators for AuthPolicy with overrides and atomic strategy", func(ctx SpecContext) {
			routeName := "test-route"
			gwAuthPolicyName := "gw-auth"
			routeAuthPolicyName := "route-auth"

			// Create HTTPRoute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			Expect(testClient().Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// Create Gateway AuthPolicy with overrides and atomic strategy
			gwAuthPolicy := &kuadrantv1.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: kuadrantv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: gwAuthPolicyName, Namespace: testNamespace},
				Spec: kuadrantv1.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					Overrides: &kuadrantv1.MergeableAuthPolicySpec{
						Strategy: kuadrantv1.AtomicMergeStrategy, // Atomic strategy with overrides
						AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
							AuthScheme: &kuadrantv1.AuthSchemeSpec{
								Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
									"gateway-auth": {
										AuthenticationSpec: authorinoapi.AuthenticationSpec{
											AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
												ApiKey: &authorinoapi.ApiKeyAuthenticationSpec{
													Selector: &metav1.LabelSelector{
														MatchLabels: map[string]string{"app": "gateway"},
													},
												},
											},
											Credentials: authorinoapi.Credentials{
												AuthorizationHeader: &authorinoapi.Prefixed{Prefix: "GATEWAY-KEY"},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient().Create(ctx, gwAuthPolicy)).To(Succeed())
			gwAuthPolicyKey := client.ObjectKeyFromObject(gwAuthPolicy)
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), gwAuthPolicy)).WithContext(ctx).Should(BeTrue())

			// Check EnvoyExtensionPolicy exists and has correct source for gateway policy
			extKey := client.ObjectKey{Name: wasm.ExtensionName(TestGatewayName), Namespace: testNamespace}
			existingExt := &egv1alpha1.EnvoyExtensionPolicy{}
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())
				g.Expect(existingWASMConfig.ActionSets[0].Actions).ToNot(BeEmpty())
				// Single policy source (gateway with overrides)
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(Equal([]string{
					"authpolicy.kuadrant.io:" + gwAuthPolicyKey.String(),
				}))
			}).WithContext(ctx).Should(Succeed())

			// Create Route AuthPolicy - with atomic overrides, gateway policy should override route policy entirely
			routeAuthPolicy := &kuadrantv1.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: kuadrantv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: routeAuthPolicyName, Namespace: testNamespace},
				Spec: kuadrantv1.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(routeName),
						},
					},
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: &kuadrantv1.AuthSchemeSpec{
							Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
								"route-auth": {
									AuthenticationSpec: authorinoapi.AuthenticationSpec{
										AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
											ApiKey: &authorinoapi.ApiKeyAuthenticationSpec{
												Selector: &metav1.LabelSelector{
													MatchLabels: map[string]string{"app": "route"},
												},
											},
										},
										Credentials: authorinoapi.Credentials{
											AuthorizationHeader: &authorinoapi.Prefixed{Prefix: "ROUTE-KEY"},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient().Create(ctx, routeAuthPolicy)).To(Succeed())
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), routeAuthPolicy)).WithContext(ctx).Should(BeTrue())

			// With atomic overrides strategy, gateway policy atomically overrides route policy - should have only gateway policy source
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())

				// Should have 1 auth action with only gateway policy (atomic override)
				g.Expect(existingWASMConfig.ActionSets[0].Actions).ToNot(BeEmpty())

				// The action should list ONLY the gateway policy source (atomic overrides route policy)
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].ServiceName).To(Equal(wasm.AuthServiceName))
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(Equal([]string{
					"authpolicy.kuadrant.io:" + gwAuthPolicyKey.String(),
				}))
			}).WithContext(ctx).Should(Succeed())

		}, testTimeOut)

		It("EnvoyExtensionPolicy config includes source policy locators for AuthPolicy with overrides and merge strategy", func(ctx SpecContext) {
			routeName := "test-route"
			gwAuthPolicyName := "gw-auth"
			routeAuthPolicyName := "route-auth"

			// Create HTTPRoute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			Expect(testClient().Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// Create Gateway AuthPolicy with overrides and merge strategy
			gwAuthPolicy := &kuadrantv1.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: kuadrantv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: gwAuthPolicyName, Namespace: testNamespace},
				Spec: kuadrantv1.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					Overrides: &kuadrantv1.MergeableAuthPolicySpec{
						Strategy: kuadrantv1.PolicyRuleMergeStrategy, // Merge strategy with overrides
						AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
							AuthScheme: &kuadrantv1.AuthSchemeSpec{
								Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
									"gateway-auth": {
										AuthenticationSpec: authorinoapi.AuthenticationSpec{
											AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
												ApiKey: &authorinoapi.ApiKeyAuthenticationSpec{
													Selector: &metav1.LabelSelector{
														MatchLabels: map[string]string{"app": "gateway"},
													},
												},
											},
											Credentials: authorinoapi.Credentials{
												AuthorizationHeader: &authorinoapi.Prefixed{Prefix: "GATEWAY-KEY"},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient().Create(ctx, gwAuthPolicy)).To(Succeed())
			gwAuthPolicyKey := client.ObjectKeyFromObject(gwAuthPolicy)
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), gwAuthPolicy)).WithContext(ctx).Should(BeTrue())

			// Check EnvoyExtensionPolicy exists and has correct source for gateway policy
			extKey := client.ObjectKey{Name: wasm.ExtensionName(TestGatewayName), Namespace: testNamespace}
			existingExt := &egv1alpha1.EnvoyExtensionPolicy{}
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())
				g.Expect(existingWASMConfig.ActionSets[0].Actions).ToNot(BeEmpty())
				// Single policy source (gateway with overrides)
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(Equal([]string{
					"authpolicy.kuadrant.io:" + gwAuthPolicyKey.String(),
				}))
			}).WithContext(ctx).Should(Succeed())

			// Create Route AuthPolicy - with merge overrides, both policies should be present in sources
			routeAuthPolicy := &kuadrantv1.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: kuadrantv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: routeAuthPolicyName, Namespace: testNamespace},
				Spec: kuadrantv1.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(routeName),
						},
					},
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: &kuadrantv1.AuthSchemeSpec{
							Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
								"route-auth": {
									AuthenticationSpec: authorinoapi.AuthenticationSpec{
										AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
											ApiKey: &authorinoapi.ApiKeyAuthenticationSpec{
												Selector: &metav1.LabelSelector{
													MatchLabels: map[string]string{"app": "route"},
												},
											},
										},
										Credentials: authorinoapi.Credentials{
											AuthorizationHeader: &authorinoapi.Prefixed{Prefix: "ROUTE-KEY"},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient().Create(ctx, routeAuthPolicy)).To(Succeed())
			routeAuthPolicyKey := client.ObjectKeyFromObject(routeAuthPolicy)
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), routeAuthPolicy)).WithContext(ctx).Should(BeTrue())

			// With merge overrides strategy, both gateway and route policies are merged - should have both policy sources
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())

				// Should have 1 merged auth action with both policies
				g.Expect(existingWASMConfig.ActionSets[0].Actions).ToNot(BeEmpty())

				// The action should list BOTH policy sources (merge overrides merges both policies)
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].ServiceName).To(Equal(wasm.AuthServiceName))
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(ConsistOf(
					"authpolicy.kuadrant.io:"+gwAuthPolicyKey.String(),
					"authpolicy.kuadrant.io:"+routeAuthPolicyKey.String(),
				))
			}).WithContext(ctx).Should(Succeed())

		}, testTimeOut)

		It("EnvoyExtensionPolicy config includes source policy locators for RateLimitPolicy with merge strategy", func(ctx SpecContext) {
			routeName := "test-route"
			gwRLPName := "gw-rlp"
			routeRLPName := "route-rlp"

			// Create HTTPRoute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			Expect(testClient().Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// Create Gateway RateLimitPolicy with defaults and merge strategy
			gwRLP := policyFactory(
				func(p *kuadrantv1.RateLimitPolicy) {
					p.Name = gwRLPName
					p.Spec.TargetRef.Group = gatewayapiv1.GroupName
					p.Spec.TargetRef.Kind = "Gateway"
					p.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
					p.Spec.Defaults = &kuadrantv1.MergeableRateLimitPolicySpec{
						Strategy: kuadrantv1.PolicyRuleMergeStrategy,
						RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
							Limits: map[string]kuadrantv1.Limit{
								"gateway-limit": {
									Rates: []kuadrantv1.Rate{{Limit: 10, Window: kuadrantv1.Duration("1m")}},
								},
							},
						},
					}
				},
			)
			Expect(testClient().Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKeyFromObject(gwRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Check EnvoyExtensionPolicy exists and has correct source for gateway policy
			extKey := client.ObjectKey{Name: wasm.ExtensionName(TestGatewayName), Namespace: testNamespace}
			existingExt := &egv1alpha1.EnvoyExtensionPolicy{}
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())
				g.Expect(existingWASMConfig.ActionSets[0].Actions).ToNot(BeEmpty())
				// Single policy source
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(Equal([]string{
					"ratelimitpolicy.kuadrant.io:" + gwRLPKey.String(),
				}))
			}).WithContext(ctx).Should(Succeed())

			// Create Route RateLimitPolicy with merge strategy
			routeRLP := policyFactory(
				func(p *kuadrantv1.RateLimitPolicy) {
					p.Name = routeRLPName
					p.Spec.TargetRef.Group = gatewayapiv1.GroupName
					p.Spec.TargetRef.Kind = "HTTPRoute"
					p.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
					p.Spec.Limits = map[string]kuadrantv1.Limit{
						"route-limit": {
							Rates: []kuadrantv1.Rate{{Limit: 100, Window: "1m"}},
						},
					}
				},
			)
			Expect(testClient().Create(ctx, routeRLP)).To(Succeed())
			routeRLPKey := client.ObjectKeyFromObject(routeRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// Check EnvoyExtensionPolicy config now has BOTH policies in sources (merged)
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())

				// Should have 1 merged action with both limits' data
				g.Expect(existingWASMConfig.ActionSets[0].Actions).To(HaveLen(1))

				// The merged action should list BOTH policy sources
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(ConsistOf(
					"ratelimitpolicy.kuadrant.io:"+gwRLPKey.String(),
					"ratelimitpolicy.kuadrant.io:"+routeRLPKey.String(),
				))

				// Verify both limits' data is present
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].ConditionalData).To(HaveLen(2))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("EnvoyExtensionPolicy config includes source policy locators for RateLimitPolicy with atomic strategy", func(ctx SpecContext) {
			routeName := "test-route"
			gwRLPName := "gw-rlp"
			routeRLPName := "route-rlp"

			// Create HTTPRoute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			Expect(testClient().Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// Create Gateway RateLimitPolicy with defaults and atomic strategy
			gwRLP := policyFactory(
				func(p *kuadrantv1.RateLimitPolicy) {
					p.Name = gwRLPName
					p.Spec.TargetRef.Group = gatewayapiv1.GroupName
					p.Spec.TargetRef.Kind = "Gateway"
					p.Spec.TargetRef.Name = TestGatewayName
					p.Spec.Defaults = &kuadrantv1.MergeableRateLimitPolicySpec{
						Strategy: kuadrantv1.AtomicMergeStrategy, // Atomic strategy
						RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
							Limits: map[string]kuadrantv1.Limit{
								"gateway-limit": {
									Rates: []kuadrantv1.Rate{{Limit: 10, Window: kuadrantv1.Duration("1m")}},
								},
							},
						},
					}
				},
			)
			Expect(testClient().Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKeyFromObject(gwRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Check EnvoyExtensionPolicy exists and has correct source for gateway policy
			extKey := client.ObjectKey{Name: wasm.ExtensionName(TestGatewayName), Namespace: testNamespace}
			existingExt := &egv1alpha1.EnvoyExtensionPolicy{}
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())
				g.Expect(existingWASMConfig.ActionSets[0].Actions).ToNot(BeEmpty())
				// Single policy source
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(Equal([]string{
					"ratelimitpolicy.kuadrant.io:" + gwRLPKey.String(),
				}))
			}).WithContext(ctx).Should(Succeed())

			// Create Route RLP - with atomic strategy, route policy should replace gateway defaults entirely
			routeRLP := policyFactory(
				func(p *kuadrantv1.RateLimitPolicy) {
					p.Name = routeRLPName
					p.Spec.TargetRef.Group = gatewayapiv1.GroupName
					p.Spec.TargetRef.Kind = "HTTPRoute"
					p.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
					p.Spec.Limits = map[string]kuadrantv1.Limit{
						"route-limit": {
							Rates: []kuadrantv1.Rate{{Limit: 100, Window: "1m"}},
						},
					}
				},
			)
			Expect(testClient().Create(ctx, routeRLP)).To(Succeed())
			routeRLPKey := client.ObjectKeyFromObject(routeRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// With atomic strategy, route policy replaces gateway defaults - should have only route policy source
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())

				// Should have 1 action with only route RLP data (atomic replacement)
				g.Expect(existingWASMConfig.ActionSets[0].Actions).To(HaveLen(1))

				// The action should list ONLY the route policy source (atomic replaces gateway defaults)
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(Equal([]string{
					"ratelimitpolicy.kuadrant.io:" + routeRLPKey.String(),
				}))

				// Verify only route limit's data is present
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].ConditionalData).To(HaveLen(1))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("EnvoyExtensionPolicy config includes source policy locators for RateLimitPolicy with overrides with atomic strategy", func(ctx SpecContext) {
			routeName := "test-route"
			gwRLPName := "gw-rlp"
			routeRLPName := "route-rlp"

			// Create HTTPRoute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			Expect(testClient().Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// Create Gateway RateLimitPolicy with overrides and atomic strategy
			gwRLP := policyFactory(
				func(p *kuadrantv1.RateLimitPolicy) {
					p.Name = gwRLPName
					p.Spec.TargetRef.Group = gatewayapiv1.GroupName
					p.Spec.TargetRef.Kind = "Gateway"
					p.Spec.TargetRef.Name = TestGatewayName
					p.Spec.Overrides = &kuadrantv1.MergeableRateLimitPolicySpec{
						Strategy: kuadrantv1.AtomicMergeStrategy, // Atomic strategy with overrides
						RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
							Limits: map[string]kuadrantv1.Limit{
								"gateway-limit": {
									Rates: []kuadrantv1.Rate{{Limit: 5, Window: "1m"}},
								},
							},
						},
					}
				},
			)
			Expect(testClient().Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKeyFromObject(gwRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Check EnvoyExtensionPolicy exists and has correct source for gateway policy
			extKey := client.ObjectKey{Name: wasm.ExtensionName(TestGatewayName), Namespace: testNamespace}
			existingExt := &egv1alpha1.EnvoyExtensionPolicy{}
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())
				g.Expect(existingWASMConfig.ActionSets[0].Actions).ToNot(BeEmpty())
				// Single policy source (gateway with overrides)
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(Equal([]string{
					"ratelimitpolicy.kuadrant.io:" + gwRLPKey.String(),
				}))
			}).WithContext(ctx).Should(Succeed())

			// Create Route RLP - with atomic overrides, gateway policy should override route policy entirely
			routeRLP := policyFactory(
				func(p *kuadrantv1.RateLimitPolicy) {
					p.Name = routeRLPName
					p.Spec.TargetRef.Group = gatewayapiv1.GroupName
					p.Spec.TargetRef.Kind = "HTTPRoute"
					p.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
					p.Spec.Limits = map[string]kuadrantv1.Limit{
						"route-limit": {
							Rates: []kuadrantv1.Rate{{Limit: 100, Window: "1m"}},
						},
					}
				},
			)
			Expect(testClient().Create(ctx, routeRLP)).To(Succeed())
			routeRLPKey := client.ObjectKeyFromObject(routeRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// With atomic overrides strategy, gateway policy atomically overrides route policy - should have only gateway policy source
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())

				// Should have 1 action with only gateway RLP data (atomic override)
				g.Expect(existingWASMConfig.ActionSets[0].Actions).To(HaveLen(1))

				// The action should list ONLY the gateway policy source (atomic overrides route policy)
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(Equal([]string{
					"ratelimitpolicy.kuadrant.io:" + gwRLPKey.String(),
				}))

				// Verify only gateway limit's data is present
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].ConditionalData).To(HaveLen(1))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("EnvoyExtensionPolicy config includes source policy locators for RateLimitPolicy with overrides with merge strategy", func(ctx SpecContext) {
			routeName := "test-route"
			gwRLPName := "gw-rlp"
			routeRLPName := "route-rlp"

			// Create HTTPRoute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			Expect(testClient().Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// Create Gateway RateLimitPolicy with overrides and merge strategy
			gwRLP := policyFactory(
				func(p *kuadrantv1.RateLimitPolicy) {
					p.Name = gwRLPName
					p.Spec.TargetRef.Group = gatewayapiv1.GroupName
					p.Spec.TargetRef.Kind = "Gateway"
					p.Spec.TargetRef.Name = gatewayapiv1.ObjectName(TestGatewayName)
					p.Spec.Overrides = &kuadrantv1.MergeableRateLimitPolicySpec{
						Strategy: kuadrantv1.PolicyRuleMergeStrategy, // Merge strategy with overrides
						RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
							Limits: map[string]kuadrantv1.Limit{
								"gateway-limit": {
									Rates: []kuadrantv1.Rate{{Limit: 5, Window: "1m"}},
								},
							},
						},
					}
				},
			)
			Expect(testClient().Create(ctx, gwRLP)).To(Succeed())
			gwRLPKey := client.ObjectKeyFromObject(gwRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Check EnvoyExtensionPolicy exists and has correct source for gateway policy
			extKey := client.ObjectKey{Name: wasm.ExtensionName(TestGatewayName), Namespace: testNamespace}
			existingExt := &egv1alpha1.EnvoyExtensionPolicy{}
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())
				g.Expect(existingWASMConfig.ActionSets[0].Actions).ToNot(BeEmpty())
				// Single policy source (gateway with overrides)
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(Equal([]string{
					"ratelimitpolicy.kuadrant.io:" + gwRLPKey.String(),
				}))
			}).WithContext(ctx).Should(Succeed())

			// Create Route RLP - with merge overrides, both policies should be present in sources
			routeRLP := policyFactory(
				func(p *kuadrantv1.RateLimitPolicy) {
					p.Name = routeRLPName
					p.Spec.TargetRef.Group = gatewayapiv1.GroupName
					p.Spec.TargetRef.Kind = "HTTPRoute"
					p.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeName)
					p.Spec.Limits = map[string]kuadrantv1.Limit{
						"route-limit": {
							Rates: []kuadrantv1.Rate{{Limit: 100, Window: "1m"}},
						},
					}
				},
			)
			Expect(testClient().Create(ctx, routeRLP)).To(Succeed())
			routeRLPKey := client.ObjectKeyFromObject(routeRLP)
			Eventually(tests.RLPIsAccepted(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeTrue())

			// With merge overrides strategy, both gateway and route policies are merged - should have both policy sources
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, extKey, existingExt)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromJSON(existingExt.Spec.Wasm[0].Config)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig.ActionSets).ToNot(BeEmpty())

				// Should have 1 merged action with both policies' data
				g.Expect(existingWASMConfig.ActionSets[0].Actions).To(HaveLen(1))

				// The action should list BOTH policy sources (merge overrides merges both policies)
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].SourcePolicyLocators).To(ConsistOf(
					"ratelimitpolicy.kuadrant.io:"+gwRLPKey.String(),
					"ratelimitpolicy.kuadrant.io:"+routeRLPKey.String(),
				))

				// Verify both limits' data is present
				g.Expect(existingWASMConfig.ActionSets[0].Actions[0].ConditionalData).To(HaveLen(2))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
