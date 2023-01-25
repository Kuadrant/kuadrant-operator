//go:build integration

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
)

func testBuildBasicGateway(gwName, ns string) *gatewayapiv1alpha2.Gateway {
	return &gatewayapiv1alpha2.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: gatewayapiv1alpha2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      gwName,
			Namespace: ns,
			Labels:    map[string]string{"app": "rlptest"},
		},
		Spec: gatewayapiv1alpha2.GatewaySpec{
			GatewayClassName: gatewayapiv1alpha2.ObjectName("istio"),
			Listeners: []gatewayapiv1alpha2.Listener{
				{
					Name:     gatewayapiv1alpha2.SectionName("default"),
					Port:     gatewayapiv1alpha2.PortNumber(80),
					Protocol: gatewayapiv1alpha2.ProtocolType("HTTP"),
				},
			},
		},
	}
}

func testBuildBasicHttpRoute(routeName, gwName, ns string, hostnamesStrSlice []string) *gatewayapiv1alpha2.HTTPRoute {
	tmpMatchPathPrefix := gatewayapiv1alpha2.PathMatchPathPrefix
	tmpMatchValue := "/toy"
	tmpMatchMethod := gatewayapiv1alpha2.HTTPMethod("GET")
	gwNamespace := gatewayapiv1alpha2.Namespace(ns)

	var hostnames []gatewayapiv1alpha2.Hostname
	for _, str := range hostnamesStrSlice {
		hostnames = append(hostnames, gatewayapiv1alpha2.Hostname(str))
	}

	return &gatewayapiv1alpha2.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: gatewayapiv1alpha2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: ns,
			Labels:    map[string]string{"app": "rlptest"},
		},
		Spec: gatewayapiv1alpha2.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1alpha2.CommonRouteSpec{
				ParentRefs: []gatewayapiv1alpha2.ParentReference{
					{
						Name:      gatewayapiv1alpha2.ObjectName(gwName),
						Namespace: &gwNamespace,
					},
				},
			},
			Hostnames: hostnames,
			Rules: []gatewayapiv1alpha2.HTTPRouteRule{
				{
					Matches: []gatewayapiv1alpha2.HTTPRouteMatch{
						{
							Path: &gatewayapiv1alpha2.HTTPPathMatch{
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

func testBuildBasicRoutePolicy(policyName, ns, routeName string) *kuadrantv1beta1.RateLimitPolicy {
	genericDescriptorKey := "op"

	return &kuadrantv1beta1.RateLimitPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RateLimitPolicy",
			APIVersion: kuadrantv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyName,
			Namespace: ns,
		},
		Spec: kuadrantv1beta1.RateLimitPolicySpec{
			TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
				Group: gatewayapiv1alpha2.Group("gateway.networking.k8s.io"),
				Kind:  "HTTPRoute",
				Name:  gatewayapiv1alpha2.ObjectName(routeName),
			},
			RateLimits: []kuadrantv1beta1.RateLimit{
				{
					Configurations: []kuadrantv1beta1.Configuration{
						{
							Actions: []kuadrantv1beta1.ActionSpecifier{
								{
									GenericKey: &kuadrantv1beta1.GenericKeySpec{
										DescriptorValue: "1",
										DescriptorKey:   &genericDescriptorKey,
									},
								},
							},
						},
					},
					Limits: []kuadrantv1beta1.Limit{
						{
							MaxValue:   5,
							Seconds:    10,
							Conditions: []string{"op == 1"},
							Variables:  []string{},
						},
					},
				},
			},
		},
	}
}

var _ = Describe("RateLimitPolicy controller", func() {
	var (
		testNamespace        string
		genericDescriptorKey string = "op"
		routeName                   = "toystore-route"
		gwName                      = "toystore-gw"
		rlpName                     = "toystore-rlp"
		gateway              *gatewayapiv1alpha2.Gateway
	)

	beforeEachCallback := func() {
		CreateNamespace(&testNamespace)
		gateway = testBuildBasicGateway(gwName, testNamespace)
		err := k8sClient.Create(context.Background(), gateway)
		Expect(err).ToNot(HaveOccurred())
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
				existingRLP := &kuadrantv1beta1.RateLimitPolicy{}
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
			existingRoute := &gatewayapiv1alpha2.HTTPRoute{}
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
				MaxValue:   5,
				Seconds:    10,
				Namespace:  common.MarshallNamespace(client.ObjectKeyFromObject(gateway), "*.example.com"),
				Conditions: []string{"op == 1"},
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
			Expect(existingWASMConfig).To(Equal(&rlptools.WASMPlugin{
				FailureModeDeny: true,
				RateLimitPolicies: []rlptools.RateLimitPolicy{
					{
						Name:            "*.example.com",
						RateLimitDomain: common.MarshallNamespace(client.ObjectKeyFromObject(gateway), "*.example.com"),
						UpstreamCluster: common.KuadrantRateLimitClusterName,
						Hostnames:       []string{"*.example.com"},
						GatewayActions: []rlptools.GatewayAction{
							{
								Rules: []kuadrantv1beta1.Rule{
									{
										Hosts:   []string{"*.example.com"},
										Paths:   []string{"/toy*"},
										Methods: []string{"GET"},
									},
								},
								Configurations: []kuadrantv1beta1.Configuration{
									{
										Actions: []kuadrantv1beta1.ActionSpecifier{
											{
												GenericKey: &kuadrantv1beta1.GenericKeySpec{
													DescriptorValue: "1",
													DescriptorKey:   &genericDescriptorKey,
												},
											},
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
			existingGateway := &gatewayapiv1alpha2.Gateway{}
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
