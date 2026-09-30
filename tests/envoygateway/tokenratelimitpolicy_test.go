//go:build integration

package envoygateway_test

import (
	"fmt"
	"strings"
	"time"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
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

// defaultTotalTokensCEL mirrors the CEL expression the operator generates by default (see
// api/v1alpha1.DefaultTotalTokensPointers) when a TokenRateLimitPolicy doesn't override dataExtraction.
var defaultTotalTokensCEL = controllers.ResponseBodyJSONTotalTokensCEL(kuadrantv1alpha1.DefaultTotalTokensPointers)

var _ = Describe("TokenRateLimitPolicy enforcement modes", Serial, func() {
	const (
		testTimeOut       = NodeTimeout(2 * time.Minute)
		beforeEachTimeOut = NodeTimeout(1 * time.Minute)
		afterEachTimeOut  = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gwHost        = fmt.Sprintf("*.toystore-%s.com", rand.String(4))
		gatewayClass  *gatewayapiv1.GatewayClass
		gateway       *gatewayapiv1.Gateway
	)

	randomHostFromGWHost := func() string {
		return strings.Replace(gwHost, "*", rand.String(4), 1)
	}

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

		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())
	}, beforeEachTimeOut)

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	buildTRLP := func() *kuadrantv1alpha1.TokenRateLimitPolicy {
		return &kuadrantv1alpha1.TokenRateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "TokenRateLimitPolicy", APIVersion: kuadrantv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "toystore-trlp", Namespace: testNamespace},
			Spec: kuadrantv1alpha1.TokenRateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "HTTPRoute",
						Name:  gatewayapiv1.ObjectName(TestHTTPRouteName),
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

	It("Reservation mode (default) reserves zero capacity (no-op, equivalent to Optimistic)", func(ctx SpecContext) {
		// create httproute (no backendRequest timeout, so reservation ttl stays unset)
		gwRoute := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
		err := testClient().Create(ctx, gwRoute)
		Expect(err).ToNot(HaveOccurred())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(gwRoute))).WithContext(ctx).Should(BeTrue())

		// create tokenratelimitpolicy targeting the route, with no reservation
		// overrides. The reconciler defaults reservation.amount to 0, a documented
		// Limitador short-circuit that skips holding capacity, so upgrading to
		// Reservation mode is behavior-neutral for any policy that doesn't
		// explicitly opt in with a non-zero amount.
		trlp := buildTRLP()
		err = testClient().Create(ctx, trlp)
		Expect(err).ToNot(HaveOccurred())

		trlpKey := client.ObjectKeyFromObject(trlp)
		Eventually(tests.TokenRateLimitPolicyIsAccepted(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())
		Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())

		extKey := client.ObjectKey{
			Name:      wasm.ExtensionName(TestGatewayName),
			Namespace: testNamespace,
		}
		Eventually(IsEnvoyExtensionPolicyAccepted).
			WithContext(ctx).
			WithArguments(testClient(), extKey, client.ObjectKeyFromObject(gateway)).
			Should(Succeed())

		ext := &egv1alpha1.EnvoyExtensionPolicy{}
		err = testClient().Get(ctx, extKey, ext)
		Expect(err).ToNot(HaveOccurred())
		Expect(ext.Spec.Wasm).To(HaveLen(1))
		existingWASMConfig, err := wasm.ConfigFromJSON(ext.Spec.Wasm[0].Config)
		Expect(err).ToNot(HaveOccurred())

		// the reserve/commit services are always registered, regardless of which policies are in play
		Expect(existingWASMConfig.Services).To(HaveKey(wasm.RateLimitReserveServiceName))
		Expect(existingWASMConfig.Services).To(HaveKey(wasm.RateLimitCommitServiceName))

		Expect(existingWASMConfig.ActionSets).To(HaveLen(1))
		actionSet := existingWASMConfig.ActionSets[0]

		limitIdentifier := controllers.TokenLimitNameToLimitadorIdentifier(trlpKey, "l1")
		scope := string(controllers.LimitsNamespaceFromRoute(gwRoute).ToActionScope())
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
						},
					},
				},
				Reservation: &wasm.ReservationSpec{ID: limitIdentifier, Amount: "0"},
			},
			{
				ServiceName: wasm.RateLimitCommitServiceName,
				Scope:       scope,
				Sources:     []string{source},
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: limitIdentifier, Value: "1"}}},
						},
					},
				},
				Reservation: &wasm.ReservationSpec{ID: limitIdentifier, ActualAmount: defaultTotalTokensCEL},
			},
		})

		Expect(actionSet.Actions).To(HaveLen(len(expectedActions)))
		for i, expected := range expectedActions {
			Expect(actionSet.Actions[i].EqualTo(expected)).To(BeTrue())
		}
	}, testTimeOut)

	It("Reservation mode with explicit amount reserves real capacity", func(ctx SpecContext) {
		gwRoute := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
		err := testClient().Create(ctx, gwRoute)
		Expect(err).ToNot(HaveOccurred())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(gwRoute))).WithContext(ctx).Should(BeTrue())

		trlp := buildTRLP()
		limit := trlp.Spec.Limits["l1"]
		limit.Reservation = &kuadrantv1alpha1.Reservation{Amount: ptr.To(intstr.FromInt32(8000))}
		trlp.Spec.Limits["l1"] = limit
		err = testClient().Create(ctx, trlp)
		Expect(err).ToNot(HaveOccurred())

		trlpKey := client.ObjectKeyFromObject(trlp)
		Eventually(tests.TokenRateLimitPolicyIsAccepted(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())
		Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())

		extKey := client.ObjectKey{
			Name:      wasm.ExtensionName(TestGatewayName),
			Namespace: testNamespace,
		}
		Eventually(IsEnvoyExtensionPolicyAccepted).
			WithContext(ctx).
			WithArguments(testClient(), extKey, client.ObjectKeyFromObject(gateway)).
			Should(Succeed())

		ext := &egv1alpha1.EnvoyExtensionPolicy{}
		err = testClient().Get(ctx, extKey, ext)
		Expect(err).ToNot(HaveOccurred())
		Expect(ext.Spec.Wasm).To(HaveLen(1))
		existingWASMConfig, err := wasm.ConfigFromJSON(ext.Spec.Wasm[0].Config)
		Expect(err).ToNot(HaveOccurred())

		Expect(existingWASMConfig.ActionSets).To(HaveLen(1))
		actionSet := existingWASMConfig.ActionSets[0]

		limitIdentifier := controllers.TokenLimitNameToLimitadorIdentifier(trlpKey, "l1")
		scope := string(controllers.LimitsNamespaceFromRoute(gwRoute).ToActionScope())
		source := "tokenratelimitpolicy.kuadrant.io:" + trlpKey.String()

		expectedActions := wasm.BuildActions([]wasm.ActionSpec{
			{
				ServiceName: wasm.RateLimitReserveServiceName,
				Scope:       scope,
				Sources:     []string{source},
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: limitIdentifier, Value: "1"}}},
						},
					},
				},
				Reservation: &wasm.ReservationSpec{ID: limitIdentifier, Amount: "8000"},
			},
			{
				ServiceName: wasm.RateLimitCommitServiceName,
				Scope:       scope,
				Sources:     []string{source},
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: limitIdentifier, Value: "1"}}},
						},
					},
				},
				Reservation: &wasm.ReservationSpec{ID: limitIdentifier, ActualAmount: defaultTotalTokensCEL},
			},
		})

		Expect(actionSet.Actions).To(HaveLen(len(expectedActions)))
		for i, expected := range expectedActions {
			Expect(actionSet.Actions[i].EqualTo(expected)).To(BeTrue())
		}
	}, testTimeOut)

	It("Optimistic mode creates Check/Report actions", func(ctx SpecContext) {
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
		kuadrantObj.Spec.TokenRateLimiting = &kuadrantv1beta1.TokenRateLimiting{Mode: kuadrantv1beta1.TokenRateLimitingModeOptimistic}
		Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())

		gwRoute := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
		err := testClient().Create(ctx, gwRoute)
		Expect(err).ToNot(HaveOccurred())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(gwRoute))).WithContext(ctx).Should(BeTrue())

		trlp := buildTRLP()
		err = testClient().Create(ctx, trlp)
		Expect(err).ToNot(HaveOccurred())

		trlpKey := client.ObjectKeyFromObject(trlp)
		Eventually(tests.TokenRateLimitPolicyIsAccepted(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())
		Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())

		extKey := client.ObjectKey{
			Name:      wasm.ExtensionName(TestGatewayName),
			Namespace: testNamespace,
		}
		Eventually(IsEnvoyExtensionPolicyAccepted).
			WithContext(ctx).
			WithArguments(testClient(), extKey, client.ObjectKeyFromObject(gateway)).
			Should(Succeed())

		ext := &egv1alpha1.EnvoyExtensionPolicy{}
		err = testClient().Get(ctx, extKey, ext)
		Expect(err).ToNot(HaveOccurred())
		Expect(ext.Spec.Wasm).To(HaveLen(1))
		existingWASMConfig, err := wasm.ConfigFromJSON(ext.Spec.Wasm[0].Config)
		Expect(err).ToNot(HaveOccurred())

		Expect(existingWASMConfig.Services).To(HaveKey(wasm.RateLimitCheckServiceName))
		Expect(existingWASMConfig.Services).To(HaveKey(wasm.RateLimitReportServiceName))

		Expect(existingWASMConfig.ActionSets).To(HaveLen(1))
		actionSet := existingWASMConfig.ActionSets[0]

		limitIdentifier := controllers.TokenLimitNameToLimitadorIdentifier(trlpKey, "l1")
		scope := string(controllers.LimitsNamespaceFromRoute(gwRoute).ToActionScope())
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
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "ratelimit.hits_addend", Value: defaultTotalTokensCEL}}},
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

	It("Custom dataExtraction.response.totalTokens overrides the defaults", func(ctx SpecContext) {
		gwRoute := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{randomHostFromGWHost()})
		err := testClient().Create(ctx, gwRoute)
		Expect(err).ToNot(HaveOccurred())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(gwRoute))).WithContext(ctx).Should(BeTrue())

		customPointers := []string{"/custom/pointer"}
		trlp := buildTRLP()
		trlp.Spec.DataExtraction = &kuadrantv1alpha1.DataExtraction{
			Response: kuadrantv1alpha1.ResponseDataExtraction{
				kuadrantv1alpha1.ResponseDataExtractionKeyTotalTokens: customPointers,
			},
		}
		err = testClient().Create(ctx, trlp)
		Expect(err).ToNot(HaveOccurred())

		trlpKey := client.ObjectKeyFromObject(trlp)
		Eventually(tests.TokenRateLimitPolicyIsAccepted(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())
		Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())

		extKey := client.ObjectKey{
			Name:      wasm.ExtensionName(TestGatewayName),
			Namespace: testNamespace,
		}
		Eventually(IsEnvoyExtensionPolicyAccepted).
			WithContext(ctx).
			WithArguments(testClient(), extKey, client.ObjectKeyFromObject(gateway)).
			Should(Succeed())

		ext := &egv1alpha1.EnvoyExtensionPolicy{}
		err = testClient().Get(ctx, extKey, ext)
		Expect(err).ToNot(HaveOccurred())
		Expect(ext.Spec.Wasm).To(HaveLen(1))
		existingWASMConfig, err := wasm.ConfigFromJSON(ext.Spec.Wasm[0].Config)
		Expect(err).ToNot(HaveOccurred())

		Expect(existingWASMConfig.ActionSets).To(HaveLen(1))
		actionSet := existingWASMConfig.ActionSets[0]

		limitIdentifier := controllers.TokenLimitNameToLimitadorIdentifier(trlpKey, "l1")
		scope := string(controllers.LimitsNamespaceFromRoute(gwRoute).ToActionScope())
		source := "tokenratelimitpolicy.kuadrant.io:" + trlpKey.String()
		customTotalTokensCEL := controllers.ResponseBodyJSONTotalTokensCEL(customPointers)

		expectedActions := wasm.BuildActions([]wasm.ActionSpec{
			{
				ServiceName: wasm.RateLimitReserveServiceName,
				Scope:       scope,
				Sources:     []string{source},
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: limitIdentifier, Value: "1"}}},
						},
					},
				},
				Reservation: &wasm.ReservationSpec{ID: limitIdentifier, Amount: "0"},
			},
			{
				ServiceName: wasm.RateLimitCommitServiceName,
				Scope:       scope,
				Sources:     []string{source},
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{
							{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: limitIdentifier, Value: "1"}}},
						},
					},
				},
				Reservation: &wasm.ReservationSpec{ID: limitIdentifier, ActualAmount: customTotalTokensCEL},
			},
		})

		Expect(actionSet.Actions).To(HaveLen(len(expectedActions)))
		for i, expected := range expectedActions {
			Expect(actionSet.Actions[i].EqualTo(expected)).To(BeTrue())
		}
	}, testTimeOut)
})
