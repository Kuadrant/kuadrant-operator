//go:build integration

package istio_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/kuadrant-operator/internal/extension"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Extension action WasmPlugin controller", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		store         *extension.RegisteredDataStore
		policyID      extension.ResourceID
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())

		gateway := tests.BuildBasicGateway(TestGatewayName, testNamespace)
		Expect(testClient().Create(ctx, gateway)).To(Succeed())
		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

		store = extension.NewRegisteredDataStore()
		policyID = extension.ResourceID{
			Kind:      "ThreatPolicy",
			Namespace: testNamespace,
			Name:      "tp-1",
		}
		mutator := extension.NewRegisteredDataMutator[*wasm.Config](store)
		extension.GlobalMutatorRegistry.RegisterWasmConfigMutator(mutator)
	})

	AfterEach(func(ctx SpecContext) {
		store.ClearPolicyData(policyID)
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("with registered upstream and action targeting Gateway", func() {
		It("injects extension action into WasmPlugin", func(ctx SpecContext) {
			targetRef := extension.TargetRef{
				Group:     "gateway.networking.k8s.io",
				Kind:      "Gateway",
				Name:      TestGatewayName,
				Namespace: testNamespace,
			}

			// Register upstream
			store.SetUpstream(
				extension.RegisteredUpstreamKey{Policy: policyID, URL: "grpc://threat-svc:8080"},
				extension.RegisteredUpstreamEntry{
					ClusterName: "ext-threat-svc-8080",
					Host:        "threat-svc",
					Port:        8080,
					TargetRef:   targetRef,
					FailureMode: "deny",
					Timeout:     "100ms",
				},
			)

			// Register action
			store.SetAction(
				extension.RegisteredActionKey{Policy: policyID, Scope: "threat-assess"},
				extension.RegisteredActionEntry{
					ServiceName:         "ext-threat-svc-8080",
					Scope:               "threat-assess",
					Dispatch:            `threat.v1.AssessRequest{uri: request.url_path}`,
					ResponsePredicate:   "service.response.threat_level < 5",
					DenialResponse:      &extension.ActionDenialResponse{StatusCode: 403, Headers: map[string]string{"X-Deny-Reason": "threat"}, Body: "blocked"},
					TargetRef:           targetRef,
					SourcePolicyLocator: "ThreatPolicy/default/tp-1",
					FailureMode:         "deny",
					Timeout:             "100ms",
				},
			)

			// Create HTTPRoute to trigger reconciliation
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.toystore.com"})
			Expect(testClient().Create(ctx, route)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			// Verify WasmPlugin is created and contains the extension action
			wasmPluginKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}
			Eventually(tests.WasmPluginIsAvailable(ctx, testClient(), wasmPluginKey)).WithContext(ctx).Should(BeTrue())

			existingWasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{}
			Eventually(func(g Gomega, ctx context.Context) {
				g.Expect(testClient().Get(ctx, wasmPluginKey, existingWasmPlugin)).To(Succeed())
				existingWASMConfig, err := wasm.ConfigFromStruct(existingWasmPlugin.Spec.PluginConfig)
				g.Expect(err).ToNot(HaveOccurred())

				// Verify service was injected for the upstream
				g.Expect(existingWASMConfig.Services).ToNot(BeEmpty())

				// Verify extension action appears in action sets
				foundAction := false
				for _, actionSet := range existingWASMConfig.ActionSets {
					for _, action := range actionSet.Actions {
						if action.Dispatch == `threat.v1.AssessRequest{uri: request.url_path}` {
							foundAction = true
							g.Expect(action.ResponsePredicate).To(Equal("service.response.threat_level < 5"))
							g.Expect(action.DenialResponse).ToNot(BeNil())
							g.Expect(action.DenialResponse.StatusCode).To(Equal(uint32(403)))
							g.Expect(action.SourcePolicyLocators).To(ContainElement("ThreatPolicy/default/tp-1"))
						}
					}
				}
				g.Expect(foundAction).To(BeTrue(), "expected extension action to be injected into WasmPlugin action sets")
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("removes extension action from WasmPlugin on ClearPolicy", func(ctx SpecContext) {
			targetRef := extension.TargetRef{
				Group:     "gateway.networking.k8s.io",
				Kind:      "Gateway",
				Name:      TestGatewayName,
				Namespace: testNamespace,
			}

			store.SetUpstream(
				extension.RegisteredUpstreamKey{Policy: policyID, URL: "grpc://threat-svc:8080"},
				extension.RegisteredUpstreamEntry{
					ClusterName: "ext-threat-svc-8080",
					Host:        "threat-svc",
					Port:        8080,
					TargetRef:   targetRef,
					FailureMode: "deny",
					Timeout:     "100ms",
				},
			)
			store.SetAction(
				extension.RegisteredActionKey{Policy: policyID, Scope: "threat-assess"},
				extension.RegisteredActionEntry{
					ServiceName:         "ext-threat-svc-8080",
					Scope:               "threat-assess",
					Dispatch:            `threat.v1.AssessRequest{uri: request.url_path}`,
					ResponsePredicate:   "service.response.threat_level < 5",
					TargetRef:           targetRef,
					SourcePolicyLocator: "ThreatPolicy/default/tp-1",
					FailureMode:         "deny",
					Timeout:             "100ms",
				},
			)

			// Create HTTPRoute to trigger reconciliation
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.toystore.com"})
			Expect(testClient().Create(ctx, route)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			wasmPluginKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}
			Eventually(tests.WasmPluginIsAvailable(ctx, testClient(), wasmPluginKey)).WithContext(ctx).Should(BeTrue())

			// Clear the policy data and trigger re-reconciliation
			store.ClearPolicyData(policyID)
			Expect(testClient().Delete(ctx, route)).To(Succeed())

			// Verify no extension actions remain in the WasmPlugin (or it is deleted entirely)
			Eventually(func(g Gomega, ctx context.Context) {
				wp := &istioclientgoextensionv1alpha1.WasmPlugin{}
				err := testClient().Get(ctx, wasmPluginKey, wp)
				if err != nil {
					// WasmPlugin deleted entirely is also acceptable
					return
				}
				wasmConfig, err := wasm.ConfigFromStruct(wp.Spec.PluginConfig)
				g.Expect(err).ToNot(HaveOccurred())
				for _, actionSet := range wasmConfig.ActionSets {
					for _, action := range actionSet.Actions {
						g.Expect(action.Dispatch).To(BeEmpty(), "extension action should have been removed")
					}
				}
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
