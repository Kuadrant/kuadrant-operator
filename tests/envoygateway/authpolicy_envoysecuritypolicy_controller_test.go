//go:build integration

package envoygateway_test

import (
	"fmt"
	"strings"
	"time"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/controllers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

var _ = Describe("Auth Envoy SecurityPolicy controller", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gwHost        = fmt.Sprintf("*.toystore-%s.com", rand.String(4))
		gateway       *gatewayapiv1.Gateway
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway = tests.NewGatewayBuilder(TestGatewayName, tests.GatewayClassName, testNamespace).
			WithHTTPListener("test-listener", gwHost).
			Gateway
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
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "HTTPRoute",
					Name:  TestHTTPRouteName,
				},
				Defaults: &kuadrantv1beta2.AuthPolicyCommonSpec{
					AuthScheme: tests.BuildBasicAuthScheme(),
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

	Context("Auth Policy attached to the gateway", func() {

		var (
			gwPolicy *kuadrantv1beta2.AuthPolicy
		)

		BeforeEach(func(ctx SpecContext) {
			gwRoute := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err := k8sClient.Create(ctx, gwRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(gwRoute))).WithContext(ctx).Should(BeTrue())

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

		It("Creates security policy", func(ctx SpecContext) {
			spKey := types.NamespacedName{Name: controllers.EnvoySecurityPolicyName(TestGatewayName), Namespace: testNamespace}
			sp := &egv1alpha1.SecurityPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, spKey, sp)
				logf.Log.V(1).Info("Fetching envoy SecurityPolicy", "key", spKey.String(), "error", err)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			//has correct configuration
			Expect(*sp).To(
				MatchFields(IgnoreExtras, Fields{
					"Spec": MatchFields(IgnoreExtras, Fields{
						"PolicyTargetReferences": MatchFields(IgnoreExtras, Fields{
							"TargetRefs": ConsistOf(MatchFields(IgnoreExtras, Fields{
								"LocalPolicyTargetReference": MatchFields(IgnoreExtras, Fields{
									"Group": Equal(gatewayapiv1.Group(gatewayapiv1.GroupName)),
									"Kind":  Equal(gatewayapiv1.Kind("Gateway")),
									"Name":  Equal(gatewayapiv1.ObjectName(TestGatewayName)),
								}),
							})),
						}),
						"ExtAuth": PointTo(MatchFields(IgnoreExtras, Fields{
							"GRPC": PointTo(MatchFields(IgnoreExtras, Fields{
								"BackendRefs": ConsistOf(MatchFields(IgnoreExtras, Fields{
									"BackendObjectReference": MatchFields(IgnoreExtras, Fields{
										"Group":     PointTo(Equal(gatewayapiv1.Group(""))),
										"Kind":      PointTo(Equal(gatewayapiv1.Kind("Service"))),
										"Name":      Equal(gatewayapiv1.ObjectName(kuadrant.AuthorinoServiceName)),
										"Namespace": PointTo(Equal(gatewayapiv1.Namespace(kuadrantInstallationNS))),
										"Port":      PointTo(Equal(gatewayapiv1.PortNumber(50051))),
									}),
								})),
							})),
						})),
					}),
				}))
		}, testTimeOut)

		It("Deletes security policy when auth policy is deleted", func(ctx SpecContext) {
			err := k8sClient.Delete(ctx, gwPolicy)
			logf.Log.V(1).Info("Deleting AuthPolicy", "key", client.ObjectKeyFromObject(gwPolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			spKey := types.NamespacedName{Name: controllers.EnvoySecurityPolicyName(TestGatewayName), Namespace: testNamespace}
			sp := &egv1alpha1.SecurityPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, spKey, sp)
				logf.Log.V(1).Info("Fetching envoy SecurityPolicy", "key", spKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Deletes security policy if gateway is deleted", func(ctx SpecContext) {
			err := k8sClient.Delete(ctx, gateway)
			logf.Log.V(1).Info("Deleting Gateway", "key", client.ObjectKeyFromObject(gateway).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			spKey := types.NamespacedName{Name: controllers.EnvoySecurityPolicyName(TestGatewayName), Namespace: testNamespace}
			sp := &egv1alpha1.SecurityPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, spKey, sp)
				logf.Log.V(1).Info("Fetching envoy SecurityPolicy", "key", spKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("Auth Policy attached to the route", func() {

		var (
			routePolicy *kuadrantv1beta2.AuthPolicy
			gwRoute     *gatewayapiv1.HTTPRoute
		)

		BeforeEach(func(ctx SpecContext) {
			gwRoute = tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err := k8sClient.Create(ctx, gwRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(gwRoute))).WithContext(ctx).Should(BeTrue())

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

		It("Creates security policy", func(ctx SpecContext) {
			spKey := types.NamespacedName{Name: controllers.EnvoySecurityPolicyName(TestHTTPRouteName), Namespace: testNamespace}
			sp := &egv1alpha1.SecurityPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, spKey, sp)
				logf.Log.V(1).Info("Fetching envoy SecurityPolicy", "key", spKey.String(), "error", err)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			//has correct configuration
			Expect(*sp).To(
				MatchFields(IgnoreExtras, Fields{
					"Spec": MatchFields(IgnoreExtras, Fields{
						"PolicyTargetReferences": MatchFields(IgnoreExtras, Fields{
							"TargetRefs": ConsistOf(MatchFields(IgnoreExtras, Fields{
								"LocalPolicyTargetReference": MatchFields(IgnoreExtras, Fields{
									"Group": Equal(gatewayapiv1.Group(gatewayapiv1.GroupName)),
									"Kind":  Equal(gatewayapiv1.Kind("HTTPRoute")),
									"Name":  Equal(gatewayapiv1.ObjectName(TestHTTPRouteName)),
								}),
							})),
						}),
						"ExtAuth": PointTo(MatchFields(IgnoreExtras, Fields{
							"GRPC": PointTo(MatchFields(IgnoreExtras, Fields{
								"BackendRefs": ConsistOf(MatchFields(IgnoreExtras, Fields{
									"BackendObjectReference": MatchFields(IgnoreExtras, Fields{
										"Group":     PointTo(Equal(gatewayapiv1.Group(""))),
										"Kind":      PointTo(Equal(gatewayapiv1.Kind("Service"))),
										"Name":      Equal(gatewayapiv1.ObjectName(kuadrant.AuthorinoServiceName)),
										"Namespace": PointTo(Equal(gatewayapiv1.Namespace(kuadrantInstallationNS))),
										"Port":      PointTo(Equal(gatewayapiv1.PortNumber(50051))),
									}),
								})),
							})),
						})),
					}),
				}))
		}, testTimeOut)

		It("Security policy deleted when auth policy is deleted", func(ctx SpecContext) {
			err := k8sClient.Delete(ctx, routePolicy)
			logf.Log.V(1).Info("Deleting AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			spKey := types.NamespacedName{Name: controllers.EnvoySecurityPolicyName(TestHTTPRouteName), Namespace: testNamespace}
			sp := &egv1alpha1.SecurityPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, spKey, sp)
				logf.Log.V(1).Info("Fetching envoy SecurityPolicy", "key", spKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Deletes security policy if route is deleted", func(ctx SpecContext) {
			err := k8sClient.Delete(ctx, gwRoute)
			logf.Log.V(1).Info("Deleting AuthPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			spKey := types.NamespacedName{Name: controllers.EnvoySecurityPolicyName(TestHTTPRouteName), Namespace: testNamespace}
			sp := &egv1alpha1.SecurityPolicy{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, spKey, sp)
				logf.Log.V(1).Info("Fetching envoy SecurityPolicy", "key", spKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})
})
