//go:build integration

package controllers

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	secv1beta1resources "istio.io/client-go/pkg/apis/security/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

const (
	testGatewayName   = "toystore-gw"
	testHTTPRouteName = "toystore-route"
)

var _ = Describe("AuthPolicy controller", func() {
	var testNamespace string

	BeforeEach(func() {
		CreateNamespace(&testNamespace)

		gateway := testBuildBasicGateway(testGatewayName, testNamespace)
		err := k8sClient.Create(context.Background(), gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() bool {
			existingGateway := &gatewayapiv1.Gateway{}
			err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), existingGateway)
			return err == nil && meta.IsStatusConditionTrue(existingGateway.Status.Conditions, common.GatewayProgrammedConditionType)
		}, 15*time.Second, 5*time.Second).Should(BeTrue())

		ApplyKuadrantCR(testNamespace)
	})

	AfterEach(DeleteNamespaceCallback(&testNamespace))

	Context("Basic HTTPRoute", func() {
		BeforeEach(func() {
			err := ApplyResources(filepath.Join("..", "examples", "toystore", "toystore.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())

			route := testBuildBasicHttpRoute(testHTTPRouteName, testGatewayName, testNamespace, []string{"*.toystore.com"})
			err = k8sClient.Create(context.Background(), route)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				existingRoute := &gatewayapiv1.HTTPRoute{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(route), existingRoute)
				return err == nil && common.IsHTTPRouteAccepted(existingRoute)
			}, 15*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("Attaches policy to the Gateway", func() {
			policy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-auth",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "Gateway",
						Name:      testGatewayName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}
			policy.Spec.AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"

			err := k8sClient.Create(context.Background(), policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(testPolicyIsReady(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: istioAuthorizationPolicyName(testGatewayName, policy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			Expect(iap.Spec.Rules).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[0].To[0].Operation.Hosts).To(Equal([]string{"*"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"GET"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/toy*"}))

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: authConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil || authConfig.Status.Ready()
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
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
		})

		It("Attaches policy to the HTTPRoute", func() {
			policy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "HTTPRoute",
						Name:      testHTTPRouteName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}

			err := k8sClient.Create(context.Background(), policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(testPolicyIsReady(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: istioAuthorizationPolicyName(testGatewayName, policy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			Expect(iap.Spec.Rules).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[0].To[0].Operation.Hosts).To(Equal([]string{"*.toystore.com"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"GET"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/toy*"}))

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: authConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			logf.Log.V(1).Info("authConfig.Spec", "hosts", authConfig.Spec.Hosts, "conditions", authConfig.Spec.Conditions)
			Expect(authConfig.Spec.Hosts).To(Equal([]string{"*.toystore.com"}))
			Expect(authConfig.Spec.Conditions[0].Any).To(HaveLen(1))        // 1 HTTPRouteRule in the HTTPRoute
			Expect(authConfig.Spec.Conditions[0].Any[0].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal("GET"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("/toy.*"))
		})

		It("Attaches policy to the Gateway while having other policies attached to some HTTPRoutes", func() {
			routePolicy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "HTTPRoute",
						Name:      testHTTPRouteName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}

			err := k8sClient.Create(context.Background(), routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(testPolicyIsReady(routePolicy), 30*time.Second, 5*time.Second).Should(BeTrue())

			// create second (policyless) httproute
			otherRoute := testBuildBasicHttpRoute("policyless-route", testGatewayName, testNamespace, []string{"*.other"})
			otherRoute.Spec.Rules = []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Method: ptr.To(gatewayapiv1.HTTPMethod("POST")),
						},
					},
				},
			}
			err = k8sClient.Create(context.Background(), otherRoute)
			Expect(err).ToNot(HaveOccurred())

			// attach policy to the gatewaay
			gwPolicy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-auth",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "Gateway",
						Name:      testGatewayName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}

			err = k8sClient.Create(context.Background(), gwPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(gwPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(testPolicyIsReady(gwPolicy), 30*time.Second, 5*time.Second).Should(BeTrue())

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: istioAuthorizationPolicyName(testGatewayName, gwPolicy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			Expect(iap.Spec.Rules).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[0].To[0].Operation.Hosts).To(Equal([]string{"*"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"POST"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/*"}))

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: authConfigName(client.ObjectKeyFromObject(gwPolicy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil || authConfig.Status.Ready()
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			logf.Log.V(1).Info("authConfig.Spec", "hosts", authConfig.Spec.Hosts, "conditions", authConfig.Spec.Conditions)
			Expect(authConfig.Spec.Hosts).To(Equal([]string{"*"}))
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
		})

		It("Attaches policy to the Gateway while having other policies attached to all HTTPRoutes", func() {
			routePolicy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "HTTPRoute",
						Name:      testHTTPRouteName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}

			err := k8sClient.Create(context.Background(), routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(testPolicyIsReady(routePolicy), 30*time.Second, 5*time.Second).Should(BeTrue())

			// attach policy to the gatewaay
			gwPolicy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-auth",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "Gateway",
						Name:      testGatewayName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}

			err = k8sClient.Create(context.Background(), gwPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(gwPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(func() bool {
				existingPolicy := &api.AuthPolicy{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gwPolicy), existingPolicy)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(existingPolicy.Status.Conditions, APAvailableConditionType)
				return condition != nil && condition.Reason == "AuthSchemeNotReady"
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: istioAuthorizationPolicyName(testGatewayName, gwPolicy.Spec.TargetRef), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), iapKey, &secv1beta1resources.AuthorizationPolicy{})
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: authConfigName(client.ObjectKeyFromObject(gwPolicy)), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authConfigKey, &authorinoapi.AuthConfig{})
				return apierrors.IsNotFound(err)
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("Rejects policy with only unmatching top-level route selectors while trying to configure the gateway", func() {
			policy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "HTTPRoute",
						Name:      testHTTPRouteName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					RouteSelectors: []api.RouteSelector{
						{ // does not select any HTTPRouteRule
							Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
								{
									Method: ptr.To(gatewayapiv1alpha2.HTTPMethod("DELETE")),
								},
							},
						},
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}

			err := k8sClient.Create(context.Background(), policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(func() bool {
				existingPolicy := &api.AuthPolicy{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(policy), existingPolicy)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(existingPolicy.Status.Conditions, APAvailableConditionType)
				return condition != nil && condition.Reason == "ReconciliationError" && strings.Contains(condition.Message, "cannot match any route rules, check for invalid route selectors in the policy")
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: istioAuthorizationPolicyName(testGatewayName, policy.Spec.TargetRef), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), iapKey, &secv1beta1resources.AuthorizationPolicy{})
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: authConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authConfigKey, &authorinoapi.AuthConfig{})
				return apierrors.IsNotFound(err)
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("Rejects policy with only unmatching config-level route selectors post-configuring the gateway", func() {
			policy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "HTTPRoute",
						Name:      testHTTPRouteName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}
			config := policy.Spec.AuthScheme.Authentication["apiKey"]
			config.RouteSelectors = []api.RouteSelector{
				{ // does not select any HTTPRouteRule
					Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
						{
							Method: ptr.To(gatewayapiv1alpha2.HTTPMethod("DELETE")),
						},
					},
				},
			}
			policy.Spec.AuthScheme.Authentication["apiKey"] = config

			err := k8sClient.Create(context.Background(), policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(func() bool {
				existingPolicy := &api.AuthPolicy{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(policy), existingPolicy)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(existingPolicy.Status.Conditions, APAvailableConditionType)
				return condition != nil && condition.Reason == "ReconciliationError" && strings.Contains(condition.Message, "cannot match any route rules, check for invalid route selectors in the policy")
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			iapKey := types.NamespacedName{Name: istioAuthorizationPolicyName(testGatewayName, policy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			Expect(iap.Spec.Rules).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[0].To[0].Operation.Hosts).To(Equal([]string{"*.toystore.com"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"GET"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/toy*"}))

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: authConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authConfigKey, &authorinoapi.AuthConfig{})
				return apierrors.IsNotFound(err)
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("Deletes resources when the policy is deleted", func() {
			policy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "HTTPRoute",
						Name:      testHTTPRouteName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}

			err := k8sClient.Create(context.Background(), policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(testPolicyIsReady(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			// delete policy
			err = k8sClient.Delete(context.Background(), policy)
			logf.Log.V(1).Info("Deleting AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: istioAuthorizationPolicyName(testGatewayName, policy.Spec.TargetRef), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), iapKey, &secv1beta1resources.AuthorizationPolicy{})
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: authConfigName(client.ObjectKey{Name: "toystore", Namespace: testNamespace}), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authConfigKey, &authorinoapi.AuthConfig{})
				return apierrors.IsNotFound(err)
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("Maps to all fields of the AuthConfig", func() {
			policy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "HTTPRoute",
						Name:      testHTTPRouteName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					NamedPatterns: map[string]authorinoapi.PatternExpressions{
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
					},
					Conditions: []authorinoapi.PatternExpressionOrRef{
						{
							PatternRef: authorinoapi.PatternRef{
								Name: "internal-source",
							},
						},
					},
					AuthScheme: api.AuthSchemeSpec{
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
					},
				},
			}

			err := k8sClient.Create(context.Background(), policy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(testPolicyIsReady(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: authConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			authConfigSpecAsJSON, _ := json.Marshal(authConfig.Spec)
			Expect(string(authConfigSpecAsJSON)).To(Equal(`{"hosts":["*.toystore.com"],"patterns":{"authz-and-rl-required":[{"selector":"source.ip","operator":"neq","value":"192.168.0.10"}],"internal-source":[{"selector":"source.ip","operator":"matches","value":"192\\.168\\..*"}]},"when":[{"patternRef":"internal-source"},{"any":[{"any":[{"all":[{"selector":"request.method","operator":"eq","value":"GET"},{"selector":"request.url_path","operator":"matches","value":"/toy.*"}]}]}]}],"authentication":{"jwt":{"when":[{"selector":"filter_metadata.envoy\\.filters\\.http\\.jwt_authn|verified_jwt","operator":"neq"}],"credentials":{"authorizationHeader":{}},"plain":{"selector":"filter_metadata.envoy\\.filters\\.http\\.jwt_authn|verified_jwt"}}},"metadata":{"user-groups":{"when":[{"selector":"auth.identity.admin","operator":"neq","value":"true"}],"http":{"url":"http://user-groups/username={auth.identity.username}","method":"GET","contentType":"application/x-www-form-urlencoded","credentials":{"authorizationHeader":{}}}}},"authorization":{"admin-or-privileged":{"when":[{"patternRef":"authz-and-rl-required"}],"patternMatching":{"patterns":[{"any":[{"selector":"auth.identity.admin","operator":"eq","value":"true"},{"selector":"auth.metadata.user-groups","operator":"incl","value":"privileged"}]}]}}},"response":{"unauthenticated":{"message":{"value":"Missing verified JWT injected by the gateway"}},"unauthorized":{"message":{"value":"User must be admin or member of privileged group"}},"success":{"headers":{"x-username":{"when":[{"selector":"request.headers.x-propagate-username.@case:lower","operator":"matches","value":"1|yes|true"}],"plain":{"value":null,"selector":"auth.identity.username"}}},"dynamicMetadata":{"x-auth-data":{"when":[{"patternRef":"authz-and-rl-required"}],"json":{"properties":{"groups":{"value":null,"selector":"auth.metadata.user-groups"},"username":{"value":null,"selector":"auth.identity.username"}}}}}}},"callbacks":{"unauthorized-attempt":{"when":[{"patternRef":"authz-and-rl-required"},{"selector":"auth.authorization.admin-or-privileged","operator":"neq","value":"true"}],"http":{"url":"http://events/unauthorized","method":"POST","body":{"value":null,"selector":"\\{\"identity\":{auth.identity},\"request-id\":{request.id}\\}"},"contentType":"application/json","credentials":{"authorizationHeader":{}}}}}}`))
		})
	})

	Context("Complex HTTPRoute with multiple rules and hostnames", func() {
		BeforeEach(func() {
			err := ApplyResources(filepath.Join("..", "examples", "toystore", "toystore.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())

			route := testBuildMultipleRulesHttpRoute(testHTTPRouteName, testGatewayName, testNamespace, []string{"*.toystore.com", "*.admin.toystore.com"})
			err = k8sClient.Create(context.Background(), route)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				existingRoute := &gatewayapiv1.HTTPRoute{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(route), existingRoute)
				return err == nil && common.IsHTTPRouteAccepted(existingRoute)
			}, 15*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("Attaches simple policy to the HTTPRoute", func() {
			policy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "HTTPRoute",
						Name:      testHTTPRouteName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}

			err := k8sClient.Create(context.Background(), policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(testPolicyIsReady(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: istioAuthorizationPolicyName(testGatewayName, policy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			Expect(iap.Spec.Rules).To(HaveLen(3))
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[0].To[0].Operation.Hosts).To(Equal([]string{"*.toystore.com", "*.admin.toystore.com"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"POST"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/admin*"}))
			Expect(iap.Spec.Rules[1].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[1].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[1].To[0].Operation.Hosts).To(Equal([]string{"*.toystore.com", "*.admin.toystore.com"}))
			Expect(iap.Spec.Rules[1].To[0].Operation.Methods).To(Equal([]string{"DELETE"}))
			Expect(iap.Spec.Rules[1].To[0].Operation.Paths).To(Equal([]string{"/admin*"}))
			Expect(iap.Spec.Rules[2].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[2].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[2].To[0].Operation.Hosts).To(Equal([]string{"*.toystore.com", "*.admin.toystore.com"}))
			Expect(iap.Spec.Rules[2].To[0].Operation.Methods).To(Equal([]string{"GET"}))
			Expect(iap.Spec.Rules[2].To[0].Operation.Paths).To(Equal([]string{"/private*"}))

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: authConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil || authConfig.Status.Ready()
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			logf.Log.V(1).Info("authConfig.Spec", "hosts", authConfig.Spec.Hosts, "conditions", authConfig.Spec.Conditions)
			Expect(authConfig.Spec.Hosts).To(Equal([]string{"*.toystore.com", "*.admin.toystore.com"}))
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
		})

		It("Attaches policy with top-level route selectors to the HTTPRoute", func() {
			policy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "HTTPRoute",
						Name:      testHTTPRouteName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					RouteSelectors: []api.RouteSelector{
						{ // Selects: POST|DELETE *.admin.toystore.com/admin*
							Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
								{
									Path: &gatewayapiv1alpha2.HTTPPathMatch{
										Type:  ptr.To(gatewayapiv1alpha2.PathMatchType("PathPrefix")),
										Value: ptr.To("/admin"),
									},
								},
							},
							Hostnames: []gatewayapiv1.Hostname{"*.admin.toystore.com"},
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
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}

			err := k8sClient.Create(context.Background(), policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(testPolicyIsReady(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: istioAuthorizationPolicyName(testGatewayName, policy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			Expect(iap.Spec.Rules).To(HaveLen(3))
			// POST *.admin.toystore.com/admin*
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[0].To[0].Operation.Hosts).To(Equal([]string{"*.admin.toystore.com"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"POST"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/admin*"}))
			// DELETE *.admin.toystore.com/admin*
			Expect(iap.Spec.Rules[1].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[1].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[1].To[0].Operation.Hosts).To(Equal([]string{"*.admin.toystore.com"}))
			Expect(iap.Spec.Rules[1].To[0].Operation.Methods).To(Equal([]string{"DELETE"}))
			Expect(iap.Spec.Rules[1].To[0].Operation.Paths).To(Equal([]string{"/admin*"}))
			// GET (*.toystore.com|*.admin.toystore.com)/private*
			Expect(iap.Spec.Rules[2].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[2].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[2].To[0].Operation.Hosts).To(Equal([]string{"*.toystore.com", "*.admin.toystore.com"}))
			Expect(iap.Spec.Rules[2].To[0].Operation.Methods).To(Equal([]string{"GET"}))
			Expect(iap.Spec.Rules[2].To[0].Operation.Paths).To(Equal([]string{"/private*"}))

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: authConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			logf.Log.V(1).Info("authConfig.Spec", "hosts", authConfig.Spec.Hosts, "conditions", authConfig.Spec.Conditions)
			Expect(authConfig.Spec.Hosts).To(Equal([]string{"*.toystore.com", "*.admin.toystore.com"}))
			Expect(authConfig.Spec.Conditions).To(HaveLen(1))
			Expect(authConfig.Spec.Conditions[0].Any).To(HaveLen(2))        // 2 HTTPRouteRules in the HTTPRoute
			Expect(authConfig.Spec.Conditions[0].Any[0].Any).To(HaveLen(2)) // 2 HTTPRouteMatches in the 1st HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All).To(HaveLen(3))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("request.host"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal(`.*\.admin\.toystore\.com`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal("request.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("POST"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[2].Selector).To(Equal(`request.url_path`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[2].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[2].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All).To(HaveLen(3))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Selector).To(Equal("request.host"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Value).To(Equal(`.*\.admin\.toystore\.com`))
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
		})

		It("Attaches policy with config-level route selectors to the HTTPRoute", func() {
			policy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "HTTPRoute",
						Name:      testHTTPRouteName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}
			config := policy.Spec.AuthScheme.Authentication["apiKey"]
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
					Hostnames: []gatewayapiv1.Hostname{"*.admin.toystore.com"},
				},
			}
			policy.Spec.AuthScheme.Authentication["apiKey"] = config

			err := k8sClient.Create(context.Background(), policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(testPolicyIsReady(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: istioAuthorizationPolicyName(testGatewayName, policy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			Expect(iap.Spec.Rules).To(HaveLen(3))
			// POST *.admin.toystore.com/admin*
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[2].To[0].Operation.Hosts).To(Equal([]string{"*.toystore.com", "*.admin.toystore.com"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"POST"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/admin*"}))
			// DELETE *.admin.toystore.com/admin*
			Expect(iap.Spec.Rules[1].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[1].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[2].To[0].Operation.Hosts).To(Equal([]string{"*.toystore.com", "*.admin.toystore.com"}))
			Expect(iap.Spec.Rules[1].To[0].Operation.Methods).To(Equal([]string{"DELETE"}))
			Expect(iap.Spec.Rules[1].To[0].Operation.Paths).To(Equal([]string{"/admin*"}))
			// GET (*.toystore.com|*.admin.toystore.com)/private*
			Expect(iap.Spec.Rules[2].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[2].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[2].To[0].Operation.Hosts).To(Equal([]string{"*.toystore.com", "*.admin.toystore.com"}))
			Expect(iap.Spec.Rules[2].To[0].Operation.Methods).To(Equal([]string{"GET"}))
			Expect(iap.Spec.Rules[2].To[0].Operation.Paths).To(Equal([]string{"/private*"}))

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: authConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			apiKeyConditions := authConfig.Spec.Authentication["apiKey"].Conditions
			logf.Log.V(1).Info("authConfig.Spec", "hosts", authConfig.Spec.Hosts, "conditions", authConfig.Spec.Conditions, "apiKey conditions", apiKeyConditions)
			Expect(authConfig.Spec.Hosts).To(Equal([]string{"*.toystore.com", "*.admin.toystore.com"}))
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
			Expect(apiKeyConditions[0].Any[0].Any[0].All[0].Value).To(Equal(`.*\.admin\.toystore\.com`))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[1].Selector).To(Equal("request.method"))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[1].Value).To(Equal("POST"))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[2].Selector).To(Equal(`request.url_path`))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[2].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[2].Value).To(Equal("/admin.*"))
			Expect(apiKeyConditions[0].Any[0].Any[1].All).To(HaveLen(3))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[0].Selector).To(Equal("request.host"))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[0].Value).To(Equal(`.*\.admin\.toystore\.com`))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[1].Selector).To(Equal("request.method"))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[1].Value).To(Equal("DELETE"))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[2].Selector).To(Equal(`request.url_path`))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[2].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[2].Value).To(Equal("/admin.*"))
		})

		It("Mixes route selectors into other conditions", func() {
			policy := &api.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: api.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     "gateway.networking.k8s.io",
						Kind:      "HTTPRoute",
						Name:      testHTTPRouteName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}
			config := policy.Spec.AuthScheme.Authentication["apiKey"]
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
			policy.Spec.AuthScheme.Authentication["apiKey"] = config

			err := k8sClient.Create(context.Background(), policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(testPolicyIsReady(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			// check authorino authconfig
			authConfigKey := types.NamespacedName{Name: authConfigName(client.ObjectKeyFromObject(policy)), Namespace: testNamespace}
			authConfig := &authorinoapi.AuthConfig{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authConfigKey, authConfig)
				logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", authConfigKey.String(), "error", err)
				return err == nil && authConfig.Status.Ready()
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			apiKeyConditions := authConfig.Spec.Authentication["apiKey"].Conditions
			logf.Log.V(1).Info("authConfig.Spec", "hosts", authConfig.Spec.Hosts, "conditions", authConfig.Spec.Conditions, "apiKey conditions", apiKeyConditions)
			Expect(authConfig.Spec.Hosts).To(Equal([]string{"*.toystore.com", "*.admin.toystore.com"}))
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
		})
	})

	Context("TODO: Targeted resource does not exist", func() {})
})

func testBasicAuthScheme() api.AuthSchemeSpec {
	return api.AuthSchemeSpec{
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

func testPolicyIsReady(policy *api.AuthPolicy) func() bool {
	return func() bool {
		existingPolicy := &api.AuthPolicy{}
		err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(policy), existingPolicy)
		return err == nil && meta.IsStatusConditionTrue(existingPolicy.Status.Conditions, "Available")
	}
}
