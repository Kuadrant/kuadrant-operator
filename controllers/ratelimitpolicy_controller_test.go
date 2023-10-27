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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"k8s.io/utils/ptr"
)

var _ = Describe("RateLimitPolicy controller", func() {
	var (
		testNamespace string
		routeName     = "toystore-route"
		gwName        = "toystore-gw"
		rlpName       = "toystore-rlp"
		gateway       *gatewayapiv1.Gateway
	)

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

			if meta.IsStatusConditionFalse(existingGateway.Status.Conditions, common.GatewayProgrammedConditionType) {
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

			// create ratelimitpolicy
			rlp := &kuadrantv1beta2.RateLimitPolicy{
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
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(testRLPIsAvailable(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

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
			}))

			// Check wasm plugin
			wpName := fmt.Sprintf("kuadrant-%s", gwName)
			wasmPluginKey := client.ObjectKey{Name: wpName, Namespace: testNamespace}
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

			// create ratelimitpolicy
			rlp := &kuadrantv1beta2.RateLimitPolicy{
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
					},
				},
			}
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(testRLPIsAvailable(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check wasm plugin
			wpName := fmt.Sprintf("kuadrant-%s", gwName)
			wasmPluginKey := client.ObjectKey{Name: wpName, Namespace: testNamespace}
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

			// create ratelimitpolicy
			rlp := &kuadrantv1beta2.RateLimitPolicy{
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
						Kind:  "Gateway",
						Name:  gatewayapiv1.ObjectName(gwName),
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
			err = k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAvailable(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

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
			}))

			// Check wasm plugin
			wpName := fmt.Sprintf("kuadrant-%s", gwName)
			wasmPluginKey := client.ObjectKey{Name: wpName, Namespace: testNamespace}
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
			rlp := &kuadrantv1beta2.RateLimitPolicy{
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
						Kind:  "Gateway",
						Name:  gatewayapiv1.ObjectName(gwName),
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
			err := k8sClient.Create(context.Background(), rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAvailable(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

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
			}))

			// Check wasm plugin
			wpName := fmt.Sprintf("kuadrant-%s", gwName)
			wasmPluginKey := client.ObjectKey{Name: wpName, Namespace: testNamespace}
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
})

func testRLPIsAvailable(rlpKey client.ObjectKey) func() bool {
	return func() bool {
		existingRLP := &kuadrantv1beta2.RateLimitPolicy{}
		err := k8sClient.Get(context.Background(), rlpKey, existingRLP)
		if err != nil {
			return false
		}
		if !meta.IsStatusConditionTrue(existingRLP.Status.Conditions, "Available") {
			return false
		}

		return true
	}
}
