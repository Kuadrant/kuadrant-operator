//go:build integration

package envoygateway_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("tracing cluster controller", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gwHost        = fmt.Sprintf("*.toystore-%s.com", rand.String(4))
		gateway       *gatewayapiv1.Gateway
	)

	randomHostFromGWHost := func() string {
		return strings.Replace(gwHost, "*", rand.String(4), 1)
	}

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
		Expect(testClient().Create(ctx, policy)).To(Succeed())
	}

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway = tests.NewGatewayBuilder(TestGatewayName, tests.GatewayClassName, testNamespace).
			WithHTTPListener("test-listener", gwHost).
			Gateway
		err := testClient().Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

		gwRoute := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
		err = testClient().Create(ctx, gwRoute)
		Expect(err).ToNot(HaveOccurred())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(gwRoute))).WithContext(ctx).Should(BeTrue())
	})

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
		It("Creates envoypatchpolicy for tracing cluster with insecure connection when policy exists", func(ctx SpecContext) {
			createAuthPolicy(ctx)

			// Configure tracing with insecure connection
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			original := kuadrantObj.DeepCopy()
			kuadrantObj.Spec.Observability.Tracing = &kuadrantv1beta1.Tracing{
				DefaultEndpoint: "http://jaeger-collector.observability.svc:14268/api/traces",
				Insecure:        true,
			}
			Expect(testClient().Patch(ctx, kuadrantObj, client.MergeFrom(original))).To(Succeed())

			patchKey := client.ObjectKey{
				Name:      controllers.TracingClusterName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(IsEnvoyPatchPolicyAccepted).
				WithContext(ctx).
				WithArguments(testClient(), patchKey, client.ObjectKeyFromObject(gateway)).
				Should(Succeed())

			envoyPatch := &egv1alpha1.EnvoyPatchPolicy{}
			err := testClient().Get(ctx, patchKey, envoyPatch)
			Expect(err).ToNot(HaveOccurred())

			Expect(envoyPatch.Spec.TargetRef.Group).To(Equal(gatewayapiv1.Group("gateway.networking.k8s.io")))
			Expect(envoyPatch.Spec.TargetRef.Kind).To(Equal(gatewayapiv1.Kind("Gateway")))
			Expect(envoyPatch.Spec.TargetRef.Name).To(Equal(gatewayapiv1.ObjectName(TestGatewayName)))

			// Verify patch contains cluster configuration
			Expect(envoyPatch.Spec.JSONPatches).NotTo(BeEmpty())
			Expect(envoyPatch.Spec.JSONPatches[0].Type).To(Equal(egv1alpha1.EnvoyResourceType("type.googleapis.com/envoy.config.cluster.v3.Cluster")))
			Expect(envoyPatch.Spec.JSONPatches[0].Operation.Op).To(Equal(egv1alpha1.JSONPatchOperationType("add")))

			// Verify cluster details
			var clusterConfig map[string]interface{}
			err = json.Unmarshal(envoyPatch.Spec.JSONPatches[0].Operation.Value.Raw, &clusterConfig)
			Expect(err).ToNot(HaveOccurred())
			Expect(clusterConfig["name"]).To(Equal(kuadrant.KuadrantTracingClusterName))

			// Verify endpoint configuration
			loadAssignment := clusterConfig["load_assignment"].(map[string]interface{})
			endpoints := loadAssignment["endpoints"].([]interface{})[0].(map[string]interface{})
			lbEndpoints := endpoints["lb_endpoints"].([]interface{})[0].(map[string]interface{})
			endpoint := lbEndpoints["endpoint"].(map[string]interface{})
			address := endpoint["address"].(map[string]interface{})
			socketAddress := address["socket_address"].(map[string]interface{})
			Expect(socketAddress["address"]).To(Equal("jaeger-collector.observability.svc"))
			Expect(socketAddress["port_value"]).To(BeNumerically("==", 14268))

			// No mTLS when insecure: true
			Expect(clusterConfig).NotTo(HaveKey("transport_socket"))
		}, testTimeOut)

		It("Creates envoypatchpolicy for tracing cluster with mTLS when policy exists", func(ctx SpecContext) {
			createAuthPolicy(ctx)

			// Configure tracing with mTLS
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			original := kuadrantObj.DeepCopy()
			kuadrantObj.Spec.Observability.Tracing = &kuadrantv1beta1.Tracing{
				DefaultEndpoint: "https://secure-collector.observability.svc:443",
				Insecure:        false, // mTLS enabled
			}
			Expect(testClient().Patch(ctx, kuadrantObj, client.MergeFrom(original))).To(Succeed())

			patchKey := client.ObjectKey{
				Name:      controllers.TracingClusterName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(IsEnvoyPatchPolicyAccepted).
				WithContext(ctx).
				WithArguments(testClient(), patchKey, client.ObjectKeyFromObject(gateway)).
				Should(Succeed())

			envoyPatch := &egv1alpha1.EnvoyPatchPolicy{}
			err := testClient().Get(ctx, patchKey, envoyPatch)
			Expect(err).ToNot(HaveOccurred())

			// Verify cluster details
			var clusterConfig map[string]interface{}
			err = json.Unmarshal(envoyPatch.Spec.JSONPatches[0].Operation.Value.Raw, &clusterConfig)
			Expect(err).ToNot(HaveOccurred())

			// Verify mTLS configuration is present
			Expect(clusterConfig).To(HaveKey("transport_socket"))
			transportSocket := clusterConfig["transport_socket"].(map[string]interface{})
			Expect(transportSocket["name"]).To(Equal("envoy.transport_sockets.tls"))

			typedConfig := transportSocket["typed_config"].(map[string]interface{})
			Expect(typedConfig["@type"]).To(Equal("type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext"))

			commonTLS := typedConfig["common_tls_context"].(map[string]interface{})
			Expect(commonTLS).To(HaveKey("tls_certificate_sds_secret_configs"))
			Expect(commonTLS).To(HaveKey("validation_context_sds_secret_config"))
		}, testTimeOut)

		It("Does not create envoypatchpolicy when tracing is not configured", func(ctx SpecContext) {
			// Ensure tracing is not configured
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			original := kuadrantObj.DeepCopy()
			kuadrantObj.Spec.Observability.Tracing = nil
			Expect(testClient().Patch(ctx, kuadrantObj, client.MergeFrom(original))).To(Succeed())

			createAuthPolicy(ctx)

			patchKey := client.ObjectKey{
				Name:      controllers.TracingClusterName(TestGatewayName),
				Namespace: testNamespace,
			}

			// Verify patch is not created
			Consistently(func(g Gomega) {
				patch := &egv1alpha1.EnvoyPatchPolicy{}
				err := testClient().Get(ctx, patchKey, patch)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Does not create envoypatchpolicy when tracing is configured but no policy exists", func(ctx SpecContext) {
			// Configure tracing but don't create any policies
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			original := kuadrantObj.DeepCopy()
			kuadrantObj.Spec.Observability.Tracing = &kuadrantv1beta1.Tracing{
				DefaultEndpoint: "http://jaeger-collector.observability.svc:14268/api/traces",
				Insecure:        true,
			}
			Expect(testClient().Patch(ctx, kuadrantObj, client.MergeFrom(original))).To(Succeed())

			patchKey := client.ObjectKey{
				Name:      controllers.TracingClusterName(TestGatewayName),
				Namespace: testNamespace,
			}

			// Verify patch is not created even though tracing is configured
			Consistently(func(g Gomega) {
				patch := &egv1alpha1.EnvoyPatchPolicy{}
				err := testClient().Get(ctx, patchKey, patch)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Deletes envoypatchpolicy when tracing config is removed", func(ctx SpecContext) {
			createAuthPolicy(ctx)

			// First, configure tracing
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			original := kuadrantObj.DeepCopy()
			kuadrantObj.Spec.Observability.Tracing = &kuadrantv1beta1.Tracing{
				DefaultEndpoint: "http://jaeger:14268",
				Insecure:        true,
			}
			Expect(testClient().Patch(ctx, kuadrantObj, client.MergeFrom(original))).To(Succeed())

			patchKey := client.ObjectKey{
				Name:      controllers.TracingClusterName(TestGatewayName),
				Namespace: testNamespace,
			}

			// Verify patch is created
			Eventually(IsEnvoyPatchPolicyAccepted).
				WithContext(ctx).
				WithArguments(testClient(), patchKey, client.ObjectKeyFromObject(gateway)).
				Should(Succeed())

			// Remove tracing configuration
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			original = kuadrantObj.DeepCopy()
			kuadrantObj.Spec.Observability.Tracing = nil
			Expect(testClient().Patch(ctx, kuadrantObj, client.MergeFrom(original))).To(Succeed())

			// Verify patch is deleted
			Eventually(func(g Gomega) {
				patch := &egv1alpha1.EnvoyPatchPolicy{}
				err := testClient().Get(ctx, patchKey, patch)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
