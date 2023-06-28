//go:build unit

package rlptools

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/google/go-cmp/cmp"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

// TODO(eastizle): missing WASMPluginMutator tests
// TODO(eastizle): missing TestWasmRules use cases tests. Only happy path
func TestWasmRules(t *testing.T) {
	httpRoute := &gatewayapiv1beta1.HTTPRoute{
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			Hostnames: []gatewayapiv1beta1.Hostname{
				"*.example.com",
			},
			Rules: []gatewayapiv1beta1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1beta1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1beta1.HTTPPathMatch{
								Type:  &[]gatewayapiv1beta1.PathMatchType{gatewayapiv1beta1.PathMatchPathPrefix}[0],
								Value: &[]string{"/toy"}[0],
							},
							Method: &[]gatewayapiv1beta1.HTTPMethod{gatewayapiv1beta1.HTTPMethod("GET")}[0],
						},
					},
				},
			},
		},
	}

	t.Run("minimal RLP", func(subT *testing.T) {
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RateLimitPolicy",
				APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rlpA",
				Namespace: "nsA",
			},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				Limits: map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit:    1,
								Duration: 3,
								Unit:     kuadrantv1beta2.TimeUnit("minute"),
							},
						},
						RouteSelectors: []kuadrantv1beta2.RouteSelector{
							{
								Hostnames: []gatewayapiv1beta1.Hostname{
									"*.example.com",
									"myapp.apps.example.com", // ignored
								},
							},
						},
					},
				},
			},
		}

		expectedRule := wasm.Rule{
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
						{
							Selector: "request.host",
							Operator: wasm.PatternOperator(kuadrantv1beta2.EndsWithOperator),
							Value:    ".example.com",
						},
					},
				},
			},
			Data: []wasm.DataItem{
				{
					Static: &wasm.StaticSpec{
						Key:   "nsA/rlpA/l1",
						Value: "1",
					},
				},
			},
		}

		rules := WasmRules(rlp, httpRoute)
		if len(rules) != 1 {
			subT.Errorf("expected 1 rule, got (%d)", len(rules))
		}

		if !reflect.DeepEqual(rules[0], expectedRule) {
			diff := cmp.Diff(rules[0], expectedRule)
			subT.Errorf("expected rule not found: %s", diff)
		}
	})
}
