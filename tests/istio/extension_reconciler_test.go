//go:build integration

package istio_test

import (
	"context"
	"reflect"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/kuadrant/policy-machinery/machinery"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/controllers"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/wasm"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Rate Limiting WasmPlugin controller", func() {
	const (
		testTimeOut      = SpecTimeout(3 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
	)

	assertPolicyIsAcceptedAndEnforced := func(ctx context.Context, key client.ObjectKey) func() bool {
		return func() bool {
			return tests.RLPIsAccepted(ctx, testClient(), key)() && tests.RLPIsEnforced(ctx, testClient(), key)()
		}
	}

	assertPolicyIsAcceptedAndNotEnforced := func(ctx context.Context, key client.ObjectKey) func() bool {
		return func() bool {
			return tests.RLPIsAccepted(ctx, testClient(), key)() && !tests.RLPIsEnforced(ctx, testClient(), key)()
		}
	}

	beforeEachCallback := func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	}

	BeforeEach(beforeEachCallback)
	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("Basic tests", func() {
		var (
			routeName    = "toystore-route"
			rlpName      = "toystore-rlp"
			gatewayClass *gatewayapiv1.GatewayClass
			gateway      *gatewayapiv1.Gateway
		)

		beforeEachCallback := func(ctx SpecContext) {
			gatewayClass = &gatewayapiv1.GatewayClass{}
			err := testClient().Get(ctx, types.NamespacedName{Name: tests.GatewayClassName}, gatewayClass)
			Expect(err).ToNot(HaveOccurred())
			gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)
			err = testClient().Create(ctx, gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
		}

		BeforeEach(beforeEachCallback)

		It("Simple RLP targeting HTTPRoute creates wasmplugin", func(ctx SpecContext) {
			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			err := testClient().Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: httpRoute}
			pathID := kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRoute.Spec.Rules[0], Name: "rule-1"},
			})

			// create ratelimitpolicy
			rlp := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpName, Namespace: testNamespace},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(routeName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1beta3.Limit{
							"l1": {
								Rates: []kuadrantv1beta3.Rate{
									{
										Limit: 1, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
									},
								},
							},
						},
					},
				},
			}
			err = testClient().Create(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// Check wasm plugin
			wasmPluginKey := client.ObjectKey{Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace}
			Eventually(tests.WasmPluginIsAvailable(ctx, testClient(), wasmPluginKey)).WithContext(ctx).Should(BeTrue())
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			err = testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			// has the correct target ref
			Expect(existingWasmPlugin.Spec.TargetRef).To(Not(BeNil()))
			Expect(existingWasmPlugin.Spec.TargetRef.Group).To(Equal("gateway.networking.k8s.io"))
			Expect(existingWasmPlugin.Spec.TargetRef.Kind).To(Equal("Gateway"))
			Expect(existingWasmPlugin.Spec.TargetRef.Name).To(Equal(gateway.Name))
			existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig).To(Equal(&wasm.Config{
				Services: map[string]wasm.Service{
					wasm.RateLimitServiceName: {
						Type:        wasm.RateLimitServiceType,
						Endpoint:    common.KuadrantRateLimitClusterName,
						FailureMode: wasm.FailureModeAllow,
					},
				},
				ActionSets: []wasm.ActionSet{
					{
						Name: wasm.ActionSetNameForPath(pathID, 1, "*.example.com"),
						RouteRuleConditions: wasm.RouteRuleConditions{
							Hostnames: []string{"*.example.com"},
							Matches: []wasm.Predicate{
								{
									Selector: "request.method",
									Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
									Value:    "GET",
								},
								{
									Selector: "request.url_path",
									Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
									Value:    "/toy",
								},
							},
						},
						Actions: []wasm.Action{
							{
								ServiceName: wasm.RateLimitServiceName,
								Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
								Data: []wasm.DataType{
									{
										Value: &wasm.Static{
											Static: wasm.StaticSpec{
												Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "l1"),
												Value: "1",
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

		It("Full featured RLP targeting HTTPRoute creates wasmplugin", func(ctx SpecContext) {
			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.toystore.acme.com", "api.toystore.io"})
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
			err := testClient().Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create ratelimitpolicy
			rlp := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RateLimitPolicy",
					APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      rlpName,
					Namespace: testNamespace,
				},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(routeName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1beta3.Limit{
							"users": {
								Rates: []kuadrantv1beta3.Rate{
									{Limit: 50, Duration: 1, Unit: kuadrantv1beta3.TimeUnit("minute")},
								},
								Counters: []kuadrantv1beta3.ContextSelector{"auth.identity.username"},
								When: []kuadrantv1beta3.WhenCondition{
									{
										Selector: "auth.identity.group",
										Operator: kuadrantv1beta3.WhenConditionOperator("neq"),
										Value:    "admin",
									},
								},
							},
							"all": {
								Rates: []kuadrantv1beta3.Rate{
									{Limit: 5, Duration: 1, Unit: kuadrantv1beta3.TimeUnit("minute")},
									{Limit: 100, Duration: 12, Unit: kuadrantv1beta3.TimeUnit("hour")},
								},
							},
						},
					},
				},
			}
			err = testClient().Create(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// Check wasm plugin
			wasmPluginKey := client.ObjectKey{Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace}
			Eventually(tests.WasmPluginIsAvailable(ctx, testClient(), wasmPluginKey)).WithContext(ctx).Should(BeTrue())
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			err = testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig.Services).To(HaveKeyWithValue(wasm.RateLimitServiceName, wasm.Service{
				Endpoint:    common.KuadrantRateLimitClusterName,
				FailureMode: wasm.FailureModeAllow,
				Type:        wasm.RateLimitServiceType,
			}))
			Expect(existingWASMConfig.ActionSets).To(HaveLen(6))

			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: httpRoute}
			basePath := []machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
			}
			httpRouteRuleToys := &machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRoute.Spec.Rules[0], Name: "rule-1"}
			httpRouteRuleAssets := &machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRoute.Spec.Rules[1], Name: "rule-2"}

			// GET api.toystore.io/toys*
			actionSet := existingWASMConfig.ActionSets[0]
			pathID := kuadrantv1.PathID(append(basePath, httpRouteRuleToys))
			Expect(actionSet.Name).To(Equal(wasm.ActionSetNameForPath(pathID, 1, "api.toystore.io")))
			Expect(actionSet.RouteRuleConditions.Hostnames).To(Equal([]string{"api.toystore.io"}))
			Expect(actionSet.RouteRuleConditions.Matches).To(ContainElements(
				wasm.Predicate{
					Selector: "request.method",
					Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
					Value:    "GET",
				},
				wasm.Predicate{
					Selector: "request.url_path",
					Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
					Value:    "/toys",
				},
			))
			Expect(actionSet.Actions).To(HaveLen(2))
			Expect(actionSet.Actions).To(ContainElements(
				wasm.Action{ // action to activate the 'users' limit definition
					ServiceName: wasm.RateLimitServiceName,
					Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
					Conditions: []wasm.Predicate{
						{
							Selector: "auth.identity.group",
							Operator: wasm.PatternOperator(kuadrantv1beta3.NotEqualOperator),
							Value:    "admin",
						},
					},
					Data: []wasm.DataType{
						{
							Value: &wasm.Static{
								Static: wasm.StaticSpec{
									Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "users"),
									Value: "1",
								},
							},
						},
						{
							Value: &wasm.Selector{
								Selector: wasm.SelectorSpec{
									Selector: kuadrantv1beta3.ContextSelector("auth.identity.username"),
								},
							},
						},
					},
				},
				wasm.Action{ // action to activate the 'all' limit definition
					ServiceName: wasm.RateLimitServiceName,
					Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
					Data: []wasm.DataType{
						{
							Value: &wasm.Static{
								Static: wasm.StaticSpec{
									Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "all"),
									Value: "1",
								},
							},
						},
					},
				},
			))

			// POST api.toystore.io/toys*
			actionSet = existingWASMConfig.ActionSets[1]
			pathID = kuadrantv1.PathID(append(basePath, httpRouteRuleToys))
			Expect(actionSet.Name).To(Equal(wasm.ActionSetNameForPath(pathID, 2, "api.toystore.io")))
			Expect(actionSet.RouteRuleConditions.Hostnames).To(Equal([]string{"api.toystore.io"}))
			Expect(actionSet.RouteRuleConditions.Matches).To(ContainElements(
				wasm.Predicate{
					Selector: "request.method",
					Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
					Value:    "POST",
				},
				wasm.Predicate{
					Selector: "request.url_path",
					Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
					Value:    "/toys",
				},
			))
			Expect(actionSet.Actions).To(HaveLen(2))
			Expect(actionSet.Actions).To(ContainElements(
				wasm.Action{ // action to activate the 'users' limit definition
					ServiceName: wasm.RateLimitServiceName,
					Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
					Conditions: []wasm.Predicate{
						{
							Selector: "auth.identity.group",
							Operator: wasm.PatternOperator(kuadrantv1beta3.NotEqualOperator),
							Value:    "admin",
						},
					},
					Data: []wasm.DataType{
						{
							Value: &wasm.Static{
								Static: wasm.StaticSpec{
									Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "users"),
									Value: "1",
								},
							},
						},
						{
							Value: &wasm.Selector{
								Selector: wasm.SelectorSpec{
									Selector: kuadrantv1beta3.ContextSelector("auth.identity.username"),
								},
							},
						},
					},
				},
				wasm.Action{ // action to activate the 'all' limit definition
					ServiceName: wasm.RateLimitServiceName,
					Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
					Data: []wasm.DataType{
						{
							Value: &wasm.Static{
								Static: wasm.StaticSpec{
									Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "all"),
									Value: "1",
								},
							},
						},
					},
				},
			))

			// api.toystore.io/assets*
			actionSet = existingWASMConfig.ActionSets[2]
			pathID = kuadrantv1.PathID(append(basePath, httpRouteRuleAssets))
			Expect(actionSet.Name).To(Equal(wasm.ActionSetNameForPath(pathID, 3, "api.toystore.io")))
			Expect(actionSet.RouteRuleConditions.Hostnames).To(Equal([]string{"api.toystore.io"}))
			Expect(actionSet.RouteRuleConditions.Matches).To(ContainElements(
				wasm.Predicate{
					Selector: "request.url_path",
					Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
					Value:    "/assets",
				},
			))
			Expect(actionSet.Actions).To(HaveLen(2))
			Expect(actionSet.Actions).To(ContainElements(
				wasm.Action{ // action to activate the 'users' limit definition
					ServiceName: wasm.RateLimitServiceName,
					Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
					Conditions: []wasm.Predicate{
						{
							Selector: "auth.identity.group",
							Operator: wasm.PatternOperator(kuadrantv1beta3.NotEqualOperator),
							Value:    "admin",
						},
					},
					Data: []wasm.DataType{
						{
							Value: &wasm.Static{
								Static: wasm.StaticSpec{
									Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "users"),
									Value: "1",
								},
							},
						},
						{
							Value: &wasm.Selector{
								Selector: wasm.SelectorSpec{
									Selector: kuadrantv1beta3.ContextSelector("auth.identity.username"),
								},
							},
						},
					},
				},
				wasm.Action{ // action to activate the 'all' limit definition
					ServiceName: wasm.RateLimitServiceName,
					Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
					Data: []wasm.DataType{
						{
							Value: &wasm.Static{
								Static: wasm.StaticSpec{
									Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "all"),
									Value: "1",
								},
							},
						},
					},
				},
			))

			// GET *.toystore.acme.com/toys*
			actionSet = existingWASMConfig.ActionSets[3]
			pathID = kuadrantv1.PathID(append(basePath, httpRouteRuleToys))
			Expect(actionSet.Name).To(Equal(wasm.ActionSetNameForPath(pathID, 1, "*.toystore.acme.com")))
			Expect(actionSet.RouteRuleConditions.Hostnames).To(Equal([]string{"*.toystore.acme.com"}))
			Expect(actionSet.RouteRuleConditions.Matches).To(ContainElements(
				wasm.Predicate{
					Selector: "request.method",
					Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
					Value:    "GET",
				},
				wasm.Predicate{
					Selector: "request.url_path",
					Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
					Value:    "/toys",
				},
			))
			Expect(actionSet.Actions).To(HaveLen(2))
			Expect(actionSet.Actions).To(ContainElements(
				wasm.Action{ // action to activate the 'users' limit definition
					ServiceName: wasm.RateLimitServiceName,
					Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
					Conditions: []wasm.Predicate{
						{
							Selector: "auth.identity.group",
							Operator: wasm.PatternOperator(kuadrantv1beta3.NotEqualOperator),
							Value:    "admin",
						},
					},
					Data: []wasm.DataType{
						{
							Value: &wasm.Static{
								Static: wasm.StaticSpec{
									Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "users"),
									Value: "1",
								},
							},
						},
						{
							Value: &wasm.Selector{
								Selector: wasm.SelectorSpec{
									Selector: kuadrantv1beta3.ContextSelector("auth.identity.username"),
								},
							},
						},
					},
				},
				wasm.Action{ // action to activate the 'all' limit definition
					ServiceName: wasm.RateLimitServiceName,
					Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
					Data: []wasm.DataType{
						{
							Value: &wasm.Static{
								Static: wasm.StaticSpec{
									Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "all"),
									Value: "1",
								},
							},
						},
					},
				},
			))

			// POST *.toystore.acme.com/toys*
			actionSet = existingWASMConfig.ActionSets[4]
			pathID = kuadrantv1.PathID(append(basePath, httpRouteRuleToys))
			Expect(actionSet.Name).To(Equal(wasm.ActionSetNameForPath(pathID, 2, "*.toystore.acme.com")))
			Expect(actionSet.RouteRuleConditions.Hostnames).To(Equal([]string{"*.toystore.acme.com"}))
			Expect(actionSet.RouteRuleConditions.Matches).To(ContainElements(
				wasm.Predicate{
					Selector: "request.method",
					Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
					Value:    "POST",
				},
				wasm.Predicate{
					Selector: "request.url_path",
					Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
					Value:    "/toys",
				},
			))
			Expect(actionSet.Actions).To(HaveLen(2))
			Expect(actionSet.Actions).To(ContainElements(
				wasm.Action{ // action to activate the 'users' limit definition
					ServiceName: wasm.RateLimitServiceName,
					Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
					Conditions: []wasm.Predicate{
						{
							Selector: "auth.identity.group",
							Operator: wasm.PatternOperator(kuadrantv1beta3.NotEqualOperator),
							Value:    "admin",
						},
					},
					Data: []wasm.DataType{
						{
							Value: &wasm.Static{
								Static: wasm.StaticSpec{
									Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "users"),
									Value: "1",
								},
							},
						},
						{
							Value: &wasm.Selector{
								Selector: wasm.SelectorSpec{
									Selector: kuadrantv1beta3.ContextSelector("auth.identity.username"),
								},
							},
						},
					},
				},
				wasm.Action{ // action to activate the 'all' limit definition
					ServiceName: wasm.RateLimitServiceName,
					Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
					Data: []wasm.DataType{
						{
							Value: &wasm.Static{
								Static: wasm.StaticSpec{
									Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "all"),
									Value: "1",
								},
							},
						},
					},
				},
			))

			// *.toystore.acme.com/assets*
			actionSet = existingWASMConfig.ActionSets[5]
			pathID = kuadrantv1.PathID(append(basePath, httpRouteRuleAssets))
			Expect(actionSet.Name).To(Equal(wasm.ActionSetNameForPath(pathID, 3, "*.toystore.acme.com")))
			Expect(actionSet.RouteRuleConditions.Hostnames).To(Equal([]string{"*.toystore.acme.com"}))
			Expect(actionSet.RouteRuleConditions.Matches).To(ContainElements(
				wasm.Predicate{
					Selector: "request.url_path",
					Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
					Value:    "/assets",
				},
			))
			Expect(actionSet.Actions).To(HaveLen(2))
			Expect(actionSet.Actions).To(ContainElements(
				wasm.Action{ // action to activate the 'users' limit definition
					ServiceName: wasm.RateLimitServiceName,
					Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
					Conditions: []wasm.Predicate{
						{
							Selector: "auth.identity.group",
							Operator: wasm.PatternOperator(kuadrantv1beta3.NotEqualOperator),
							Value:    "admin",
						},
					},
					Data: []wasm.DataType{
						{
							Value: &wasm.Static{
								Static: wasm.StaticSpec{
									Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "users"),
									Value: "1",
								},
							},
						},
						{
							Value: &wasm.Selector{
								Selector: wasm.SelectorSpec{
									Selector: kuadrantv1beta3.ContextSelector("auth.identity.username"),
								},
							},
						},
					},
				},
				wasm.Action{ // action to activate the 'all' limit definition
					ServiceName: wasm.RateLimitServiceName,
					Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
					Data: []wasm.DataType{
						{
							Value: &wasm.Static{
								Static: wasm.StaticSpec{
									Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "all"),
									Value: "1",
								},
							},
						},
					},
				},
			))
		}, testTimeOut)

		It("Simple RLP targeting Gateway parented by one HTTPRoute creates wasmplugin", func(ctx SpecContext) {
			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			err := testClient().Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: httpRoute}
			pathID := kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRoute.Spec.Rules[0], Name: "rule-1"},
			})

			// create ratelimitpolicy
			rlp := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpName, Namespace: testNamespace},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1beta3.Limit{
							"l1": {
								Rates: []kuadrantv1beta3.Rate{
									{
										Limit: 1, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
									},
								},
							},
						},
					},
				},
			}
			err = testClient().Create(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// Check wasm plugin
			wasmPluginKey := client.ObjectKey{Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace}
			Eventually(tests.WasmPluginIsAvailable(ctx, testClient(), wasmPluginKey)).WithContext(ctx).Should(BeTrue())
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			err = testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig).To(Equal(&wasm.Config{
				Services: map[string]wasm.Service{
					wasm.RateLimitServiceName: {
						Endpoint:    common.KuadrantRateLimitClusterName,
						FailureMode: wasm.FailureModeAllow,
						Type:        wasm.RateLimitServiceType,
					},
				},
				ActionSets: []wasm.ActionSet{
					{
						Name: wasm.ActionSetNameForPath(pathID, 1, "*.example.com"),
						RouteRuleConditions: wasm.RouteRuleConditions{
							Hostnames: []string{"*.example.com"},
							Matches: []wasm.Predicate{
								{
									Selector: "request.method",
									Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
									Value:    "GET",
								},
								{
									Selector: "request.url_path",
									Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
									Value:    "/toy",
								},
							},
						},
						Actions: []wasm.Action{
							{
								ServiceName: wasm.RateLimitServiceName,
								Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
								Data: []wasm.DataType{
									{
										Value: &wasm.Static{
											Static: wasm.StaticSpec{
												Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "l1"),
												Value: "1",
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

	Context("RLP targeting HTTPRoute-less Gateway", func() {
		var (
			rlpName         = "toystore-rlp"
			TestGatewayName = "toystore-gw"
			gateway         *gatewayapiv1.Gateway
		)

		beforeEachCallback := func(ctx SpecContext) {
			gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)
			err := testClient().Create(ctx, gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
		}

		BeforeEach(beforeEachCallback)

		It("Wasmplugin must not be created", func(ctx SpecContext) {
			// create ratelimitpolicy
			rlp := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpName, Namespace: testNamespace},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1beta3.Limit{
							"l1": {
								Rates: []kuadrantv1beta3.Rate{
									{
										Limit: 1, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
									},
								},
							},
						},
					},
				},
			}
			err := testClient().Create(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), rlpKey, kuadrant.PolicyReasonUnknown, "RateLimitPolicy has encountered some issues: no free routes to enforce policy"))

			// Check wasm plugin
			wasmPluginKey := client.ObjectKey{Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace}
			// Wait a bit to catch cases where wasmplugin is created and takes a bit to be created
			Eventually(tests.WasmPluginIsAvailable(ctx, testClient(), wasmPluginKey), 20*time.Second, 5*time.Second).Should(BeFalse())
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			// must not exist
			err = testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}, testTimeOut)
	})

	Context("HTTPRoute switches parentship from one gateway to another", func() {
		var (
			routeName       = "route-a"
			rlpName         = "rlp-a"
			TestGatewayName = "toystore-gw"
			gatewayClass    *gatewayapiv1.GatewayClass
			gateway         *gatewayapiv1.Gateway
			gwBName         = "gw-b"
		)

		beforeEachCallback := func(ctx SpecContext) {
			gatewayClass = &gatewayapiv1.GatewayClass{}
			err := testClient().Get(ctx, types.NamespacedName{Name: tests.GatewayClassName}, gatewayClass)
			Expect(err).ToNot(HaveOccurred())
			gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)
			err = testClient().Create(ctx, gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
		}

		BeforeEach(beforeEachCallback)

		It("RLP targeting a gateway, GwA should not have wasmplugin and GwB should not have wasmplugin", func(ctx SpecContext) {
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

			// Gw A will be the pre-existing $gateway with name $TestGatewayName

			// create RLP A -> Gw A
			rlpA := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpName, Namespace: testNamespace},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1beta3.Limit{
							"l1": {
								Rates: []kuadrantv1beta3.Rate{
									{
										Limit: 1, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
									},
								},
							},
						},
					},
				},
			}
			err := testClient().Create(ctx, rlpA)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndNotEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())
			Expect(tests.RLPEnforcedCondition(ctx, testClient(), rlpKey, kuadrant.PolicyReasonUnknown, "RateLimitPolicy has encountered some issues: no free routes to enforce policy"))

			// create Route A -> Gw A
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			err = testClient().Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())
			//Eventually(testRLPIsEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// create Gateway B
			gwB := tests.BuildBasicGateway(gwBName, testNamespace)
			err = testClient().Create(ctx, gwB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gwB)).WithContext(ctx).Should(BeTrue())

			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: httpRoute}
			pathID := kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRoute.Spec.Rules[0], Name: "rule-1"},
			})

			// Initial state set.
			// Check wasm plugin for gateway A has configuration from the route
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{
					Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace,
				}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin could not be deserialized", "key", wasmPluginKey, "error", err)
					return false
				}

				expectedPlugin := &wasm.Config{
					Services: map[string]wasm.Service{
						wasm.RateLimitServiceName: {
							Endpoint:    common.KuadrantRateLimitClusterName,
							FailureMode: wasm.FailureModeAllow,
							Type:        wasm.RateLimitServiceType,
						},
					},
					ActionSets: []wasm.ActionSet{
						{
							Name: wasm.ActionSetNameForPath(pathID, 1, "*.example.com"),
							RouteRuleConditions: wasm.RouteRuleConditions{
								Hostnames: []string{"*.example.com"},
								Matches: []wasm.Predicate{
									{
										Selector: "request.method",
										Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
										Value:    "GET",
									},
									{
										Selector: "request.url_path",
										Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
										Value:    "/toy",
									},
								},
							},
							Actions: []wasm.Action{
								{
									ServiceName: wasm.RateLimitServiceName,
									Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
									Data: []wasm.DataType{
										{
											Value: &wasm.Static{
												Static: wasm.StaticSpec{
													Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "l1"),
													Value: "1",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				if !reflect.DeepEqual(existingWASMConfig, expectedPlugin) {
					diff := cmp.Diff(existingWASMConfig, expectedPlugin)
					logf.Log.V(1).Info("wasmplugin does not match", "key", wasmPluginKey, "diff", diff)
					return false
				}

				return true
			}).WithContext(ctx).Should(BeTrue())

			// Check wasm plugin for gateway B does not exist
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				// Check wasm plugin
				wasmPluginKey := client.ObjectKey{Name: wasm.ExtensionName(gwB.GetName()), Namespace: testNamespace}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err == nil {
					logf.Log.V(1).Info("wasmplugin found unexpectedly", "key", wasmPluginKey)
					return false
				}
				if !apierrors.IsNotFound(err) {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				// not found
				return true
			})

			// Proceed with the update:
			// From Route A -> Gw A
			// To Route A -> Gw B
			Eventually(func(g Gomega) {
				httpRouteUpdated := &gatewayapiv1.HTTPRoute{}
				err = testClient().Get(ctx, client.ObjectKeyFromObject(httpRoute), httpRouteUpdated)
				g.Expect(err).ToNot(HaveOccurred())
				httpRouteUpdated.Spec.CommonRouteSpec.ParentRefs[0].Name = gatewayapiv1.ObjectName(gwBName)
				err = testClient().Update(ctx, httpRouteUpdated)
				g.Expect(err).ToNot(HaveOccurred())
			}).Should(Succeed())

			// Check wasm plugin for gateway A no longer exists
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err == nil {
					logf.Log.V(1).Info("wasmplugin found unexpectedly", "key", wasmPluginKey)
					return false
				}
				if !apierrors.IsNotFound(err) {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				// not found
				return true
			})

			// Check wasm plugin for gateway B does not exist
			// There is not RLP targeting Gateway B or any route parented by Gateway B
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{Name: wasm.ExtensionName(gwB.GetName()), Namespace: testNamespace}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err == nil {
					logf.Log.V(1).Info("wasmplugin found unexpectedly", "key", wasmPluginKey)
					return false
				}
				if !apierrors.IsNotFound(err) {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				// not found
				return true
			})
		}, testTimeOut)

		It("RLP targeting a route, GwA should not have wasmplugin and GwB should have wasmplugin", func(ctx SpecContext) {
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

			// Gw A will be the pre-existing $gateway with name $TestGatewayName

			// create Gateway B
			gwB := tests.BuildBasicGateway(gwBName, testNamespace)
			err := testClient().Create(ctx, gwB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gwB)).WithContext(ctx).Should(BeTrue())

			// create Route A -> Gw A
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			err = testClient().Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: httpRoute}
			pathID := kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRoute.Spec.Rules[0], Name: "rule-1"},
			})

			// create RLP A -> Route A
			rlpA := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpName, Namespace: testNamespace},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(routeName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1beta3.Limit{
							"l1": {
								Rates: []kuadrantv1beta3.Rate{
									{
										Limit: 1, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
									},
								},
							},
						},
					},
				},
			}
			err = testClient().Create(ctx, rlpA)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			// Initial state set.
			// Check wasm plugin for gateway A has configuration from the route
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{
					Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace,
				}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin could not be deserialized", "key", wasmPluginKey, "error", err)
					return false
				}

				expectedPlugin := &wasm.Config{
					Services: map[string]wasm.Service{
						wasm.RateLimitServiceName: {
							Endpoint:    common.KuadrantRateLimitClusterName,
							FailureMode: wasm.FailureModeAllow,
							Type:        wasm.RateLimitServiceType,
						},
					},
					ActionSets: []wasm.ActionSet{
						{
							Name: wasm.ActionSetNameForPath(pathID, 1, "*.example.com"),
							RouteRuleConditions: wasm.RouteRuleConditions{
								Hostnames: []string{"*.example.com"},
								Matches: []wasm.Predicate{
									{
										Selector: "request.method",
										Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
										Value:    "GET",
									},
									{
										Selector: "request.url_path",
										Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
										Value:    "/toy",
									},
								},
							},
							Actions: []wasm.Action{
								{
									ServiceName: wasm.RateLimitServiceName,
									Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
									Data: []wasm.DataType{
										{
											Value: &wasm.Static{
												Static: wasm.StaticSpec{
													Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "l1"),
													Value: "1",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				if !reflect.DeepEqual(existingWASMConfig, expectedPlugin) {
					diff := cmp.Diff(existingWASMConfig, expectedPlugin)
					logf.Log.V(1).Info("wasmplugin does not match", "key", wasmPluginKey, "diff", diff)
					return false
				}

				return true
			}).WithContext(ctx).Should(BeTrue())

			// Check wasm plugin for gateway B does not exist
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				// Check wasm plugin
				wasmPluginKey := client.ObjectKey{Name: wasm.ExtensionName(gwB.GetName()), Namespace: testNamespace}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err == nil {
					logf.Log.V(1).Info("wasmplugin found unexpectedly", "key", wasmPluginKey)
					return false
				}
				if !apierrors.IsNotFound(err) {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				// not found
				return true
			})

			// Proceed with the update:
			// From Route A -> Gw A
			// To Route A -> Gw B
			Eventually(func(g Gomega) {
				httpRouteUpdated := &gatewayapiv1.HTTPRoute{}
				err = testClient().Get(ctx, client.ObjectKeyFromObject(httpRoute), httpRouteUpdated)
				g.Expect(err).ToNot(HaveOccurred())
				httpRouteUpdated.Spec.CommonRouteSpec.ParentRefs[0].Name = gatewayapiv1.ObjectName(gwBName)
				err = testClient().Update(ctx, httpRouteUpdated)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// Check wasm plugin for gateway A no longer exists
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err == nil {
					logf.Log.V(1).Info("wasmplugin found unexpectedly", "key", wasmPluginKey)
					return false
				}
				if !apierrors.IsNotFound(err) {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				// not found
				return true
			})

			mGateway = &machinery.Gateway{Gateway: gwB}
			pathID = kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRoute.Spec.Rules[0], Name: "rule-1"},
			})

			// Check wasm plugin for gateway B has configuration from the route
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{
					Name: wasm.ExtensionName(gwB.GetName()), Namespace: testNamespace,
				}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin could not be deserialized", "key", wasmPluginKey, "error", err)
					return false
				}

				expectedPlugin := &wasm.Config{
					Services: map[string]wasm.Service{
						wasm.RateLimitServiceName: {
							Endpoint:    common.KuadrantRateLimitClusterName,
							FailureMode: wasm.FailureModeAllow,
							Type:        wasm.RateLimitServiceType,
						},
					},
					ActionSets: []wasm.ActionSet{
						{
							Name: wasm.ActionSetNameForPath(pathID, 1, "*.example.com"),
							RouteRuleConditions: wasm.RouteRuleConditions{
								Hostnames: []string{"*.example.com"},
								Matches: []wasm.Predicate{
									{
										Selector: "request.method",
										Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
										Value:    "GET",
									},
									{
										Selector: "request.url_path",
										Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
										Value:    "/toy",
									},
								},
							},
							Actions: []wasm.Action{
								{
									ServiceName: wasm.RateLimitServiceName,
									Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
									Data: []wasm.DataType{
										{
											Value: &wasm.Static{
												Static: wasm.StaticSpec{
													Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "l1"),
													Value: "1",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				if !reflect.DeepEqual(existingWASMConfig, expectedPlugin) {
					diff := cmp.Diff(existingWASMConfig, expectedPlugin)
					logf.Log.V(1).Info("wasmplugin does not match", "key", wasmPluginKey, "diff", diff)
					return false
				}

				return true
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("RLP switches targetRef from one route A to another route B", func() {
		var (
			TestGatewayName = "toystore-gw"
			gatewayClass    *gatewayapiv1.GatewayClass
			gateway         *gatewayapiv1.Gateway
		)

		beforeEachCallback := func(ctx SpecContext) {
			gatewayClass = &gatewayapiv1.GatewayClass{}
			err := testClient().Get(ctx, types.NamespacedName{Name: tests.GatewayClassName}, gatewayClass)
			Expect(err).ToNot(HaveOccurred())
			gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)
			err = testClient().Create(ctx, gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
		}

		BeforeEach(beforeEachCallback)

		It("wasmplugin config should update config", func(ctx SpecContext) {
			// Initial state
			// Gw A
			// Route A -> Gw A
			// Route B -> Gw A
			// RLP R -> Route A
			//
			// Switch targetRef
			// Gw A
			// Route A -> Gw A
			// Route B -> Gw A
			// RLP R -> Route B

			var (
				routeAName = "route-a"
				routeBName = "route-b"
				rlpName    = "rlp-r"
			)

			//
			// create Route A -> Gw A on *.a.example.com
			//
			httpRouteA := tests.BuildBasicHttpRoute(routeAName, TestGatewayName, testNamespace, []string{"*.a.example.com"})
			// GET /routeA
			httpRouteA.Spec.Rules = []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
								Value: ptr.To("/routeA"),
							},
							Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
						},
					},
				},
			}
			err := testClient().Create(ctx, httpRouteA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteA))).WithContext(ctx).Should(BeTrue())

			//
			// create Route B -> Gw A on *.b.example.com
			//
			httpRouteB := tests.BuildBasicHttpRoute(routeBName, TestGatewayName, testNamespace, []string{"*.b.example.com"})
			// GET /routeB
			httpRouteB.Spec.Rules = []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
								Value: ptr.To("/routeB"),
							},
							Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
						},
					},
				},
			}
			err = testClient().Create(ctx, httpRouteB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteB))).WithContext(ctx).Should(BeTrue())

			//
			// create RLP R -> Route A
			//
			rlpR := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpName, Namespace: testNamespace},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(routeAName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1beta3.Limit{
							"l1": {
								Rates: []kuadrantv1beta3.Rate{
									{
										Limit: 1, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
									},
								},
							},
						},
					},
				},
			}
			err = testClient().Create(ctx, rlpR)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: httpRouteA}
			pathID := kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRouteA.Spec.Rules[0], Name: "rule-1"},
			})

			// Initial state set.
			// Check wasm plugin has configuration from the route A
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{
					Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace,
				}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin could not be deserialized", "key", wasmPluginKey, "error", err)
					return false
				}

				expectedPlugin := &wasm.Config{
					Services: map[string]wasm.Service{
						wasm.RateLimitServiceName: {
							Endpoint:    common.KuadrantRateLimitClusterName,
							FailureMode: wasm.FailureModeAllow,
							Type:        wasm.RateLimitServiceType,
						},
					},
					ActionSets: []wasm.ActionSet{
						{
							Name: wasm.ActionSetNameForPath(pathID, 1, "*.a.example.com"),
							RouteRuleConditions: wasm.RouteRuleConditions{
								Hostnames: []string{"*.a.example.com"},
								Matches: []wasm.Predicate{
									{
										Selector: "request.method",
										Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
										Value:    "GET",
									},
									{
										Selector: "request.url_path",
										Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
										Value:    "/routeA",
									},
								},
							},
							Actions: []wasm.Action{
								{
									ServiceName: wasm.RateLimitServiceName,
									Scope:       controllers.LimitsNamespaceFromRoute(httpRouteA),
									Data: []wasm.DataType{
										{
											Value: &wasm.Static{
												Static: wasm.StaticSpec{
													Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "l1"),
													Value: "1",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				if !reflect.DeepEqual(existingWASMConfig, expectedPlugin) {
					diff := cmp.Diff(existingWASMConfig, expectedPlugin)
					logf.Log.V(1).Info("wasmplugin does not match", "key", wasmPluginKey, "diff", diff)
					return false
				}

				return true
			}).WithContext(ctx).Should(BeTrue())

			// Proceed with the update:
			// From RLP R -> Route A
			// To RLP R -> Route B
			Eventually(func(g Gomega) {
				rlpUpdated := &kuadrantv1beta3.RateLimitPolicy{}
				err = testClient().Get(ctx, client.ObjectKeyFromObject(rlpR), rlpUpdated)
				g.Expect(err).ToNot(HaveOccurred())
				rlpUpdated.Spec.TargetRef.Name = gatewayapiv1.ObjectName(routeBName)
				err = testClient().Update(ctx, rlpUpdated)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			mHTTPRoute = &machinery.HTTPRoute{HTTPRoute: httpRouteB}
			pathID = kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRouteB.Spec.Rules[0], Name: "rule-1"},
			})

			// Check wasm plugin has configuration from the route B
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{
					Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace,
				}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin could not be deserialized", "key", wasmPluginKey, "error", err)
					return false
				}

				expectedPlugin := &wasm.Config{
					Services: map[string]wasm.Service{
						wasm.RateLimitServiceName: {
							Endpoint:    common.KuadrantRateLimitClusterName,
							FailureMode: wasm.FailureModeAllow,
							Type:        wasm.RateLimitServiceType,
						},
					},
					ActionSets: []wasm.ActionSet{
						{
							Name: wasm.ActionSetNameForPath(pathID, 1, "*.b.example.com"),
							RouteRuleConditions: wasm.RouteRuleConditions{
								Hostnames: []string{"*.b.example.com"},
								Matches: []wasm.Predicate{
									{
										Selector: "request.method",
										Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
										Value:    "GET",
									},
									{
										Selector: "request.url_path",
										Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
										Value:    "/routeB",
									},
								},
							},
							Actions: []wasm.Action{
								{
									ServiceName: wasm.RateLimitServiceName,
									Scope:       controllers.LimitsNamespaceFromRoute(httpRouteA),
									Data: []wasm.DataType{
										{
											Value: &wasm.Static{
												Static: wasm.StaticSpec{
													Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "l1"),
													Value: "1",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				if !reflect.DeepEqual(existingWASMConfig, expectedPlugin) {
					diff := cmp.Diff(existingWASMConfig, expectedPlugin)
					logf.Log.V(1).Info("wasmplugin does not match", "key", wasmPluginKey, "diff", diff)
					return false
				}

				return true
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("Free Route gets dedicated RLP", func() {
		var (
			TestGatewayName = "toystore-gw"
			gatewayClass    *gatewayapiv1.GatewayClass
			gateway         *gatewayapiv1.Gateway
		)

		beforeEachCallback := func(ctx SpecContext) {
			gatewayClass = &gatewayapiv1.GatewayClass{}
			err := testClient().Get(ctx, types.NamespacedName{Name: tests.GatewayClassName}, gatewayClass)
			Expect(err).ToNot(HaveOccurred())
			gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)
			err = testClient().Create(ctx, gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
		}

		BeforeEach(beforeEachCallback)

		It("wasmplugin should update config", func(ctx SpecContext) {
			// Initial state
			// Gw A
			// Route A -> Gw A (free route, i.e. no rlp targeting it)
			// RLP 1 -> Gw A
			//
			// Add new RLP 2
			// Gw A
			// Route A -> Gw A
			// RLP 1 -> Gw A
			// RLP 2 -> Route A

			var (
				routeAName = "route-a"
				rlp1Name   = "rlp-1"
				rlp2Name   = "rlp-2"
			)

			//
			// create Route A -> Gw A on *.a.example.com
			//
			httpRouteA := tests.BuildBasicHttpRoute(routeAName, TestGatewayName, testNamespace, []string{"*.a.example.com"})
			// GET /routeA
			httpRouteA.Spec.Rules = []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
								Value: ptr.To("/routeA"),
							},
							Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
						},
					},
				},
			}
			err := testClient().Create(ctx, httpRouteA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteA))).WithContext(ctx).Should(BeTrue())

			// create RLP 1 -> Gw A
			rlp1 := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlp1Name, Namespace: testNamespace},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1beta3.Limit{
							"gatewaylimit": {
								Rates: []kuadrantv1beta3.Rate{
									{
										Limit: 1, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
									},
								},
							},
						},
					},
				},
			}
			err = testClient().Create(ctx, rlp1)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			rlp1Key := client.ObjectKey{Name: rlp1Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlp1Key)).WithContext(ctx).Should(BeTrue())

			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: httpRouteA}
			pathID := kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRouteA.Spec.Rules[0], Name: "rule-1"},
			})

			// Initial state set.
			// Check wasm plugin for gateway A has configuration from the route 1
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{
					Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace,
				}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin could not be deserialized", "key", wasmPluginKey, "error", err)
					return false
				}

				expectedPlugin := &wasm.Config{
					Services: map[string]wasm.Service{
						wasm.RateLimitServiceName: {
							Endpoint:    common.KuadrantRateLimitClusterName,
							FailureMode: wasm.FailureModeAllow,
							Type:        wasm.RateLimitServiceType,
						},
					},
					ActionSets: []wasm.ActionSet{
						{
							Name: wasm.ActionSetNameForPath(pathID, 1, "*.a.example.com"),
							RouteRuleConditions: wasm.RouteRuleConditions{
								Hostnames: []string{"*.a.example.com"},
								Matches: []wasm.Predicate{
									{
										Selector: "request.method",
										Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
										Value:    "GET",
									},
									{
										Selector: "request.url_path",
										Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
										Value:    "/routeA",
									},
								},
							},
							Actions: []wasm.Action{
								{
									ServiceName: wasm.RateLimitServiceName,
									Scope:       controllers.LimitsNamespaceFromRoute(httpRouteA),
									Data: []wasm.DataType{
										{
											Value: &wasm.Static{
												Static: wasm.StaticSpec{
													Key:   controllers.LimitNameToLimitadorIdentifier(rlp1Key, "gatewaylimit"),
													Value: "1",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				if !reflect.DeepEqual(existingWASMConfig, expectedPlugin) {
					diff := cmp.Diff(existingWASMConfig, expectedPlugin)
					logf.Log.V(1).Info("wasmplugin does not match", "key", wasmPluginKey, "diff", diff)
					return false
				}

				return true
			}).WithContext(ctx).Should(BeTrue())

			// Proceed with the update:
			// New RLP 2 -> Route A

			//
			// create RLP 2 -> Route A
			//
			rlp2 := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlp2Name, Namespace: testNamespace},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(routeAName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1beta3.Limit{
							"routelimit": {
								Rates: []kuadrantv1beta3.Rate{
									{
										Limit: 4, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
									},
								},
							},
						},
					},
				},
			}
			err = testClient().Create(ctx, rlp2)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			rlp2Key := client.ObjectKey{Name: rlp2Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlp2Key)).WithContext(ctx).Should(BeTrue())

			// Check wasm plugin has configuration from the route A and RLP 2.
			// RLP 1 should not add any config to the wasm plugin
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{
					Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace,
				}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin could not be deserialized", "key", wasmPluginKey, "error", err)
					return false
				}

				expectedPlugin := &wasm.Config{
					Services: map[string]wasm.Service{
						wasm.RateLimitServiceName: {
							Endpoint:    common.KuadrantRateLimitClusterName,
							FailureMode: wasm.FailureModeAllow,
							Type:        wasm.RateLimitServiceType,
						},
					},
					ActionSets: []wasm.ActionSet{
						{
							Name: wasm.ActionSetNameForPath(pathID, 1, "*.a.example.com"),
							RouteRuleConditions: wasm.RouteRuleConditions{
								Hostnames: []string{"*.a.example.com"},
								Matches: []wasm.Predicate{
									{
										Selector: "request.method",
										Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
										Value:    "GET",
									},
									{
										Selector: "request.url_path",
										Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
										Value:    "/routeA",
									},
								},
							},
							Actions: []wasm.Action{
								{
									ServiceName: wasm.RateLimitServiceName,
									Scope:       controllers.LimitsNamespaceFromRoute(httpRouteA),
									Data: []wasm.DataType{
										{
											Value: &wasm.Static{
												Static: wasm.StaticSpec{
													Key:   controllers.LimitNameToLimitadorIdentifier(rlp2Key, "routelimit"),
													Value: "1",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				if !reflect.DeepEqual(existingWASMConfig, expectedPlugin) {
					diff := cmp.Diff(existingWASMConfig, expectedPlugin)
					logf.Log.V(1).Info("wasmplugin does not match", "key", wasmPluginKey, "diff", diff)
					return false
				}

				return true
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("New free route on a Gateway with RLP", func() {
		var (
			TestGatewayName = "toystore-gw"
			gatewayClass    *gatewayapiv1.GatewayClass
			gateway         *gatewayapiv1.Gateway
		)

		beforeEachCallback := func(ctx SpecContext) {
			gatewayClass = &gatewayapiv1.GatewayClass{}
			err := testClient().Get(ctx, types.NamespacedName{Name: tests.GatewayClassName}, gatewayClass)
			Expect(err).ToNot(HaveOccurred())
			gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)
			err = testClient().Create(ctx, gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
		}

		BeforeEach(beforeEachCallback)

		It("wasmplugin should update config", func(ctx SpecContext) {
			// Initial state
			// Gw A
			// Route A -> Gw A
			// RLP 1 -> Gw A
			// RLP 2 -> Route A
			//
			// Add new Route B (free route, i.e. no rlp targeting it)
			// Gw A
			// Route A -> Gw A
			// Route B -> Gw A
			// RLP 1 -> Gw A
			// RLP 2 -> Route A

			var (
				routeAName = "route-a"
				routeBName = "route-b"
				rlp1Name   = "rlp-1"
				rlp2Name   = "rlp-2"
			)

			//
			// create Route A -> Gw A on *.a.example.com
			//
			httpRouteA := tests.BuildBasicHttpRoute(routeAName, TestGatewayName, testNamespace, []string{"*.a.example.com"})
			// GET /routeA
			httpRouteA.Spec.Rules = []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
								Value: ptr.To("/routeA"),
							},
							Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
						},
					},
				},
			}
			err := testClient().Create(ctx, httpRouteA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteA))).WithContext(ctx).Should(BeTrue())

			// create RLP 1 -> Gw A
			rlp1 := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlp1Name, Namespace: testNamespace},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1beta3.Limit{
							"gatewaylimit": {
								Rates: []kuadrantv1beta3.Rate{
									{
										Limit: 1, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
									},
								},
							},
						},
					},
				},
			}
			err = testClient().Create(ctx, rlp1)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			rlp1Key := client.ObjectKey{Name: rlp1Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlp1Key)).WithContext(ctx).Should(BeTrue())

			// create RLP 2 -> Route A
			rlp2 := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlp2Name, Namespace: testNamespace},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(routeAName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1beta3.Limit{
							"routelimit": {
								Rates: []kuadrantv1beta3.Rate{
									{
										Limit: 4, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
									},
								},
							},
						},
					},
				},
			}
			err = testClient().Create(ctx, rlp2)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			rlp2Key := client.ObjectKey{Name: rlp2Name, Namespace: testNamespace}
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlp2Key)).WithContext(ctx).Should(BeTrue())

			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: httpRouteA}
			pathID := kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRouteA.Spec.Rules[0], Name: "rule-1"},
			})

			// Initial state set.
			// Check wasm plugin for gateway A has configuration from the route A only affected by RLP 2
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{
					Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace,
				}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin could not be deserialized", "key", wasmPluginKey, "error", err)
					return false
				}

				expectedPlugin := &wasm.Config{
					Services: map[string]wasm.Service{
						wasm.RateLimitServiceName: {
							Endpoint:    common.KuadrantRateLimitClusterName,
							FailureMode: wasm.FailureModeAllow,
							Type:        wasm.RateLimitServiceType,
						},
					},
					ActionSets: []wasm.ActionSet{
						{
							Name: wasm.ActionSetNameForPath(pathID, 1, "*.a.example.com"),
							RouteRuleConditions: wasm.RouteRuleConditions{
								Hostnames: []string{"*.a.example.com"},
								Matches: []wasm.Predicate{
									{
										Selector: "request.method",
										Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
										Value:    "GET",
									},
									{
										Selector: "request.url_path",
										Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
										Value:    "/routeA",
									},
								},
							},
							Actions: []wasm.Action{
								{
									ServiceName: wasm.RateLimitServiceName,
									Scope:       controllers.LimitsNamespaceFromRoute(httpRouteA),
									Data: []wasm.DataType{
										{
											Value: &wasm.Static{
												Static: wasm.StaticSpec{
													Key:   controllers.LimitNameToLimitadorIdentifier(rlp2Key, "routelimit"),
													Value: "1",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				if !reflect.DeepEqual(existingWASMConfig, expectedPlugin) {
					diff := cmp.Diff(existingWASMConfig, expectedPlugin)
					logf.Log.V(1).Info("wasmplugin does not match", "key", wasmPluginKey, "diff", diff)
					return false
				}

				return true
			}).WithContext(ctx).Should(BeTrue())

			// Proceed with the update:
			// New Route B -> Gw A (free route, i.e. no rlp targeting it)

			//
			// create Route B -> Gw A on *.b.example.com
			//
			httpRouteB := tests.BuildBasicHttpRoute(routeBName, TestGatewayName, testNamespace, []string{"*.b.example.com"})
			// GET /routeB
			httpRouteB.Spec.Rules = []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
								Value: ptr.To("/routeB"),
							},
							Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
						},
					},
				},
			}
			err = testClient().Create(ctx, httpRouteB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRouteB))).WithContext(ctx).Should(BeTrue())

			// Check wasm plugin has configuration from:
			// - the route A with route level RLP 2
			// - the route B with gateway level RLP 1
			// it may take some reconciliation loops to get to that, so checking it with eventually
			Eventually(func() bool {
				wasmPluginKey := client.ObjectKey{
					Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace,
				}
				existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin not read", "key", wasmPluginKey, "error", err)
					return false
				}
				existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
				if err != nil {
					logf.Log.V(1).Info("wasmplugin could not be deserialized", "key", wasmPluginKey, "error", err)
					return false
				}

				mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: httpRouteB}
				pathID_B := kuadrantv1.PathID([]machinery.Targetable{
					&machinery.GatewayClass{GatewayClass: gatewayClass},
					mGateway,
					&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
					mHTTPRoute,
					&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRouteB.Spec.Rules[0], Name: "rule-1"},
				})

				expectedPlugin := &wasm.Config{
					Services: map[string]wasm.Service{
						wasm.RateLimitServiceName: {
							Endpoint:    common.KuadrantRateLimitClusterName,
							FailureMode: wasm.FailureModeAllow,
							Type:        wasm.RateLimitServiceType,
						},
					},
					ActionSets: []wasm.ActionSet{
						{
							Name: wasm.ActionSetNameForPath(pathID, 1, "*.a.example.com"),
							RouteRuleConditions: wasm.RouteRuleConditions{
								Hostnames: []string{"*.a.example.com"},
								Matches: []wasm.Predicate{
									{
										Selector: "request.method",
										Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
										Value:    "GET",
									},
									{
										Selector: "request.url_path",
										Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
										Value:    "/routeA",
									},
								},
							},
							Actions: []wasm.Action{
								{
									ServiceName: wasm.RateLimitServiceName,
									Scope:       controllers.LimitsNamespaceFromRoute(httpRouteA),
									Data: []wasm.DataType{
										{
											Value: &wasm.Static{
												Static: wasm.StaticSpec{
													Key:   controllers.LimitNameToLimitadorIdentifier(rlp2Key, "routelimit"),
													Value: "1",
												},
											},
										},
									},
								},
							},
						},
						{
							Name: wasm.ActionSetNameForPath(pathID_B, 1, "*.b.example.com"),
							RouteRuleConditions: wasm.RouteRuleConditions{
								Hostnames: []string{"*.b.example.com"},
								Matches: []wasm.Predicate{
									{
										Selector: "request.method",
										Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
										Value:    "GET",
									},
									{
										Selector: "request.url_path",
										Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
										Value:    "/routeB",
									},
								},
							},
							Actions: []wasm.Action{
								{
									ServiceName: wasm.RateLimitServiceName,
									Scope:       controllers.LimitsNamespaceFromRoute(httpRouteB),
									Data: []wasm.DataType{
										{
											Value: &wasm.Static{
												Static: wasm.StaticSpec{
													Key:   controllers.LimitNameToLimitadorIdentifier(rlp1Key, "gatewaylimit"),
													Value: "1",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				if !reflect.DeepEqual(existingWASMConfig, expectedPlugin) {
					diff := cmp.Diff(existingWASMConfig, expectedPlugin)
					logf.Log.V(1).Info("wasmplugin does not match", "key", wasmPluginKey, "diff", diff)
					return false
				}

				return true
			}).WithContext(ctx).Should(BeTrue())

		}, testTimeOut)
	})

	Context("Gateway with hostname in listener", func() {
		var (
			TestGatewayName = "toystore-gw"
			routeName       = "toystore-route"
			rlpName         = "rlp-a"
			gatewayClass    *gatewayapiv1.GatewayClass
			gateway         *gatewayapiv1.Gateway
			gwHostname      = "*.gw.example.com"
		)

		beforeEachCallback := func(ctx SpecContext) {
			gatewayClass = &gatewayapiv1.GatewayClass{}
			err := testClient().Get(ctx, types.NamespacedName{Name: tests.GatewayClassName}, gatewayClass)
			Expect(err).ToNot(HaveOccurred())
			gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)
			gateway.Spec.Listeners[0].Hostname = ptr.To(gatewayapiv1.Hostname(gwHostname))
			err = testClient().Create(ctx, gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
		}

		BeforeEach(beforeEachCallback)

		It("RLP with hostnames in route selector targeting hostname less HTTPRoute creates wasmplugin", func(ctx SpecContext) {
			// create httproute
			var emptyRouteHostnames []string
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, emptyRouteHostnames)
			err := testClient().Create(ctx, httpRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create ratelimitpolicy
			rlp := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: rlpName, Namespace: testNamespace},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(routeName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1beta3.Limit{
							"l1": {
								Rates: []kuadrantv1beta3.Rate{
									{
										Limit: 1, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
									},
								},
							},
						},
					},
				},
			}
			err = testClient().Create(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check RLP status is available
			rlpKey := client.ObjectKeyFromObject(rlp)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, rlpKey)).WithContext(ctx).Should(BeTrue())

			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: httpRoute}
			pathID := kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRoute.Spec.Rules[0], Name: "rule-1"},
			})

			// Check wasm plugin
			wasmPluginKey := client.ObjectKey{Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace}
			Eventually(tests.WasmPluginIsAvailable(ctx, testClient(), wasmPluginKey)).WithContext(ctx).Should(BeTrue())
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			err = testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig).To(Equal(&wasm.Config{
				Services: map[string]wasm.Service{
					wasm.RateLimitServiceName: {
						Endpoint:    common.KuadrantRateLimitClusterName,
						FailureMode: wasm.FailureModeAllow,
						Type:        wasm.RateLimitServiceType,
					},
				},
				ActionSets: []wasm.ActionSet{
					{
						Name: wasm.ActionSetNameForPath(pathID, 1, gwHostname),
						RouteRuleConditions: wasm.RouteRuleConditions{
							Hostnames: []string{gwHostname},
							Matches: []wasm.Predicate{
								{
									Selector: "request.method",
									Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
									Value:    "GET",
								},
								{
									Selector: "request.url_path",
									Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
									Value:    "/toy",
								},
							},
						},
						Actions: []wasm.Action{
							{
								ServiceName: wasm.RateLimitServiceName,
								Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
								Data: []wasm.DataType{
									{
										Value: &wasm.Static{
											Static: wasm.StaticSpec{
												Key:   controllers.LimitNameToLimitadorIdentifier(rlpKey, "l1"),
												Value: "1",
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

	Context("Gateway defaults & overrides", func() {
		var (
			routeName       = "toystore-route"
			gwRLPName       = "gw-rlp"
			routeRLPName    = "route-rlp"
			TestGatewayName = "toystore-gw"
			gatewayClass    *gatewayapiv1.GatewayClass
			gateway         *gatewayapiv1.Gateway
		)

		beforeEachCallback := func(ctx SpecContext) {
			gatewayClass = &gatewayapiv1.GatewayClass{}
			err := testClient().Get(ctx, types.NamespacedName{Name: tests.GatewayClassName}, gatewayClass)
			Expect(err).ToNot(HaveOccurred())
			gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)
			err = testClient().Create(ctx, gateway)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
		}

		expectedWasmPluginConfig := func(rlpKey client.ObjectKey, httpRoute *gatewayapiv1.HTTPRoute, key, hostname string) *wasm.Config {
			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: httpRoute}
			pathID := kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &httpRoute.Spec.Rules[0], Name: "rule-1"},
			})

			return &wasm.Config{
				Services: map[string]wasm.Service{
					wasm.RateLimitServiceName: {
						Endpoint:    common.KuadrantRateLimitClusterName,
						FailureMode: wasm.FailureModeAllow,
						Type:        wasm.RateLimitServiceType,
					},
				},
				ActionSets: []wasm.ActionSet{
					{
						Name: wasm.ActionSetNameForPath(pathID, 1, hostname),
						RouteRuleConditions: wasm.RouteRuleConditions{
							Hostnames: []string{hostname},
							Matches: []wasm.Predicate{
								{
									Selector: "request.method",
									Operator: wasm.PatternOperator(kuadrantv1beta3.EqualOperator),
									Value:    "GET",
								},
								{
									Selector: "request.url_path",
									Operator: wasm.PatternOperator(kuadrantv1beta3.StartsWithOperator),
									Value:    "/toy",
								},
							},
						},
						Actions: []wasm.Action{
							{
								ServiceName: wasm.RateLimitServiceName,
								Scope:       controllers.LimitsNamespaceFromRoute(httpRoute),
								Data: []wasm.DataType{
									{
										Value: &wasm.Static{
											Static: wasm.StaticSpec{
												Key:   key,
												Value: "1",
											},
										},
									},
								},
							},
						},
					},
				},
			}
		}

		BeforeEach(beforeEachCallback)

		It("Limit key shifts correctly from Gateway RLP default -> Route RLP -> Gateway RLP overrides", func(ctx SpecContext) {
			// create httproute
			httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
			Expect(testClient().Create(ctx, httpRoute)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

			// create GW ratelimitpolicy with defaults
			gwRLP := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: gwRLPName, Namespace: testNamespace},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "Gateway",
							Name:  gatewayapiv1.ObjectName(TestGatewayName),
						},
					},
					Defaults: &kuadrantv1beta3.MergeableRateLimitPolicySpec{
						RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
							Limits: map[string]kuadrantv1beta3.Limit{
								"gateway": {
									Rates: []kuadrantv1beta3.Rate{
										{
											Limit: 1, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient().Create(ctx, gwRLP)).To(Succeed())

			// Check RLP status is available
			gwRLPKey := client.ObjectKeyFromObject(gwRLP)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, gwRLPKey)).WithContext(ctx).Should(BeTrue())

			// Check wasm plugin
			wasmPluginKey := client.ObjectKey{Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace}
			Eventually(tests.WasmPluginIsAvailable(ctx, testClient(), wasmPluginKey)).WithContext(ctx).Should(BeTrue())
			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			// must exist
			Expect(testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)).To(Succeed())
			existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig).To(Equal(expectedWasmPluginConfig(gwRLPKey, httpRoute, controllers.LimitNameToLimitadorIdentifier(gwRLPKey, "gateway"), "*.example.com")))

			// Create Route RLP
			routeRLP := &kuadrantv1beta3.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta3.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: routeRLPName, Namespace: testNamespace},
				Spec: kuadrantv1beta3.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.GroupName,
							Kind:  "HTTPRoute",
							Name:  gatewayapiv1.ObjectName(routeName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1beta3.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1beta3.Limit{
							"route": {
								Rates: []kuadrantv1beta3.Rate{
									{
										Limit: 10, Duration: 3, Unit: kuadrantv1beta3.TimeUnit("minute"),
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient().Create(ctx, routeRLP)).To(Succeed())
			routeRLPKey := client.ObjectKeyFromObject(routeRLP)
			Eventually(assertPolicyIsAcceptedAndEnforced(ctx, routeRLPKey)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.RLPIsEnforced(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeFalse())
			// Wasm plugin config should now use route RLP limit key
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)).To(Succeed())
				existingWASMConfig, err = wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig).To(Equal(expectedWasmPluginConfig(routeRLPKey, httpRoute, controllers.LimitNameToLimitadorIdentifier(routeRLPKey, "route"), "*.example.com")))
			}).WithContext(ctx).Should(Succeed())

			// Update GW RLP to overrides
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, gwRLPKey, gwRLP)).To(Succeed())
				gwRLP.Spec.Overrides = gwRLP.Spec.Defaults.DeepCopy()
				gwRLP.Spec.Defaults = nil
				g.Expect(testClient().Update(ctx, gwRLP)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())
			Eventually(tests.RLPIsEnforced(ctx, testClient(), gwRLPKey)).WithContext(ctx).Should(BeTrue())
			Eventually(tests.RLPIsEnforced(ctx, testClient(), routeRLPKey)).WithContext(ctx).Should(BeFalse())
			// Wasm plugin config should now use GW RLP limit key for route
			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)).To(Succeed())
				existingWASMConfig, err = wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(existingWASMConfig).To(Equal(expectedWasmPluginConfig(routeRLPKey, httpRoute, controllers.LimitNameToLimitadorIdentifier(routeRLPKey, "gateway"), "*.example.com")))
			}).WithContext(ctx).Should(Succeed())

		}, testTimeOut)
	})
})
