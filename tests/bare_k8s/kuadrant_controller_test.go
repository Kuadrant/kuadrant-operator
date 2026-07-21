//go:build integration

package bare_k8s_test

import (
	"context"
	"time"

	authorinoopapi "github.com/kuadrant/authorino-operator/api/v1beta1"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta3"
	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/internal/openshift"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Kuadrant controller when Gateway API is missing", func() {
	var (
		testNamespace    string
		testTimeOut      = SpecTimeout(30 * time.Second)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("when default kuadrant CR is created", func() {
		var kuadrantCR *kuadrantv1beta1.Kuadrant

		BeforeEach(func(ctx SpecContext) {
			kuadrantCR = &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "local",
					Namespace: testNamespace,
				},
			}
			Expect(testClient().Create(ctx, kuadrantCR)).ToNot(HaveOccurred())
		})

		It("Status is populated with missing Gateway API", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)).To(Succeed())

				cond := meta.FindStatusCondition(kuadrantCR.Status.Conditions, controllers.ReadyConditionType)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("MissingDependency"))
				g.Expect(cond.Message).To(Equal("[Gateway API, Gateway API provider (istio / envoy gateway)] is not installed, please restart Kuadrant Operator pod once dependency has been installed"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Limitador CR is ready", func(ctx SpecContext) {
			limitador := &limitadorv1alpha1.Limitador{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Limitador",
					APIVersion: limitadorv1alpha1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      kuadrant.LimitadorName,
					Namespace: testNamespace,
				},
			}

			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(limitador), limitador)).To(Succeed())
				g.Expect(limitador.Spec.MetricLabelsDefault).ToNot(BeNil())
				g.Expect(limitador.Spec.MetricLabelsDefault).To(Equal(ptr.To("descriptors[1]")))
				g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(limitador), limitador)).To(Succeed())

				cond := meta.FindStatusCondition(limitador.Status.Conditions, limitadorv1alpha1.StatusConditionReady)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Limitador CR should retain user fields and restore default", func(ctx SpecContext) {
			By("Patching Limitador CR with user fields")
			limitador := &limitadorv1alpha1.Limitador{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Limitador",
					APIVersion: limitadorv1alpha1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      kuadrant.LimitadorName,
					Namespace: testNamespace,
				},
				Spec: limitadorv1alpha1.LimitadorSpec{
					MetricLabelsDefault: ptr.To("descriptors[0]"),
					Listener: &limitadorv1alpha1.Listener{
						HTTP: &limitadorv1alpha1.TransportProtocol{
							Port: ptr.To(int32(9000)),
						},
					},
				},
			}

			Expect(testClient().Patch(ctx, limitador, client.Apply, &client.PatchOptions{FieldManager: "test-manager"})).ToNot(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(limitador), limitador)).To(Succeed())

				// Check that managed fields are restored
				g.Expect(limitador.Spec.MetricLabelsDefault).ToNot(BeNil())
				g.Expect(limitador.Spec.MetricLabelsDefault).To(Equal(ptr.To("descriptors[1]")))

				// Check that user added fields are retained
				g.Expect(limitador.Spec.Listener).ToNot(BeNil())
				g.Expect(limitador.Spec.Listener.HTTP).ToNot(BeNil())
				g.Expect(limitador.Spec.Listener.HTTP.Port).ToNot(BeNil())
				g.Expect(*limitador.Spec.Listener.HTTP.Port).To(Equal(int32(9000)))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when dns policy is created", func() {
		It("Status is populated with missing Gateway API", func(ctx SpecContext) {
			policy := kuadrantv1.NewDNSPolicy("dns", testNamespace).WithTargetGateway("test")
			policy.Spec.ProviderRefs = append(policy.Spec.ProviderRefs, kuadrantdnsv1alpha1.ProviderRef{
				Name: "dnsProviderSecret",
			})
			Expect(testClient().Create(ctx, policy)).To(Succeed())

			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(policy), policy)
				g.Expect(err).ToNot(HaveOccurred())

				cond := meta.FindStatusCondition(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("MissingDependency"))
				g.Expect(cond.Message).To(Equal("[Gateway API] is not installed, please restart Kuadrant Operator pod once dependency has been installed"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when tls policy is created", func() {
		It("Status is populated with missing Gateway API", func(ctx SpecContext) {
			policy := kuadrantv1.NewTLSPolicy("tls", testNamespace).
				WithTargetGateway("test")

			Expect(testClient().Create(ctx, policy)).To(Succeed())

			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(policy), policy)
				g.Expect(err).ToNot(HaveOccurred())

				cond := meta.FindStatusCondition(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("MissingDependency"))
				g.Expect(cond.Message).To(Equal("[Gateway API] is not installed, please restart Kuadrant Operator pod once dependency has been installed"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when rate limit policy is created", func() {
		It("Status is populated with missing Gateway API", func(ctx SpecContext) {
			policy := &kuadrantv1.RateLimitPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rlp",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Kind:  "Gateway",
							Group: gatewayapiv1.GroupName,
							Name:  "test",
						},
					},
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{
							"test": {
								Rates: []kuadrantv1.Rate{
									{
										Limit:  10,
										Window: "10s",
									},
								},
							},
						},
					},
				},
			}

			Expect(testClient().Create(ctx, policy)).To(Succeed())

			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(policy), policy)
				g.Expect(err).ToNot(HaveOccurred())

				cond := meta.FindStatusCondition(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("MissingDependency"))
				g.Expect(cond.Message).To(Equal("[Gateway API, Gateway API provider (istio / envoy gateway)] is not installed, please restart Kuadrant Operator pod once dependency has been installed"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when auth policy is created", func() {
		It("Status is populated with missing Gateway API", func(ctx SpecContext) {
			policy := &kuadrantv1.AuthPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Kind:  "Gateway",
							Group: gatewayapiv1.GroupName,
							Name:  "test",
						},
					},
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: &kuadrantv1.AuthSchemeSpec{
							Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
								"anyonmous": {
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

			Expect(testClient().Create(ctx, policy)).To(Succeed())

			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(policy), policy)
				g.Expect(err).ToNot(HaveOccurred())

				cond := meta.FindStatusCondition(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("MissingDependency"))
				g.Expect(cond.Message).To(Equal("[Gateway API, Gateway API provider (istio / envoy gateway)] is not installed, please restart Kuadrant Operator pod once dependency has been installed"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("tracing configuration", func() {
		var kuadrantCR *kuadrantv1beta1.Kuadrant

		BeforeEach(func(ctx SpecContext) {
			kuadrantCR = &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "local",
					Namespace: testNamespace,
				},
			}
			Expect(testClient().Create(ctx, kuadrantCR)).To(Succeed())
		})

		It("Propagated to limitador and authorino", func(ctx SpecContext) {
			assertTracingIsNotConfigured := func(g Gomega) {
				limtador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}, limtador)).To(Succeed())
				g.Expect(limtador.Spec.Tracing).To(BeNil())

				authorino := &authorinoopapi.Authorino{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())
				g.Expect(authorino.Spec.Tracing).ToNot(BeNil())
				g.Expect(authorino.Spec.Tracing.Endpoint).To(BeEmpty())
				g.Expect(authorino.Spec.Tracing.Insecure).To(BeFalse())
			}
			Eventually(assertTracingIsNotConfigured).WithContext(ctx).Should(Succeed())

			By("Patching Kuadrant CR with tracing configuration")
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "local", Namespace: testNamespace}, kuadrantCR)).To(Succeed())
				kuadrantCR.Spec.Observability.Tracing = &kuadrantv1beta1.Tracing{
					DefaultEndpoint: "grpc://kuadrant-collector:4317",
					Insecure:        true,
				}
				g.Expect(testClient().Update(ctx, kuadrantCR)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())
			assertTracingIsConfigured := func(g Gomega) {
				authorino := &authorinoopapi.Authorino{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())
				g.Expect(authorino.Spec.Tracing.Endpoint).To(Equal(kuadrantCR.Spec.Observability.Tracing.DefaultEndpoint))
				g.Expect(authorino.Spec.Tracing.Insecure).To(Equal(kuadrantCR.Spec.Observability.Tracing.Insecure))

				limitador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}, limitador)).To(Succeed())
				g.Expect(limitador.Spec.Tracing.Endpoint).To(Equal(kuadrantCR.Spec.Observability.Tracing.DefaultEndpoint))
			}
			Eventually(assertTracingIsConfigured).WithContext(ctx).Should(Succeed())

			By("Resetting removing tracing configuration")
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "local", Namespace: testNamespace}, kuadrantCR)).To(Succeed())
				kuadrantCR.Spec.Observability.Tracing = nil
				g.Expect(testClient().Update(ctx, kuadrantCR)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())
			Eventually(assertTracingIsNotConfigured).WithContext(ctx).Should(Succeed())

			By("Patching Kuadrant CR with tracing configuration")
			kuadrantCR = &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "local",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1beta1.KuadrantSpec{
					Observability: kuadrantv1beta1.Observability{
						Tracing: &kuadrantv1beta1.Tracing{
							DefaultEndpoint: "grpc://kuadrant-collector:4317",
							Insecure:        true,
						},
					},
				},
			}

			Expect(testClient().Patch(ctx, kuadrantCR, client.Apply, &client.PatchOptions{FieldManager: "test-manager"})).To(Succeed())
			Eventually(assertTracingIsConfigured).WithContext(ctx).Should(Succeed())

			By("Patching Kuadrant tracing endpoint to be empty")
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "local", Namespace: testNamespace}, kuadrantCR)).To(Succeed())
				kuadrantCR.Spec.Observability.Tracing = &kuadrantv1beta1.Tracing{
					DefaultEndpoint: "",
					Insecure:        false,
				}
				g.Expect(testClient().Update(ctx, kuadrantCR)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())
			Eventually(assertTracingIsNotConfigured).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Ownership is ceded to user when set in child CRs", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				limtador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}, limtador)).To(Succeed())

				og := limtador.DeepCopy()

				limtador.Spec.Tracing = &limitadorv1alpha1.Tracing{
					Endpoint: "grpc://limitador-collector:4317",
				}
				limtador.Spec.Image = ptr.To("limitador-image:test")
				g.Expect(testClient().Patch(ctx, limtador, client.MergeFrom(og))).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				limitador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}, limitador)).To(Succeed())
				g.Expect(limitador.Spec.Tracing).ToNot(BeNil())
				g.Expect(limitador.Spec.Image).ToNot(BeNil())
				g.Expect(limitador.Spec.Tracing.Endpoint).To(Equal("grpc://limitador-collector:4317"))
				g.Expect(limitador.Spec.Image).To(Equal(ptr.To("limitador-image:test")))
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				authorino := &authorinoopapi.Authorino{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())

				og := authorino.DeepCopy()

				authorino.Spec.Tracing = authorinoopapi.Tracing{
					Endpoint: "grpc://authorino-collector:4317",
					Insecure: true,
				}
				authorino.Spec.ClusterWide = false
				g.Expect(testClient().Patch(ctx, authorino, client.MergeFrom(og))).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				authorino := &authorinoopapi.Authorino{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())
				g.Expect(authorino.Spec.Tracing.Endpoint).To(Equal("grpc://authorino-collector:4317"))
				g.Expect(authorino.Spec.Tracing.Insecure).To(Equal(true))
				g.Expect(authorino.Spec.ClusterWide).To(Equal(false))
			}).WithContext(ctx).Should(Succeed())

			By("Patching Kuadrant CR with tracing configuration")
			kuadrantCR := &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "local",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1beta1.KuadrantSpec{
					Observability: kuadrantv1beta1.Observability{
						Tracing: &kuadrantv1beta1.Tracing{
							DefaultEndpoint: "grpc://kuadrant-collector:4317",
							Insecure:        false,
						},
					},
				},
			}

			Expect(testClient().Patch(ctx, kuadrantCR, client.Apply, &client.PatchOptions{FieldManager: "test-manager"})).To(Succeed())

			Eventually(func(g Gomega) {
				authorino := &authorinoopapi.Authorino{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())
				// Tracing field is still unmanaged
				g.Expect(authorino.Spec.Tracing.Endpoint).To(Equal("grpc://authorino-collector:4317"))
				g.Expect(authorino.Spec.Tracing.Insecure).To(Equal(true))
				// Other unmanaged fields are still kept
				g.Expect(authorino.Spec.ClusterWide).To(Equal(false))
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				limitador := &limitadorv1alpha1.Limitador{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}, limitador)).To(Succeed())
				// Tracing field is still unmanaged
				g.Expect(limitador.Spec.Tracing.Endpoint).To(Equal("grpc://limitador-collector:4317"))
				// Other unmanaged fields are still kept
				g.Expect(limitador.Spec.Image).ToNot(BeNil())
				g.Expect(*limitador.Spec.Image).To(Equal("limitador-image:test"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("TLS profile configuration", func() {
		var kuadrantCR *kuadrantv1beta1.Kuadrant

		tlsTestTimeOut := SpecTimeout(60 * time.Second)

		BeforeEach(func(ctx SpecContext) {
			kuadrantCR = &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "local",
					Namespace: testNamespace,
				},
			}
			Expect(testClient().Create(ctx, kuadrantCR)).To(Succeed())
		})

		enableTLS := func(ctx context.Context) {
			authorino := &authorinoopapi.Authorino{}
			Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())
			if authorino.Spec.Listener.Tls.Enabled != nil && *authorino.Spec.Listener.Tls.Enabled {
				return
			}
			og := authorino.DeepCopy()
			authorino.Spec.Listener.Tls.Enabled = ptr.To(true)
			authorino.Spec.OIDCServer.Tls.Enabled = ptr.To(true)
			Expect(testClient().Patch(ctx, authorino, client.MergeFrom(og))).To(Succeed())
		}

		It("Propagates default TLS profile to Authorino when TLS is enabled", func(ctx SpecContext) {
			expectedMinVersion, expectedCipherSuites := openshift.ResolveTLSProfile(nil)

			By("Verifying Authorino is created with TLS disabled and no profile fields")
			Eventually(func(g Gomega) {
				authorino := &authorinoopapi.Authorino{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())
				g.Expect(authorino.Spec.Listener.Tls.Enabled).ToNot(BeNil())
				g.Expect(*authorino.Spec.Listener.Tls.Enabled).To(BeFalse())
				g.Expect(authorino.Spec.Listener.Tls.MinVersion).To(BeEmpty())
				g.Expect(authorino.Spec.Listener.Tls.CipherSuites).To(BeEmpty())
				g.Expect(authorino.Spec.OIDCServer.Tls.Enabled).ToNot(BeNil())
				g.Expect(*authorino.Spec.OIDCServer.Tls.Enabled).To(BeFalse())
				g.Expect(authorino.Spec.OIDCServer.Tls.MinVersion).To(BeEmpty())
				g.Expect(authorino.Spec.OIDCServer.Tls.CipherSuites).To(BeEmpty())
			}).WithContext(ctx).Should(Succeed())

			By("Enabling TLS and verifying default Intermediate profile is applied")
			enableTLS(ctx)
			Eventually(func(g Gomega) {
				enableTLS(ctx)
				authorino := &authorinoopapi.Authorino{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())
				g.Expect(authorino.Spec.Listener.Tls.MinVersion).To(Equal(expectedMinVersion))
				g.Expect(authorino.Spec.Listener.Tls.CipherSuites).To(Equal(expectedCipherSuites))
				g.Expect(authorino.Spec.OIDCServer.Tls.MinVersion).To(Equal(expectedMinVersion))
				g.Expect(authorino.Spec.OIDCServer.Tls.CipherSuites).To(Equal(expectedCipherSuites))
			}).WithContext(ctx).Should(Succeed())

			By("Disabling TLS on Authorino")
			Eventually(func(g Gomega) {
				authorino := &authorinoopapi.Authorino{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())
				og := authorino.DeepCopy()
				authorino.Spec.Listener.Tls.Enabled = ptr.To(false)
				authorino.Spec.OIDCServer.Tls.Enabled = ptr.To(false)
				g.Expect(testClient().Patch(ctx, authorino, client.MergeFrom(og))).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			By("Verifying TLS profile fields are cleared")
			Eventually(func(g Gomega) {
				authorino := &authorinoopapi.Authorino{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())
				g.Expect(authorino.Spec.Listener.Tls.MinVersion).To(BeEmpty())
				g.Expect(authorino.Spec.Listener.Tls.CipherSuites).To(BeEmpty())
				g.Expect(authorino.Spec.OIDCServer.Tls.MinVersion).To(BeEmpty())
				g.Expect(authorino.Spec.OIDCServer.Tls.CipherSuites).To(BeEmpty())
			}).WithContext(ctx).Should(Succeed())
		}, tlsTestTimeOut)

		It("Propagates OpenShift APIServer TLS profile to Authorino", func(ctx SpecContext) {
			By("Waiting for Authorino to be created with TLS disabled")
			Eventually(func(g Gomega) {
				authorino := &authorinoopapi.Authorino{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())
				g.Expect(authorino.Spec.Listener.Tls.Enabled).To(Equal(ptr.To(false)))
			}).WithContext(ctx).Should(Succeed())

			By("Enabling TLS and waiting for Intermediate profile to stabilize")
			enableTLS(ctx)
			defaultMinVersion, defaultCipherSuites := openshift.ResolveTLSProfile(nil)
			Eventually(func(g Gomega) {
				enableTLS(ctx)
				authorino := &authorinoopapi.Authorino{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())
				g.Expect(authorino.Spec.Listener.Tls.Enabled).To(Equal(ptr.To(true)))
				g.Expect(authorino.Spec.Listener.Tls.MinVersion).To(Equal(defaultMinVersion))
				g.Expect(authorino.Spec.Listener.Tls.CipherSuites).To(Equal(defaultCipherSuites))
			}).WithContext(ctx).Should(Succeed())

			By("Creating an APIServer CR with Old TLS profile")
			apiServer := &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{
					Name: openshift.DefaultAPIServerCRName,
				},
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileOldType,
					},
				},
			}
			Expect(testClient().Create(ctx, apiServer)).To(Succeed())
			defer func() {
				_ = testClient().Delete(context.Background(), apiServer)
			}()

			expectedMinVersion, expectedCipherSuites := openshift.ResolveTLSProfile(apiServer.Spec.TLSSecurityProfile)

			By("Verifying Old TLS profile is applied")
			Eventually(func(g Gomega) {
				authorino := &authorinoopapi.Authorino{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())
				g.Expect(authorino.Spec.Listener.Tls.MinVersion).To(Equal(expectedMinVersion))
				g.Expect(authorino.Spec.Listener.Tls.CipherSuites).To(Equal(expectedCipherSuites))
				g.Expect(authorino.Spec.OIDCServer.Tls.MinVersion).To(Equal(expectedMinVersion))
				g.Expect(authorino.Spec.OIDCServer.Tls.CipherSuites).To(Equal(expectedCipherSuites))
			}).WithContext(ctx).Should(Succeed())

			By("Updating APIServer CR to Modern TLS profile")
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(apiServer), apiServer)).To(Succeed())
				apiServer.Spec.TLSSecurityProfile = &configv1.TLSSecurityProfile{
					Type: configv1.TLSProfileModernType,
				}
				g.Expect(testClient().Update(ctx, apiServer)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			expectedMinVersion, expectedCipherSuites = openshift.ResolveTLSProfile(apiServer.Spec.TLSSecurityProfile)

			By("Verifying Modern TLS profile is applied")
			Eventually(func(g Gomega) {
				authorino := &authorinoopapi.Authorino{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: "authorino", Namespace: testNamespace}, authorino)).To(Succeed())
				g.Expect(authorino.Spec.Listener.Tls.MinVersion).To(Equal(expectedMinVersion))
				g.Expect(authorino.Spec.Listener.Tls.CipherSuites).To(Equal(expectedCipherSuites))
				g.Expect(authorino.Spec.OIDCServer.Tls.MinVersion).To(Equal(expectedMinVersion))
				g.Expect(authorino.Spec.OIDCServer.Tls.CipherSuites).To(Equal(expectedCipherSuites))
			}).WithContext(ctx).Should(Succeed())
		}, tlsTestTimeOut)
	})
})
