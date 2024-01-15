//go:build integration

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

var _ = Describe("RateLimitPolicy controller", func() {
	var (
		testNamespace string
		routeName     = "toystore-route"
		gwName        = "toystore-gw"
		rlpName       = "toystore-rlp"
		gateway       *gatewayapiv1.Gateway
	)

	policyFactory := func(mutateFns ...func(policy *kuadrantv1beta2.RateLimitPolicy)) *kuadrantv1beta2.RateLimitPolicy {
		policy := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RateLimitPolicy",
				APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      rlpName,
				Namespace: testNamespace,
			},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.Group("gateway.networking.k8s.io"),
					Kind:  "HTTPRoute",
					Name:  gatewayapiv1.ObjectName(routeName),
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
		for _, mutateFn := range mutateFns {
			mutateFn(policy)
		}

		return policy
	}

	beforeEachCallback := func() {
		CreateNamespace(&testNamespace)
		gateway = testBuildBasicGateway(gwName, testNamespace)
		err := k8sClient.Create(context.Background(), gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() bool {
			existingGateway := &gatewayapiv1.Gateway{}
			err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), existingGateway)
			if err != nil {
				logf.Log.V(1).Info("[WARN] Creating gateway failed", "error", err)
				return false
			}

			if meta.IsStatusConditionFalse(existingGateway.Status.Conditions, string(gatewayapiv1.GatewayConditionProgrammed)) {
				logf.Log.V(1).Info("[WARN] Gateway not ready")
				return false
			}

			return true
		}, 15*time.Second, 5*time.Second).Should(BeTrue())

		ApplyKuadrantCR(testNamespace)

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
	}

	BeforeEach(beforeEachCallback)
	AfterEach(DeleteNamespaceCallback(&testNamespace))

	Context("RLP targeting HTTPRoute", func() {
		It("Creates all the resources for a basic HTTPRoute and RateLimitPolicy", func() {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory()
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check HTTPRoute direct back reference
			routeKey := client.ObjectKey{Name: routeName, Namespace: testNamespace}
			existingRoute := &gatewayapiv1.HTTPRoute{}
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
				Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
				Conditions: []string{`limit.l1__2804bad6 == "1"`},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(rlp),
			}))

			// Check wasm plugin
			wasmPluginKey := client.ObjectKey{Name: rlptools.WASMPluginName(gateway), Namespace: testNamespace}
			Eventually(testWasmPluginIsAvailable(wasmPluginKey), time.Minute, 5*time.Second).Should(BeTrue())
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			err = k8sClient.Get(context.Background(), wasmPluginKey, existingWasmPlugin)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			existingWASMConfig, err := rlptools.WASMPluginFromStruct(existingWasmPlugin.Spec.PluginConfig)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig).To(Equal(&wasm.Plugin{
				FailureMode: wasm.FailureModeDeny,
				RateLimitPolicies: []wasm.RateLimitPolicy{
					{
						Name:   rlpKey.String(),
						Domain: rlptools.LimitsNamespaceFromRLP(rlp),
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
										},
									},
								},
								Data: []wasm.DataItem{
									{
										Static: &wasm.StaticSpec{
											Key:   `limit.l1__2804bad6`,
											Value: "1",
										},
									},
								},
							},
						},
						Hostnames: []string{"*.example.com"},
						Service:   common.KuadrantRateLimitClusterName,
					},
				},
			}))

			// Check gateway back references
			gwKey := client.ObjectKey{Name: gwName, Namespace: testNamespace}
			existingGateway := &gatewayapiv1.Gateway{}
			err = k8sClient.Get(context.Background(), gwKey, existingGateway)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			refs := []client.ObjectKey{rlpKey}
			serialized, err := json.Marshal(refs)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(
				common.RateLimitPoliciesBackRefAnnotation, string(serialized)))
		})

		It("Creates the correct WasmPlugin for a complex HTTPRoute and a RateLimitPolicy", func() {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.toystore.acme.com", "api.toystore.io"})
			httpRoute.Spec.Rules = []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{ // get /toys*
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
								Value: ptr.To("/toys"),
							},
							Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
						},
						{ // post /toys*
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
								Value: ptr.To("/toys"),
							},
							Method: ptr.To(gatewayapiv1.HTTPMethod("POST")),
						},
					},
				},
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{ // /assets*
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
								Value: ptr.To("/assets"),
							},
						},
					},
				},
			}
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.Limits = map[string]kuadrantv1beta2.Limit{
					"toys": {
						Rates: []kuadrantv1beta2.Rate{
							{Limit: 50, Duration: 1, Unit: kuadrantv1beta2.TimeUnit("minute")},
						},
						Counters: []kuadrantv1beta2.ContextSelector{"auth.identity.username"},
						RouteSelectors: []kuadrantv1beta2.RouteSelector{
							{ // selects the 1st HTTPRouteRule (i.e. get|post /toys*) for one of the hostnames
								Matches: []gatewayapiv1.HTTPRouteMatch{
									{
										Path: &gatewayapiv1.HTTPPathMatch{
											Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
											Value: ptr.To("/toys"),
										},
									},
								},
								Hostnames: []gatewayapiv1.Hostname{"*.toystore.acme.com"},
							},
						},
						When: []kuadrantv1beta2.WhenCondition{
							{
								Selector: "auth.identity.group",
								Operator: kuadrantv1beta2.WhenConditionOperator("neq"),
								Value:    "admin",
							},
						},
					},
					"assets": {
						Rates: []kuadrantv1beta2.Rate{
							{Limit: 5, Duration: 1, Unit: kuadrantv1beta2.TimeUnit("minute")},
							{Limit: 100, Duration: 12, Unit: kuadrantv1beta2.TimeUnit("hour")},
						},
						RouteSelectors: []kuadrantv1beta2.RouteSelector{
							{ // selects the 2nd HTTPRouteRule (i.e. /assets*) for all hostnames
								Matches: []gatewayapiv1.HTTPRouteMatch{
									{
										Path: &gatewayapiv1.HTTPPathMatch{
											Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
											Value: ptr.To("/assets"),
										},
									},
								},
							},
						},
					},
				}
			})
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check wasm plugin
			wasmPluginKey := client.ObjectKey{Name: rlptools.WASMPluginName(gateway), Namespace: testNamespace}
			Eventually(testWasmPluginIsAvailable(wasmPluginKey), time.Minute, 5*time.Second).Should(BeTrue())
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			err = k8sClient.Get(context.Background(), wasmPluginKey, existingWasmPlugin)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			existingWASMConfig, err := rlptools.WASMPluginFromStruct(existingWasmPlugin.Spec.PluginConfig)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig.FailureMode).To(Equal(wasm.FailureModeDeny))
			Expect(existingWASMConfig.RateLimitPolicies).To(HaveLen(1))
			wasmRLP := existingWASMConfig.RateLimitPolicies[0]
			Expect(wasmRLP.Name).To(Equal(rlpKey.String()))
			Expect(wasmRLP.Domain).To(Equal(rlptools.LimitsNamespaceFromRLP(rlp)))
			Expect(wasmRLP.Rules).To(ContainElement(wasm.Rule{ // rule to activate the 'toys' limit definition
				Conditions: []wasm.Condition{
					{
						AllOf: []wasm.PatternExpression{
							{
								Selector: "request.url_path",
								Operator: wasm.PatternOperator(kuadrantv1beta2.StartsWithOperator),
								Value:    "/toys",
							},
							{
								Selector: "request.method",
								Operator: wasm.PatternOperator(kuadrantv1beta2.EqualOperator),
								Value:    "GET",
							},
							{
								Selector: "request.host",
								Operator: wasm.PatternOperator(kuadrantv1beta2.EndsWithOperator),
								Value:    ".toystore.acme.com",
							},
							{
								Selector: "auth.identity.group",
								Operator: wasm.PatternOperator(kuadrantv1beta2.NotEqualOperator),
								Value:    "admin",
							},
						},
					},
					{
						AllOf: []wasm.PatternExpression{
							{
								Selector: "request.url_path",
								Operator: wasm.PatternOperator(kuadrantv1beta2.StartsWithOperator),
								Value:    "/toys",
							},
							{
								Selector: "request.method",
								Operator: wasm.PatternOperator(kuadrantv1beta2.EqualOperator),
								Value:    "POST",
							},
							{
								Selector: "request.host",
								Operator: wasm.PatternOperator(kuadrantv1beta2.EndsWithOperator),
								Value:    ".toystore.acme.com",
							},
							{
								Selector: "auth.identity.group",
								Operator: wasm.PatternOperator(kuadrantv1beta2.NotEqualOperator),
								Value:    "admin",
							},
						},
					},
				},
				Data: []wasm.DataItem{
					{
						Static: &wasm.StaticSpec{
							Key:   "limit.toys__3bfcbeee",
							Value: "1",
						},
					},
					{
						Selector: &wasm.SelectorSpec{
							Selector: kuadrantv1beta2.ContextSelector("auth.identity.username"),
						},
					},
				},
			}))
			Expect(wasmRLP.Rules).To(ContainElement(wasm.Rule{ // rule to activate the 'assets' limit definition
				Conditions: []wasm.Condition{
					{
						AllOf: []wasm.PatternExpression{
							{
								Selector: "request.url_path",
								Operator: wasm.PatternOperator(kuadrantv1beta2.StartsWithOperator),
								Value:    "/assets",
							},
						},
					},
				},
				Data: []wasm.DataItem{
					{
						Static: &wasm.StaticSpec{
							Key:   "limit.assets__8bf729ff",
							Value: "1",
						},
					},
				},
			}))
			Expect(wasmRLP.Hostnames).To(Equal([]string{"*.toystore.acme.com", "api.toystore.io"}))
			Expect(wasmRLP.Service).To(Equal(common.KuadrantRateLimitClusterName))
		})
	})

	Context("RLP targeting Gateway", func() {
		It("Creates all the resources for a basic Gateway and RateLimitPolicy", func() {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// create ratelimitpolicy
			rlp := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
			})
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check Gateway direct back reference
			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1.Gateway{}
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
				Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
				Conditions: []string{`limit.l1__2804bad6 == "1"`},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(rlp),
			}))

			// Check wasm plugin
			wasmPluginKey := client.ObjectKey{Name: rlptools.WASMPluginName(gateway), Namespace: testNamespace}
			Eventually(testWasmPluginIsAvailable(wasmPluginKey), time.Minute, 5*time.Second).Should(BeTrue())
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			err = k8sClient.Get(context.Background(), wasmPluginKey, existingWasmPlugin)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			existingWASMConfig, err := rlptools.WASMPluginFromStruct(existingWasmPlugin.Spec.PluginConfig)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig).To(Equal(&wasm.Plugin{
				FailureMode: wasm.FailureModeDeny,
				RateLimitPolicies: []wasm.RateLimitPolicy{
					{
						Name:   rlpKey.String(),
						Domain: rlptools.LimitsNamespaceFromRLP(rlp),
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
										},
									},
								},
								Data: []wasm.DataItem{
									{
										Static: &wasm.StaticSpec{
											Key:   `limit.l1__2804bad6`,
											Value: "1",
										},
									},
								},
							},
						},
						Hostnames: []string{"*"},
						Service:   common.KuadrantRateLimitClusterName,
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
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(common.RateLimitPoliciesBackRefAnnotation, string(serialized)))
		})

		It("Creates all the resources for a basic Gateway and RateLimitPolicy when missing a HTTPRoute attached to the Gateway", func() {
			// create ratelimitpolicy
			rlp := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gwName)
			})
			err := k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check Gateway direct back reference
			gwKey := client.ObjectKeyFromObject(gateway)
			existingGateway := &gatewayapiv1.Gateway{}
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
				Namespace:  rlptools.LimitsNamespaceFromRLP(rlp),
				Conditions: []string{`limit.l1__2804bad6 == "1"`},
				Variables:  []string{},
				Name:       rlptools.LimitsNameFromRLP(rlp),
			}))

			// Check wasm plugin
			wasmPluginKey := client.ObjectKey{Name: rlptools.WASMPluginName(gateway), Namespace: testNamespace}
			// Wait a bit to catch cases where wasmplugin is created and takes a bit to be created
			Eventually(testWasmPluginIsAvailable(wasmPluginKey), 20*time.Second, 5*time.Second).Should(BeFalse())
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			// must not exist
			err = k8sClient.Get(context.Background(), wasmPluginKey, existingWasmPlugin)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Check gateway back references
			err = k8sClient.Get(context.Background(), gwKey, existingGateway)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			refs := []client.ObjectKey{rlpKey}
			serialized, err := json.Marshal(refs)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingGateway.GetAnnotations()).To(HaveKeyWithValue(common.RateLimitPoliciesBackRefAnnotation, string(serialized)))
		})
	})

	Context("RLP accepted condition reasons", func() {
		assertAcceptedConditionFalse := func(rlp *kuadrantv1beta2.RateLimitPolicy, reason, message string) func() bool {
			return func() bool {
				rlpKey := client.ObjectKeyFromObject(rlp)
				existingRLP := &kuadrantv1beta2.RateLimitPolicy{}
				err := k8sClient.Get(context.Background(), rlpKey, existingRLP)
				if err != nil {
					return false
				}

				cond := meta.FindStatusCondition(existingRLP.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				if cond == nil {
					return false
				}

				return cond.Status == metav1.ConditionFalse && cond.Reason == reason && cond.Message == message
			}
		}

		// Accepted reason is already tested generally by the existing tests

		It("Target not found reason", func() {
			rlp := policyFactory()
			err := k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			Eventually(assertAcceptedConditionFalse(rlp, string(gatewayapiv1alpha2.PolicyReasonTargetNotFound),
				fmt.Sprintf("RateLimitPolicy target %s was not found", routeName)),
				time.Minute, 5*time.Second).Should(BeTrue())
		})

		It("Conflict reason", func() {
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			rlp := policyFactory()
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			rlp2 := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Name = "conflicting-rlp"
			})
			err = k8sClient.Create(context.Background(), rlp2)
			Expect(err).ToNot(HaveOccurred())

			Eventually(assertAcceptedConditionFalse(rlp2, string(gatewayapiv1alpha2.PolicyReasonConflicted),
				fmt.Sprintf("RateLimitPolicy is conflicted by %[1]v/toystore-rlp: the gateway.networking.k8s.io/v1, Kind=HTTPRoute target %[1]v/toystore-route is already referenced by policy %[1]v/toystore-rlp", testNamespace)),
				time.Minute, 5*time.Second).Should(BeTrue())
		})

		It("Validation reason", func() {
			const targetRefName, targetRefNamespace = "istio-ingressgateway", "istio-system"

			rlp := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
				policy.Spec.TargetRef.Name = targetRefName
				policy.Spec.TargetRef.Namespace = ptr.To(gatewayapiv1.Namespace(targetRefNamespace))
			})
			err := k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			Eventually(assertAcceptedConditionFalse(rlp, string(gatewayapiv1alpha2.PolicyReasonInvalid),
				fmt.Sprintf("RateLimitPolicy target is invalid: invalid targetRef.Namespace %s. Currently only supporting references to the same namespace", targetRefNamespace)),
				time.Minute, 5*time.Second).Should(BeTrue())
		})
	})
})

var _ = Describe("RateLimitPolicy CEL Validations", func() {
	var testNamespace string

	BeforeEach(func() {
		CreateNamespace(&testNamespace)
	})

	AfterEach(DeleteNamespaceCallback(&testNamespace))

	Context("Spec TargetRef Validations", func() {
		policyFactory := func(mutateFns ...func(policy *kuadrantv1beta2.RateLimitPolicy)) *kuadrantv1beta2.RateLimitPolicy {
			policy := &kuadrantv1beta2.RateLimitPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-policy",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1beta2.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: "gateway.networking.k8s.io",
						Kind:  "HTTPRoute",
						Name:  "my-target",
					},
				},
			}
			for _, mutateFn := range mutateFns {
				mutateFn(policy)
			}

			return policy
		}
		It("Valid policy targeting HTTPRoute", func() {
			policy := policyFactory()
			err := k8sClient.Create(context.Background(), policy)
			Expect(err).To(BeNil())
		})

		It("Valid policy targeting Gateway", func() {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "Gateway"
			})
			err := k8sClient.Create(context.Background(), policy)
			Expect(err).To(BeNil())
		})

		It("Invalid Target Ref Group", func() {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Group = "not-gateway.networking.k8s.io"
			})
			err := k8sClient.Create(context.Background(), policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'")).To(BeTrue())
		})

		It("Invalid Target Ref Kind", func() {
			policy := policyFactory(func(policy *kuadrantv1beta2.RateLimitPolicy) {
				policy.Spec.TargetRef.Kind = "TCPRoute"
			})
			err := k8sClient.Create(context.Background(), policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), "Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'")).To(BeTrue())
		})
	})

	Context("Route Selector Validation", func() {
		const (
			gateWayRouteSelectorErrorMessage = "route selectors not supported when targeting a Gateway"
		)

		It("invalid usage of limit route selectors with a gateway targetRef", func() {
			policy := &kuadrantv1beta2.RateLimitPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-policy",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1beta2.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: "gateway.networking.k8s.io",
						Kind:  "Gateway",
						Name:  "my-gw",
					},
					Limits: map[string]kuadrantv1beta2.Limit{
						"l1": {
							Rates: []kuadrantv1beta2.Rate{
								{
									Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
								},
							},
							RouteSelectors: []kuadrantv1beta2.RouteSelector{
								{
									Hostnames: []gatewayapiv1.Hostname{"*.foo.io"},
									Matches: []gatewayapiv1.HTTPRouteMatch{
										{
											Path: &gatewayapiv1.HTTPPathMatch{
												Value: ptr.To("/foo"),
											},
										},
									},
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(context.Background(), policy)
			Expect(err).To(Not(BeNil()))
			Expect(strings.Contains(err.Error(), gateWayRouteSelectorErrorMessage)).To(BeTrue())
		})
	})
})
