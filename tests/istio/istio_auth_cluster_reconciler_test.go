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

var _ = Describe("Authorino Cluster EnvoyFilter controller", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		apName        = "toystore-authpolicy"
	)

	beforeEachCallback := func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway := tests.BuildBasicGateway(TestGatewayName, testNamespace)
		err := testClient().Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

		Eventually(tests.AuthorinoIsReady(testClient(), client.ObjectKey{
			Name:      "authorino",
			Namespace: kuadrantInstallationNS,
		})).WithContext(ctx).Should(Succeed())
	}

	BeforeEach(beforeEachCallback)
	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("AuthPolicy targeting Gateway", func() {

		// kuadrant mTLS is off
		BeforeEach(func(ctx SpecContext) {
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, kuadrantKey, kuadrantObj)).To(Succeed())
				kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: false}
				g.Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())
		})

		It("EnvoyFilter only created if KAP is in the path to a route", func(ctx SpecContext) {
			// create authpolicy
			authPolicy := &kuadrantv1.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: kuadrantv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      apName,
					Namespace: testNamespace,
				},
				Spec: kuadrantv1.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					Defaults: &kuadrantv1.MergeableAuthPolicySpec{
						AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
							AuthScheme: tests.BuildBasicAuthScheme(),
						},
					},
				},
			}
			Expect(testClient().Create(ctx, authPolicy)).ToNot(HaveOccurred())
			// Check policy status is available
			policyKey := client.ObjectKey{Name: apName, Namespace: testNamespace}
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), authPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), authPolicy)).WithContext(ctx).Should(BeFalse())
			Eventually(tests.IsAuthPolicyEnforcedCondition(ctx, testClient(), policyKey, kuadrant.PolicyReasonUnknown, "AuthPolicy is not in the path to any existing routes")).WithContext(ctx).Should(BeTrue())

			// Check envoy filter has not been created
			Eventually(func(g Gomega, ctx context.Context) {
				existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
				efKey := client.ObjectKey{Name: controllers.AuthClusterName(TestGatewayName), Namespace: testNamespace}
				err := testClient().Get(ctx, efKey, existingEF)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())

			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.toystore.com"})
			Expect(k8sClient.Create(ctx, route)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			// Check envoy filter has been created
			existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
			Eventually(func(g Gomega, ctx context.Context) {
				efKey := client.ObjectKey{Name: controllers.AuthClusterName(TestGatewayName), Namespace: testNamespace}
				g.Expect(testClient().Get(ctx, efKey, existingEF)).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			//check envoy filter does not have transport configured with TLS
			Expect(existingEF.Spec.ConfigPatches).To(HaveLen(1))
			Expect(existingEF.Spec.ConfigPatches[0].Patch).NotTo(BeNil())
			Expect(existingEF.Spec.ConfigPatches[0].Patch.Value).NotTo(BeNil())
			// Need to marshal/unmarshal to assert on fields of the patch's value
			patchValueRaw, err := json.Marshal(existingEF.Spec.ConfigPatches[0].Patch.Value)
			Expect(err).ToNot(HaveOccurred())
			var patchValue map[string]any
			Expect(json.Unmarshal(patchValueRaw, &patchValue)).ToNot(HaveOccurred())
			Expect(patchValue).To(HaveKey("name"))
			// transport_socket config only added when mTLS is configured
			Expect(patchValue).NotTo(HaveKey("transport_socket"))

			err = testClient().Delete(ctx, authPolicy)
			Expect(err).ToNot(HaveOccurred())

			// Check envoy filter is gone
			Eventually(func() bool {
				existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
				efKey := client.ObjectKey{Name: controllers.AuthClusterName(TestGatewayName), Namespace: testNamespace}
				err = testClient().Get(ctx, efKey, existingEF)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("when mTLS is on", func() {

		BeforeEach(func(ctx SpecContext) {
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, kuadrantKey, kuadrantObj)).To(Succeed())
				kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: true}
				g.Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())
		})

		It("envoy filter has transport configured with TLS", func(ctx SpecContext) {
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.toystore.com"})
			Expect(k8sClient.Create(ctx, route)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			// create authpolicy
			authPolicy := &kuadrantv1.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: kuadrantv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      apName,
					Namespace: testNamespace,
				},
				Spec: kuadrantv1.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					Defaults: &kuadrantv1.MergeableAuthPolicySpec{
						AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
							AuthScheme: tests.BuildBasicAuthScheme(),
						},
					},
				},
			}
			Expect(testClient().Create(ctx, authPolicy)).ToNot(HaveOccurred())
			// Check policy status is available
			Eventually(tests.IsAuthPolicyAccepted(ctx, testClient(), authPolicy)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.IsAuthPolicyEnforced(ctx, testClient(), authPolicy)).WithContext(ctx).Should(BeTrue())

			existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
			Eventually(func(g Gomega, ctx context.Context) {
				efKey := client.ObjectKey{Name: controllers.AuthClusterName(TestGatewayName), Namespace: testNamespace}
				g.Expect(testClient().Get(ctx, efKey, existingEF)).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			Expect(existingEF.Spec.ConfigPatches).To(HaveLen(1))
			Expect(existingEF.Spec.ConfigPatches[0].Patch).NotTo(BeNil())
			Expect(existingEF.Spec.ConfigPatches[0].Patch.Value).NotTo(BeNil())
			// Need to marshal/unmarshal to assert on fields of the patch's value
			patchValueRaw, err := json.Marshal(existingEF.Spec.ConfigPatches[0].Patch.Value)
			Expect(err).ToNot(HaveOccurred())
			var patchValue map[string]any
			Expect(json.Unmarshal(patchValueRaw, &patchValue)).ToNot(HaveOccurred())
			Expect(patchValue).To(HaveKey("name"))
			// transport_socket config only added when mTLS is configured
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
})
