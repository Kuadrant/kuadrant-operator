//go:build integration

package controllers

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var _ = Describe("Target status reconciler", func() {
	var testNamespace string

	BeforeEach(func() {
		// create namespace
		CreateNamespace(&testNamespace)

		// create gateway
		gateway := testBuildBasicGateway(testGatewayName, testNamespace, func(gateway *gatewayapiv1.Gateway) {
			gateway.Spec.Listeners = []gatewayapiv1.Listener{{
				Name:     gatewayapiv1.SectionName("test-listener-toystore-com"),
				Hostname: ptr.To(gatewayapiv1.Hostname("*.toystore.com")),
				Port:     gatewayapiv1.PortNumber(80),
				Protocol: gatewayapiv1.HTTPProtocolType,
			}}
		})
		err := k8sClient.Create(context.Background(), gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(testGatewayIsReady(gateway), 15*time.Second, 5*time.Second).Should(BeTrue())

		// create kuadrant instance
		ApplyKuadrantCR(testNamespace)

		// create application
		err = ApplyResources(filepath.Join("..", "examples", "toystore", "toystore.yaml"), k8sClient, testNamespace)
		Expect(err).ToNot(HaveOccurred())
		route := testBuildBasicHttpRoute(testHTTPRouteName, testGatewayName, testNamespace, []string{"*.toystore.com"})
		err = k8sClient.Create(context.Background(), route)
		Expect(err).ToNot(HaveOccurred())
		Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(route)), time.Minute, 5*time.Second).Should(BeTrue())
	})

	AfterEach(DeleteNamespaceCallback(&testNamespace))

	gatewayAffected := func(gatewayName, conditionType string, policyKey client.ObjectKey) bool {
		gateway := &gatewayapiv1.Gateway{}
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: gatewayName, Namespace: testNamespace}, gateway)
		if err != nil {
			return false
		}
		condition := meta.FindStatusCondition(gateway.Status.Conditions, conditionType)
		return condition != nil && condition.Status == metav1.ConditionTrue && strings.Contains(condition.Message, policyKey.String())
	}

	routeAffected := func(routeName, conditionType string, policyKey client.ObjectKey) bool {
		route := &gatewayapiv1.HTTPRoute{}
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: routeName, Namespace: testNamespace}, route)
		if err != nil {
			return false
		}
		routeParentStatus, found := utils.Find(route.Status.RouteStatus.Parents, findRouteParentStatusFunc(route, client.ObjectKey{Name: testGatewayName, Namespace: testNamespace}, kuadrant.ControllerName))
		if !found {
			return false
		}
		condition := meta.FindStatusCondition(routeParentStatus.Conditions, conditionType)
		return condition.Status == metav1.ConditionTrue && strings.Contains(condition.Message, policyKey.String())
	}

	targetsAffected := func(policyKey client.ObjectKey, conditionType string, targetRef gatewayapiv1alpha2.PolicyTargetReference, routeNames ...string) bool {
		switch string(targetRef.Kind) {
		case "Gateway":
			if !gatewayAffected(string(targetRef.Name), conditionType, policyKey) {
				return false
			}
		case "HTTPRoute":
			routeNames = append(routeNames, string(targetRef.Name))
		}

		for _, routeName := range routeNames {
			if !routeAffected(routeName, conditionType, policyKey) {
				return false
			}
		}

		return true
	}

	Context("AuthPolicy", func() {
		policyAffectedCondition := policyAffectedConditionType("AuthPolicy")

		// policyFactory builds a standards AuthPolicy object that targets the test HTTPRoute by default, with the given mutate functions applied
		policyFactory := func(mutateFns ...func(policy *v1beta2.AuthPolicy)) *v1beta2.AuthPolicy {
			policy := &v1beta2.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: v1beta2.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: v1beta2.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group:     gatewayapiv1.GroupName,
						Kind:      "HTTPRoute",
						Name:      testHTTPRouteName,
						Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
					},
					Defaults: &v1beta2.AuthPolicyCommonSpec{
						AuthScheme: &v1beta2.AuthSchemeSpec{
							Authentication: map[string]v1beta2.AuthenticationSpec{
								"anonymous": {
									AuthenticationSpec: authorinoapi.AuthenticationSpec{
										AuthenticationMethodSpec: authorinoapi.AuthenticationMethodSpec{
											AnonymousAccess: &authorinoapi.AnonymousAccessSpec{},
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

		// policyAcceptedAndTargetsAffected returns an assertion function that checks if an AuthPolicy is accepted
		// and the statuses of its target object and other optional route objects have been all updated as affected by the policy
		policyAcceptedAndTargetsAffected := func(policy *v1beta2.AuthPolicy, routeNames ...string) func() bool {
			return func() bool {
				if !isAuthPolicyAccepted(policy)() {
					return false
				}
				return targetsAffected(client.ObjectKeyFromObject(policy), policyAffectedCondition, policy.Spec.TargetRef, routeNames...)
			}
		}

		It("adds PolicyAffected status condition to the targeted route", func() {
			policy := policyFactory()
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(policy), 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("removes PolicyAffected status condition from the targeted route when the policy is deleted", func() {
			policy := policyFactory()
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			Expect(k8sClient.Delete(context.Background(), policy)).To(Succeed())

			Eventually(func() bool { // route is not affected by the policy
				route := &gatewayapiv1.HTTPRoute{}
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: testHTTPRouteName, Namespace: testNamespace}, route)
				if err != nil {
					return false
				}
				routeParentStatus, found := utils.Find(route.Status.RouteStatus.Parents, findRouteParentStatusFunc(route, client.ObjectKey{Name: testGatewayName, Namespace: testNamespace}, kuadrant.ControllerName))
				return !found || meta.IsStatusConditionFalse(routeParentStatus.Conditions, policyAffectedCondition)
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("adds PolicyAffected status condition to the targeted gateway and routes", func() {
			policy := policyFactory(func(policy *v1beta2.AuthPolicy) {
				policy.Name = "gateway-auth"
				policy.Spec.TargetRef = gatewayapiv1alpha2.PolicyTargetReference{
					Group:     gatewayapiv1.GroupName,
					Kind:      "Gateway",
					Name:      testGatewayName,
					Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
				}
			})
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(policy), 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("removes PolicyAffected status condition from the targeted gateway and routes when the policy is deleted", func() {
			policy := policyFactory(func(policy *v1beta2.AuthPolicy) {
				policy.Name = "gateway-auth"
				policy.Spec.TargetRef = gatewayapiv1alpha2.PolicyTargetReference{
					Group:     gatewayapiv1.GroupName,
					Kind:      "Gateway",
					Name:      testGatewayName,
					Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
				}
			})
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			Expect(k8sClient.Delete(context.Background(), policy)).To(Succeed())

			Eventually(func() bool { // gateway and route not affected by the policy
				gateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: testGatewayName, Namespace: testNamespace}, gateway)
				if err != nil || meta.IsStatusConditionTrue(gateway.Status.Conditions, policyAffectedCondition) {
					return false
				}

				route := &gatewayapiv1.HTTPRoute{}
				err = k8sClient.Get(context.Background(), client.ObjectKey{Name: testHTTPRouteName, Namespace: testNamespace}, route)
				if err != nil {
					return false
				}
				routeParentStatus, found := utils.Find(route.Status.RouteStatus.Parents, findRouteParentStatusFunc(route, client.ObjectKey{Name: testGatewayName, Namespace: testNamespace}, kuadrant.ControllerName))
				return !found || meta.IsStatusConditionFalse(routeParentStatus.Conditions, policyAffectedCondition)
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("adds PolicyAffected status condition to the targeted gateway and non-targeted routes", func() {
			routePolicy := policyFactory()
			Expect(k8sClient.Create(context.Background(), routePolicy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(routePolicy), 30*time.Second, 5*time.Second).Should(BeTrue())

			otherRouteName := testHTTPRouteName + "-other"
			otherRoute := testBuildBasicHttpRoute(otherRouteName, testGatewayName, testNamespace, []string{"other.toystore.com"})
			Expect(k8sClient.Create(context.Background(), otherRoute)).To(Succeed())

			gatewayPolicy := policyFactory(func(policy *v1beta2.AuthPolicy) {
				policy.Name = "gateway-auth"
				policy.Spec.TargetRef = gatewayapiv1alpha2.PolicyTargetReference{
					Group:     gatewayapiv1.GroupName,
					Kind:      "Gateway",
					Name:      testGatewayName,
					Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
				}
			})
			Expect(k8sClient.Create(context.Background(), gatewayPolicy)).To(Succeed())

			Eventually(func() bool {
				return testRouteIsAccepted(client.ObjectKeyFromObject(otherRoute))() &&
					policyAcceptedAndTargetsAffected(routePolicy)() &&
					policyAcceptedAndTargetsAffected(gatewayPolicy, otherRouteName)()
			}, time.Minute, 5*time.Second).Should(BeTrue())

			// remove route policy and check if the gateway policy has been rolled out to the status of the newly non-targeted route
			Expect(k8sClient.Delete(context.Background(), routePolicy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(gatewayPolicy, otherRouteName, testHTTPRouteName), time.Minute, 5*time.Second).Should(BeTrue())
		})
	})

	Context("RateLimitPolicy", func() {
		policyAffectedCondition := policyAffectedConditionType("RateLimitPolicy")

		// policyFactory builds a standards RateLimitPolicy object that targets the test HTTPRoute by default, with the given mutate functions applied
		policyFactory := func(mutateFns ...func(policy *v1beta2.RateLimitPolicy)) *v1beta2.RateLimitPolicy {
			policy := &v1beta2.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RateLimitPolicy",
					APIVersion: v1beta2.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: v1beta2.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "HTTPRoute",
						Name:  gatewayapiv1.ObjectName(testHTTPRouteName),
					},
					Defaults: &v1beta2.RateLimitPolicyCommonSpec{
						Limits: map[string]v1beta2.Limit{
							"l1": {
								Rates: []v1beta2.Rate{
									{
										Limit: 1, Duration: 3, Unit: v1beta2.TimeUnit("minute"),
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

		// policyAcceptedAndTargetsAffected returns an assertion function that checks if an RateLimitPolicy is accepted
		// and the statuses of its target object and other optional route objects have been all updated as affected by the policy
		policyAcceptedAndTargetsAffected := func(policy *v1beta2.RateLimitPolicy, routeNames ...string) func() bool {
			return func() bool {
				policyKey := client.ObjectKeyFromObject(policy)
				if !testRLPIsAccepted(policyKey)() {
					return false
				}
				return targetsAffected(policyKey, policyAffectedCondition, policy.Spec.TargetRef, routeNames...)
			}
		}

		It("adds PolicyAffected status condition to the targeted route", func() {
			policy := policyFactory()
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(policy), 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("removes PolicyAffected status condition from the targeted route when the policy is deleted", func() {
			policy := policyFactory()
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			Expect(k8sClient.Delete(context.Background(), policy)).To(Succeed())

			Eventually(func() bool { // route is not affected by the policy
				route := &gatewayapiv1.HTTPRoute{}
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: testHTTPRouteName, Namespace: testNamespace}, route)
				if err != nil {
					return false
				}
				routeParentStatus, found := utils.Find(route.Status.RouteStatus.Parents, findRouteParentStatusFunc(route, client.ObjectKey{Name: testGatewayName, Namespace: testNamespace}, kuadrant.ControllerName))
				return !found || meta.IsStatusConditionFalse(routeParentStatus.Conditions, policyAffectedCondition)
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("adds PolicyAffected status condition to the targeted gateway and routes", func() {
			policy := policyFactory(func(policy *v1beta2.RateLimitPolicy) {
				policy.Name = "gateway-rlp"
				policy.Spec.TargetRef = gatewayapiv1alpha2.PolicyTargetReference{
					Group:     gatewayapiv1.GroupName,
					Kind:      "Gateway",
					Name:      testGatewayName,
					Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
				}
			})
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(policy), 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("removes PolicyAffected status condition from the targeted gateway and routes when the policy is deleted", func() {
			policy := policyFactory(func(policy *v1beta2.RateLimitPolicy) {
				policy.Name = "gateway-rlp"
				policy.Spec.TargetRef = gatewayapiv1alpha2.PolicyTargetReference{
					Group:     gatewayapiv1.GroupName,
					Kind:      "Gateway",
					Name:      testGatewayName,
					Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
				}
			})
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			Expect(k8sClient.Delete(context.Background(), policy)).To(Succeed())

			Eventually(func() bool { // gateway and route not affected by the policy
				gateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: testGatewayName, Namespace: testNamespace}, gateway)
				if err != nil || meta.IsStatusConditionTrue(gateway.Status.Conditions, policyAffectedCondition) {
					return false
				}

				route := &gatewayapiv1.HTTPRoute{}
				err = k8sClient.Get(context.Background(), client.ObjectKey{Name: testHTTPRouteName, Namespace: testNamespace}, route)
				if err != nil {
					return false
				}
				routeParentStatus, found := utils.Find(route.Status.RouteStatus.Parents, findRouteParentStatusFunc(route, client.ObjectKey{Name: testGatewayName, Namespace: testNamespace}, kuadrant.ControllerName))
				return !found || meta.IsStatusConditionFalse(routeParentStatus.Conditions, policyAffectedCondition)
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("adds PolicyAffected status condition to the targeted gateway and non-targeted routes", func() {
			routePolicy := policyFactory()
			Expect(k8sClient.Create(context.Background(), routePolicy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(routePolicy), 30*time.Second, 5*time.Second).Should(BeTrue())

			otherRouteName := testHTTPRouteName + "-other"
			otherRoute := testBuildBasicHttpRoute(otherRouteName, testGatewayName, testNamespace, []string{"other.toystore.com"})
			Expect(k8sClient.Create(context.Background(), otherRoute)).To(Succeed())

			gatewayPolicy := policyFactory(func(policy *v1beta2.RateLimitPolicy) {
				policy.Name = "gateway-rlp"
				policy.Spec.TargetRef = gatewayapiv1alpha2.PolicyTargetReference{
					Group:     gatewayapiv1.GroupName,
					Kind:      "Gateway",
					Name:      testGatewayName,
					Namespace: ptr.To(gatewayapiv1.Namespace(testNamespace)),
				}
			})
			Expect(k8sClient.Create(context.Background(), gatewayPolicy)).To(Succeed())

			Eventually(func() bool {
				return testRouteIsAccepted(client.ObjectKeyFromObject(otherRoute))() &&
					policyAcceptedAndTargetsAffected(routePolicy)() &&
					policyAcceptedAndTargetsAffected(gatewayPolicy, otherRouteName)()
			}, time.Minute, 5*time.Second).Should(BeTrue())

			// remove route policy and check if the gateway policy has been rolled out to the status of the newly non-targeted route
			Expect(k8sClient.Delete(context.Background(), routePolicy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(gatewayPolicy, otherRouteName, testHTTPRouteName), time.Minute, 5*time.Second).Should(BeTrue())
		})
	})

	Context("DNSPolicy", func() {
		policyAffectedCondition := policyAffectedConditionType("DNSPolicy")

		// policyFactory builds a standards DNSPolicy object that targets the test gateway by default, with the given mutate functions applied
		policyFactory := func(mutateFns ...func(policy *v1alpha1.DNSPolicy)) *v1alpha1.DNSPolicy {
			policy := v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).WithTargetGateway(testGatewayName).WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)
			for _, mutateFn := range mutateFns {
				mutateFn(policy)
			}
			return policy
		}

		isDNSPolicyAccepted := func(policyKey client.ObjectKey) bool {
			policy := &v1alpha1.DNSPolicy{}
			err := k8sClient.Get(context.Background(), policyKey, policy)
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
		}

		// policyAcceptedAndTargetsAffected returns an assertion function that checks if a DNSPolicy is accepted
		// and the statuses of its target object has been all updated as affected by the policy
		policyAcceptedAndTargetsAffected := func(policy *v1alpha1.DNSPolicy) func() bool {
			return func() bool {
				policyKey := client.ObjectKeyFromObject(policy)
				if !isDNSPolicyAccepted(policyKey) {
					return false
				}
				return targetsAffected(policyKey, policyAffectedCondition, policy.Spec.TargetRef)
			}
		}

		var managedZone *kuadrantdnsv1alpha1.ManagedZone

		BeforeEach(func() {
			managedZone = testBuildManagedZone("mz-toystore-com", testNamespace, "toystore.com")
			Expect(k8sClient.Create(context.Background(), managedZone)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), managedZone)).To(Succeed())
		})

		It("adds PolicyAffected status condition to the targeted gateway", func() {
			policy := policyFactory()
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(policy), 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("removes PolicyAffected status condition from the targeted gateway when the policy is deleted", func() {
			policy := policyFactory()
			policyKey := client.ObjectKeyFromObject(policy)
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			Expect(k8sClient.Delete(context.Background(), policy)).To(Succeed())

			Eventually(func() bool {
				gateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: testGatewayName, Namespace: testNamespace}, gateway)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(gateway.Status.Conditions, testGatewayName)
				return condition == nil || !strings.Contains(condition.Message, policyKey.String()) || condition.Status == metav1.ConditionFalse
			})
		})
	})

	Context("TLSPolicy", func() {
		policyAffectedCondition := policyAffectedConditionType("TLSPolicy")

		var issuer *certmanv1.Issuer
		var issuerRef *certmanmetav1.ObjectReference

		// policyFactory builds a standards TLSPolicy object that targets the test gateway by default, with the given mutate functions applied
		policyFactory := func(mutateFns ...func(policy *v1alpha1.TLSPolicy)) *v1alpha1.TLSPolicy {
			policy := v1alpha1.NewTLSPolicy("test-tls-policy", testNamespace).WithTargetGateway(testGatewayName).WithIssuerRef(*issuerRef)
			for _, mutateFn := range mutateFns {
				mutateFn(policy)
			}
			return policy
		}

		isTLSPolicyAccepted := func(policyKey client.ObjectKey) bool {
			policy := &v1alpha1.TLSPolicy{}
			err := k8sClient.Get(context.Background(), policyKey, policy)
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
		}

		// policyAcceptedAndTargetsAffected returns an assertion function that checks if a TLSPolicy is accepted
		// and the statuses of its target object has been all updated as affected by the policy
		policyAcceptedAndTargetsAffected := func(policy *v1alpha1.TLSPolicy) func() bool {
			return func() bool {
				policyKey := client.ObjectKeyFromObject(policy)
				if !isTLSPolicyAccepted(policyKey) {
					return false
				}
				return targetsAffected(policyKey, policyAffectedCondition, policy.Spec.TargetRef)
			}
		}

		BeforeEach(func() {
			issuer, issuerRef = testBuildSelfSignedIssuer("testissuer", testNamespace)
			Expect(k8sClient.Create(context.Background(), issuer)).To(BeNil())
		})

		AfterEach(func() {
			if issuer != nil {
				err := k8sClient.Delete(context.Background(), issuer)
				Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
			}
		})

		It("adds PolicyAffected status condition to the targeted gateway", func() {
			policy := policyFactory()
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(policy), 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("removes PolicyAffected status condition from the targeted gateway when the policy is deleted", func() {
			policy := policyFactory()
			policyKey := client.ObjectKeyFromObject(policy)
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(policy), 30*time.Second, 5*time.Second).Should(BeTrue())

			Expect(k8sClient.Delete(context.Background(), policy)).To(Succeed())

			Eventually(func() bool {
				gateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: testGatewayName, Namespace: testNamespace}, gateway)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(gateway.Status.Conditions, testGatewayName)
				return condition == nil || !strings.Contains(condition.Message, policyKey.String()) || condition.Status == metav1.ConditionFalse
			})
		})
	})
})
