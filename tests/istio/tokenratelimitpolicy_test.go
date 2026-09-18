//go:build integration

package istio_test

import (
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/types/known/structpb"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istioclientgonetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	"github.com/kuadrant/kuadrant-operator/tests"
)

// extractWasmConfigFromEnvoyFilter is shared with extension_reconciler_test.go.
func extractWasmConfigFromEnvoyFilter(ef *istioclientgonetworkingv1alpha3.EnvoyFilter) (*wasm.Config, error) {
	if len(ef.Spec.ConfigPatches) == 0 {
		return nil, fmt.Errorf("no config patches found in EnvoyFilter")
	}

	// Find the wasm HTTP filter patch
	var patchValue *structpb.Struct
	for _, patch := range ef.Spec.ConfigPatches {
		if patch.ApplyTo == istioapinetworkingv1alpha3.EnvoyFilter_HTTP_FILTER {
			patchValue = patch.Patch.Value
			break
		}
	}

	if patchValue == nil {
		return nil, fmt.Errorf("no HTTP_FILTER patch found in EnvoyFilter config patches")
	}

	// Marshal to JSON to navigate the nested structure
	valueJSON, err := patchValue.MarshalJSON()
	if err != nil {
		return nil, err
	}

	// Unmarshal to a map to extract the wasm configuration
	var filterConfig map[string]any
	if err := json.Unmarshal(valueJSON, &filterConfig); err != nil {
		return nil, err
	}

	// Navigate: typed_config -> value -> config -> configuration (JSON string)
	typedConfig, ok := filterConfig["typed_config"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing typed_config in filter configuration")
	}

	value, ok := typedConfig["value"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing value in typed_config")
	}

	config, ok := value["config"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing config in typed_config.value")
	}

	configuration, ok := config["configuration"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing configuration in config")
	}

	configJSON, ok := configuration["value"].(string)
	if !ok {
		return nil, fmt.Errorf("missing value in configuration")
	}

	var wasmConfig wasm.Config
	if err := json.Unmarshal([]byte(configJSON), &wasmConfig); err != nil {
		return nil, err
	}

	return &wasmConfig, nil
}

var _ = Describe("TokenRateLimitPolicy enforcement modes", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(3 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		routeName     = "toystore-route"
		trlpName      = "toystore-trlp"
		gateway       *gatewayapiv1.Gateway
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)
		err := testClient().Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())
		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	buildTRLP := func() *kuadrantv1alpha1.TokenRateLimitPolicy {
		return &kuadrantv1alpha1.TokenRateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "TokenRateLimitPolicy", APIVersion: kuadrantv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: trlpName, Namespace: testNamespace},
			Spec: kuadrantv1alpha1.TokenRateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "HTTPRoute",
						Name:  gatewayapiv1.ObjectName(routeName),
					},
				},
				TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
					Limits: map[string]kuadrantv1alpha1.TokenLimit{
						"l1": {
							Rates: []kuadrantv1.Rate{
								{Limit: 5000, Window: kuadrantv1.Duration("1m")},
							},
						},
					},
				},
			},
		}
	}

	It("Reservation mode (default) creates Reserve/Commit actions", func(ctx SpecContext) {
		// create httproute (no backendRequest timeout, so reservation ttl stays unset)
		httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
		err := testClient().Create(ctx, httpRoute)
		Expect(err).ToNot(HaveOccurred())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

		// create tokenratelimitpolicy targeting the route, with no reservation
		// overrides, so the reconciler generates the RFC 0021 defaults: a flat
		// amount and no ttl (no route backendRequest timeout to fall back to).
		trlp := buildTRLP()
		err = testClient().Create(ctx, trlp)
		Expect(err).ToNot(HaveOccurred())

		trlpKey := client.ObjectKeyFromObject(trlp)
		Eventually(tests.TokenRateLimitPolicyIsAccepted(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())
		Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())

		// Check envoy filter
		envoyFilterKey := client.ObjectKey{Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace}
		Eventually(tests.EnvoyFilterIsAvailable(ctx, testClient(), envoyFilterKey)).WithContext(ctx).Should(BeTrue())
		existingEnvoyFilter := &istioclientgonetworkingv1alpha3.EnvoyFilter{}
		err = testClient().Get(ctx, envoyFilterKey, existingEnvoyFilter)
		Expect(err).ToNot(HaveOccurred())
		existingWASMConfig, err := extractWasmConfigFromEnvoyFilter(existingEnvoyFilter)
		Expect(err).ToNot(HaveOccurred())

		// the reserve/commit services are always registered, regardless of which policies are in play
		Expect(existingWASMConfig.Services).To(HaveKey(wasm.RateLimitReserveServiceName))
		Expect(existingWASMConfig.Services).To(HaveKey(wasm.RateLimitCommitServiceName))

		Expect(existingWASMConfig.ActionSets).To(HaveLen(1))
		actionSet := existingWASMConfig.ActionSets[0]

		limitIdentifier := controllers.TokenLimitNameToLimitadorIdentifier(trlpKey, "l1")
		scope := string(controllers.LimitsNamespaceFromRoute(httpRoute).ToActionScope())
		source := "tokenratelimitpolicy.kuadrant.io:" + trlpKey.String()

		// Built via wasm.BuildActions (not ActionSpec.Build directly) because the
		// commit spec's reservation.actual_amount references responseBodyJSON(...),
		// which BuildActions hoists into a shared response-body-extraction action
		// ahead of the reserve/commit actions themselves.
		expectedActions := wasm.BuildActions([]wasm.ActionSpec{
			{
				ServiceName: wasm.RateLimitReserveServiceName,
				Scope:       scope,
				Sources:     []string{source},
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: limitIdentifier, Value: "1"}}},
							{Value: &wasm.Static{Static: wasm.StaticSpec{Key: "reservation.id", Value: limitIdentifier}}},
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "reservation.amount", Value: "5000"}}},
						},
					},
				},
			},
			{
				ServiceName: wasm.RateLimitCommitServiceName,
				Scope:       scope,
				Sources:     []string{source},
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: limitIdentifier, Value: "1"}}},
							{Value: &wasm.Static{Static: wasm.StaticSpec{Key: "reservation.id", Value: limitIdentifier}}},
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "reservation.actual_amount", Value: `responseBodyJSON("/usage/total_tokens")`}}},
						},
					},
				},
			},
		})

		Expect(actionSet.Actions).To(HaveLen(len(expectedActions)))
		for i, expected := range expectedActions {
			Expect(actionSet.Actions[i].EqualTo(expected)).To(BeTrue())
		}
	}, testTimeOut)

	It("CheckReport mode creates Check/Report actions", func(ctx SpecContext) {
		// Mode is a cluster-wide switch on the singleton Kuadrant CR (kuadrantInstallationNS),
		// so restore it on cleanup to avoid poisoning other specs.
		kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
		DeferCleanup(func(ctx SpecContext) {
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			Expect(testClient().Get(ctx, kuadrantKey, kuadrantObj)).To(Succeed())
			kuadrantObj.Spec.TokenRateLimiting = nil
			Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())
		})

		kuadrantObj := &kuadrantv1beta1.Kuadrant{}
		Expect(testClient().Get(ctx, kuadrantKey, kuadrantObj)).To(Succeed())
		kuadrantObj.Spec.TokenRateLimiting = &kuadrantv1beta1.TokenRateLimiting{Mode: kuadrantv1beta1.TokenRateLimitingModeCheckReport}
		Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())

		httpRoute := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.example.com"})
		err := testClient().Create(ctx, httpRoute)
		Expect(err).ToNot(HaveOccurred())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(httpRoute))).WithContext(ctx).Should(BeTrue())

		trlp := buildTRLP()
		err = testClient().Create(ctx, trlp)
		Expect(err).ToNot(HaveOccurred())

		trlpKey := client.ObjectKeyFromObject(trlp)
		Eventually(tests.TokenRateLimitPolicyIsAccepted(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())
		Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())

		envoyFilterKey := client.ObjectKey{Name: wasm.ExtensionName(gateway.GetName()), Namespace: testNamespace}
		Eventually(tests.EnvoyFilterIsAvailable(ctx, testClient(), envoyFilterKey)).WithContext(ctx).Should(BeTrue())
		existingEnvoyFilter := &istioclientgonetworkingv1alpha3.EnvoyFilter{}
		err = testClient().Get(ctx, envoyFilterKey, existingEnvoyFilter)
		Expect(err).ToNot(HaveOccurred())
		existingWASMConfig, err := extractWasmConfigFromEnvoyFilter(existingEnvoyFilter)
		Expect(err).ToNot(HaveOccurred())

		Expect(existingWASMConfig.Services).To(HaveKey(wasm.RateLimitCheckServiceName))
		Expect(existingWASMConfig.Services).To(HaveKey(wasm.RateLimitReportServiceName))

		Expect(existingWASMConfig.ActionSets).To(HaveLen(1))
		actionSet := existingWASMConfig.ActionSets[0]

		limitIdentifier := controllers.TokenLimitNameToLimitadorIdentifier(trlpKey, "l1")
		scope := string(controllers.LimitsNamespaceFromRoute(httpRoute).ToActionScope())
		source := "tokenratelimitpolicy.kuadrant.io:" + trlpKey.String()

		expectedActions := wasm.BuildActions([]wasm.ActionSpec{
			{
				ServiceName: wasm.RateLimitCheckServiceName,
				Scope:       scope,
				Sources:     []string{source},
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: limitIdentifier, Value: "1"}}},
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "ratelimit.hits_addend", Value: "0"}}},
						},
					},
				},
			},
			{
				ServiceName: wasm.RateLimitReportServiceName,
				Scope:       scope,
				Sources:     []string{source},
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: limitIdentifier, Value: "1"}}},
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "ratelimit.hits_addend", Value: `responseBodyJSON("/usage/total_tokens")`}}},
						},
					},
				},
			},
		})

		Expect(actionSet.Actions).To(HaveLen(len(expectedActions)))
		for i, expected := range expectedActions {
			Expect(actionSet.Actions[i].EqualTo(expected)).To(BeTrue())
		}
	}, testTimeOut)
})
