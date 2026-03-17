//go:build integration

package envoygateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/extension"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Upstream cluster EnvoyPatchPolicy controller", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gwHost        = fmt.Sprintf("*.toystore-%s.com", rand.String(4))
		gateway       *gatewayapiv1.Gateway
		store         *extension.RegisteredDataStore
		policyID      extension.ResourceID
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())

		// Create the gateway first so it is in the topology before the test runs
		gateway = tests.NewGatewayBuilder(TestGatewayName, tests.GatewayClassName, testNamespace).
			WithHTTPListener("test-listener", gwHost).
			Gateway
		Expect(testClient().Create(ctx, gateway)).To(Succeed())
		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

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
		It("creates and cleans up EnvoyPatchPolicy for upstream clusters", func(ctx SpecContext) {
			// Populate the store with an upstream entry, then create an HTTPRoute
			// to trigger reconciliation. The gateway is already in the topology
			// (created in BeforeEach).
			upstreamKey := extension.RegisteredUpstreamKey{
				Policy: policyID,
				URL:    "grpc://test-upstream.example.com:50051",
			}
			store.SetUpstream(upstreamKey, extension.RegisteredUpstreamEntry{
				URL:         "grpc://test-upstream.example.com:50051",
				ClusterName: "test-upstream-cluster",
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
			route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{gwHost})
			Expect(testClient().Create(ctx, route)).To(Succeed())
			Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

			// Verify the upstream EnvoyPatchPolicy is created
			patchKey := client.ObjectKey{
				Name:      controllers.UpstreamClusterName(TestGatewayName),
				Namespace: testNamespace,
			}
			patch := &egv1alpha1.EnvoyPatchPolicy{}
			Eventually(func(g Gomega, ctx context.Context) {
				g.Expect(testClient().Get(ctx, patchKey, patch)).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// Verify the patch targets the gateway
			Expect(patch.Spec.TargetRef.Group).To(Equal(gatewayapiv1.Group("gateway.networking.k8s.io")))
			Expect(patch.Spec.TargetRef.Kind).To(Equal(gatewayapiv1.Kind("Gateway")))
			Expect(patch.Spec.TargetRef.Name).To(Equal(gatewayapiv1.ObjectName(gateway.Name)))
			Expect(patch.Spec.Type).To(Equal(egv1alpha1.JSONPatchEnvoyPatchType))
			Expect(patch.Spec.JSONPatches).To(HaveLen(1))
			Expect(patch.Spec.JSONPatches[0].Type).To(Equal(egv1alpha1.ClusterEnvoyResourceType))
			Expect(patch.Spec.JSONPatches[0].Name).To(Equal("test-upstream-cluster"))
			Expect(patch.Spec.JSONPatches[0].Operation.Op).To(Equal(egv1alpha1.JSONPatchOperationType("add")))

			// Verify the patch value contains the correct cluster configuration
			patchValueBytes, err := patch.Spec.JSONPatches[0].Operation.Value.MarshalJSON()
			Expect(err).ToNot(HaveOccurred())
			var patchValue map[string]any
			Expect(json.Unmarshal(patchValueBytes, &patchValue)).ToNot(HaveOccurred())
			Expect(patchValue["name"]).To(Equal("test-upstream-cluster"))
			Expect(patchValue["type"]).To(Equal("STRICT_DNS"))

			// Verify the upstream EnvoyPatchPolicy has the correct labels
			Expect(patch.Labels).To(HaveKeyWithValue("kuadrant.io/upstream", "true"))

			// Clear the store to simulate upstream removal, then trigger re-reconciliation
			// by deleting the HTTPRoute (a subscribed event for the workflow)
			store.ClearPolicyData(policyID)
			Expect(testClient().Delete(ctx, route)).To(Succeed())

			// Verify the upstream EnvoyPatchPolicy is cleaned up
			Eventually(func() bool {
				err := testClient().Get(ctx, patchKey, &egv1alpha1.EnvoyPatchPolicy{})
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})
})
