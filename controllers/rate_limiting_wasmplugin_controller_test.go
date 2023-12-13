//go:build integration

package controllers

import (
	"context"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

var _ = Describe("Rate Limiting WasmPlugin controller", func() {
	var (
		testNamespace string
		gwName        = "toystore-gw"
		gateway       *gatewayapiv1.Gateway
	)

	beforeEachCallback := func() {
		CreateNamespace(&testNamespace)
		gateway = testBuildBasicGateway(gwName, testNamespace)
		err := k8sClient.Create(context.Background(), gateway)
		Expect(err).ToNot(HaveOccurred())
		Eventually(testGatewayIsReady(gateway), 30*time.Second, 5*time.Second).Should(BeTrue())
		ApplyKuadrantCR(testNamespace)
	}

	BeforeEach(beforeEachCallback)
	AfterEach(DeleteNamespaceCallback(&testNamespace))

	Context("Basic tests", func() {
		var (
			routeName = "toystore-route"
			rlpName   = "toystore-rlp"
		)

		It("Simple RLP targeting HTTPRoute creates wasmplugin", func() {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// create ratelimitpolicy
			rlp := &kuadrantv1beta2.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpName, Namespace: testNamespace},
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
		})

		It("Full featured RLP targeting HTTPRoute creates wasmplugin", func() {
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

		It("Simple RLP targeting Gateway parenting one HTTPRoute creates wasmplugin", func() {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// create ratelimitpolicy
			rlp := &kuadrantv1beta2.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpName, Namespace: testNamespace},
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
		})
	})

	Context("RLP targeting HTTPRoute-less Gateway", func() {
		var (
			rlpName = "toystore-rlp"
		)

		It("Wasmplugin must not be created", func() {
			// create ratelimitpolicy
			rlp := &kuadrantv1beta2.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpName, Namespace: testNamespace},
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

			// Check wasm plugin
			wasmPluginKey := client.ObjectKey{Name: rlptools.WASMPluginName(gateway), Namespace: testNamespace}
			// Wait a bit to catch cases where wasmplugin is created and takes a bit to be created
			Eventually(testWasmPluginIsAvailable(wasmPluginKey), 20*time.Second, 5*time.Second).Should(BeFalse())
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			// must not exist
			err = k8sClient.Get(context.Background(), wasmPluginKey, existingWasmPlugin)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("RLP targeting HTTPRoute when route selection match is empty", func() {
		var (
			routeName = "toystore-route"
			rlpName   = "toystore-rlp"
		)

		It("When the gateway does not have more policies, the wasmplugin resource is not created", func() {
			// create httproute
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err := k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// create ratelimitpolicy with no matching routes
			rlp := &kuadrantv1beta2.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpName, Namespace: testNamespace},
				Spec: kuadrantv1beta2.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: gatewayapiv1.Group("gateway.networking.k8s.io"),
						Kind:  "HTTPRoute",
						Name:  gatewayapiv1.ObjectName(routeName),
					},
					Limits: map[string]kuadrantv1beta2.Limit{
						"l1": {
							RouteSelectors: []kuadrantv1beta2.RouteSelector{
								{ // does no select any HTTPRouteRule (i.e. GET /toys*)
									Matches: []gatewayapiv1.HTTPRouteMatch{
										{
											Path: &gatewayapiv1.HTTPPathMatch{
												Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
												Value: ptr.To("/other"),
											},
										},
									},
								},
							},
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

			// Check wasm plugin
			wasmPluginKey := client.ObjectKey{Name: rlptools.WASMPluginName(gateway), Namespace: testNamespace}
			// Wait a bit to catch cases where wasmplugin is created and takes a bit to be created
			Eventually(testWasmPluginIsAvailable(wasmPluginKey), 20*time.Second, 5*time.Second).Should(BeFalse())
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			// must not exist
			err = k8sClient.Get(context.Background(), wasmPluginKey, existingWasmPlugin)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("When the gateway has more policies, the wasmplugin resource does not have any configuration regarding the current RLP", func() {
			// Gw A
			// Route B -> Gw A
			// RLP A -> Gw A
			// Route C -> GW A
			// RLP B -> Route C (however, no matching routes)

			var (
				routeBName = "toystore-b"
				routeCName = "toystore-c"
				rlpAName   = "toystore-a"
				rlpBName   = "toystore-b"
			)

			// create httproute B
			httpRouteB := testBuildBasicHttpRoute(routeBName, gwName, testNamespace, []string{"*.b.example.com"})
			err := k8sClient.Create(context.Background(), httpRouteB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRouteB)), time.Minute, 5*time.Second).Should(BeTrue())

			// create RLP A -> Gw A
			rlpA := &kuadrantv1beta2.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpAName, Namespace: testNamespace},
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
			err = k8sClient.Create(context.Background(), rlpA)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpAKey := client.ObjectKey{Name: rlpAName, Namespace: testNamespace}
			Eventually(testRLPIsAvailable(rlpAKey), time.Minute, 5*time.Second).Should(BeTrue())

			// create httproute C
			httpRouteC := testBuildBasicHttpRoute(routeCName, gwName, testNamespace, []string{"*.c.example.com"})
			httpRouteC.Spec.Rules = []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
								Value: ptr.To("/otherPathRouteC"),
							},
							Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
						},
					},
				},
			}

			err = k8sClient.Create(context.Background(), httpRouteC)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRouteC)), time.Minute, 5*time.Second).Should(BeTrue())

			// create RLP B -> Route C (however, no matching routes)
			rlpB := &kuadrantv1beta2.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpBName, Namespace: testNamespace},
				Spec: kuadrantv1beta2.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: gatewayapiv1.Group("gateway.networking.k8s.io"),
						Kind:  "HTTPRoute",
						Name:  gatewayapiv1.ObjectName(routeCName),
					},
					Limits: map[string]kuadrantv1beta2.Limit{
						"l1": {
							RouteSelectors: []kuadrantv1beta2.RouteSelector{
								{ // does no select any HTTPRouteRule (i.e. GET /otherPathRouteC*)
									Matches: []gatewayapiv1.HTTPRouteMatch{
										{
											Path: &gatewayapiv1.HTTPPathMatch{
												Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
												Value: ptr.To("/notmatchingpath"),
											},
										},
									},
								},
							},
							Rates: []kuadrantv1beta2.Rate{
								{
									Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
								},
							},
						},
					},
				},
			}
			err = k8sClient.Create(context.Background(), rlpB)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpBKey := client.ObjectKey{Name: rlpBName, Namespace: testNamespace}
			Eventually(testRLPIsAvailable(rlpBKey), time.Minute, 5*time.Second).Should(BeTrue())

			// Check wasm plugin only has configuration ONLY from the RLP targeting the gateway
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{
					Name: rlptools.WASMPluginName(gateway), Namespace: testNamespace,
				}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := k8sClient.Get(context.Background(), wasmPluginKey, existingWasmPlugin)
				if err != nil {
					return false
				}
				existingWASMConfig, err := rlptools.WASMPluginFromStruct(existingWasmPlugin.Spec.PluginConfig)
				if err != nil {
					return false
				}

				expectedPlugin := &wasm.Plugin{
					FailureMode: wasm.FailureModeDeny,
					RateLimitPolicies: []wasm.RateLimitPolicy{
						{
							Name:   rlpAKey.String(),
							Domain: rlptools.LimitsNamespaceFromRLP(rlpA),
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
				}

				if !reflect.DeepEqual(existingWASMConfig, expectedPlugin) {
					return false
				}

				return true
			}, time.Minute, 5*time.Second).Should(BeTrue())
		})
	})

	Context("HTTPRoute switches parentship from one gateway to another", func() {
		var (
			routeName = "route-a"
			rlpName   = "rlp-a"
			gwBName   = "gw-b"
		)

		It("RLP targeting a gateway, GwA should not have wasmplugin and GwB should have wasmplugin", func() {
			// Initial state
			// Gw A
			// Gw B
			// RLP A -> Gw A
			// Route A -> Gw A
			//
			// Switch parentship
			// Gw A
			// Gw B
			// RLP A -> Gw A
			// Route A -> Gw B

			// Gw A will be the pre-existing $gateway with name $gwName

			// create RLP A -> Gw A
			rlpA := &kuadrantv1beta2.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpName, Namespace: testNamespace},
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
			err := k8sClient.Create(context.Background(), rlpA)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAvailable(rlpKey), time.Minute, 5*time.Second).Should(BeTrue())

			// create Route A -> Gw A
			httpRoute := testBuildBasicHttpRoute(routeName, gwName, testNamespace, []string{"*.example.com"})
			err = k8sClient.Create(context.Background(), httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testRouteIsAccepted(client.ObjectKeyFromObject(httpRoute)), time.Minute, 5*time.Second).Should(BeTrue())

			// create Gateway B
			gwB := testBuildBasicGateway(gwBName, testNamespace)
			err = k8sClient.Create(context.Background(), gwB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gwB), 30*time.Second, 5*time.Second).Should(BeTrue())

			// Initial state set.
			// Check wasm plugin for gateway A has configuration from the route
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{
					Name: rlptools.WASMPluginName(gateway), Namespace: testNamespace,
				}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := k8sClient.Get(context.Background(), wasmPluginKey, existingWasmPlugin)
				if err != nil {
					return false
				}
				existingWASMConfig, err := rlptools.WASMPluginFromStruct(existingWasmPlugin.Spec.PluginConfig)
				if err != nil {
					return false
				}

				expectedPlugin := &wasm.Plugin{
					FailureMode: wasm.FailureModeDeny,
					RateLimitPolicies: []wasm.RateLimitPolicy{
						{
							Name:   rlpKey.String(),
							Domain: rlptools.LimitsNamespaceFromRLP(rlpA),
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
				}

				if !reflect.DeepEqual(existingWASMConfig, expectedPlugin) {
					return false
				}

				return true
			}, time.Minute, 5*time.Second).Should(BeTrue())

			// Check wasm plugin for gateway B does not exist
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				// Check wasm plugin
				wasmPluginKey := client.ObjectKey{Name: rlptools.WASMPluginName(gwB), Namespace: testNamespace}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := k8sClient.Get(context.Background(), wasmPluginKey, existingWasmPlugin)
				return apierrors.IsNotFound(err)
			})

			// Proceed with the update:
			// From Route A -> Gw A
			// To Route A -> Gw B
			httpRouteUpdated := &gatewayapiv1.HTTPRoute{}
			err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(httpRoute), httpRouteUpdated)
			Expect(err).ToNot(HaveOccurred())
			httpRouteUpdated.Spec.CommonRouteSpec.ParentRefs[0].Name = gatewayapiv1.ObjectName(gwBName)
			err = k8sClient.Update(context.Background(), httpRouteUpdated)
			Expect(err).ToNot(HaveOccurred())

			// Check wasm plugin for gateway A no longer exists
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{Name: rlptools.WASMPluginName(gateway), Namespace: testNamespace}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := k8sClient.Get(context.Background(), wasmPluginKey, existingWasmPlugin)
				return apierrors.IsNotFound(err)
			})

			// Check wasm plugin for gateway B does not exist
			// There is not RLP targeting Gateway B or any route parenting Gateway B
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{Name: rlptools.WASMPluginName(gwB), Namespace: testNamespace}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := k8sClient.Get(context.Background(), wasmPluginKey, existingWasmPlugin)
				return apierrors.IsNotFound(err)
			})
		})

		It("RLP targeting a route, GwA should not have wasmplugin and GwB should have wasmplugin", func() {
			// Initial state
			// Gw A
			// Gw B
			// Route A -> Gw A
			// RLP A -> Route A
			//
			// Switch parentship
			// Gw A
			// Gw B
			// Route A -> Gw B
			// RLP A -> Route A
		})
	})
})

func testWasmPluginIsAvailable(key client.ObjectKey) func() bool {
	return func() bool {
		wp := &istioclientgoextensionv1alpha1.WasmPlugin{}
		err := k8sClient.Get(context.Background(), key, wp)
		if err != nil {
			return false
		}

		// Unfortunately, WasmPlugin does not have status yet
		// Leaving this here for future use
		//if !meta.IsStatusConditionTrue(wp.Status.Conditions, "Available") {
		//	return false
		//}

		return true
	}
}
