//go:build integration

package envoygateway_test

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("wasm controller", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gwHost        = fmt.Sprintf("*.toystore-%s.com", rand.String(4))
		gatewayClass  *gatewayapiv1.GatewayClass
		gateway       *gatewayapiv1.Gateway
		logger        logr.Logger
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gatewayClass = &gatewayapiv1.GatewayClass{}
		err := testClient().Get(ctx, types.NamespacedName{Name: tests.GatewayClassName}, gatewayClass)
		Expect(err).ToNot(HaveOccurred())
		gateway = tests.NewGatewayBuilder(TestGatewayName, tests.GatewayClassName, testNamespace).
			WithHTTPListener("test-listener", gwHost).
			Gateway
		err = testClient().Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())
		logger = controller.LoggerFromContext(ctx).WithName("EnvoyExtensionReconcilerTest")

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

	Context("RateLimitPolicy attached to the gateway", func() {

		var (
			gwPolicy      *kuadrantv1.RateLimitPolicy
			gwRoute       *gatewayapiv1.HTTPRoute
			actionSetName string
		)

		BeforeEach(func(ctx SpecContext) {
			gwRoute = tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err := testClient().Create(ctx, gwRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(gwRoute))).WithContext(ctx).Should(BeTrue())

			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: gwRoute}
			pathID := kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &gwRoute.Spec.Rules[0], Name: "rule-1"},
			})
			actionSetName = wasm.ActionSetNameForPath(pathID, 0, string(gwRoute.Spec.Hostnames[0]))

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

		It("Creates envoyextensionpolicy", func(ctx SpecContext) {
			extKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}

			gwPolicyKey := client.ObjectKeyFromObject(gwPolicy)

			Eventually(IsEnvoyExtensionPolicyAccepted).
				WithContext(ctx).
				WithArguments(testClient(), extKey, client.ObjectKeyFromObject(gateway)).
				Should(Succeed())

			ext := &egv1alpha1.EnvoyExtensionPolicy{}
			err := testClient().Get(ctx, extKey, ext)
			// must exist
			Expect(err).ToNot(HaveOccurred())

			Expect(ext.Spec.PolicyTargetReferences.TargetRefs).To(HaveLen(1))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs[0].LocalPolicyTargetReference.Group).To(Equal(gatewayapiv1.Group("gateway.networking.k8s.io")))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs[0].LocalPolicyTargetReference.Kind).To(Equal(gatewayapiv1.Kind("Gateway")))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs[0].LocalPolicyTargetReference.Name).To(Equal(gatewayapiv1.ObjectName(gateway.Name)))
			Expect(ext.Spec.Wasm).To(HaveLen(1))
			Expect(ext.Spec.Wasm[0].Code.Type).To(Equal(egv1alpha1.ImageWasmCodeSourceType))
			Expect(ext.Spec.Wasm[0].Code.Image).To(Not(BeNil()))
			Expect(ext.Spec.Wasm[0].Code.Image.URL).To(Equal(controllers.WASMFilterImageURL))
			existingWASMConfig, err := wasm.ConfigFromJSON(ext.Spec.Wasm[0].Config)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig).To(Equal(&wasm.Config{
				Services: map[string]wasm.Service{
					wasm.AuthServiceName: {
						Type:        wasm.AuthServiceType,
						Endpoint:    kuadrant.KuadrantAuthClusterName,
						FailureMode: wasm.AuthServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.AuthServiceTimeout()),
					},
					wasm.RateLimitCheckServiceName: {
						Type:        wasm.RateLimitCheckServiceType,
						Endpoint:    kuadrant.KuadrantRateLimitClusterName,
						FailureMode: wasm.RatelimitCheckServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.RatelimitCheckServiceTimeout()),
					},
					wasm.RateLimitServiceName: {
						Type:        wasm.RateLimitServiceType,
						Endpoint:    kuadrant.KuadrantRateLimitClusterName,
						FailureMode: wasm.RatelimitServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.RatelimitServiceTimeout()),
					},
					wasm.RateLimitReportServiceName: {
						Type:        wasm.RateLimitReportServiceType,
						Endpoint:    kuadrant.KuadrantRateLimitClusterName,
						FailureMode: wasm.RatelimitReportServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.RatelimitReportServiceTimeout()),
					},
				},
				ActionSets: []wasm.ActionSet{
					{
						Name: actionSetName,
						RouteRuleConditions: wasm.RouteRuleConditions{
							Hostnames: []string{string(gwRoute.Spec.Hostnames[0])},
							Predicates: []string{
								"request.method == 'GET'",
								"request.url_path.startsWith('/toy')",
							},
						},
						Actions: []wasm.Action{
							{
								ServiceName: wasm.RateLimitServiceName,
								Scope:       controllers.LimitsNamespaceFromRoute(gwRoute),
								Data: []wasm.DataType{
									{
										Value: &wasm.Expression{
											ExpressionItem: wasm.ExpressionItem{
												Key:   controllers.LimitNameToLimitadorIdentifier(gwPolicyKey, "l1"),
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

		It("Deletes envoyextensionpolicy when rate limit policy is deleted", func(ctx SpecContext) {
			gwPolicyKey := client.ObjectKeyFromObject(gwPolicy)
			err := testClient().Delete(ctx, gwPolicy)
			logf.Log.V(1).Info("Deleting RateLimitPolicy", "key", gwPolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			extKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(func() bool {
				err := testClient().Get(ctx, extKey, &egv1alpha1.EnvoyExtensionPolicy{})
				logf.Log.V(1).Info("Fetching EnvoyExtensionPolicy", "key", extKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Deletes envoyextensionpolicy if gateway is deleted", func(ctx SpecContext) {
			err := testClient().Delete(ctx, gateway)
			logf.Log.V(1).Info("Deleting Gateway", "key", client.ObjectKeyFromObject(gateway).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			extKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(func() bool {
				err := testClient().Get(ctx, extKey, &egv1alpha1.EnvoyExtensionPolicy{})
				logf.Log.V(1).Info("Fetching EnvoyExtensionPolicy", "key", extKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("RateLimitPolicy attached to the route", func() {

		var (
			routePolicy   *kuadrantv1.RateLimitPolicy
			gwRoute       *gatewayapiv1.HTTPRoute
			actionSetName string
		)

		BeforeEach(func(ctx SpecContext) {
			gwRoute = tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
			err := testClient().Create(ctx, gwRoute)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(gwRoute))).WithContext(ctx).Should(BeTrue())

			mGateway := &machinery.Gateway{Gateway: gateway}
			mHTTPRoute := &machinery.HTTPRoute{HTTPRoute: gwRoute}
			pathID := kuadrantv1.PathID([]machinery.Targetable{
				&machinery.GatewayClass{GatewayClass: gatewayClass},
				mGateway,
				&machinery.Listener{Listener: &gateway.Spec.Listeners[0], Gateway: mGateway},
				mHTTPRoute,
				&machinery.HTTPRouteRule{HTTPRoute: mHTTPRoute, HTTPRouteRule: &gwRoute.Spec.Rules[0], Name: "rule-1"},
			})
			actionSetName = wasm.ActionSetNameForPath(pathID, 0, string(gwRoute.Spec.Hostnames[0]))

			routePolicy = policyFactory(func(policy *kuadrantv1.RateLimitPolicy) {
				policy.Name = "route"
				policy.Spec.TargetRef.Group = gatewayapiv1.GroupName
				policy.Spec.TargetRef.Kind = "HTTPRoute"
				policy.Spec.TargetRef.Name = TestHTTPRouteName
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

			err = testClient().Create(ctx, routePolicy)
			logf.Log.V(1).Info("Creating RateLimitPolicy", "key", client.ObjectKeyFromObject(routePolicy).String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			// check policy status
			Eventually(tests.IsRLPAcceptedAndEnforced).
				WithContext(ctx).
				WithArguments(testClient(), client.ObjectKeyFromObject(routePolicy)).Should(Succeed())
		})

		It("Creates envoyextensionpolicy", func(ctx SpecContext) {
			extKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}

			routePolicyKey := client.ObjectKeyFromObject(routePolicy)

			Eventually(IsEnvoyExtensionPolicyAccepted).
				WithContext(ctx).
				WithArguments(testClient(), extKey, client.ObjectKeyFromObject(gateway)).
				Should(Succeed())

			ext := &egv1alpha1.EnvoyExtensionPolicy{}
			err := testClient().Get(ctx, extKey, ext)
			// must exist
			Expect(err).ToNot(HaveOccurred())

			Expect(gwRoute.Spec.Hostnames).To(Not(BeEmpty()))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs).To(HaveLen(1))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs[0].LocalPolicyTargetReference.Group).To(Equal(gatewayapiv1.Group("gateway.networking.k8s.io")))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs[0].LocalPolicyTargetReference.Kind).To(Equal(gatewayapiv1.Kind("Gateway")))
			Expect(ext.Spec.PolicyTargetReferences.TargetRefs[0].LocalPolicyTargetReference.Name).To(Equal(gatewayapiv1.ObjectName(gateway.Name)))
			Expect(ext.Spec.Wasm).To(HaveLen(1))
			Expect(ext.Spec.Wasm[0].Code.Type).To(Equal(egv1alpha1.ImageWasmCodeSourceType))
			Expect(ext.Spec.Wasm[0].Code.Image).To(Not(BeNil()))
			Expect(ext.Spec.Wasm[0].Code.Image.URL).To(Equal(controllers.WASMFilterImageURL))
			existingWASMConfig, err := wasm.ConfigFromJSON(ext.Spec.Wasm[0].Config)
			Expect(err).ToNot(HaveOccurred())
			Expect(existingWASMConfig).To(Equal(&wasm.Config{
				Services: map[string]wasm.Service{
					wasm.AuthServiceName: {
						Type:        wasm.AuthServiceType,
						Endpoint:    kuadrant.KuadrantAuthClusterName,
						FailureMode: wasm.AuthServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.AuthServiceTimeout()),
					},
					wasm.RateLimitCheckServiceName: {
						Type:        wasm.RateLimitCheckServiceType,
						Endpoint:    kuadrant.KuadrantRateLimitClusterName,
						FailureMode: wasm.RatelimitCheckServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.RatelimitCheckServiceTimeout()),
					},
					wasm.RateLimitServiceName: {
						Type:        wasm.RateLimitServiceType,
						Endpoint:    kuadrant.KuadrantRateLimitClusterName,
						FailureMode: wasm.RatelimitServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.RatelimitServiceTimeout()),
					},
					wasm.RateLimitReportServiceName: {
						Type:        wasm.RateLimitReportServiceType,
						Endpoint:    kuadrant.KuadrantRateLimitClusterName,
						FailureMode: wasm.RatelimitReportServiceFailureMode(&logger),
						Timeout:     ptr.To(wasm.RatelimitReportServiceTimeout()),
					},
				},
				ActionSets: []wasm.ActionSet{
					{
						Name: actionSetName,
						RouteRuleConditions: wasm.RouteRuleConditions{
							Hostnames: []string{string(gwRoute.Spec.Hostnames[0])},
							Predicates: []string{
								"request.method == 'GET'",
								"request.url_path.startsWith('/toy')",
							},
						},
						Actions: []wasm.Action{
							{
								ServiceName: wasm.RateLimitServiceName,
								Scope:       controllers.LimitsNamespaceFromRoute(gwRoute),
								Data: []wasm.DataType{
									{
										Value: &wasm.Expression{
											ExpressionItem: wasm.ExpressionItem{
												Key:   controllers.LimitNameToLimitadorIdentifier(routePolicyKey, "l1"),
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

		It("Deletes envoyextensionpolicy when rate limit policy is deleted", func(ctx SpecContext) {
			routePolicyKey := client.ObjectKeyFromObject(routePolicy)
			err := testClient().Delete(ctx, routePolicy)
			logf.Log.V(1).Info("Deleting RateLimitPolicy", "key", routePolicyKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			extKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(func() bool {
				err := testClient().Get(ctx, extKey, &egv1alpha1.EnvoyExtensionPolicy{})
				logf.Log.V(1).Info("Fetching EnvoyExtensionPolicy", "key", extKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Deletes envoyextensionpolicy if route is deleted", func(ctx SpecContext) {
			gwRouteKey := client.ObjectKeyFromObject(gwRoute)
			err := testClient().Delete(ctx, gwRoute)
			logf.Log.V(1).Info("Deleting Route", "key", gwRouteKey.String(), "error", err)
			Expect(err).ToNot(HaveOccurred())

			extKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(func() bool {
				err := testClient().Get(ctx, extKey, &egv1alpha1.EnvoyExtensionPolicy{})
				logf.Log.V(1).Info("Fetching EnvoyExtensionPolicy", "key", extKey.String(), "error", err)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})
})
