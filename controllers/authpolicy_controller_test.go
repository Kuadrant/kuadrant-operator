//go:build integration

package controllers

import (
	"context"
	"path/filepath"
	"time"

	authorinov1beta1 "github.com/kuadrant/authorino/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	secv1beta1resources "istio.io/client-go/pkg/apis/security/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

const (
	IstioGatewayName      = "istio-ingressgateway"
	IstioGatewayNamespace = "istio-system"
	CustomGatewayName     = "toystore-gw"
)

var _ = Describe("AuthPolicy controller", func() {
	var (
		testNamespace string
		routeName     = "toystore-route"
		gwName        = CustomGatewayName
	)

	beforeEachCallback := func() {
		CreateNamespace(&testNamespace)
		gateway := testBuildBasicGateway(gwName, testNamespace)
		err := k8sClient.Create(context.Background(), gateway)
		Expect(err).ToNot(HaveOccurred())
		ApplyKuadrantCR(testNamespace)
	}

	BeforeEach(beforeEachCallback)

	AfterEach(DeleteNamespaceCallback(&testNamespace))

	Context("Attach to HTTPRoute and Gateway", func() {
		It("Should create and delete everything successfully", func() {
			err := ApplyResources(filepath.Join("..", "examples", "toystore", "toystore.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())

			err = ApplyResources(filepath.Join("..", "examples", "toystore", "httproute.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())

			authpolicies := authPolicies(testNamespace)

			// creating authpolicies
			for idx := range authpolicies {
				err = k8sClient.Create(context.Background(), authpolicies[idx])
				logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(authpolicies[idx]).String(), "error", err)
				Expect(err).ToNot(HaveOccurred())

				// check Istio's AuthorizationPolicy existence
				iap := &secv1beta1resources.AuthorizationPolicy{}
				namespace := IstioGatewayNamespace
				name := IstioGatewayName
				if authpolicies[idx].Spec.TargetRef.Kind == "Gateway" {
					namespace = testNamespace
					name = CustomGatewayName
				}
				iapKey := types.NamespacedName{
					Name:      istioAuthorizationPolicyName(name, authpolicies[idx].Spec.TargetRef),
					Namespace: namespace,
				}
				Eventually(func() bool {
					err := k8sClient.Get(context.Background(), iapKey, iap)
					logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
					if err != nil && !apierrors.IsAlreadyExists(err) {
						return false
					}
					return true
				}, 2*time.Minute, 5*time.Second).Should(BeTrue())

				// check Authorino's AuthConfig existence
				ac := &authorinov1beta1.AuthConfig{}
				acKey := types.NamespacedName{
					Name:      authConfigName(client.ObjectKeyFromObject(authpolicies[idx])),
					Namespace: testNamespace,
				}
				Eventually(func() bool {
					err := k8sClient.Get(context.Background(), acKey, ac)
					logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", acKey.String(), "error", err)
					if err != nil && !apierrors.IsAlreadyExists(err) {
						return false
					}
					return true
				}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			}

			// deleting authpolicies
			for idx := range authpolicies {
				err = k8sClient.Delete(context.Background(), authpolicies[idx])
				logf.Log.V(1).Info("Deleting AuthPolicy", "key", client.ObjectKeyFromObject(authpolicies[idx]).String(), "error", err)
				Expect(err).ToNot(HaveOccurred())

				// check Istio's AuthorizationPolicy existence
				iap := &secv1beta1resources.AuthorizationPolicy{}
				namespace := IstioGatewayNamespace
				name := IstioGatewayName
				if authpolicies[idx].Spec.TargetRef.Kind == "Gateway" {
					namespace = testNamespace
					name = CustomGatewayName
				}
				iapKey := types.NamespacedName{
					Name:      istioAuthorizationPolicyName(name, authpolicies[idx].Spec.TargetRef),
					Namespace: namespace,
				}
				Eventually(func() bool {
					err := k8sClient.Get(context.Background(), iapKey, iap)
					logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
					if err != nil && apierrors.IsNotFound(err) {
						return true
					}
					return false
				}, 2*time.Minute, 5*time.Second).Should(BeTrue())

				// check Authorino's AuthConfig existence
				ac := &authorinov1beta1.AuthConfig{}
				acKey := types.NamespacedName{
					Name:      authConfigName(client.ObjectKeyFromObject(authpolicies[idx])),
					Namespace: testNamespace,
				}
				Eventually(func() bool {
					err := k8sClient.Get(context.Background(), acKey, ac)
					logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", acKey.String(), "error", err)
					if err != nil && apierrors.IsNotFound(err) {
						return true
					}
					return false
				}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			}
		})

	})

	Context("Some rules without hosts", func() {
		BeforeEach(func() {
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.toystore.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())

			typedNamespace := v1alpha2.Namespace(testNamespace)
			policy := &kuadrantv1beta1.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1beta1.AuthPolicySpec{
					TargetRef: v1alpha2.PolicyTargetReference{
						Group:     gatewayapiv1alpha2.Group(gatewayapiv1alpha2.GroupVersion.Group),
						Kind:      "HTTPRoute",
						Name:      gatewayapiv1alpha2.ObjectName(routeName),
						Namespace: &typedNamespace,
					},
					AuthRules: []kuadrantv1beta1.AuthRule{
						{
							Hosts:   []string{"*.admin.toystore.com"},
							Methods: []string{"DELETE", "POST"},
							Paths:   []string{"/admin*"},
						},
						{
							Methods: []string{"GET"},
							Paths:   []string{"/private*"},
						},
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}

			err = k8sClient.Create(context.Background(), policy)
			Expect(err).ToNot(HaveOccurred())

			kapKey := client.ObjectKey{Name: "toystore", Namespace: testNamespace}
			// Check KAP status is available
			Eventually(func() bool {
				existingKAP := &kuadrantv1beta1.AuthPolicy{}
				err := k8sClient.Get(context.Background(), kapKey, existingKAP)
				if err != nil {
					return false
				}
				if !meta.IsStatusConditionTrue(existingKAP.Status.Conditions, "Available") {
					return false
				}

				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("authconfig's hosts should be route's hostnames", func() {
			// Check authconfig's hosts
			kapKey := client.ObjectKey{Name: "toystore", Namespace: testNamespace}
			existingAuthC := &authorinov1beta1.AuthConfig{}
			authCKey := types.NamespacedName{Name: authConfigName(kapKey), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authCKey, existingAuthC)
				return err == nil
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
			Expect(existingAuthC.Spec.Hosts).To(Equal([]string{"*.toystore.com"}))
		})

		It("Istio's authorizationpolicy should include network resource hostnames on kuadrant rules without hosts", func() {
			typedNamespace := v1alpha2.Namespace(testNamespace)
			targetRef := v1alpha2.PolicyTargetReference{
				Group:     gatewayapiv1alpha2.Group(gatewayapiv1alpha2.GroupVersion.Group),
				Kind:      "HTTPRoute",
				Name:      gatewayapiv1alpha2.ObjectName(routeName),
				Namespace: &typedNamespace,
			}

			// Check Istio's authorization policy rules
			existingIAP := &secv1beta1resources.AuthorizationPolicy{}
			key := types.NamespacedName{
				Name:      istioAuthorizationPolicyName(gwName, targetRef),
				Namespace: testNamespace,
			}

			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, existingIAP)
				return err == nil
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			Expect(existingIAP.Spec.Rules).To(HaveLen(1))
			Expect(existingIAP.Spec.Rules[0].To).To(HaveLen(2))
			// operation 1
			Expect(existingIAP.Spec.Rules[0].To[0].Operation).ShouldNot(BeNil())
			Expect(existingIAP.Spec.Rules[0].To[0].Operation.Hosts).To(Equal([]string{"*.admin.toystore.com"}))
			Expect(existingIAP.Spec.Rules[0].To[0].Operation.Methods).To(Equal([]string{"DELETE", "POST"}))
			Expect(existingIAP.Spec.Rules[0].To[0].Operation.Paths).To(Equal([]string{"/admin*"}))
			// operation 2
			Expect(existingIAP.Spec.Rules[0].To[1].Operation).ShouldNot(BeNil())
			Expect(existingIAP.Spec.Rules[0].To[1].Operation.Hosts).To(Equal([]string{"*.toystore.com"}))
			Expect(existingIAP.Spec.Rules[0].To[1].Operation.Methods).To(Equal([]string{"GET"}))
			Expect(existingIAP.Spec.Rules[0].To[1].Operation.Paths).To(Equal([]string{"/private*"}))
		})
	})

	Context("All rules with subdomains", func() {
		BeforeEach(func() {
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.toystore.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())

			typedNamespace := v1alpha2.Namespace(testNamespace)
			policy := &kuadrantv1beta1.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1beta1.AuthPolicySpec{
					TargetRef: v1alpha2.PolicyTargetReference{
						Group:     gatewayapiv1alpha2.Group(gatewayapiv1alpha2.GroupVersion.Group),
						Kind:      "HTTPRoute",
						Name:      gatewayapiv1alpha2.ObjectName(routeName),
						Namespace: &typedNamespace,
					},
					AuthRules: []kuadrantv1beta1.AuthRule{
						{
							Hosts:   []string{"*.a.toystore.com"},
							Methods: []string{"DELETE", "POST"},
							Paths:   []string{"/admin*"},
						},
						{
							Hosts:   []string{"*.b.toystore.com"},
							Methods: []string{"POST"},
							Paths:   []string{"/other*"},
						},
						{
							Hosts:   []string{"*.a.toystore.com", "*.b.toystore.com"},
							Methods: []string{"GET"},
							Paths:   []string{"/private*"},
						},
					},
					AuthScheme: testBasicAuthScheme(),
				},
			}

			err = k8sClient.Create(context.Background(), policy)
			Expect(err).ToNot(HaveOccurred())

			kapKey := client.ObjectKey{Name: "toystore", Namespace: testNamespace}
			// Check KAP status is available
			Eventually(func() bool {
				existingKAP := &kuadrantv1beta1.AuthPolicy{}
				err := k8sClient.Get(context.Background(), kapKey, existingKAP)
				if err != nil {
					return false
				}
				if !meta.IsStatusConditionTrue(existingKAP.Status.Conditions, "Available") {
					return false
				}

				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("authconfig's hosts should be the list of subdomains with unique elements", func() {
			// Check authconfig's hosts
			kapKey := client.ObjectKey{Name: "toystore", Namespace: testNamespace}
			existingAuthC := &authorinov1beta1.AuthConfig{}
			authCKey := types.NamespacedName{Name: authConfigName(kapKey), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authCKey, existingAuthC)
				return err == nil
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
			Expect(existingAuthC.Spec.Hosts).To(HaveLen(2))
			Expect(existingAuthC.Spec.Hosts).To(ContainElements("*.a.toystore.com", "*.b.toystore.com"))
		})
	})

	Context("No rules", func() {
		BeforeEach(func() {
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.toystore.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())

			typedNamespace := v1alpha2.Namespace(testNamespace)
			policy := &kuadrantv1beta1.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toystore",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1beta1.AuthPolicySpec{
					TargetRef: v1alpha2.PolicyTargetReference{
						Group:     gatewayapiv1alpha2.Group(gatewayapiv1alpha2.GroupVersion.Group),
						Kind:      "HTTPRoute",
						Name:      gatewayapiv1alpha2.ObjectName(routeName),
						Namespace: &typedNamespace,
					},
					AuthRules:  nil,
					AuthScheme: testBasicAuthScheme(),
				},
			}

			err = k8sClient.Create(context.Background(), policy)
			Expect(err).ToNot(HaveOccurred())
			kapKey := client.ObjectKey{Name: "toystore", Namespace: testNamespace}
			// Check KAP status is available
			Eventually(func() bool {
				existingKAP := &kuadrantv1beta1.AuthPolicy{}
				err := k8sClient.Get(context.Background(), kapKey, existingKAP)
				if err != nil {
					return false
				}
				if !meta.IsStatusConditionTrue(existingKAP.Status.Conditions, "Available") {
					return false
				}

				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})

		It("authconfig's hosts should be route's hostnames", func() {
			// Check authconfig's hosts
			kapKey := client.ObjectKey{Name: "toystore", Namespace: testNamespace}
			existingAuthC := &authorinov1beta1.AuthConfig{}
			authCKey := types.NamespacedName{Name: authConfigName(kapKey), Namespace: testNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), authCKey, existingAuthC)
				return err == nil
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
			Expect(existingAuthC.Spec.Hosts).To(Equal([]string{"*.toystore.com"}))
		})
	})
})

func testBasicAuthScheme() kuadrantv1beta1.AuthSchemeSpec {
	return kuadrantv1beta1.AuthSchemeSpec{
		Identity: []*authorinov1beta1.Identity{
			{
				Name: "apiKey",
				APIKey: &authorinov1beta1.Identity_APIKey{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "toystore",
						},
					},
				},
				Credentials: authorinov1beta1.Credentials{
					In: authorinov1beta1.Credentials_In(
						"authorization_header",
					),
					KeySelector: "APIKEY",
				},
			},
		},
	}
}

func authPolicies(namespace string) []*kuadrantv1beta1.AuthPolicy {
	typedNamespace := v1alpha2.Namespace(namespace)
	routePolicy := &kuadrantv1beta1.AuthPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "target-route",
			Namespace: namespace,
		},
		Spec: kuadrantv1beta1.AuthPolicySpec{
			TargetRef: v1alpha2.PolicyTargetReference{
				Group:     "gateway.networking.k8s.io",
				Kind:      "HTTPRoute",
				Name:      "toystore",
				Namespace: &typedNamespace,
			},
			AuthRules: []kuadrantv1beta1.AuthRule{
				{
					Hosts:   []string{"*.toystore.com"},
					Methods: []string{"DELETE", "POST"},
					Paths:   []string{"/admin*"},
				},
			},
			AuthScheme: kuadrantv1beta1.AuthSchemeSpec{
				Identity: []*authorinov1beta1.Identity{
					{
						Name: "apiKey",
						APIKey: &authorinov1beta1.Identity_APIKey{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "toystore",
								},
							},
						},
						Credentials: authorinov1beta1.Credentials{
							In: authorinov1beta1.Credentials_In(
								"authorization_header",
							),
							KeySelector: "APIKEY",
						},
					},
				},
			},
		},
	}
	gatewayPolicy := routePolicy.DeepCopy()
	gatewayPolicy.SetName("target-gateway")
	gatewayPolicy.SetNamespace(namespace)
	gatewayPolicy.Spec.TargetRef.Kind = "Gateway"
	gatewayPolicy.Spec.TargetRef.Name = CustomGatewayName
	gatewayPolicy.Spec.TargetRef.Namespace = &typedNamespace
	gatewayPolicy.Spec.AuthRules = []kuadrantv1beta1.AuthRule{
		{Hosts: []string{"*.toystore.com"}},
	}
	gatewayPolicy.Spec.AuthScheme.Identity[0].APIKey.Selector.MatchLabels["admin"] = "yes"

	return []*kuadrantv1beta1.AuthPolicy{routePolicy, gatewayPolicy}
}
