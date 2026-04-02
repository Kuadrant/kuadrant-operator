//go:build integration

package istio_test

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/extension"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Upstream cluster EnvoyFilter controller", Serial, func() {
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

		// Create the gateway first so it is in the topology before the test runs
		gateway := tests.BuildBasicGateway(TestGatewayName, testNamespace)
		Expect(testClient().Create(ctx, gateway)).To(Succeed())
		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

		// Set up a RegisteredDataStore and register a mutator with the global registry.
		// The store is populated per-test; clearing it in AfterEach makes the mutator a no-op.
		store = extension.NewRegisteredDataStore()
		policyID = extension.ResourceID{
			Kind:      "UpstreamPolicy",
			Namespace: testNamespace,
			Name:      "test-upstream",
		}
		mutator := extension.NewRegisteredDataMutator[*wasm.Config](store)
		extension.GlobalMutatorRegistry.RegisterWasmConfigMutator(mutator)
	})

	AfterEach(func(ctx SpecContext) {
		store.ClearPolicyData(policyID)
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("with registered upstream targeting Gateway", func() {
		It("creates and cleans up EnvoyFilter for upstream clusters", func(ctx SpecContext) {
			// Populate the store with an upstream entry, then create an HTTPRoute
			// to trigger reconciliation. The gateway is already in the topology
			// (created in BeforeEach).
			upstreamKey := extension.RegisteredUpstreamKey{
				Policy: policyID,
				URL:    "grpc://test-upstream.example.com:50051",
			}
			store.SetUpstream(upstreamKey, extension.RegisteredUpstreamEntry{
				ClusterName: "test-upstream-cluster",
				Host:        "test-upstream.example.com",
				Port:        50051,
				TargetRef: extension.TargetRef{
					Group:     "gateway.networking.k8s.io",
					Kind:      "Gateway",
					Name:      TestGatewayName,
					Namespace: testNamespace,
				},
				FailureMode: "deny",
				Timeout:     "10s",
			})

			// Create an HTTPRoute to trigger the data plane policies workflow
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.toystore.com"})
			Expect(testClient().Create(ctx, route)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			// Verify the upstream EnvoyFilter is created
			efKey := client.ObjectKey{
				Name:      controllers.UpstreamClusterName(TestGatewayName),
				Namespace: testNamespace,
			}
			existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
			Eventually(func(g Gomega, ctx context.Context) {
				g.Expect(testClient().Get(ctx, efKey, existingEF)).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// Verify the patches contain both descriptor service and upstream cluster
			Expect(existingEF.Spec.ConfigPatches).To(HaveLen(2))

			// Extract cluster names from patches
			var clusterNames []string
			for _, patch := range existingEF.Spec.ConfigPatches {
				Expect(patch.Patch).NotTo(BeNil())
				Expect(patch.Patch.Value).NotTo(BeNil())

				patchValueRaw, err := json.Marshal(patch.Patch.Value)
				Expect(err).ToNot(HaveOccurred())
				var patchValue map[string]any
				Expect(json.Unmarshal(patchValueRaw, &patchValue)).ToNot(HaveOccurred())

				clusterName := patchValue["name"].(string)
				clusterNames = append(clusterNames, clusterName)
				Expect(patchValue["type"]).To(Equal("STRICT_DNS"))

				// Verify descriptor service endpoint configuration
				if clusterName == "kuadrant-operator-grpc" {
					loadAssignment := patchValue["load_assignment"].(map[string]any)
					endpoints := loadAssignment["endpoints"].([]any)
					Expect(endpoints).To(HaveLen(1))

					endpoint := endpoints[0].(map[string]any)
					lbEndpoints := endpoint["lb_endpoints"].([]any)
					Expect(lbEndpoints).To(HaveLen(1))

					lbEndpoint := lbEndpoints[0].(map[string]any)
					endpointAddr := lbEndpoint["endpoint"].(map[string]any)
					address := endpointAddr["address"].(map[string]any)
					socketAddress := address["socket_address"].(map[string]any)

					Expect(socketAddress["address"]).To(Equal("kuadrant-operator-grpc.kuadrant-system.svc.cluster.local"))
					Expect(socketAddress["port_value"]).To(BeNumerically("==", 50051))
				}
			}

			// Verify both clusters are present
			Expect(clusterNames).To(ContainElement("test-upstream-cluster"))
			Expect(clusterNames).To(ContainElement("kuadrant-operator-grpc"))

			// Verify the upstream EnvoyFilter has the correct labels
			Expect(existingEF.Labels).To(HaveKeyWithValue("kuadrant.io/upstream", "true"))

			// Clear the store to simulate upstream removal, then trigger re-reconciliation
			// by deleting the HTTPRoute (a subscribed event for the workflow)
			store.ClearPolicyData(policyID)
			Expect(testClient().Delete(ctx, route)).To(Succeed())

			// Verify the upstream EnvoyFilter is cleaned up
			Eventually(func() bool {
				err := testClient().Get(ctx, efKey, &istioclientnetworkingv1alpha3.EnvoyFilter{})
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})
})
