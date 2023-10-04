//go:build integration

package controllers

import (
	"context"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	secv1beta1resources "istio.io/client-go/pkg/apis/security/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

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
			existingGateway := &gatewayapiv1beta1.Gateway{}
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
				existingRoute := &gatewayapiv1beta1.HTTPRoute{}
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
						Namespace: ptr.To(gatewayapiv1beta1.Namespace(testNamespace)),
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
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal("GET"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
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
						Namespace: ptr.To(gatewayapiv1beta1.Namespace(testNamespace)),
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
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal("GET"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("/toy.*"))
		})

		It("Attaches policy to the Gateway while having other policies attached to HTTPRoutes", func() {
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
						Namespace: ptr.To(gatewayapiv1beta1.Namespace(testNamespace)),
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
			otherRoute.Spec.Rules = []gatewayapiv1beta1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1beta1.HTTPRouteMatch{
						{
							Method: ptr.To(gatewayapiv1beta1.HTTPMethod("POST")),
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
						Namespace: ptr.To(gatewayapiv1beta1.Namespace(testNamespace)),
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
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal("POST"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("/.*"))
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
						Namespace: ptr.To(gatewayapiv1beta1.Namespace(testNamespace)),
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
	})

	Context("Complex HTTPRoute with multiple rules and hostnames", func() {
		BeforeEach(func() {
			err := ApplyResources(filepath.Join("..", "examples", "toystore", "toystore.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())

			route := testBuildMultipleRulesHttpRoute(testHTTPRouteName, testGatewayName, testNamespace, []string{"*.toystore.com", "*.admin.toystore.com"})
			err = k8sClient.Create(context.Background(), route)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				existingRoute := &gatewayapiv1beta1.HTTPRoute{}
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
						Namespace: ptr.To(gatewayapiv1beta1.Namespace(testNamespace)),
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
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal("POST"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Value).To(Equal("DELETE"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the 2nd HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Value).To(Equal("GET"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
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
						Namespace: ptr.To(gatewayapiv1beta1.Namespace(testNamespace)),
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
							Hostnames: []gatewayapiv1beta1.Hostname{"*.admin.toystore.com"},
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
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("context.request.http.host"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal(`.*\.admin\.toystore\.com`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("POST"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[2].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[2].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[2].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All).To(HaveLen(3))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Selector).To(Equal("context.request.http.host"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Value).To(Equal(`.*\.admin\.toystore\.com`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Value).To(Equal("DELETE"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[2].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[2].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[2].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the 2nd HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Value).To(Equal("GET"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
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
						Namespace: ptr.To(gatewayapiv1beta1.Namespace(testNamespace)),
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
					Hostnames: []gatewayapiv1beta1.Hostname{"*.admin.toystore.com"},
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
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal("POST"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Value).To(Equal("DELETE"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the 2nd HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Value).To(Equal("GET"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Value).To(Equal("/private.*"))
			Expect(apiKeyConditions).To(HaveLen(1))
			Expect(apiKeyConditions[0].Any).To(HaveLen(1))        // 1 HTTPRouteRule selected from the HTTPRoute
			Expect(apiKeyConditions[0].Any[0].Any).To(HaveLen(2)) // 2 HTTPRouteMatches in the HTTPRouteRule
			Expect(apiKeyConditions[0].Any[0].Any[0].All).To(HaveLen(3))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[0].Selector).To(Equal("context.request.http.host"))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[0].Value).To(Equal(`.*\.admin\.toystore\.com`))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[1].Selector).To(Equal("context.request.http.method"))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[1].Value).To(Equal("POST"))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[2].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[2].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(apiKeyConditions[0].Any[0].Any[0].All[2].Value).To(Equal("/admin.*"))
			Expect(apiKeyConditions[0].Any[0].Any[1].All).To(HaveLen(3))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[0].Selector).To(Equal("context.request.http.host"))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[0].Value).To(Equal(`.*\.admin\.toystore\.com`))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[1].Selector).To(Equal("context.request.http.method"))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[1].Value).To(Equal("DELETE"))
			Expect(apiKeyConditions[0].Any[0].Any[1].All[2].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
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
						Namespace: ptr.To(gatewayapiv1beta1.Namespace(testNamespace)),
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}
			config := policy.Spec.AuthScheme.Authentication["apiKey"]
			config.RouteSelectors = []api.RouteSelector{
				{ // Selects: GET /private*
					Matches: []gatewayapiv1beta1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1beta1.HTTPPathMatch{
								Type:  ptr.To(gatewayapiv1beta1.PathMatchType("PathPrefix")),
								Value: ptr.To("/private"),
							},
							Method: ptr.To(gatewayapiv1beta1.HTTPMethod("GET")),
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
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[0].Value).To(Equal("POST"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[0].All[1].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[0].Value).To(Equal("DELETE"))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[0].Any[1].All[1].Value).To(Equal("/admin.*"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the 2nd HTTPRouteRule
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All).To(HaveLen(2))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[0].Value).To(Equal("GET"))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(authConfig.Spec.Conditions[0].Any[1].Any[0].All[1].Value).To(Equal("/private.*"))
			Expect(apiKeyConditions).To(HaveLen(2)) // 1 existed condition + 1 HTTPRouteRule selected from the HTTPRoute
			Expect(apiKeyConditions[0].Selector).To(Equal("context.source.address.Address.SocketAddress.address"))
			Expect(apiKeyConditions[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("matches")))
			Expect(apiKeyConditions[0].Value).To(Equal(`192\.168\.0\..*`))
			Expect(apiKeyConditions[1].Any).To(HaveLen(1))        // 1 HTTPRouteRule selected from the HTTPRoute
			Expect(apiKeyConditions[1].Any[0].Any).To(HaveLen(1)) // 1 HTTPRouteMatch in the HTTPRouteRule
			Expect(apiKeyConditions[1].Any[0].Any[0].All).To(HaveLen(2))
			Expect(apiKeyConditions[1].Any[0].Any[0].All[0].Selector).To(Equal("context.request.http.method"))
			Expect(apiKeyConditions[1].Any[0].Any[0].All[0].Operator).To(Equal(authorinoapi.PatternExpressionOperator("eq")))
			Expect(apiKeyConditions[1].Any[0].Any[0].All[0].Value).To(Equal("GET"))
			Expect(apiKeyConditions[1].Any[0].Any[0].All[1].Selector).To(Equal(`context.request.http.path.@extract:{"sep":"?"}`))
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
