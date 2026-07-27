//go:build integration

package istio_test

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Tracing Cluster EnvoyFilter controller", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
	)

	createAuthPolicy := func(ctx SpecContext) {
		policy := &kuadrantv1.AuthPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AuthPolicy",
				APIVersion: kuadrantv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "toystore-auth",
				Namespace: testNamespace,
			},
			Spec: kuadrantv1.AuthPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "HTTPRoute",
						Name:  gatewayapiv1.ObjectName(TestHTTPRouteName),
					},
				},
				Defaults: &kuadrantv1.MergeableAuthPolicySpec{
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: tests.BuildBasicAuthScheme(),
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, policy)).To(Succeed())
	}

	beforeEachCallback := func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway := tests.BuildBasicGateway(TestGatewayName, testNamespace)
		err := testClient().Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

		route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.toystore.com"})
		Expect(k8sClient.Create(ctx, route)).To(Succeed())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())
	}

	BeforeEach(beforeEachCallback)
	AfterEach(func(ctx SpecContext) {
		// Clean up tracing configuration
		kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
		kuadrantObj := &kuadrantv1beta1.Kuadrant{}
		Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
		original := kuadrantObj.DeepCopy()
		kuadrantObj.Spec.Observability.Tracing = nil
		Expect(testClient().Patch(ctx, kuadrantObj, client.MergeFrom(original))).To(Succeed())

		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("Tracing configuration on Kuadrant CR", func() {
		// tracing with mTLS disabled (insecure: true)
		BeforeEach(func(ctx SpecContext) {
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			original := kuadrantObj.DeepCopy()
			kuadrantObj.Spec.Observability.Tracing = &kuadrantv1beta1.Tracing{
				DefaultEndpoint: "http://jaeger-collector.observability.svc:14268/api/traces",
				Insecure:        true,
			}
			Expect(testClient().Patch(ctx, kuadrantObj, client.MergeFrom(original))).To(Succeed())
		})

		It("EnvoyFilter created with tracing cluster when gateway has effective policy", func(ctx SpecContext) {
			createAuthPolicy(ctx)

			// Check envoy filter has been created
			existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
			Eventually(func(g Gomega, ctx context.Context) {
				efKey := client.ObjectKey{Name: controllers.TracingClusterName(TestGatewayName), Namespace: testNamespace}
				g.Expect(testClient().Get(ctx, efKey, existingEF)).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// Check envoy filter configuration
			Expect(existingEF.Spec.ConfigPatches).To(HaveLen(1))
			Expect(existingEF.Spec.ConfigPatches[0].Patch).NotTo(BeNil())
			Expect(existingEF.Spec.ConfigPatches[0].Patch.Value).NotTo(BeNil())

			// Verify cluster configuration
			patchValueRaw, err := json.Marshal(existingEF.Spec.ConfigPatches[0].Patch.Value)
			Expect(err).ToNot(HaveOccurred())
			var patchValue map[string]any
			Expect(json.Unmarshal(patchValueRaw, &patchValue)).ToNot(HaveOccurred())
			Expect(patchValue).To(HaveKey("name"))
			Expect(patchValue["name"]).To(Equal(kuadrant.KuadrantTracingClusterName))

			// Check load_assignment has correct host and port
			Expect(patchValue).To(HaveKey("load_assignment"))
			loadAssignment := patchValue["load_assignment"].(map[string]interface{})
			endpoints := loadAssignment["endpoints"].([]interface{})[0].(map[string]interface{})
			lbEndpoints := endpoints["lb_endpoints"].([]interface{})[0].(map[string]interface{})
			endpoint := lbEndpoints["endpoint"].(map[string]interface{})
			address := endpoint["address"].(map[string]interface{})
			socketAddress := address["socket_address"].(map[string]interface{})
			Expect(socketAddress["address"]).To(Equal("jaeger-collector.observability.svc"))
			Expect(socketAddress["port_value"]).To(BeNumerically("==", 14268))

			// transport_socket should NOT be present when insecure: true
			Expect(patchValue).NotTo(HaveKey("transport_socket"))
		}, testTimeOut)
	})

	Context("when tracing mTLS is enabled", func() {
		BeforeEach(func(ctx SpecContext) {
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			original := kuadrantObj.DeepCopy()
			kuadrantObj.Spec.Observability.Tracing = &kuadrantv1beta1.Tracing{
				DefaultEndpoint: "https://secure-collector.observability.svc:443",
				Insecure:        false, // mTLS enabled
			}
			Expect(testClient().Patch(ctx, kuadrantObj, client.MergeFrom(original))).To(Succeed())
		})

		It("envoy filter has transport configured with mTLS", func(ctx SpecContext) {
			createAuthPolicy(ctx)

			existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
			Eventually(func(g Gomega, ctx context.Context) {
				efKey := client.ObjectKey{Name: controllers.TracingClusterName(TestGatewayName), Namespace: testNamespace}
				g.Expect(testClient().Get(ctx, efKey, existingEF)).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			Expect(existingEF.Spec.ConfigPatches).To(HaveLen(1))
			Expect(existingEF.Spec.ConfigPatches[0].Patch).NotTo(BeNil())
			Expect(existingEF.Spec.ConfigPatches[0].Patch.Value).NotTo(BeNil())

			patchValueRaw, err := json.Marshal(existingEF.Spec.ConfigPatches[0].Patch.Value)
			Expect(err).ToNot(HaveOccurred())
			var patchValue map[string]any
			Expect(json.Unmarshal(patchValueRaw, &patchValue)).ToNot(HaveOccurred())
			Expect(patchValue).To(HaveKey("name"))

			// transport_socket config should be present when insecure: false
			Expect(patchValue).To(HaveKey("transport_socket"))
			Expect(patchValue["transport_socket"]).To(Equal(map[string]interface{}{
				"name": "envoy.transport_sockets.tls",
				"typed_config": map[string]interface{}{
					"@type": "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext",
					"common_tls_context": map[string]interface{}{
						"tls_certificate_sds_secret_configs": []interface{}{
							map[string]interface{}{
								"name": "default",
								"sds_config": map[string]interface{}{
									"api_config_source": map[string]interface{}{
										"api_type": "GRPC",
										"grpc_services": []interface{}{
											map[string]interface{}{
												"envoy_grpc": map[string]interface{}{
													"cluster_name": "sds-grpc",
												},
											},
										},
									},
								},
							},
						},
						"validation_context_sds_secret_config": map[string]interface{}{
							"name": "ROOTCA",
							"sds_config": map[string]interface{}{
								"api_config_source": map[string]interface{}{
									"api_type": "GRPC",
									"grpc_services": []interface{}{
										map[string]interface{}{
											"envoy_grpc": map[string]interface{}{
												"cluster_name": "sds-grpc",
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
	})

	Context("when tracing is not configured", func() {
		BeforeEach(func(ctx SpecContext) {
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			original := kuadrantObj.DeepCopy()
			kuadrantObj.Spec.Observability.Tracing = nil
			Expect(testClient().Patch(ctx, kuadrantObj, client.MergeFrom(original))).To(Succeed())
		})

		It("EnvoyFilter is not created", func(ctx SpecContext) {
			// Check envoy filter has not been created
			Eventually(func(g Gomega, ctx context.Context) {
				existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
				efKey := client.ObjectKey{Name: controllers.TracingClusterName(TestGatewayName), Namespace: testNamespace}
				err := testClient().Get(ctx, efKey, existingEF)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when tracing is configured but no policy exists", func() {
		BeforeEach(func(ctx SpecContext) {
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			original := kuadrantObj.DeepCopy()
			kuadrantObj.Spec.Observability.Tracing = &kuadrantv1beta1.Tracing{
				DefaultEndpoint: "http://jaeger-collector.observability.svc:14268/api/traces",
				Insecure:        true,
			}
			Expect(testClient().Patch(ctx, kuadrantObj, client.MergeFrom(original))).To(Succeed())
		})

		It("EnvoyFilter is not created", func(ctx SpecContext) {
			// Check envoy filter has not been created even though tracing is configured
			Consistently(func(g Gomega, ctx context.Context) {
				existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
				efKey := client.ObjectKey{Name: controllers.TracingClusterName(TestGatewayName), Namespace: testNamespace}
				err := testClient().Get(ctx, efKey, existingEF)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when tracing is removed", func() {
		BeforeEach(func(ctx SpecContext) {
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			original := kuadrantObj.DeepCopy()
			kuadrantObj.Spec.Observability.Tracing = &kuadrantv1beta1.Tracing{
				DefaultEndpoint: "http://jaeger:14268",
				Insecure:        true,
			}
			Expect(testClient().Patch(ctx, kuadrantObj, client.MergeFrom(original))).To(Succeed())
		})

		It("EnvoyFilter is deleted when tracing config is removed", func(ctx SpecContext) {
			createAuthPolicy(ctx)

			// Verify EnvoyFilter is created
			efKey := client.ObjectKey{Name: controllers.TracingClusterName(TestGatewayName), Namespace: testNamespace}
			existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
			Eventually(func(g Gomega, ctx context.Context) {
				g.Expect(testClient().Get(ctx, efKey, existingEF)).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// Remove tracing configuration
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			original := kuadrantObj.DeepCopy()
			kuadrantObj.Spec.Observability.Tracing = nil
			Expect(testClient().Patch(ctx, kuadrantObj, client.MergeFrom(original))).To(Succeed())

			// Verify EnvoyFilter is deleted
			Eventually(func(g Gomega, ctx context.Context) {
				err := testClient().Get(ctx, efKey, existingEF)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
