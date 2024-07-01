//go:build integration

package istio_test

import (
	"fmt"
	"strings"
	"time"

	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	secv1beta1resources "istio.io/client-go/pkg/apis/security/v1beta1"
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

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/controllers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("AuthPolicy controller managing authorization policy", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gwHost        = fmt.Sprintf("*.toystore-%s.com", rand.String(4))
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

	policyFactory := func(mutateFns ...func(policy *kuadrantv1beta2.AuthPolicy)) *kuadrantv1beta2.AuthPolicy {
		policy := &kuadrantv1beta2.AuthPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AuthPolicy",
				APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "toystore",
				Namespace: testNamespace,
			},
			Spec: kuadrantv1beta2.AuthPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group:     gatewayapiv1.GroupName,
					Kind:      "HTTPRoute",
					Name:      TestHTTPRouteName,
					Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
				},
				Defaults: &kuadrantv1beta2.AuthPolicyCommonSpec{
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
		return strings.Replace(gwHost, "*", rand.String(4), 1)
	}

	Context("policy attached to the gateway", func() {

		var (
			gwPolicy *kuadrantv1beta2.AuthPolicy
		)

		BeforeEach(func(ctx SpecContext) {
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err := k8sClient.Create(ctx, route)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			gwPolicy = policyFactory(func(policy *kuadrantv1beta2.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})

			err = k8sClient.Create(ctx, gwPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(gwPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gwPolicy)).WithContext(ctx).Should(BeTrue())
		})

		It("authpolicy has rules added", func(ctx SpecContext) {
			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: controllers.IstioAuthorizationPolicyName(TestGatewayName, gwPolicy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			// has the correct target ref
			Expect(iap.Spec.TargetRef.Group).To(Equal("gateway.networking.k8s.io"))
			Expect(iap.Spec.TargetRef.Kind).To(Equal("Gateway"))
			Expect(iap.Spec.TargetRef.Name).To(Equal(TestGatewayName))
			Expect(iap.Spec.Rules).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[0].To[0].Operation.Hosts).To(Equal([]string{gwHost}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"GET"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/toy*"}))
		}, testTimeOut)
	})

	Context("policy attached to the route", func() {
		var (
			routePolicy *kuadrantv1beta2.AuthPolicy
			routeHost   = randomHostFromGWHost()
		)

		BeforeEach(func(ctx SpecContext) {
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{routeHost})
			err := k8sClient.Create(ctx, route)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			routePolicy = policyFactory(func(policy *kuadrantv1beta2.AuthPolicy) {
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = TestHTTPRouteName
			})

			err = k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), routePolicy)).WithContext(ctx).Should(BeTrue())
		})

		It("authorization policy has rules added", func(ctx SpecContext) {
			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: controllers.IstioAuthorizationPolicyName(TestGatewayName, routePolicy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			// has the correct target ref
			Expect(iap.Spec.TargetRef.Group).To(Equal("gateway.networking.k8s.io"))
			Expect(iap.Spec.TargetRef.Kind).To(Equal("Gateway"))
			Expect(iap.Spec.TargetRef.Name).To(Equal(TestGatewayName))
			Expect(iap.Spec.Rules).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[0].To[0].Operation.Hosts).To(Equal([]string{routeHost}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"GET"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/toy*"}))
		}, testTimeOut)

		It("Deletes authorizationpolicy when the policy is deleted", func(ctx SpecContext) {
			// delete policy
			err := k8sClient.Delete(ctx, routePolicy)
			logf.Log.V(1).Info("Deleting AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: controllers.IstioAuthorizationPolicyName(TestGatewayName, routePolicy.Spec.TargetRef), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, iapKey, &secv1beta1resources.AuthorizationPolicy{})
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("Attaches policy to the Gateway while having other policies attached to some HTTPRoutes", func() {
		// Gw A
		// Route A -> Gw A
		// Route B -> Gw A
		// RLP 1 -> Gw A
		// RLP 2 -> Route A
		var (
			gwPolicy  *kuadrantv1beta2.AuthPolicy
			routeHost = randomHostFromGWHost()
		)
		BeforeEach(func(ctx SpecContext) {
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{routeHost})
			err := k8sClient.Create(ctx, route)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			gwPolicy = policyFactory(func(policy *kuadrantv1beta2.AuthPolicy) {
				policy.Name = "gw-auth"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"].ApiKey.Selector.MatchLabels["admin"] = "yes"
			})

			err = k8sClient.Create(ctx, gwPolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(gwPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), gwPolicy)).WithContext(ctx).Should(BeTrue())

			routePolicy := policyFactory(func(policy *kuadrantv1beta2.AuthPolicy) {
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = TestHTTPRouteName
			})

			err = k8sClient.Create(ctx, routePolicy)
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
		})

		It("check istio authorizationpolicy", func(ctx SpecContext) {
			iapKey := types.NamespacedName{Name: controllers.IstioAuthorizationPolicyName(TestGatewayName, gwPolicy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())
			Expect(iap.Spec.Rules).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[0].To[0].Operation.Hosts).To(Equal([]string{gwHost}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"POST"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/*"}))
		}, testTimeOut)
	})

	Context("Attaches policy to the route with only unmatching top-level route selector", func() {
		var (
			routePolicy *kuadrantv1beta2.AuthPolicy
			routeHost   = randomHostFromGWHost()
		)
		// Gw A
		// Route A -> Gw A
		// RLP 1 -> Route A
		BeforeEach(func(ctx SpecContext) {
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{routeHost})
			err := k8sClient.Create(ctx, route)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			routePolicy = policyFactory(func(policy *kuadrantv1beta2.AuthPolicy) {
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = TestHTTPRouteName
				policy.Spec.CommonSpec().RouteSelectors = []kuadrantv1beta2.RouteSelector{
					{ // does not select any HTTPRouteRule
						Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
							{
								Method: ptr.To(gatewayapiv1alpha2.HTTPMethod("DELETE")),
							},
						},
					},
				}
			})

			err = k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Rejects policy and authorizationpolicy does not exist", func(ctx SpecContext) {
			// check policy status
			Eventually(func() bool {
				existingPolicy := &kuadrantv1beta2.AuthPolicy{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(routePolicy), existingPolicy)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				return condition != nil && condition.Reason == string(kuadrant.PolicyReasonUnknown) && strings.Contains(condition.Message, "cannot match any route rules, check for invalid route selectors in the policy")
			}).WithContext(ctx).Should(BeTrue())

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: controllers.IstioAuthorizationPolicyName(TestGatewayName, routePolicy.Spec.TargetRef), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, iapKey, &secv1beta1resources.AuthorizationPolicy{})
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("Attaches policy to the route with only unmatching config-level route selector", func() {
		var (
			routePolicy *kuadrantv1beta2.AuthPolicy
			routeHost   = randomHostFromGWHost()
		)
		// Gw A
		// Route A -> Gw A
		// RLP 1 -> Route A
		BeforeEach(func(ctx SpecContext) {
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{routeHost})
			err := k8sClient.Create(ctx, route)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			routePolicy = policyFactory(func(policy *kuadrantv1beta2.AuthPolicy) {
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = TestHTTPRouteName
				config := policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"]
				config.RouteSelectors = []kuadrantv1beta2.RouteSelector{
					{ // does not select any HTTPRouteRule
						Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
							{
								Method: ptr.To(gatewayapiv1alpha2.HTTPMethod("DELETE")),
							},
						},
					},
				}
				policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"] = config
			})

			err = k8sClient.Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Rejects policy and authorizationpolicy exists", func(ctx SpecContext) {
			// check policy status
			Eventually(func() bool {
				existingPolicy := &kuadrantv1beta2.AuthPolicy{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(routePolicy), existingPolicy)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(existingPolicy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				return condition != nil && condition.Reason == string(kuadrant.PolicyReasonUnknown) && strings.Contains(condition.Message, "cannot match any route rules, check for invalid route selectors in the policy")
			}).WithContext(ctx).Should(BeTrue())

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: controllers.IstioAuthorizationPolicyName(TestGatewayName, routePolicy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())
			Expect(iap.Spec.Rules).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[0].To[0].Operation.Hosts).To(Equal([]string{routeHost}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"GET"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/toy*"}))
		}, testTimeOut)
	})

	Context("Complex HTTPRoute with multiple rules and hostnames", func() {

		var (
			routeHost1 = randomHostFromGWHost()
			routeHost2 = randomHostFromGWHost()
		)

		BeforeEach(func(ctx SpecContext) {
			route := tests.BuildMultipleRulesHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{routeHost1, routeHost2})
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

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: controllers.IstioAuthorizationPolicyName(TestGatewayName, policy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())
			Expect(iap.Spec.Rules).To(HaveLen(3))
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[0].To[0].Operation.Hosts).To(Equal([]string{routeHost1, routeHost2}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"POST"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/admin*"}))
			Expect(iap.Spec.Rules[1].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[1].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[1].To[0].Operation.Hosts).To(Equal([]string{routeHost1, routeHost2}))
			Expect(iap.Spec.Rules[1].To[0].Operation.Methods).To(Equal([]string{"DELETE"}))
			Expect(iap.Spec.Rules[1].To[0].Operation.Paths).To(Equal([]string{"/admin*"}))
			Expect(iap.Spec.Rules[2].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[2].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[2].To[0].Operation.Hosts).To(Equal([]string{routeHost1, routeHost2}))
			Expect(iap.Spec.Rules[2].To[0].Operation.Methods).To(Equal([]string{"GET"}))
			Expect(iap.Spec.Rules[2].To[0].Operation.Paths).To(Equal([]string{"/private*"}))
		}, testTimeOut)

		It("Attaches policy with top-level route selectors to the HTTPRoute", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.AuthPolicy) {
				policy.Spec.CommonSpec().RouteSelectors = []kuadrantv1beta2.RouteSelector{
					{ // Selects: POST|DELETE *.admin.toystore.com/admin*
						Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
							{
								Path: &gatewayapiv1alpha2.HTTPPathMatch{
									Type:  ptr.To(gatewayapiv1alpha2.PathMatchType("PathPrefix")),
									Value: ptr.To("/admin"),
								},
							},
						},
						Hostnames: []gatewayapiv1.Hostname{gatewayapiv1.Hostname(routeHost2)},
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

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: controllers.IstioAuthorizationPolicyName(TestGatewayName, policy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())
			Expect(iap.Spec.Rules).To(HaveLen(3))
			// POST *.admin.toystore.com/admin*
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[0].To[0].Operation.Hosts).To(Equal([]string{routeHost2}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"POST"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/admin*"}))
			// DELETE *.admin.toystore.com/admin*
			Expect(iap.Spec.Rules[1].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[1].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[1].To[0].Operation.Hosts).To(Equal([]string{routeHost2}))
			Expect(iap.Spec.Rules[1].To[0].Operation.Methods).To(Equal([]string{"DELETE"}))
			Expect(iap.Spec.Rules[1].To[0].Operation.Paths).To(Equal([]string{"/admin*"}))
			// GET (*.toystore.com|*.admin.toystore.com)/private*
			Expect(iap.Spec.Rules[2].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[2].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[2].To[0].Operation.Hosts).To(Equal([]string{routeHost1, routeHost2}))
			Expect(iap.Spec.Rules[2].To[0].Operation.Methods).To(Equal([]string{"GET"}))
			Expect(iap.Spec.Rules[2].To[0].Operation.Paths).To(Equal([]string{"/private*"}))
		}, testTimeOut)

		It("Attaches policy with config-level route selectors to the HTTPRoute", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta2.AuthPolicy) {
				config := policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"]
				config.RouteSelectors = []kuadrantv1beta2.RouteSelector{
					{ // Selects: POST|DELETE *.admin.toystore.com/admin*
						Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
							{
								Path: &gatewayapiv1alpha2.HTTPPathMatch{
									Type:  ptr.To(gatewayapiv1alpha2.PathMatchType("PathPrefix")),
									Value: ptr.To("/admin"),
								},
							},
						},
						Hostnames: []gatewayapiv1.Hostname{gatewayapiv1.Hostname(routeHost2)},
					},
				}
				policy.Spec.CommonSpec().AuthScheme.Authentication["apiKey"] = config
			})

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())

			// check istio authorizationpolicy
			iapKey := types.NamespacedName{Name: controllers.IstioAuthorizationPolicyName(TestGatewayName, policy.Spec.TargetRef), Namespace: testNamespace}
			iap := &secv1beta1resources.AuthorizationPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, iapKey, iap)
				logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())
			Expect(iap.Spec.Rules).To(HaveLen(3))
			// POST *.admin.toystore.com/admin*
			Expect(iap.Spec.Rules[0].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[2].To[0].Operation.Hosts).To(Equal([]string{routeHost1, routeHost2}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"POST"}))
			Expect(iap.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/admin*"}))
			// DELETE *.admin.toystore.com/admin*
			Expect(iap.Spec.Rules[1].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[1].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[2].To[0].Operation.Hosts).To(Equal([]string{routeHost1, routeHost2}))
			Expect(iap.Spec.Rules[1].To[0].Operation.Methods).To(Equal([]string{"DELETE"}))
			Expect(iap.Spec.Rules[1].To[0].Operation.Paths).To(Equal([]string{"/admin*"}))
			// GET (*.toystore.com|*.admin.toystore.com)/private*
			Expect(iap.Spec.Rules[2].To).To(HaveLen(1))
			Expect(iap.Spec.Rules[2].To[0].Operation).ShouldNot(BeNil())
			Expect(iap.Spec.Rules[2].To[0].Operation.Hosts).To(Equal([]string{routeHost1, routeHost2}))
			Expect(iap.Spec.Rules[2].To[0].Operation.Methods).To(Equal([]string{"GET"}))
			Expect(iap.Spec.Rules[2].To[0].Operation.Paths).To(Equal([]string{"/private*"}))
		}, testTimeOut)
	})
})

func testBasicAuthScheme() *kuadrantv1beta2.AuthSchemeSpec {
	return &kuadrantv1beta2.AuthSchemeSpec{
		Authentication: map[string]kuadrantv1beta2.AuthenticationSpec{
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
