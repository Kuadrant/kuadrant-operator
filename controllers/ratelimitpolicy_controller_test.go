//go:build integration

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
)

func testBuildBasicGateway(gwName, ns string) *gatewayapiv1beta1.Gateway {
	return &gatewayapiv1beta1.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: gatewayapiv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        gwName,
			Namespace:   ns,
			Labels:      map[string]string{"app": "rlptest"},
			Annotations: map[string]string{"networking.istio.io/service-type": string(corev1.ServiceTypeClusterIP)},
		},
		Spec: gatewayapiv1beta1.GatewaySpec{
			GatewayClassName: gatewayapiv1beta1.ObjectName("istio"),
			Listeners: []gatewayapiv1beta1.Listener{
				{
					Name:     gatewayapiv1beta1.SectionName("default"),
					Port:     gatewayapiv1beta1.PortNumber(80),
					Protocol: gatewayapiv1beta1.ProtocolType("HTTP"),
				},
			},
		},
	}
}

func testBuildBasicHttpRoute(routeName, gwName, ns string, hostnamesStrSlice []string) *gatewayapiv1beta1.HTTPRoute {
	tmpMatchPathPrefix := gatewayapiv1beta1.PathMatchPathPrefix
	tmpMatchValue := "/toy"
	tmpMatchMethod := gatewayapiv1beta1.HTTPMethod("GET")
	gwNamespace := gatewayapiv1beta1.Namespace(ns)

	var hostnames []gatewayapiv1beta1.Hostname
	for _, str := range hostnamesStrSlice {
		hostnames = append(hostnames, gatewayapiv1beta1.Hostname(str))
	}

	return &gatewayapiv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: gatewayapiv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: ns,
			Labels:    map[string]string{"app": "rlptest"},
		},
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1beta1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1beta1.ParentReference{
					{
						Name:      gatewayapiv1beta1.ObjectName(gwName),
						Namespace: &gwNamespace,
					},
				},
			},
			Hostnames: hostnames,
			Rules: []gatewayapiv1beta1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1beta1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1beta1.HTTPPathMatch{
								Type:  &tmpMatchPathPrefix,
								Value: &tmpMatchValue,
							},
							Method: &tmpMatchMethod,
						},
					},
				},
			},
		},
	}
}

func testBuildBasicRoutePolicy(policyName, ns, routeName string) *kuadrantv1beta2.RateLimitPolicy {
	return &kuadrantv1beta2.RateLimitPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RateLimitPolicy",
			APIVersion: kuadrantv1beta2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyName,
			Namespace: ns,
		},
		Spec: kuadrantv1beta2.RateLimitPolicySpec{
			TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
				Group: gatewayapiv1beta1.Group("gateway.networking.k8s.io"),
				Kind:  "HTTPRoute",
				Name:  gatewayapiv1beta1.ObjectName(routeName),
			},
			Limits: map[string]kuadrantv1beta2.Limit{
				"l1": {
					Rates: []kuadrantv1beta2.Rate{
						{
							Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
						},
					},
				},
			},
		},
	}
}

func testBuildGatewayPolicy(policyName, ns, gwName string) *kuadrantv1beta2.RateLimitPolicy {
	return &kuadrantv1beta2.RateLimitPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RateLimitPolicy",
			APIVersion: kuadrantv1beta2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyName,
			Namespace: ns,
		},
		Spec: kuadrantv1beta2.RateLimitPolicySpec{
			TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
				Group: gatewayapiv1beta1.Group("gateway.networking.k8s.io"),
				Kind:  "Gateway",
				Name:  gatewayapiv1beta1.ObjectName(gwName),
			},
			Limits: map[string]kuadrantv1beta2.Limit{
				"l1": {
					Rates: []kuadrantv1beta2.Rate{
						{
							Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
						},
					},
				},
			},
		},
	}
}

var _ = Describe("RateLimitPolicy controller", func() {
	var (
		testNamespace string
		routeName     = "toystore-route"
		gwName        = "toystore-gw"
		rlpName       = "toystore-rlp"
		gateway       *gatewayapiv1beta1.Gateway
	)

	beforeEachCallback := func() {
		CreateNamespace(&testNamespace)
		gateway = testBuildBasicGateway(gwName, testNamespace)
		err := k8sClient.Create(context.Background(), gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() bool {
			existingGateway := &gatewayapiv1beta1.Gateway{}
			err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), existingGateway)
			if err != nil {
				logf.Log.V(1).Info("[WARN] Creating gateway failed", "error", err)
				return false
			}

			if meta.IsStatusConditionFalse(existingGateway.Status.Conditions, common.GatewayProgrammedConditionType) {
				logf.Log.V(1).Info("[WARN] Gateway not ready")
				return false
			}

			return true
		}, 15*time.Second, 5*time.Second).Should(BeTrue())

		ApplyKuadrantCR(testNamespace)
	}

	BeforeEach(beforeEachCallback)
	AfterEach(DeleteNamespaceCallback(&testNamespace))

	Context("Basic: RLP targeting HTTPRoute", func() {
		It("check created resources", func() {
			// Check Limitador Status is Ready
			Eventually(func() bool {
				limitador := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, limitador)
				if err != nil {
					return false
				}
				if !meta.IsStatusConditionTrue(limitador.Status.Conditions, "Ready") {
					return false
				}
				return true
			}, time.Minute, 5*time.Second).Should(BeTrue())

			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())

			rlp := testBuildBasicRoutePolicy(rlpName, testNamespace, routeName)
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			Eventually(func() bool {
				existingRLP := &kuadrantv1beta2.RateLimitPolicy{}
				err := k8sClient.Get(context.Background(), rlpKey, existingRLP)
				if err != nil {
					return false
				}
				if !meta.IsStatusConditionTrue(existingRLP.Status.Conditions, "Available") {
					return false
				}

				return true
			}, time.Minute, 5*time.Second).Should(BeTrue())

			// Check HTTPRoute direct back reference
			routeKey := client.ObjectKey{Name: routeName, Namespace: testNamespace}
			existingRoute := &gatewayapiv1beta1.HTTPRoute{}
			err = k8sClient.Get(context.Background(), routeKey, existingRoute)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			Expect(existingRoute.GetAnnotations()).To(HaveKeyWithValue(
				common.RateLimitPolicyBackRefAnnotation, client.ObjectKeyFromObject(rlp).String()))

			// check limits
			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
			existingLimitador := &limitadorv1alpha1.Limitador{}
			err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    3 * 60,
				Namespace:  common.MarshallNamespace(client.ObjectKeyFromObject(gateway), "*.example.com"),
				Conditions: []string{fmt.Sprintf("%s/%s/l1 == \"1\"", testNamespace, rlpName)},
				Variables:  []string{},
			}))

			// Check envoy filter
			efName := fmt.Sprintf("kuadrant-ratelimiting-cluster-%s", gwName)
			efKey := client.ObjectKey{Name: efName, Namespace: testNamespace}
			existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
			err = k8sClient.Get(context.Background(), efKey, existingEF)
			// must exist
			Expect(err).ToNot(HaveOccurred())

			// Check wasm plugin
			wpName := fmt.Sprintf("kuadrant-%s", gwName)
			wasmPluginKey := client.ObjectKey{Name: wpName, Namespace: testNamespace}
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			err = k8sClient.Get(context.Background(), wasmPluginKey, existingWasmPlugin)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			existingWASMConfig, err := rlptools.WASMPluginFromStruct(existingWasmPlugin.Spec.PluginConfig)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig).To(Equal(&wasm.WASMPlugin{
				FailureMode: wasm.FailureModeDeny,
				RateLimitPolicies: []wasm.RateLimitPolicy{
					{
						Name:      "*.example.com",
						Domain:    common.MarshallNamespace(client.ObjectKeyFromObject(gateway), "*.example.com"),
						Service:   common.KuadrantRateLimitClusterName,
						Hostnames: []string{"*.example.com"},
						Rules: []wasm.Rule{
							{
								Conditions: []wasm.Condition{
									{
										AllOf: []wasm.PatternExpression{
											{
												Selector: "request.url_path",
												Operator: wasm.PatternOperator(kuadrantv1beta2.StartsWithOperator),
												Value:    "/toy",
											},
											{
												Selector: "request.method",
												Operator: wasm.PatternOperator(kuadrantv1beta2.EqualOperator),
												Value:    "GET",
											},
											{
												Selector: "request.host",
												Operator: wasm.PatternOperator(kuadrantv1beta2.EqualOperator),
												Value:    "*.example.com",
											},
										},
									},
								},
								Data: []wasm.DataItem{
									{
										Static: &wasm.StaticSpec{
											Key:   fmt.Sprintf("%s/%s/l1", testNamespace, rlpName),
											Value: "1",
										},
									},
								},
							},
						},
					},
				},
			}))

			// Check gateway back references
			gwKey := client.ObjectKey{Name: gwName, Namespace: testNamespace}
			existingGateway := &gatewayapiv1beta1.Gateway{}
			err = k8sClient.Get(context.Background(), gwKey, existingGateway)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			refs := []client.ObjectKey{rlpKey}
			serialized, err := json.Marshal(refs)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
				common.RateLimitPoliciesBackRefAnnotation, string(serialized)))
		})
	})

	Context("Basic: RLP targeting Gateway", func() {
		It("check created resources", func() {
			// Check Limitador Status is Ready
			Eventually(func() bool {
				limitador := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, limitador)
				if err != nil {
					return false
				}
				if !meta.IsStatusConditionTrue(limitador.Status.Conditions, "Ready") {
					return false
				}
				return true
			}, time.Minute, 5*time.Second).Should(BeTrue())

			rlp := testBuildGatewayPolicy(rlpName, testNamespace, gwName)
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			err := k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			Eventually(func() bool {
				existingRLP := &kuadrantv1beta2.RateLimitPolicy{}
				err := k8sClient.Get(context.Background(), rlpKey, existingRLP)
				if err != nil {
					return false
				}
				if !meta.IsStatusConditionTrue(existingRLP.Status.Conditions, "Available") {
					return false
				}

				return true
			}, time.Minute, 5*time.Second).Should(BeTrue())

			// Check Gateway direct back reference
			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1beta1.Gateway{}
			err = k8sClient.Get(context.Background(), gwKey, existingGateway)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
				common.RateLimitPolicyBackRefAnnotation, client.ObjectKeyFromObject(rlp).String()))

			// check limits
			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}
			existingLimitador := &limitadorv1alpha1.Limitador{}
			err = k8sClient.Get(context.Background(), limitadorKey, existingLimitador)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			Expect(existingLimitador.Spec.Limits).To(ContainElements(limitadorv1alpha1.RateLimit{
				MaxValue:   1,
				Seconds:    3 * 60,
				Namespace:  common.MarshallNamespace(client.ObjectKeyFromObject(gateway), "*"),
				Conditions: []string{fmt.Sprintf("%s/%s/l1 == \"1\"", testNamespace, rlpName)},
				Variables:  []string{},
			}))

			// Check envoy filter
			efName := fmt.Sprintf("kuadrant-ratelimiting-cluster-%s", gwName)
			efKey := client.ObjectKey{Name: efName, Namespace: testNamespace}
			existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
			err = k8sClient.Get(context.Background(), efKey, existingEF)
			// must exist
			Expect(err).ToNot(HaveOccurred())

			// Check wasm plugin
			wpName := fmt.Sprintf("kuadrant-%s", gwName)
			wasmPluginKey := client.ObjectKey{Name: wpName, Namespace: testNamespace}
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			err = k8sClient.Get(context.Background(), wasmPluginKey, existingWasmPlugin)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			existingWASMConfig, err := rlptools.WASMPluginFromStruct(existingWasmPlugin.Spec.PluginConfig)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig).To(Equal(&wasm.WASMPlugin{
				FailureMode: wasm.FailureModeDeny,
				RateLimitPolicies: []wasm.RateLimitPolicy{
					{
						Name:      "*",
						Domain:    common.MarshallNamespace(client.ObjectKeyFromObject(gateway), "*"),
						Service:   common.KuadrantRateLimitClusterName,
						Hostnames: []string{"*"},
						Rules: []wasm.Rule{
							{
								Conditions: []wasm.Condition{
									{
										AllOf: []wasm.PatternExpression{
											{
												Selector: "request.host",
												Operator: wasm.PatternOperator(kuadrantv1beta2.EqualOperator),
												Value:    "*",
											},
										},
									},
								},
								Data: []wasm.DataItem{
									{
										Static: &wasm.StaticSpec{
											Key:   fmt.Sprintf("%s/%s/l1", testNamespace, rlpName),
											Value: "1",
										},
									},
								},
							},
						},
					},
				},
			}))

			// Check gateway back references
			err = k8sClient.Get(context.Background(), gwKey, existingGateway)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			refs := []client.ObjectKey{rlpKey}
			serialized, err := json.Marshal(refs)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
				common.RateLimitPoliciesBackRefAnnotation, string(serialized)))
		})
	})
})
