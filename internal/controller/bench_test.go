//go:build unit

package controllers

import (
	"context"
	"fmt"
	"sync"
	"testing"

	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantauthorino "github.com/kuadrant/kuadrant-operator/internal/authorino"
)

type benchTopologyParams struct {
	numGatewayClasses  int
	numRoutesPerGW     int
	numRulesPerRoute   int
	attachAuthPolicies bool
	attachRLPolicies   bool
}

func buildBenchTopology(b *testing.B, params benchTopologyParams) (*machinery.Topology, *kuadrantv1beta1.Kuadrant, *sync.Map) {
	b.Helper()

	kuadrant := &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantv1beta1.KuadrantGroupKind.Kind,
			APIVersion: kuadrantv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kuadrant",
			Namespace: "kuadrant-system",
			UID:       types.UID("uid-kuadrant"),
		},
	}

	store := make(controller.Store)
	store[string(kuadrant.UID)] = kuadrant

	var gatewayClasses []*gatewayapiv1.GatewayClass
	var gateways []*gatewayapiv1.Gateway
	var httpRoutes []*gatewayapiv1.HTTPRoute
	var policies []machinery.Policy

	for gc := 0; gc < params.numGatewayClasses; gc++ {
		gcName := fmt.Sprintf("gc-%d", gc)
		gatewayClass := &gatewayapiv1.GatewayClass{
			TypeMeta: metav1.TypeMeta{
				Kind:       machinery.GatewayClassGroupKind.Kind,
				APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: gcName,
				UID:  types.UID(fmt.Sprintf("uid-gc-%d", gc)),
			},
			Spec: gatewayapiv1.GatewayClassSpec{
				ControllerName: "kuadrant.io/policy-controller",
			},
		}
		gatewayClasses = append(gatewayClasses, gatewayClass)
		store[string(gatewayClass.UID)] = gatewayClass

		gwName := fmt.Sprintf("gw-%d", gc)
		gateway := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       machinery.GatewayGroupKind.Kind,
				APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      gwName,
				Namespace: "default",
				UID:       types.UID(fmt.Sprintf("uid-gw-%d", gc)),
			},
			Spec: gatewayapiv1.GatewaySpec{
				GatewayClassName: gatewayapiv1.ObjectName(gcName),
				Listeners: []gatewayapiv1.Listener{
					{
						Name:     "http",
						Hostname: ptr.To(gatewayapiv1.Hostname(fmt.Sprintf("*.gw-%d.example.com", gc))),
					},
				},
			},
		}
		gateways = append(gateways, gateway)
		store[string(gateway.UID)] = gateway

		if params.attachAuthPolicies {
			policy := &kuadrantv1.AuthPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AuthPolicy",
					APIVersion: kuadrantv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("auth-%d", gc),
					Namespace: "default",
					UID:       types.UID(fmt.Sprintf("uid-auth-%d", gc)),
				},
				Spec: kuadrantv1.AuthPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1alpha2.Group(machinery.GatewayGroupKind.Group),
							Kind:  gatewayapiv1.Kind(machinery.GatewayGroupKind.Kind),
							Name:  gatewayapiv1.ObjectName(gwName),
						},
					},
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: &kuadrantv1.AuthSchemeSpec{
							Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
								"jwt": {
									AuthenticationSpec: authorinov1beta3.AuthenticationSpec{
										AuthenticationMethodSpec: authorinov1beta3.AuthenticationMethodSpec{
											Jwt: &authorinov1beta3.JwtAuthenticationSpec{
												IssuerUrl: "http://auth.example.com",
											},
										},
									},
								},
							},
						},
					},
				},
				Status: kuadrantv1.AuthPolicyStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(gatewayapiv1alpha2.PolicyConditionAccepted),
							Status: metav1.ConditionTrue,
						},
					},
				},
			}
			policies = append(policies, policy)
			store[string(policy.UID)] = policy
		}

		if params.attachRLPolicies {
			policy := &kuadrantv1.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RateLimitPolicy",
					APIVersion: kuadrantv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("rlp-%d", gc),
					Namespace: "default",
					UID:       types.UID(fmt.Sprintf("uid-rlp-%d", gc)),
				},
				Spec: kuadrantv1.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1alpha2.Group(machinery.GatewayGroupKind.Group),
							Kind:  gatewayapiv1.Kind(machinery.GatewayGroupKind.Kind),
							Name:  gatewayapiv1.ObjectName(gwName),
						},
					},
					RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
						Limits: map[string]kuadrantv1.Limit{
							"requests": {
								Rates: []kuadrantv1.Rate{
									{Limit: 10, Window: "10s"},
								},
							},
						},
					},
				},
				Status: kuadrantv1.RateLimitPolicyStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(gatewayapiv1alpha2.PolicyConditionAccepted),
							Status: metav1.ConditionTrue,
						},
					},
				},
			}
			policies = append(policies, policy)
			store[string(policy.UID)] = policy
		}

		for r := 0; r < params.numRoutesPerGW; r++ {
			routeName := fmt.Sprintf("route-%d-%d", gc, r)
			rules := make([]gatewayapiv1.HTTPRouteRule, params.numRulesPerRoute)
			for k := 0; k < params.numRulesPerRoute; k++ {
				rules[k] = gatewayapiv1.HTTPRouteRule{
					Name: ptr.To(gatewayapiv1.SectionName(fmt.Sprintf("rule-%d", k))),
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Value: ptr.To(fmt.Sprintf("/%s/rule-%d", routeName, k)),
							},
						},
					},
					BackendRefs: []gatewayapiv1.HTTPBackendRef{
						{
							BackendRef: gatewayapiv1.BackendRef{
								BackendObjectReference: gatewayapiv1.BackendObjectReference{
									Name: gatewayapiv1.ObjectName(fmt.Sprintf("svc-%s", routeName)),
								},
							},
						},
					},
				}
			}

			route := &gatewayapiv1.HTTPRoute{
				TypeMeta: metav1.TypeMeta{
					Kind:       machinery.HTTPRouteGroupKind.Kind,
					APIVersion: gatewayapiv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      routeName,
					Namespace: "default",
					UID:       types.UID(fmt.Sprintf("uid-route-%d-%d", gc, r)),
				},
				Spec: gatewayapiv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{
							{Name: gatewayapiv1.ObjectName(gwName)},
						},
					},
					Rules: rules,
				},
			}
			httpRoutes = append(httpRoutes, route)
			store[string(route.UID)] = route
		}
	}

	topology, err := machinery.NewGatewayAPITopology(
		machinery.WithGatewayClasses(gatewayClasses...),
		machinery.WithGateways(gateways...),
		machinery.ExpandGatewayListeners(),
		machinery.WithHTTPRoutes(httpRoutes...),
		machinery.ExpandHTTPRouteRules(),
		machinery.WithGatewayAPITopologyPolicies(policies...),
		machinery.WithGatewayAPITopologyObjects(kuadrant),
		machinery.WithGatewayAPITopologyLinks(
			kuadrantv1beta1.LinkKuadrantToGatewayClasses(store),
		),
	)
	if err != nil {
		b.Fatalf("failed to build topology: %v", err)
	}

	state := &sync.Map{}

	return topology, kuadrant, state
}

// BenchmarkCalculateEffectiveAuthPolicies measures the O(GatewayClasses × RouteRules) loop
// that calls Paths() for each pair — the most expensive reconciliation path.
func BenchmarkCalculateEffectiveAuthPolicies(b *testing.B) {
	cases := []struct {
		name string
		p    benchTopologyParams
	}{
		{"1gc-10routes", benchTopologyParams{1, 10, 2, true, false}},
		{"1gc-100routes", benchTopologyParams{1, 100, 2, true, false}},
		{"1gc-300routes", benchTopologyParams{1, 300, 2, true, false}},
		{"3gc-100routes", benchTopologyParams{3, 100, 2, true, false}},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			topology, kuadrant, state := buildBenchTopology(b, tc.p)
			ctx := context.Background()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				CalculateEffectiveAuthPolicies(ctx, topology, kuadrant, state)
			}
		})
	}
}

// BenchmarkTopologyToDot measures the DAG-to-DOT serialization called every reconciliation cycle.
func BenchmarkTopologyToDot(b *testing.B) {
	cases := []struct {
		name   string
		routes int
		rules  int
	}{
		{"10obj", 2, 1},
		{"100obj", 20, 2},
		{"300obj", 60, 2},
		{"1000obj", 200, 2},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			topology, _, _ := buildBenchTopology(b, benchTopologyParams{1, tc.routes, tc.rules, false, false})
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				topology.ToDot()
			}
		})
	}
}

// BenchmarkBuildDesiredAuthConfig measures AuthConfig generation for N route rules.
func BenchmarkBuildDesiredAuthConfig(b *testing.B) {
	cases := []struct {
		name  string
		rules int
	}{
		{"1-rule", 1},
		{"10-rules", 10},
		{"100-rules", 100},
		{"300-rules", 300},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			topology, kuadrant, state := buildBenchTopology(b, benchTopologyParams{1, 1, tc.rules, true, false})
			ctx := context.Background()

			effectivePolicies := CalculateEffectiveAuthPolicies(ctx, topology, kuadrant, state)

			type authConfigInput struct {
				pathID           string
				effectivePolicy  EffectiveAuthPolicy
				name             string
				namespace        string
				annotationKey    string
				routeRuleLocator string
			}
			var inputs []authConfigInput
			for pathID, ep := range effectivePolicies {
				inputs = append(inputs, authConfigInput{
					pathID:           pathID,
					effectivePolicy:  ep,
					name:             AuthConfigNameForPath(pathID),
					namespace:        "kuadrant-system",
					annotationKey:    kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation,
					routeRuleLocator: ep.Path[len(ep.Path)-1].GetLocator(),
				})
			}

			r := &AuthConfigsReconciler{client: nil}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, in := range inputs {
					r.buildDesiredAuthConfig(ctx, in.effectivePolicy, in.name, in.namespace, in.annotationKey, in.routeRuleLocator)
				}
			}
		})
	}
}

// BenchmarkBuildLimitadorLimits measures merging effective rate limit policies into Limitador limits.
func BenchmarkBuildLimitadorLimits(b *testing.B) {
	cases := []struct {
		name   string
		routes int
	}{
		{"10-policies", 5},
		{"100-policies", 50},
		{"300-policies", 150},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			topology, kuadrant, state := buildBenchTopology(b, benchTopologyParams{1, tc.routes, 2, false, true})
			ctx := context.Background()

			targetables := topology.Targetables()
			gatewayClasses := targetables.Children(kuadrant)
			httpRouteRules := targetables.Items(func(o machinery.Object) bool {
				_, ok := o.(*machinery.HTTPRouteRule)
				return ok
			})

			allPolicies := topology.Policies().Items()
			var rlPolicies []machinery.Policy
			for _, p := range allPolicies {
				if _, ok := p.(*kuadrantv1.RateLimitPolicy); ok {
					rlPolicies = append(rlPolicies, p)
				}
			}
			if len(rlPolicies) == 0 {
				b.Fatal("no rate limit policies in topology")
			}

			effectivePolicies := EffectiveRateLimitPolicies{}
			for _, gc := range gatewayClasses {
				for _, rule := range httpRouteRules {
					paths := targetables.Paths(gc, rule)
					for _, path := range paths {
						pathID := kuadrantv1.PathID(path)
						rlp := rlPolicies[0].(*kuadrantv1.RateLimitPolicy)
						effectivePolicies[pathID] = EffectiveRateLimitPolicy{
							Path:           path,
							Spec:           *rlp,
							SourcePolicies: []string{rlp.GetLocator()},
						}
					}
				}
			}
			state.Store(StateEffectiveRateLimitPolicies, effectivePolicies)

			r := &LimitadorLimitsReconciler{client: nil}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r.buildLimitadorLimits(ctx, state)
			}
		})
	}
}

// BenchmarkTopologyPaths measures the Paths() DFS traversal — the single most expensive operation.
func BenchmarkTopologyPaths(b *testing.B) {
	cases := []struct {
		name   string
		p      benchTopologyParams
	}{
		{"shallow-narrow", benchTopologyParams{1, 5, 2, false, false}},
		{"shallow-wide", benchTopologyParams{1, 100, 2, false, false}},
		{"deep-wide", benchTopologyParams{3, 100, 5, false, false}},
		{"stress", benchTopologyParams{1, 300, 2, false, false}},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			topology, kuadrant, _ := buildBenchTopology(b, tc.p)

			targetables := topology.Targetables()
			gatewayClasses := targetables.Children(kuadrant)
			httpRouteRules := targetables.Items(func(o machinery.Object) bool {
				_, ok := o.(*machinery.HTTPRouteRule)
				return ok
			})
			if len(gatewayClasses) == 0 || len(httpRouteRules) == 0 {
				b.Fatal("topology has no gateway classes or route rules")
			}
			from := gatewayClasses[0]
			to := httpRouteRules[len(httpRouteRules)-1]

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				targetables.Paths(from, to)
			}
		})
	}
}
