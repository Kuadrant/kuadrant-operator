//go:build integration

package envoygateway_test

import (
	"context"
	"fmt"
	"time"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/kuadrant-operator/internal/extension"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Extension action EnvoyExtensionPolicy controller", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gwHost        = fmt.Sprintf("*.toystore-%s.com", rand.String(4))
		store         *extension.RegisteredDataStore
		policyID      extension.ResourceID
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())

		gateway := tests.NewGatewayBuilder(TestGatewayName, tests.GatewayClassName, testNamespace).
			WithHTTPListener("test-listener", gwHost).
			Gateway
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
		It("injects extension action into EnvoyExtensionPolicy", func(ctx SpecContext) {
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
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{gwHost})
			Expect(testClient().Create(ctx, route)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			// Verify EnvoyExtensionPolicy is created and contains the extension action
			extKey := client.ObjectKey{
				Name:      wasm.ExtensionName(TestGatewayName),
				Namespace: testNamespace,
			}

			Eventually(func(g Gomega, ctx context.Context) {
				ext := &egv1alpha1.EnvoyExtensionPolicy{}
				g.Expect(testClient().Get(ctx, extKey, ext)).To(Succeed())
				g.Expect(ext.Spec.Wasm).To(HaveLen(1))

				existingWASMConfig, err := wasm.ConfigFromJSON(ext.Spec.Wasm[0].Config)
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
				g.Expect(foundAction).To(BeTrue(), "expected extension action to be injected into EnvoyExtensionPolicy action sets")
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
