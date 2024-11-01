//go:build integration

package targetstatus

import (
	"context"
	"fmt"
	"strings"
	"time"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/controllers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Target status reconciler", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gwHost        = fmt.Sprintf("*.toystore-%s.com", rand.String(6))
	)

	randomHostFromGWHost := func() string {
		return strings.Replace(gwHost, "*", rand.String(6), 1)
	}

	BeforeEach(func(ctx SpecContext) {
		// create namespace
		testNamespace = tests.CreateNamespace(ctx, testClient())

		// create gateway
		gateway := tests.BuildBasicGateway(TestGatewayName, testNamespace, func(gateway *gatewayapiv1.Gateway) {
			gateway.Spec.Listeners = []gatewayapiv1.Listener{
				{
					Name:     "test-listener-toystore-com",
					Hostname: ptr.To(gatewayapiv1.Hostname(gwHost)),
					Port:     gatewayapiv1.PortNumber(80),
					Protocol: gatewayapiv1.HTTPProtocolType,
				},
				{
					Name:     "test-listener-2",
					Hostname: ptr.To(gatewayapiv1.Hostname(gwHost)),
					Port:     gatewayapiv1.PortNumber(88),
					Protocol: gatewayapiv1.HTTPProtocolType,
				},
			}
		})
		err := k8sClient.Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

		// create application
		route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
		err = k8sClient.Create(ctx, route)
		Expect(err).ToNot(HaveOccurred())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	gatewayAffected := func(ctx context.Context, gatewayName, conditionType string, policyKey client.ObjectKey) bool {
		gateway := &gatewayapiv1.Gateway{}
		err := k8sClient.Get(ctx, client.ObjectKey{Name: gatewayName, Namespace: testNamespace}, gateway)
		if err != nil {
			return false
		}
		condition := meta.FindStatusCondition(gateway.Status.Conditions, conditionType)
		gatewayHasCondition := condition != nil && condition.Status == metav1.ConditionTrue && strings.Contains(condition.Message, policyKey.String())
		listenersHasCondition := lo.EveryBy(gateway.Status.Listeners, func(item gatewayapiv1.ListenerStatus) bool {
			lCond := meta.FindStatusCondition(item.Conditions, conditionType)
			return lCond != nil && lCond.Status == metav1.ConditionTrue && strings.Contains(lCond.Message, policyKey.String())
		})

		return gatewayHasCondition && listenersHasCondition
	}

	routeAffected := func(ctx context.Context, routeName, conditionType string, policyKey ...client.ObjectKey) bool {
		route := &gatewayapiv1.HTTPRoute{}
		err := k8sClient.Get(ctx, client.ObjectKey{Name: routeName, Namespace: testNamespace}, route)
		if err != nil {
			return false
		}
		routeParentStatus, found := utils.Find(route.Status.RouteStatus.Parents, controllers.FindRouteParentStatusFunc(route, client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}, kuadrant.ControllerName))
		if !found {
			return false
		}
		condition := meta.FindStatusCondition(routeParentStatus.Conditions, conditionType)
		return condition.Status == metav1.ConditionTrue && lo.EveryBy(policyKey, func(item client.ObjectKey) bool {
			return strings.Contains(condition.Message, item.String())
		})
	}

	targetsAffected := func(ctx context.Context, policyKey client.ObjectKey, conditionType string, targetRef gatewayapiv1alpha2.LocalPolicyTargetReference, routeNames ...string) bool {
		switch string(targetRef.Kind) {
		case "Gateway":
			if !gatewayAffected(ctx, string(targetRef.Name), conditionType, policyKey) {
				return false
			}
		case "HTTPRoute":
			routeNames = append(routeNames, string(targetRef.Name))
		}

		for _, routeName := range routeNames {
			if !routeAffected(ctx, routeName, conditionType, policyKey) {
				return false
			}
		}

		return true
	}

	Context("AuthPolicy", func() {
		policyAffectedCondition := controllers.PolicyAffectedConditionType("AuthPolicy")

		// policyFactory builds a standards AuthPolicy object that targets the test HTTPRoute by default, with the given mutate functions applied
		policyFactory := func(mutateFns ...func(policy *kuadrantv1beta3.AuthPolicy)) *kuadrantv1beta3.AuthPolicy {
			policy := &kuadrantv1beta3.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1beta3.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  TestHTTPRouteName,
						},
					},
					Defaults: &kuadrantv1beta3.MergeableAuthPolicySpec{
						AuthPolicySpecProper: kuadrantv1beta3.AuthPolicySpecProper{
							AuthScheme: &kuadrantv1beta3.AuthSchemeSpec{
								Authentication: map[string]kuadrantv1beta3.MergeableAuthenticationSpec{
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
				},
			}
			for _, mutateFn := range mutateFns {
				mutateFn(policy)
			}
			return policy
		}

		// policyAcceptedAndTargetsAffected returns an assertion function that checks if an AuthPolicy is accepted
		// and the statuses of its target object and other optional route objects have been all updated as affected by the policy
		policyAcceptedAndTargetsAffected := func(ctx context.Context, policy *kuadrantv1beta3.AuthPolicy, routeNames ...string) func() bool {
			return func() bool {
				if !tests.IsAuthPolicyAccepted(ctx, testClient(), policy)() {
					return false
				}
				return targetsAffected(ctx, client.ObjectKeyFromObject(policy), policyAffectedCondition, policy.GetTargetRef(), routeNames...)
			}
		}

		It("adds PolicyAffected status condition to the targeted route", func(ctx SpecContext) {
			policy := policyFactory()
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, policy)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Adds truthy PolicyAffected status condition if there is at least one policy accepted", func(ctx SpecContext) {
			routePolicy1 := policyFactory(func(p *kuadrantv1beta3.AuthPolicy) {
				p.Name = "route-auth-1"
			})
			Expect(k8sClient.Create(ctx, routePolicy1)).To(Succeed())

			Eventually(policyAcceptedAndTargetsAffected(ctx, routePolicy1)).WithContext(ctx).Should(BeTrue())

			routePolicy2 := policyFactory(func(p *kuadrantv1beta3.AuthPolicy) { // another policy that targets the same route. this policy will not be accepted
				p.Name = "route-auth-2"
			})
			Expect(k8sClient.Create(ctx, routePolicy2)).To(Succeed())

			Eventually(func() bool {
				return policyAcceptedAndTargetsAffected(ctx, routePolicy1)() &&
					!tests.IsAuthPolicyAccepted(ctx, testClient(), routePolicy2)() &&
					!routeAffected(ctx, TestHTTPRouteName, policyAffectedCondition, client.ObjectKeyFromObject(routePolicy2))
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("removes PolicyAffected status condition from the targeted route when the policy is deleted", func(ctx SpecContext) {
			policy := policyFactory()
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, policy)).WithContext(ctx).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, policy)).To(Succeed())

			Eventually(func() bool { // route is not affected by the policy
				route := &gatewayapiv1.HTTPRoute{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: TestHTTPRouteName, Namespace: testNamespace}, route)
				if err != nil {
					return false
				}
				routeParentStatus, found := utils.Find(route.Status.RouteStatus.Parents, controllers.FindRouteParentStatusFunc(route, client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}, kuadrant.ControllerName))
				return !found || meta.IsStatusConditionFalse(routeParentStatus.Conditions, policyAffectedCondition)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("adds PolicyAffected status condition to the targeted gateway and routes", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta3.AuthPolicy) {
				policy.Name = "gateway-auth"
				policy.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "Gateway",
						Name:  TestGatewayName,
					},
				}
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, policy, TestHTTPRouteName)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("removes PolicyAffected status condition from the targeted gateway and routes when the policy is deleted", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta3.AuthPolicy) {
				policy.Name = "gateway-auth"
				policy.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "Gateway",
						Name:  TestGatewayName,
					},
				}
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, policy)).WithContext(ctx).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, policy)).To(Succeed())

			Eventually(func() bool { // gateway and route not affected by the policy
				gateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}, gateway)
				if err != nil || meta.IsStatusConditionTrue(gateway.Status.Conditions, policyAffectedCondition) {
					return false
				}

				route := &gatewayapiv1.HTTPRoute{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: TestHTTPRouteName, Namespace: testNamespace}, route)
				if err != nil {
					return false
				}
				routeParentStatus, found := utils.Find(route.Status.RouteStatus.Parents, controllers.FindRouteParentStatusFunc(route, client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}, kuadrant.ControllerName))
				return !found || meta.IsStatusConditionFalse(routeParentStatus.Conditions, policyAffectedCondition)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("adds PolicyAffected status condition to the targeted gateway and non-targeted routes", func(ctx SpecContext) {
			routePolicy := policyFactory()
			Expect(k8sClient.Create(ctx, routePolicy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, routePolicy)).WithContext(ctx).Should(BeTrue())

			otherRouteName := TestHTTPRouteName + "-other"
			otherRoute := tests.BuildBasicHttpRoute(otherRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			Expect(k8sClient.Create(ctx, otherRoute)).To(Succeed())

			gatewayPolicy := policyFactory(func(policy *kuadrantv1beta3.AuthPolicy) {
				policy.Name = "gateway-auth"
				policy.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "Gateway",
						Name:  TestGatewayName,
					},
				}
			})
			Expect(k8sClient.Create(ctx, gatewayPolicy)).To(Succeed())

			Eventually(func() bool {
				return tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(otherRoute))() &&
					policyAcceptedAndTargetsAffected(ctx, routePolicy)() &&
					policyAcceptedAndTargetsAffected(ctx, gatewayPolicy, otherRouteName)()
			}).WithContext(ctx).Should(BeTrue())

			// remove route policy and check if the gateway policy has been rolled out to the status of the newly non-targeted route
			Expect(k8sClient.Delete(ctx, routePolicy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, gatewayPolicy, otherRouteName, TestHTTPRouteName)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("RateLimitPolicy", func() {
		policyAffectedCondition := controllers.PolicyAffectedConditionType("RateLimitPolicy")

		// policyFactory builds a standards RateLimitPolicy object that targets the test HTTPRoute by default, with the given mutate functions applied
		policyFactory := func(mutateFns ...func(policy *kuadrantv1beta3.RateLimitPolicy)) *kuadrantv1beta3.RateLimitPolicy {
			policy := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RateLimitPolicy",
					APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(TestHTTPRouteName),
						},
					},
					Defaults: &kuadrantv1beta3.MergeableRateLimitPolicySpec{
						RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
							Limits: map[string]kuadrantv1beta3.Limit{
								"l1": {
									Rates: []kuadrantv1beta3.Rate{
										{
											Limit: 1, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
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

		// policyAcceptedAndTargetsAffected returns an assertion function that checks if an RateLimitPolicy is accepted
		// and the statuses of its target object and other optional route objects have been all updated as affected by the policy
		policyAcceptedAndTargetsAffected := func(ctx context.Context, policy *kuadrantv1beta3.RateLimitPolicy, routeNames ...string) func() bool {
			return func() bool {
				policyKey := client.ObjectKeyFromObject(policy)
				if !tests.RLPIsAccepted(ctx, testClient(), policyKey)() {
					return false
				}
				return targetsAffected(ctx, policyKey, policyAffectedCondition, policy.Spec.TargetRef.LocalPolicyTargetReference, routeNames...)
			}
		}

		It("adds PolicyAffected status condition to the targeted route", func(ctx SpecContext) {
			policy := policyFactory()
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, policy)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("removes PolicyAffected status condition from the targeted route when the policy is deleted", func(ctx SpecContext) {
			policy := policyFactory()
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, policy)).WithContext(ctx).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, policy)).To(Succeed())

			Eventually(func() bool { // route is not affected by the policy
				route := &gatewayapiv1.HTTPRoute{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: TestHTTPRouteName, Namespace: testNamespace}, route)
				if err != nil {
					return false
				}
				routeParentStatus, found := utils.Find(route.Status.RouteStatus.Parents, controllers.FindRouteParentStatusFunc(route, client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}, kuadrant.ControllerName))
				return !found || meta.IsStatusConditionFalse(routeParentStatus.Conditions, policyAffectedCondition)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("adds PolicyAffected status condition to the targeted gateway and routes", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta3.RateLimitPolicy) {
				policy.Name = "gateway-rlp"
				policy.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "Gateway",
						Name:  TestGatewayName,
					},
				}
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, policy, TestHTTPRouteName)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("removes PolicyAffected status condition from the targeted gateway and routes when the policy is deleted", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta3.RateLimitPolicy) {
				policy.Name = "gateway-rlp"
				policy.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "Gateway",
						Name:  TestGatewayName,
					},
				}
			})
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, policy, TestHTTPRouteName)).WithContext(ctx).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, policy)).To(Succeed())

			Eventually(func() bool { // gateway and route not affected by the policy
				gateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}, gateway)
				if err != nil || meta.IsStatusConditionTrue(gateway.Status.Conditions, policyAffectedCondition) {
					return false
				}

				route := &gatewayapiv1.HTTPRoute{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: TestHTTPRouteName, Namespace: testNamespace}, route)
				if err != nil {
					return false
				}
				routeParentStatus, found := utils.Find(route.Status.RouteStatus.Parents, controllers.FindRouteParentStatusFunc(route, client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}, kuadrant.ControllerName))
				return !found || meta.IsStatusConditionFalse(routeParentStatus.Conditions, policyAffectedCondition)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("adds PolicyAffected status condition to the targeted gateway and non-targeted routes", func(ctx SpecContext) {
			routePolicy := policyFactory()
			Expect(k8sClient.Create(ctx, routePolicy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, routePolicy)).WithContext(ctx).Should(BeTrue())

			otherRouteName := TestHTTPRouteName + "-other"
			otherRoute := tests.BuildBasicHttpRoute(otherRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			Expect(k8sClient.Create(ctx, otherRoute)).To(Succeed())

			gatewayPolicy := policyFactory(func(policy *kuadrantv1beta3.RateLimitPolicy) {
				policy.Name = "gateway-rlp"
				policy.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "Gateway",
						Name:  TestGatewayName,
					},
				}
			})
			Expect(k8sClient.Create(ctx, gatewayPolicy)).To(Succeed())

			Eventually(func() bool {
				return tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(otherRoute))() &&
					policyAcceptedAndTargetsAffected(ctx, routePolicy)() &&
					policyAcceptedAndTargetsAffected(ctx, gatewayPolicy, TestHTTPRouteName, otherRouteName)()
			}).WithContext(ctx).Should(BeTrue())

			// remove route policy and check if the gateway policy has been rolled out to the status of the newly non-targeted route
			Expect(k8sClient.Delete(ctx, routePolicy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, gatewayPolicy, otherRouteName, TestHTTPRouteName)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("adds section name polices only to specific listener status conditions", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1beta3.RateLimitPolicy) {
				policy.Name = "section-rlp"
				policy.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "Gateway",
						Name:  TestGatewayName,
					},
					SectionName: ptr.To[gatewayapiv1.SectionName]("test-listener-toystore-com"),
				}
			})
			policyKey := client.ObjectKeyFromObject(policy)
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(tests.RLPIsAccepted(ctx, testClient(), policyKey)).WithContext(ctx).Should(BeTrue())
			Eventually(func(g Gomega) {
				gateway := &gatewayapiv1.Gateway{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}, gateway)).To(Succeed())
				condition := meta.FindStatusCondition(gateway.Status.Conditions, policyAffectedCondition)
				// Should not be in gw conditions
				g.Expect(condition).To(BeNil())

				g.Expect(lo.EveryBy(gateway.Status.Listeners, func(item gatewayapiv1.ListenerStatus) bool {
					lCond := meta.FindStatusCondition(item.Conditions, policyAffectedCondition)
					if item.Name == *policy.Spec.TargetRef.SectionName {
						// Target section should include condition
						return lCond != nil && lCond.Status == metav1.ConditionTrue && strings.Contains(lCond.Message, policyKey.String())
					}

					// all other sections should not have the condition
					return lCond == nil
				})).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("gateway policy is also listed with section policy", func(ctx SpecContext) {
			gwPolicy := policyFactory(func(policy *kuadrantv1beta3.RateLimitPolicy) {
				policy.Name = "gateway-rlp"
				policy.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "Gateway",
						Name:  TestGatewayName,
					},
				}
			})
			gwPolicyKey := client.ObjectKeyFromObject(gwPolicy)
			Expect(k8sClient.Create(ctx, gwPolicy)).To(Succeed())
			Eventually(tests.RLPIsAccepted(ctx, testClient(), gwPolicyKey)).WithContext(ctx).Should(BeTrue())

			lPolicy := policyFactory(func(policy *kuadrantv1beta3.RateLimitPolicy) {
				policy.Name = "section-rlp"
				policy.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "Gateway",
						Name:  TestGatewayName,
					},
					SectionName: ptr.To[gatewayapiv1.SectionName]("test-listener-toystore-com"),
				}
			})
			lPolicyKey := client.ObjectKeyFromObject(lPolicy)
			Expect(k8sClient.Create(ctx, lPolicy)).To(Succeed())
			Eventually(tests.RLPIsAccepted(ctx, testClient(), lPolicyKey)).WithContext(ctx).Should(BeTrue())

			Eventually(func(g Gomega) {
				gateway := &gatewayapiv1.Gateway{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}, gateway)).To(Succeed())
				condition := meta.FindStatusCondition(gateway.Status.Conditions, policyAffectedCondition)
				// Gateway should list the condition with only the gw policy
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(condition.Message).To(ContainSubstring(gwPolicyKey.String()))
				g.Expect(condition.Message).ToNot(ContainSubstring(lPolicyKey.String()))

				// Listeners
				g.Expect(lo.EveryBy(gateway.Status.Listeners, func(item gatewayapiv1.ListenerStatus) bool {
					lCond := meta.FindStatusCondition(item.Conditions, policyAffectedCondition)
					if item.Name == *lPolicy.Spec.TargetRef.SectionName {
						// Target section should include condition
						return lCond != nil && lCond.Status == metav1.ConditionTrue && strings.Contains(lCond.Message, lPolicyKey.String()) && strings.Contains(lCond.Message, gwPolicyKey.String())
					}

					// all other sections list only the gw policy
					return lCond != nil && lCond.Status == metav1.ConditionTrue && !strings.Contains(lCond.Message, lPolicyKey.String()) && strings.Contains(lCond.Message, gwPolicyKey.String())
				})).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("route should list it's own policy and the parent policies", func(ctx SpecContext) {
			gwPolicy := policyFactory(func(policy *kuadrantv1beta3.RateLimitPolicy) {
				policy.Name = "gateway-rlp"
				policy.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "Gateway",
						Name:  TestGatewayName,
					},
				}
			})
			gwPolicyKey := client.ObjectKeyFromObject(gwPolicy)
			Expect(k8sClient.Create(ctx, gwPolicy)).To(Succeed())
			Eventually(tests.RLPIsAccepted(ctx, testClient(), gwPolicyKey)).WithContext(ctx).Should(BeTrue())

			lPolicy := policyFactory(func(policy *kuadrantv1beta3.RateLimitPolicy) {
				policy.Name = "section-rlp"
				policy.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "Gateway",
						Name:  TestGatewayName,
					},
					SectionName: ptr.To[gatewayapiv1.SectionName]("test-listener-toystore-com"),
				}
			})
			lPolicyKey := client.ObjectKeyFromObject(lPolicy)
			Expect(k8sClient.Create(ctx, lPolicy)).To(Succeed())
			Eventually(tests.RLPIsAccepted(ctx, testClient(), lPolicyKey)).WithContext(ctx).Should(BeTrue())

			rPolicy := policyFactory(func(policy *kuadrantv1beta3.RateLimitPolicy) {
				policy.Name = "route-rlp"
			})
			rPolicyKey := client.ObjectKeyFromObject(rPolicy)
			Expect(k8sClient.Create(ctx, rPolicy)).To(Succeed())
			Eventually(tests.RLPIsAccepted(ctx, testClient(), rPolicyKey)).WithContext(ctx).Should(BeTrue())

			Eventually(func(g Gomega) {
				g.Expect(routeAffected(ctx, TestHTTPRouteName, policyAffectedCondition, gwPolicyKey, lPolicyKey, rPolicyKey)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("DNSPolicy", func() {
		policyAffectedCondition := controllers.PolicyAffectedConditionType("DNSPolicy")

		// policyFactory builds a standards DNSPolicy object that targets the test gateway by default, with the given mutate functions applied
		policyFactory := func(mutateFns ...func(policy *kuadrantv1alpha1.DNSPolicy)) *kuadrantv1alpha1.DNSPolicy {
			policy := kuadrantv1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).WithTargetGateway(TestGatewayName)
			for _, mutateFn := range mutateFns {
				mutateFn(policy)
			}
			return policy
		}

		isDNSPolicyAccepted := func(ctx context.Context, policyKey client.ObjectKey) bool {
			policy := &kuadrantv1alpha1.DNSPolicy{}
			err := k8sClient.Get(ctx, policyKey, policy)
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
		}

		isDNSPolicyEnforced := func(ctx context.Context, policyKey client.ObjectKey) bool {
			policy := &kuadrantv1alpha1.DNSPolicy{}
			err := k8sClient.Get(ctx, policyKey, policy)
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(policy.Status.Conditions, string(kuadrant.PolicyConditionEnforced))
		}

		// policyAcceptedAndTargetsAffected returns an assertion function that checks if a DNSPolicy is accepted
		// and the statuses of its target object has been all updated as affected by the policy
		policyAcceptedAndTargetsAffected := func(ctx context.Context, policy *kuadrantv1alpha1.DNSPolicy) func() bool {
			return func() bool {
				policyKey := client.ObjectKeyFromObject(policy)
				return isDNSPolicyAccepted(ctx, policyKey) && targetsAffected(ctx, policyKey, policyAffectedCondition, policy.Spec.TargetRef.LocalPolicyTargetReference)
			}
		}

		var dnsProviderSecret *corev1.Secret

		BeforeEach(func(ctx SpecContext) {
			dnsProviderSecret = tests.BuildInMemoryCredentialsSecret("inmemory-credentials", testNamespace, strings.Replace(gwHost, "*.", "", 1))
			Expect(k8sClient.Create(ctx, dnsProviderSecret)).To(Succeed())
		})

		AfterEach(func(ctx SpecContext) {
			// Wait until dns records are finished deleting since it can't finish deleting without the DNS provider secret
			Eventually(func(g Gomega) {
				dnsRecords := &kuadrantdnsv1alpha1.DNSRecordList{}
				err := k8sClient.List(ctx, dnsRecords, client.InNamespace(testNamespace))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecords.Items).To(HaveLen(0))
			}).WithContext(ctx).Should(Succeed())
		}, afterEachTimeOut)

		It("adds PolicyAffected status condition to the targeted gateway", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.DNSPolicy) {
				policy.Spec.ProviderRefs = append(policy.Spec.ProviderRefs, kuadrantdnsv1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				})
			})
			defer k8sClient.Delete(ctx, policy)
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			// policy should not be enforced since DNS Record is not ready because of the missing secret on the MZ
			Eventually(isDNSPolicyEnforced(ctx, client.ObjectKeyFromObject(policy))).WithContext(ctx).ShouldNot(BeTrue())
			Eventually(policyAcceptedAndTargetsAffected(ctx, policy)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("removes PolicyAffected status condition from the targeted gateway when the policy is deleted", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.DNSPolicy) {
				policy.Spec.ProviderRefs = append(policy.Spec.ProviderRefs, kuadrantdnsv1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				})
			})
			defer k8sClient.Delete(ctx, policy)
			policyKey := client.ObjectKeyFromObject(policy)
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, policy)).WithContext(ctx).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, policy)).To(Succeed())

			Eventually(func() bool {
				gateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}, gateway)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(gateway.Status.Conditions, TestGatewayName)
				return condition == nil || !strings.Contains(condition.Message, policyKey.String()) || condition.Status == metav1.ConditionFalse
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("adds section name polices only to specific listener status conditions", func(ctx SpecContext) {
			policy := policyFactory(func(policy *kuadrantv1alpha1.DNSPolicy) {
				policy.Name = "section-dns"
				policy.Spec.TargetRef.SectionName = ptr.To[gatewayapiv1.SectionName]("test-listener-toystore-com")
				policy.Spec.ProviderRefs = append(policy.Spec.ProviderRefs, kuadrantdnsv1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				})
			})
			policyKey := client.ObjectKeyFromObject(policy)
			defer k8sClient.Delete(ctx, policy)
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(func(g Gomega) {
				gateway := &gatewayapiv1.Gateway{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}, gateway)).To(Succeed())
				condition := meta.FindStatusCondition(gateway.Status.Conditions, policyAffectedCondition)
				// Should not be in gw conditions
				g.Expect(condition).To(BeNil())

				g.Expect(lo.EveryBy(gateway.Status.Listeners, func(item gatewayapiv1.ListenerStatus) bool {
					lCond := meta.FindStatusCondition(item.Conditions, policyAffectedCondition)
					if item.Name == *policy.Spec.TargetRef.SectionName {
						// Target section should include condition
						return lCond != nil && lCond.Status == metav1.ConditionTrue && strings.Contains(lCond.Message, policyKey.String())
					}

					// all other sections should not have the condition
					return lCond == nil
				})).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("gateway policy is also listed with section policy", func(ctx SpecContext) {
			gwPolicy := policyFactory(func(policy *kuadrantv1alpha1.DNSPolicy) {
				policy.Name = "gateway-dns"
				policy.Spec.ProviderRefs = append(policy.Spec.ProviderRefs, kuadrantdnsv1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				})
			})
			gwPolicyKey := client.ObjectKeyFromObject(gwPolicy)
			defer k8sClient.Delete(ctx, gwPolicy)
			Expect(k8sClient.Create(ctx, gwPolicy)).To(Succeed())

			lPolicy := policyFactory(func(policy *kuadrantv1alpha1.DNSPolicy) {
				policy.Name = "section-dns"
				policy.Spec.TargetRef.SectionName = ptr.To[gatewayapiv1.SectionName]("test-listener-toystore-com")
				policy.Spec.ProviderRefs = append(policy.Spec.ProviderRefs, kuadrantdnsv1alpha1.ProviderRef{
					Name: dnsProviderSecret.Name,
				})
			})

			lPolicyKey := client.ObjectKeyFromObject(lPolicy)
			defer k8sClient.Delete(ctx, lPolicy)
			Expect(k8sClient.Create(ctx, lPolicy)).To(Succeed())

			Eventually(func(g Gomega) {
				gateway := &gatewayapiv1.Gateway{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}, gateway)).To(Succeed())
				condition := meta.FindStatusCondition(gateway.Status.Conditions, policyAffectedCondition)
				// Gateway should list the condition with only the gw policy
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(condition.Message).To(ContainSubstring(gwPolicyKey.String()))
				g.Expect(condition.Message).ToNot(ContainSubstring(lPolicyKey.String()))

				// Listeners
				g.Expect(lo.EveryBy(gateway.Status.Listeners, func(item gatewayapiv1.ListenerStatus) bool {
					lCond := meta.FindStatusCondition(item.Conditions, policyAffectedCondition)
					if item.Name == *lPolicy.Spec.TargetRef.SectionName {
						// Target section should include condition
						return lCond != nil && lCond.Status == metav1.ConditionTrue && strings.Contains(lCond.Message, lPolicyKey.String()) && strings.Contains(lCond.Message, gwPolicyKey.String())
					}

					// all other sections list only the gw policy
					return lCond != nil && lCond.Status == metav1.ConditionTrue && !strings.Contains(lCond.Message, lPolicyKey.String()) && strings.Contains(lCond.Message, gwPolicyKey.String())
				})).To(BeTrue())

				// Route
				g.Expect(routeAffected(ctx, TestHTTPRouteName, policyAffectedCondition, gwPolicyKey, lPolicyKey)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("TLSPolicy", func() {
		policyAffectedCondition := controllers.PolicyAffectedConditionType("TLSPolicy")

		var issuer *certmanv1.Issuer
		var issuerRef *certmanmetav1.ObjectReference

		// policyFactory builds a standards TLSPolicy object that targets the test gateway by default, with the given mutate functions applied
		policyFactory := func(mutateFns ...func(policy *kuadrantv1alpha1.TLSPolicy)) *kuadrantv1alpha1.TLSPolicy {
			policy := kuadrantv1alpha1.NewTLSPolicy("test-tls-policy", testNamespace).WithTargetGateway(TestGatewayName).WithIssuerRef(*issuerRef)
			for _, mutateFn := range mutateFns {
				mutateFn(policy)
			}
			return policy
		}

		isTLSPolicyAccepted := func(ctx context.Context, policyKey client.ObjectKey) bool {
			policy := &kuadrantv1alpha1.TLSPolicy{}
			err := k8sClient.Get(ctx, policyKey, policy)
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
		}

		// policyAcceptedAndTargetsAffected returns an assertion function that checks if a TLSPolicy is accepted
		// and the statuses of its target object has been all updated as affected by the policy
		policyAcceptedAndTargetsAffected := func(ctx context.Context, policy *kuadrantv1alpha1.TLSPolicy) func() bool {
			return func() bool {
				policyKey := client.ObjectKeyFromObject(policy)
				if !isTLSPolicyAccepted(ctx, policyKey) {
					return false
				}
				return targetsAffected(ctx, policyKey, policyAffectedCondition, policy.Spec.TargetRef)
			}
		}

		BeforeEach(func(ctx SpecContext) {
			issuer, issuerRef = tests.BuildSelfSignedIssuer("testissuer", testNamespace)
			Expect(k8sClient.Create(ctx, issuer)).To(BeNil())
		})

		AfterEach(func(ctx SpecContext) {
			if issuer != nil {
				err := k8sClient.Delete(ctx, issuer)
				Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
			}
		}, afterEachTimeOut)

		It("adds PolicyAffected status condition to the targeted gateway", func(ctx SpecContext) {
			policy := policyFactory()
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, policy)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("removes PolicyAffected status condition from the targeted gateway when the policy is deleted", func(ctx SpecContext) {
			policy := policyFactory()
			policyKey := client.ObjectKeyFromObject(policy)
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			Eventually(policyAcceptedAndTargetsAffected(ctx, policy)).WithContext(ctx).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, policy)).To(Succeed())

			Eventually(func() bool {
				gateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: TestGatewayName, Namespace: testNamespace}, gateway)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(gateway.Status.Conditions, TestGatewayName)
				return condition == nil || !strings.Contains(condition.Message, policyKey.String()) || condition.Status == metav1.ConditionFalse
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})
})
