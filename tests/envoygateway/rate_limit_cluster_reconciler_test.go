//go:build integration

package envoygateway_test

import (
	"context"
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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/controllers"
	"github.com/kuadrant/kuadrant-operator/pkg/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("limitador cluster controller", func() {
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
		err := testClient().Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	policyFactory := func(mutateFns ...func(policy *kuadrantv1.RateLimitPolicy)) *kuadrantv1.RateLimitPolicy {
		policy := &kuadrantv1.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RateLimitPolicy",
				APIVersion: kuadrantv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rlp",
				Namespace: testNamespace,
			},
			Spec: kuadrantv1.RateLimitPolicySpec{},
		}

		for _, mutateFn := range mutateFns {
			mutateFn(policy)
		}

		return policy
	}

	randomHostFromGWHost := func() string {
		return strings.Replace(gwHost, "*", rand.String(4), 1)
	}

	getKuadrantNamespace := func(ctx context.Context, cl client.Client) string {
		kuadrantList := &kuadrantv1beta1.KuadrantList{}
		err := cl.List(ctx, kuadrantList)
		// must exist
		Expect(err).ToNot(HaveOccurred())
		Expect(kuadrantList.Items).To(HaveLen(1))
		return kuadrantList.Items[0].Namespace
	}

	Context("RateLimitPolicy attached to the gateway", func() {

		var (
			gwPolicy *kuadrantv1.RateLimitPolicy
			gwRoute  *gatewayapiv1.HTTPRoute
		)

		BeforeEach(func(ctx SpecContext) {
			gwRoute = tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err := testClient().Create(ctx, gwRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(gwRoute))).WithContext(ctx).Should(BeTrue())

			gwPolicy = policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Name = "gw"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = TestGatewayName
				policy.Spec.Defaults = &kuadrantv1.MergeableRateLimitPolicySpec{
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{
							"l1": {
								Rates: []kuadrantv1.Rate{
									{
										Limit: 1, Window: kuadrantv1.Duration("3m"),
									},
								},
							},
						},
					},
				}
			})

			gwPolicyKey := client.ObjectKeyFromObject(gwPolicy)

			err = testClient().Create(ctx, gwPolicy)
			logf.Log.V(1).Info("Creating RateLimitPolicy", "key", gwPolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsRLPAcceptedAndEnforced).
				WithContext(ctx).
				WithArguments(testClient(), gwPolicyKey).Should(Succeed())
		})

		It("Creates envoypatchpolicy for limitador cluster", func(ctx SpecContext) {
			patchKey := client.ObjectKey{
				Name:      controllers.RateLimitClusterName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(IsEnvoyPatchPolicyAccepted).
				WithContext(ctx).
				WithArguments(testClient(), patchKey, client.ObjectKeyFromObject(gateway)).
				Should(Succeed())

			patch := &egv1alpha1.EnvoyPatchPolicy{}
			err := testClient().Get(ctx, patchKey, patch)
			// must exist
			Expect(err).ToNot(HaveOccurred())

			Expect(patch.Spec.TargetRef.Group).To(Equal(gatewayapiv1.Group("gateway.networking.k8s.io")))
			Expect(patch.Spec.TargetRef.Kind).To(Equal(gatewayapiv1.Kind("Gateway")))
			Expect(patch.Spec.TargetRef.Name).To(Equal(gatewayapiv1.ObjectName(gateway.Name)))
			Expect(patch.Spec.Type).To(Equal(egv1alpha1.JSONPatchEnvoyPatchType))
			Expect(patch.Spec.JSONPatches).To(HaveLen(1))
			Expect(patch.Spec.JSONPatches[0].Type).To(Equal(egv1alpha1.ClusterEnvoyResourceType))
			Expect(patch.Spec.JSONPatches[0].Name).To(Equal(kuadrant.KuadrantRateLimitClusterName))
			Expect(patch.Spec.JSONPatches[0].Operation.Op).To(Equal(egv1alpha1.JSONPatchOperationType("add")))

			// Check patch value
			patchValueBytes, err := patch.Spec.JSONPatches[0].Operation.Value.MarshalJSON()
			Expect(err).ToNot(HaveOccurred())
			var existingPatchValue map[string]any
			err = json.Unmarshal(patchValueBytes, &existingPatchValue)
			Expect(err).ToNot(HaveOccurred())

			kuadrantNs := getKuadrantNamespace(ctx, testClient())
			expectedLimitadorSvcHost := fmt.Sprintf("limitador-limitador.%s.svc.cluster.local", kuadrantNs)
			var expectedLimitadorGRPCPort float64 = 8081

			Expect(existingPatchValue).To(Equal(
				map[string]any{
					"name":                   kuadrant.KuadrantRateLimitClusterName,
					"type":                   "STRICT_DNS",
					"connect_timeout":        "1s",
					"lb_policy":              "ROUND_ROBIN",
					"http2_protocol_options": map[string]any{},
					"load_assignment": map[string]any{
						"cluster_name": kuadrant.KuadrantRateLimitClusterName,
						"endpoints": []any{
							map[string]any{
								"lb_endpoints": []any{
									map[string]any{
										"endpoint": map[string]any{
											"address": map[string]any{
												"socket_address": map[string]any{
													"address":    expectedLimitadorSvcHost,
													"port_value": expectedLimitadorGRPCPort,
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

		It("Deletes envoypatchpolicy when rate limit policy is deleted", func(ctx SpecContext) {
			gwPolicyKey := client.ObjectKeyFromObject(gwPolicy)
			err := testClient().Delete(ctx, gwPolicy)
			logf.Log.V(1).Info("Deleting RateLimitPolicy", "key", gwPolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			patchKey := client.ObjectKey{
				Name:      controllers.RateLimitClusterName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(func() bool {
				err := testClient().Get(ctx, patchKey, &egv1alpha1.EnvoyPatchPolicy{})
				logf.Log.V(1).Info("Fetching EnvoyPatchPolicy", "key", patchKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Deletes envoypatchpolicy if gateway is deleted", func(ctx SpecContext) {
			err := testClient().Delete(ctx, gateway)
			logf.Log.V(1).Info("Deleting Gateway", "key", client.ObjectKeyFromObject(gateway).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			patchKey := client.ObjectKey{
				Name:      controllers.RateLimitClusterName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(func() bool {
				err := testClient().Get(ctx, patchKey, &egv1alpha1.EnvoyPatchPolicy{})
				logf.Log.V(1).Info("Fetching EnvoyPatchPolicy", "key", patchKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})
})
