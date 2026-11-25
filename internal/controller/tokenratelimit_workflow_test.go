//go:build unit

package controllers

import (
	"context"
	"regexp"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/kuadrant/policy-machinery/machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/policymachinery"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

func TestTokenLimitNameToLimitadorIdentifier(t *testing.T) {
	testCases := []struct {
		name            string
		trlpKey         k8stypes.NamespacedName
		uniqueLimitName string
		expected        *regexp.Regexp
	}{
		{
			name:            "prepends the token limitador limit identifier prefix",
			trlpKey:         k8stypes.NamespacedName{Namespace: "testNS", Name: "trlpA"},
			uniqueLimitName: "foo",
			expected:        regexp.MustCompile(`^tokenlimit\.foo.+`),
		},
		{
			name:            "creates deterministic identifier",
			trlpKey:         k8stypes.NamespacedName{Namespace: "testNS", Name: "trlpA"},
			uniqueLimitName: "foo",
			expected:        regexp.MustCompile(`^tokenlimit\.foo__13adad8e`),
		},
		{
			name:            "identifier includes unique limit name",
			trlpKey:         k8stypes.NamespacedName{Namespace: "testNS", Name: "trlpA"},
			uniqueLimitName: "myUniqueLimit",
			expected:        regexp.MustCompile(`tokenlimit\.myUniqueLimit.+`),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			identifier := TokenLimitNameToLimitadorIdentifier(tc.trlpKey, tc.uniqueLimitName)
			if !tc.expected.MatchString(identifier) {
				subT.Errorf("identifier does not match, expected(%s), got (%s)", tc.expected, identifier)
			}
		})
	}
}

func TestWasmActionSpecsFromTokenLimit(t *testing.T) {
	testCases := []struct {
		name                string
		tokenLimit          *kuadrantv1alpha1.TokenLimit
		limitIdentifier     string
		scope               ActionScope
		topLevelPredicates  kuadrantv1.WhenPredicates
		mode                kuadrantv1beta1.TokenRateLimitingMode
		defaultTTL          string
		totalTokensPointers []string
		expectedActions     []wasm.ActionSpec
	}{
		{
			name:                "token limit without conditions nor counters",
			tokenLimit:          &kuadrantv1alpha1.TokenLimit{},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			totalTokensPointers: []string{"/usage/total_tokens"},
			mode:                kuadrantv1beta1.TokenRateLimitingModeOptimistic,
			expectedActions: []wasm.ActionSpec{
				// Request phase action
				{
					ServiceName: wasm.RateLimitCheckServiceName,
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: "0",
										},
									},
								},
							},
						},
					},
					Sources: []string{"test/policy/locator"},
				},
				// Response phase action
				{
					ServiceName: wasm.RateLimitReportServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: `responseBodyJSON(["/usage/total_tokens"], "number")`,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "token limit with counter expression",
			tokenLimit: &kuadrantv1alpha1.TokenLimit{
				Counters: []kuadrantv1.Counter{
					{Expression: kuadrantv1.Expression("auth.identity.userid")},
				},
			},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeOptimistic,
			totalTokensPointers: []string{"/usage/total_tokens"},
			expectedActions: []wasm.ActionSpec{
				// Request phase action
				{
					ServiceName: wasm.RateLimitCheckServiceName,
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "auth.identity.userid",
											Value: "auth.identity.userid",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: "0",
										},
									},
								},
							},
						},
					},
					Sources: []string{"test/policy/locator"},
				},
				// Response phase action
				{
					ServiceName: wasm.RateLimitReportServiceName,
					Sources:     []string{"test/policy/locator"},
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "auth.identity.userid",
											Value: "auth.identity.userid",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: `responseBodyJSON(["/usage/total_tokens"], "number")`,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "token limit with counter and when predicates",
			tokenLimit: &kuadrantv1alpha1.TokenLimit{
				Counters: []kuadrantv1.Counter{
					{Expression: kuadrantv1.Expression("auth.identity.userid")},
				},
				When: kuadrantv1.WhenPredicates{
					{Predicate: `request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")`},
				},
			},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeOptimistic,
			totalTokensPointers: []string{"/usage/total_tokens"},
			expectedActions: []wasm.ActionSpec{
				// Request phase action
				{
					ServiceName: wasm.RateLimitCheckServiceName,
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{`request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")`},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "auth.identity.userid",
											Value: "auth.identity.userid",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: "0",
										},
									},
								},
							},
						},
					},
					Sources: []string{"test/policy/locator"},
				},
				// Response phase action
				{
					ServiceName: wasm.RateLimitReportServiceName,
					Sources:     []string{"test/policy/locator"},
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{`request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")`},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "auth.identity.userid",
											Value: "auth.identity.userid",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: `responseBodyJSON(["/usage/total_tokens"], "number")`,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "token limit with top-level and limit-level predicates",
			tokenLimit: &kuadrantv1alpha1.TokenLimit{
				When: kuadrantv1.WhenPredicates{
					{Predicate: `request.auth.claims["tier"] == "free"`},
				},
			},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			topLevelPredicates:  kuadrantv1.WhenPredicates{{Predicate: `request.method == "POST"`}},
			mode:                kuadrantv1beta1.TokenRateLimitingModeOptimistic,
			totalTokensPointers: []string{"/usage/total_tokens"},
			expectedActions: []wasm.ActionSpec{
				// Request phase action
				{
					ServiceName: wasm.RateLimitCheckServiceName,
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{`request.method == "POST"`, `request.auth.claims["tier"] == "free"`},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: "0",
										},
									},
								},
							},
						},
					},
					Sources: []string{"test/policy/locator"},
				},
				// Response phase action
				{
					ServiceName: wasm.RateLimitReportServiceName,
					Sources:     []string{"test/policy/locator"},
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{`request.method == "POST"`, `request.auth.claims["tier"] == "free"`},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: `responseBodyJSON(["/usage/total_tokens"], "number")`,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:                "token limit with multi-candidate dataExtraction",
			tokenLimit:          &kuadrantv1alpha1.TokenLimit{},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeOptimistic,
			totalTokensPointers: []string{"/usage/total_tokens", "/usageMetadata/totalTokenCount"},
			expectedActions: []wasm.ActionSpec{
				// Request phase action
				{
					ServiceName: wasm.RateLimitCheckServiceName,
					Scope:       "my-ns/my-route",
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: "0",
										},
									},
								},
							},
						},
					},
					Sources: []string{"test/policy/locator"},
				},
				// Response phase action
				{
					ServiceName: wasm.RateLimitReportServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenlimit.myTokenLimit__d681f6c3",
											Value: "1",
										},
									},
								},
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: `responseBodyJSON(["/usage/total_tokens", "/usageMetadata/totalTokenCount"], "number")`,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:                "reservation mode with defaults (no reservation spec, no route ttl)",
			tokenLimit:          &kuadrantv1alpha1.TokenLimit{},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeReservation,
			totalTokensPointers: []string{"/usage/total_tokens"},
			expectedActions: []wasm.ActionSpec{
				// Reserve (request phase): default amount (0, no capacity held), ttl omitted
				{
					ServiceName: wasm.RateLimitReserveServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:     "tokenlimit.myTokenLimit__d681f6c3",
						Amount: "0",
					},
				},
				// Commit (response phase): actual usage from response body
				{
					ServiceName: wasm.RateLimitCommitServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:           "tokenlimit.myTokenLimit__d681f6c3",
						ActualAmount: `responseBodyJSON(["/usage/total_tokens"], "number")`,
					},
				},
			},
		},
		{
			name:                "reservation mode with route backendRequest ttl fallback",
			tokenLimit:          &kuadrantv1alpha1.TokenLimit{},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeReservation,
			totalTokensPointers: []string{"/usage/total_tokens"},
			defaultTTL:          "45s",
			expectedActions: []wasm.ActionSpec{
				{
					ServiceName: wasm.RateLimitReserveServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:     "tokenlimit.myTokenLimit__d681f6c3",
						Amount: "0",
						TTL:    `duration("45s")`,
					},
				},
				{
					ServiceName: wasm.RateLimitCommitServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:           "tokenlimit.myTokenLimit__d681f6c3",
						ActualAmount: `responseBodyJSON(["/usage/total_tokens"], "number")`,
					},
				},
			},
		},
		{
			name: "reservation mode with explicit integer amount and ttl overriding route fallback",
			tokenLimit: &kuadrantv1alpha1.TokenLimit{
				Reservation: &kuadrantv1alpha1.Reservation{
					Amount: ptr.To(intstr.FromInt32(8000)),
					TTL:    ptr.To(kuadrantv1.Expression(`duration("30s")`)),
				},
			},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeReservation,
			totalTokensPointers: []string{"/usage/total_tokens"},
			defaultTTL:          "45s", // overridden by explicit policy ttl
			expectedActions: []wasm.ActionSpec{
				{
					ServiceName: wasm.RateLimitReserveServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:     "tokenlimit.myTokenLimit__d681f6c3",
						Amount: "8000",
						TTL:    `duration("30s")`,
					},
				},
				{
					ServiceName: wasm.RateLimitCommitServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:           "tokenlimit.myTokenLimit__d681f6c3",
						ActualAmount: `responseBodyJSON(["/usage/total_tokens"], "number")`,
					},
				},
			},
		},
		{
			name: "reservation mode with CEL expression amount",
			tokenLimit: &kuadrantv1alpha1.TokenLimit{
				Reservation: &kuadrantv1alpha1.Reservation{
					Amount: ptr.To(intstr.FromString("1 + 1")),
				},
			},
			limitIdentifier:     "tokenlimit.myTokenLimit__d681f6c3",
			scope:               ActionScope("my-ns/my-route"),
			mode:                kuadrantv1beta1.TokenRateLimitingModeReservation,
			totalTokensPointers: []string{"/usage/total_tokens"},
			expectedActions: []wasm.ActionSpec{
				{
					ServiceName: wasm.RateLimitReserveServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:     "tokenlimit.myTokenLimit__d681f6c3",
						Amount: "1 + 1",
					},
				},
				{
					ServiceName: wasm.RateLimitCommitServiceName,
					Scope:       "my-ns/my-route",
					Sources:     []string{"test/policy/locator"},
					ConditionalData: []wasm.ConditionalData{
						{
							Predicates: []string{},
							Data: []wasm.DataType{
								{Value: &wasm.Expression{ExpressionItem: wasm.ExpressionItem{Key: "tokenlimit.myTokenLimit__d681f6c3", Value: "1"}}},
							},
						},
					},
					Reservation: &wasm.ReservationSpec{
						ID:           "tokenlimit.myTokenLimit__d681f6c3",
						ActualAmount: `responseBodyJSON(["/usage/total_tokens"], "number")`,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			computedActions := wasmActionSpecsFromTokenLimit(tc.tokenLimit, tc.limitIdentifier, tc.scope, "test/policy/locator", tc.topLevelPredicates, tc.mode, tc.defaultTTL, tc.totalTokensPointers)
			if diff := cmp.Diff(tc.expectedActions, computedActions); diff != "" {
				t.Errorf("unexpected wasm actions (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuiltInPolicyActionOrder(t *testing.T) {
	topology, err := machinery.NewTopology()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []struct {
		name  gatewayapiv1.GatewayController
		build func(context.Context, *machinery.Topology, *sync.Map) (map[string]wasm.Config, *wasm.Observability, *wasm.ServiceBuilder, error)
	}{
		{istioGatewayControllerNames[0], (&IstioExtensionReconciler{}).buildWasmConfigs},
		{envoyGatewayGatewayControllerNames[0], (&EnvoyGatewayExtensionReconciler{}).buildWasmConfigs},
	} {
		t.Run(string(provider.name), func(t *testing.T) {
			const responseStore = "kuadrant.internal.response.body"
			for _, scenario := range []struct {
				name        string
				mode        kuadrantv1beta1.TokenRateLimitingMode
				auth        bool
				rateLimit   bool
				tokenLimits []string
				want        []string
			}{
				{
					name:        "auth with a global reservation limit",
					auth:        true,
					tokenLimits: []string{"global"},
					want:        []string{wasm.AuthServiceName, wasm.RateLimitReserveServiceName, responseStore, wasm.RateLimitCommitServiceName},
				},
				{
					name:        "auth with infrastructure and multiple reservation limits",
					auth:        true,
					rateLimit:   true,
					tokenLimits: []string{"a", "b"},
					want:        []string{wasm.RateLimitServiceName, wasm.AuthServiceName, wasm.RateLimitReserveServiceName, wasm.RateLimitReserveServiceName, responseStore, wasm.RateLimitCommitServiceName, wasm.RateLimitCommitServiceName},
				},
				{
					name:        "auth with infrastructure and an optimistic limit",
					mode:        kuadrantv1beta1.TokenRateLimitingModeOptimistic,
					auth:        true,
					rateLimit:   true,
					tokenLimits: []string{"global"},
					want:        []string{wasm.RateLimitServiceName, wasm.AuthServiceName, wasm.RateLimitCheckServiceName, responseStore, wasm.RateLimitReportServiceName},
				},
				{
					name:      "auth with infrastructure only",
					auth:      true,
					rateLimit: true,
					want:      []string{wasm.RateLimitServiceName, wasm.AuthServiceName},
				},
				{
					name:        "infrastructure RLP and Reservation-mode TRLP without auth",
					rateLimit:   true,
					tokenLimits: []string{"global"},
					want:        []string{wasm.RateLimitServiceName, wasm.RateLimitReserveServiceName, responseStore, wasm.RateLimitCommitServiceName},
				},
				{
					name:      "standalone infrastructure limit",
					rateLimit: true,
					want:      []string{wasm.RateLimitServiceName},
				},
				{
					name:        "standalone token limit",
					tokenLimits: []string{"global"},
					want:        []string{wasm.RateLimitReserveServiceName, responseStore, wasm.RateLimitCommitServiceName},
				},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					class := &machinery.GatewayClass{GatewayClass: &gatewayapiv1.GatewayClass{
						ObjectMeta: metav1.ObjectMeta{Name: "class"},
						Spec:       gatewayapiv1.GatewayClassSpec{ControllerName: provider.name},
					}}
					gateway := &machinery.Gateway{Gateway: &gatewayapiv1.Gateway{
						TypeMeta:   metav1.TypeMeta{APIVersion: gatewayapiv1.GroupVersion.String(), Kind: "Gateway"},
						ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "test"},
						Spec:       gatewayapiv1.GatewaySpec{GatewayClassName: "class"},
					}}
					listener := &machinery.Listener{Gateway: gateway, Listener: &gatewayapiv1.Listener{Name: "http", Hostname: ptr.To(gatewayapiv1.Hostname("example.test"))}}
					route := &machinery.HTTPRoute{HTTPRoute: &gatewayapiv1.HTTPRoute{
						ObjectMeta: metav1.ObjectMeta{Name: "route", Namespace: "test"},
						Spec: gatewayapiv1.HTTPRouteSpec{
							CommonRouteSpec: gatewayapiv1.CommonRouteSpec{ParentRefs: []gatewayapiv1.ParentReference{{Name: "gateway"}}},
							Hostnames:       []gatewayapiv1.Hostname{"example.test"},
							Rules:           []gatewayapiv1.HTTPRouteRule{{}},
						},
					}}
					rule := &machinery.HTTPRouteRule{HTTPRoute: route, HTTPRouteRule: &route.Spec.Rules[0]}
					path := []machinery.Targetable{class, gateway, listener, route, rule}
					if _, err := policymachinery.ParseTopologyPath(path); err != nil {
						t.Fatal(err)
					}
					pathID := kuadrantv1.PathID(path)

					var state sync.Map
					state.Store("kuadrant", &kuadrantv1beta1.Kuadrant{Spec: kuadrantv1beta1.KuadrantSpec{
						TokenRateLimiting: &kuadrantv1beta1.TokenRateLimiting{Mode: scenario.mode},
					}})
					authPolicies := EffectiveAuthPolicies{}
					if scenario.auth {
						authPolicies[pathID] = EffectiveAuthPolicy{Path: path}
					}
					state.Store(StateEffectiveAuthPolicies, authPolicies)
					accepted := []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue}}
					var attached []machinery.Policy
					if scenario.rateLimit {
						rlp := &kuadrantv1.RateLimitPolicy{
							ObjectMeta: metav1.ObjectMeta{Name: "rlp", Namespace: route.Namespace},
							Status:     kuadrantv1.RateLimitPolicyStatus{Conditions: accepted},
						}
						rlp.Spec.Proper().Limits = map[string]kuadrantv1.Limit{
							"global": {Rates: []kuadrantv1.Rate{{Limit: 100, Window: "1m"}}},
						}
						attached = append(attached, rlp)
						state.Store(StateEffectiveRateLimitPolicies, EffectiveRateLimitPolicies{pathID: {Path: path, Spec: *rlp}})
					}
					if len(scenario.tokenLimits) > 0 {
						trlp := &kuadrantv1alpha1.TokenRateLimitPolicy{
							ObjectMeta: metav1.ObjectMeta{Name: "trlp", Namespace: route.Namespace},
							Status:     kuadrantv1alpha1.TokenRateLimitPolicyStatus{Conditions: accepted},
						}
						trlp.Spec.Proper().Limits = make(map[string]kuadrantv1alpha1.TokenLimit)
						for _, name := range scenario.tokenLimits {
							trlp.Spec.Proper().Limits[name] = kuadrantv1alpha1.TokenLimit{
								Rates: []kuadrantv1.Rate{{Limit: 1000, Window: "1m"}},
							}
						}
						attached = append(attached, trlp)
						state.Store(StateEffectiveTokenRateLimitPolicies, EffectiveTokenRateLimitPolicies{pathID: {Path: path, Spec: *trlp}})
					}
					route.SetPolicies(attached)

					configs, _, _, err := provider.build(t.Context(), topology, &state)
					if err != nil {
						t.Fatal(err)
					}
					sets := configs[gateway.GetLocator()].ActionSets
					if len(sets) != 1 {
						t.Fatalf("got %d action sets for %s, want one (configs: %v)", len(sets), gateway.GetLocator(), configs)
					}

					var got []string
					for _, action := range sets[0].Actions {
						switch action := action.(type) {
						case *wasm.GrpcAction:
							got = append(got, action.Service)
							var wantExecution wasm.ExecutionMode
							hasTokenRateLimit := len(scenario.tokenLimits) > 0
							if (action.Service == wasm.RateLimitServiceName && (scenario.auth || hasTokenRateLimit)) ||
								(action.Service == wasm.AuthServiceName && hasTokenRateLimit) {
								wantExecution = wasm.ExecutionSequential
							}
							if action.Execution != wantExecution {
								t.Errorf("%s execution = %q, want %q", action.Service, action.Execution, wantExecution)
							}
							wantGuard := action.Service != wasm.RateLimitCommitServiceName && action.Service != wasm.RateLimitReportServiceName
							if action.IsGuard != wantGuard {
								t.Errorf("%s isGuard = %t, want %t", action.Service, action.IsGuard, wantGuard)
							}
						case *wasm.StoreAction:
							got = append(got, action.Path)
							if action.IsGuard {
								t.Errorf("store %s should run in the response phase", action.Path)
							}
						default:
							t.Fatalf("unexpected action type %s", action.ActionType())
						}
					}
					if diff := cmp.Diff(scenario.want, got); diff != "" {
						t.Errorf("action order mismatch (-want +got):\n%s", diff)
					}
				})
			}
		})
	}
}
