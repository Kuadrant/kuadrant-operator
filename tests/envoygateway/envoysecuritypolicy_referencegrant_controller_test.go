//go:build integration

package envoygateway_test

import (
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

var _ = Describe("Envoy SecurityPolicy ReferenceGrant controller", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		routePolicyOne *kuadrantv1beta2.AuthPolicy
		gateway        *gatewayapiv1.Gateway
		route          *gatewayapiv1.HTTPRoute
	)

	initGatewayRoutePolicy := func(ctx SpecContext, testNamespace string, policy *kuadrantv1beta2.AuthPolicy) {
		gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)
		err := k8sClient.Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())
		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

		route = tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.example.com"})
		err = k8sClient.Create(ctx, route)
		Expect(err).ToNot(HaveOccurred())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

		err = k8sClient.Create(ctx, policy)
		logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(policy).String(), "error", err)
		Expect(err).ToNot(HaveOccurred())
	}

	policyFactory := func(testNamespace string, mutateFns ...func(policy *kuadrantv1beta2.AuthPolicy)) *kuadrantv1beta2.AuthPolicy {
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

	Context("Single auth policy namespace", func() {

		var (
			testNamespaceOne string
		)

		BeforeEach(func(ctx SpecContext) {
			testNamespaceOne = tests.CreateNamespace(ctx, testClient())
			routePolicyOne = policyFactory(testNamespaceOne)
			initGatewayRoutePolicy(ctx, testNamespaceOne, routePolicyOne)
		})

		AfterEach(func(ctx SpecContext) {
			tests.DeleteNamespace(ctx, testClient(), testNamespaceOne)
		}, afterEachTimeOut)

		It("Creates reference grant", func(ctx SpecContext) {
			rgKey := types.NamespacedName{Name: controllers.KuadrantReferenceGrantName, Namespace: kuadrantInstallationNS}

			Eventually(func() *gatewayapiv1beta1.ReferenceGrant {
				rg := &gatewayapiv1beta1.ReferenceGrant{}
				err := k8sClient.Get(ctx, rgKey, rg)
				logf.Log.V(1).Info("Fetching ReferenceGrant", "key", rgKey.String(), "error", err)
				if err != nil {
					return nil
				}
				return rg
			}).WithContext(ctx).Should(PointTo(MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Name":      Equal(controllers.KuadrantReferenceGrantName),
					"Namespace": Equal(kuadrantInstallationNS),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"To": ConsistOf(MatchFields(IgnoreExtras, Fields{
						"Group": Equal(gatewayapiv1.Group("")),
						"Kind":  Equal(gatewayapiv1.Kind("Service")),
						"Name":  PointTo(Equal(gatewayapiv1.ObjectName(kuadrant.AuthorinoServiceName))),
					})),
					"From": ContainElement(MatchFields(IgnoreExtras, Fields{
						"Group":     Equal(gatewayapiv1.Group(egv1alpha1.GroupName)),
						"Kind":      Equal(gatewayapiv1.Kind(egv1alpha1.KindSecurityPolicy)),
						"Namespace": Equal(gatewayapiv1.Namespace(testNamespaceOne)),
					})),
				}),
			})))
		}, testTimeOut)

		It("Deleting auth policy removes reference grant", func(ctx SpecContext) {
			err := k8sClient.Delete(ctx, routePolicyOne)
			logf.Log.V(1).Info("Deleting AuthPolicy", "key", client.ObjectKeyFromObject(routePolicyOne).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			rgKey := types.NamespacedName{Name: controllers.KuadrantReferenceGrantName, Namespace: kuadrantInstallationNS}
			rg := &gatewayapiv1beta1.ReferenceGrant{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, rgKey, rg)
				logf.Log.V(1).Info("Fetching ReferenceGrant", "key", rgKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("Single auth policy in kuadrant installation namespace", func() {

		BeforeEach(func(ctx SpecContext) {
			routePolicyOne = policyFactory(kuadrantInstallationNS)
			initGatewayRoutePolicy(ctx, kuadrantInstallationNS, routePolicyOne)
		})

		AfterEach(func(ctx SpecContext) {
			err := k8sClient.Delete(ctx, routePolicyOne)
			logf.Log.V(1).Info("Deleting AuthPolicy", "key", client.ObjectKeyFromObject(routePolicyOne).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			err = k8sClient.Delete(ctx, route)
			logf.Log.V(1).Info("Deleting HTTPRoute", "key", client.ObjectKeyFromObject(route).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			err = k8sClient.Delete(ctx, gateway)
			logf.Log.V(1).Info("Deleting Gateway", "key", client.ObjectKeyFromObject(route).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

		}, afterEachTimeOut)

		It("Does not create reference grant", func(ctx SpecContext) {
			rgKey := types.NamespacedName{Name: controllers.KuadrantReferenceGrantName, Namespace: kuadrantInstallationNS}
			rg := &gatewayapiv1beta1.ReferenceGrant{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, rgKey, rg)
				logf.Log.V(1).Info("Fetching ReferenceGrant", "key", rgKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		})

	})

	Context("Multiple auth policy namespaces", func() {

		var (
			testNamespaceOne string
			testNamespaceTwo string
			routePolicyTwo   *kuadrantv1beta2.AuthPolicy
		)

		BeforeEach(func(ctx SpecContext) {
			testNamespaceOne = tests.CreateNamespace(ctx, testClient())
			routePolicyOne = policyFactory(testNamespaceOne)
			initGatewayRoutePolicy(ctx, testNamespaceOne, routePolicyOne)
			testNamespaceTwo = tests.CreateNamespace(ctx, testClient())
			routePolicyTwo = policyFactory(testNamespaceTwo)
			initGatewayRoutePolicy(ctx, testNamespaceTwo, routePolicyTwo)
		})

		AfterEach(func(ctx SpecContext) {
			tests.DeleteNamespace(ctx, testClient(), testNamespaceOne)
			tests.DeleteNamespace(ctx, testClient(), testNamespaceTwo)
		}, afterEachTimeOut)

		It("Creates reference grant", func(ctx SpecContext) {
			rgKey := types.NamespacedName{Name: controllers.KuadrantReferenceGrantName, Namespace: kuadrantInstallationNS}

			Eventually(func() *gatewayapiv1beta1.ReferenceGrant {
				rg := &gatewayapiv1beta1.ReferenceGrant{}
				err := k8sClient.Get(ctx, rgKey, rg)
				logf.Log.V(1).Info("Fetching ReferenceGrant", "key", rgKey.String(), "error", err)
				if err != nil {
					return nil
				}
				return rg
			}).WithContext(ctx).Should(PointTo(MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Name":      Equal(controllers.KuadrantReferenceGrantName),
					"Namespace": Equal(kuadrantInstallationNS),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"To": ConsistOf(MatchFields(IgnoreExtras, Fields{
						"Group": Equal(gatewayapiv1.Group("")),
						"Kind":  Equal(gatewayapiv1.Kind("Service")),
						"Name":  PointTo(Equal(gatewayapiv1.ObjectName(kuadrant.AuthorinoServiceName))),
					})),
					"From": ContainElements(
						MatchFields(IgnoreExtras, Fields{
							"Group":     Equal(gatewayapiv1.Group(egv1alpha1.GroupName)),
							"Kind":      Equal(gatewayapiv1.Kind(egv1alpha1.KindSecurityPolicy)),
							"Namespace": Equal(gatewayapiv1.Namespace(testNamespaceOne)),
						}),
						MatchFields(IgnoreExtras, Fields{
							"Group":     Equal(gatewayapiv1.Group(egv1alpha1.GroupName)),
							"Kind":      Equal(gatewayapiv1.Kind(egv1alpha1.KindSecurityPolicy)),
							"Namespace": Equal(gatewayapiv1.Namespace(testNamespaceTwo)),
						})),
				}),
			})))
		}, testTimeOut)

		It("Deleting policy updates reference grant", func(ctx SpecContext) {
			err := k8sClient.Delete(ctx, routePolicyTwo)
			logf.Log.V(1).Info("Deleting AuthPolicy", "key", client.ObjectKeyFromObject(routePolicyTwo).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			rgKey := types.NamespacedName{Name: controllers.KuadrantReferenceGrantName, Namespace: kuadrantInstallationNS}

			Eventually(func() *gatewayapiv1beta1.ReferenceGrant {
				rg := &gatewayapiv1beta1.ReferenceGrant{}
				err := k8sClient.Get(ctx, rgKey, rg)
				logf.Log.V(1).Info("Fetching ReferenceGrant", "key", rgKey.String(), "error", err)
				if err != nil {
					return nil
				}
				return rg
			}).WithContext(ctx).Should(PointTo(MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Name":      Equal(controllers.KuadrantReferenceGrantName),
					"Namespace": Equal(kuadrantInstallationNS),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"To": ConsistOf(MatchFields(IgnoreExtras, Fields{
						"Group": Equal(gatewayapiv1.Group("")),
						"Kind":  Equal(gatewayapiv1.Kind("Service")),
						"Name":  PointTo(Equal(gatewayapiv1.ObjectName(kuadrant.AuthorinoServiceName))),
					})),
					"From": ContainElement(MatchFields(IgnoreExtras, Fields{
						"Group":     Equal(gatewayapiv1.Group(egv1alpha1.GroupName)),
						"Kind":      Equal(gatewayapiv1.Kind(egv1alpha1.KindSecurityPolicy)),
						"Namespace": Equal(gatewayapiv1.Namespace(testNamespaceOne)),
					})),
				}),
			})))
		}, testTimeOut)
	})
})
