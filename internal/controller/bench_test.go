//go:build bench

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
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantauthorino "github.com/kuadrant/kuadrant-operator/internal/authorino"
)

// benchTopologyParams controls the shape of the synthetic topology.
// Policies can target gateways (policyTarget="gateway") or routes (policyTarget="route").
type benchTopologyParams struct {
	numGatewayClasses  int
	numRoutesPerGW     int
	numRulesPerRoute   int
	listenersPerGW     int
	attachAuthPolicies bool
	attachRLPolicies   bool
	attachTRLPolicies  bool
	policyTarget       string // "gateway" (default) or "route"
}

func (p benchTopologyParams) label() string {
	listeners := p.listenersPerGW
	if listeners == 0 {
		listeners = 1
	}
	return fmt.Sprintf("gw=%d_listen=%d_routes=%d", p.numGatewayClasses, listeners, p.numRoutesPerGW)
}

func buildBenchTopology(b testing.TB, params benchTopologyParams) (*machinery.Topology, *kuadrantv1beta1.Kuadrant, *sync.Map) {
	b.Helper()

	if params.listenersPerGW == 0 {
		params.listenersPerGW = 1
	}
	if params.policyTarget == "" {
		params.policyTarget = "gateway"
	}

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

	acceptedCondition := metav1.Condition{
		Type:   string(gatewayapiv1alpha2.PolicyConditionAccepted),
		Status: metav1.ConditionTrue,
	}

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

		listeners := make([]gatewayapiv1.Listener, params.listenersPerGW)
		for l := 0; l < params.listenersPerGW; l++ {
			listeners[l] = gatewayapiv1.Listener{
				Name:     gatewayapiv1.SectionName(fmt.Sprintf("listener-%d", l)),
				Hostname: ptr.To(gatewayapiv1.Hostname(fmt.Sprintf("*.gw%d-l%d.example.com", gc, l))),
			}
		}

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
				Listeners:        listeners,
			},
		}
		gateways = append(gateways, gateway)
		store[string(gateway.UID)] = gateway

		gwTargetRef := gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: gatewayapiv1alpha2.Group(machinery.GatewayGroupKind.Group),
				Kind:  gatewayapiv1.Kind(machinery.GatewayGroupKind.Kind),
				Name:  gatewayapiv1.ObjectName(gwName),
			},
		}

		if params.policyTarget == "gateway" {
			if params.attachAuthPolicies {
				p := &kuadrantv1.AuthPolicy{
					TypeMeta:   metav1.TypeMeta{Kind: "AuthPolicy", APIVersion: kuadrantv1.GroupVersion.String()},
					ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("auth-gw-%d", gc), Namespace: "default", UID: types.UID(fmt.Sprintf("uid-auth-gw-%d", gc))},
					Spec: kuadrantv1.AuthPolicySpec{
						TargetRef: gwTargetRef,
						AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
							AuthScheme: &kuadrantv1.AuthSchemeSpec{
								Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
									"jwt": {AuthenticationSpec: authorinov1beta3.AuthenticationSpec{AuthenticationMethodSpec: authorinov1beta3.AuthenticationMethodSpec{Jwt: &authorinov1beta3.JwtAuthenticationSpec{IssuerUrl: "http://auth.example.com"}}}},
								},
							},
						},
					},
					Status: kuadrantv1.AuthPolicyStatus{Conditions: []metav1.Condition{acceptedCondition}},
				}
				policies = append(policies, p)
				store[string(p.UID)] = p
			}
			if params.attachRLPolicies {
				p := &kuadrantv1.RateLimitPolicy{
					TypeMeta:   metav1.TypeMeta{Kind: "RateLimitPolicy", APIVersion: kuadrantv1.GroupVersion.String()},
					ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("rlp-gw-%d", gc), Namespace: "default", UID: types.UID(fmt.Sprintf("uid-rlp-gw-%d", gc))},
					Spec: kuadrantv1.RateLimitPolicySpec{
						TargetRef:                 gwTargetRef,
						RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{Limits: map[string]kuadrantv1.Limit{"requests": {Rates: []kuadrantv1.Rate{{Limit: 10, Window: "10s"}}}}},
					},
					Status: kuadrantv1.RateLimitPolicyStatus{Conditions: []metav1.Condition{acceptedCondition}},
				}
				policies = append(policies, p)
				store[string(p.UID)] = p
			}
			if params.attachTRLPolicies {
				p := &kuadrantv1alpha1.TokenRateLimitPolicy{
					TypeMeta:   metav1.TypeMeta{Kind: "TokenRateLimitPolicy", APIVersion: kuadrantv1alpha1.GroupVersion.String()},
					ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("trlp-gw-%d", gc), Namespace: "default", UID: types.UID(fmt.Sprintf("uid-trlp-gw-%d", gc))},
					Spec: kuadrantv1alpha1.TokenRateLimitPolicySpec{
						TargetRef:                      gwTargetRef,
						TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{},
					},
					Status: kuadrantv1alpha1.TokenRateLimitPolicyStatus{Conditions: []metav1.Condition{acceptedCondition}},
				}
				policies = append(policies, p)
				store[string(p.UID)] = p
			}
		}

		for r := 0; r < params.numRoutesPerGW; r++ {
			routeName := fmt.Sprintf("route-%d-%d", gc, r)
			rules := make([]gatewayapiv1.HTTPRouteRule, params.numRulesPerRoute)
			for k := 0; k < params.numRulesPerRoute; k++ {
				rules[k] = gatewayapiv1.HTTPRouteRule{
					Name: ptr.To(gatewayapiv1.SectionName(fmt.Sprintf("rule-%d", k))),
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{Path: &gatewayapiv1.HTTPPathMatch{Value: ptr.To(fmt.Sprintf("/%s/rule-%d", routeName, k))}},
					},
					BackendRefs: []gatewayapiv1.HTTPBackendRef{
						{BackendRef: gatewayapiv1.BackendRef{BackendObjectReference: gatewayapiv1.BackendObjectReference{Name: "backend"}}},
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
					Hostnames: []gatewayapiv1.Hostname{gatewayapiv1.Hostname(fmt.Sprintf("app-%d-%d.example.com", gc, r))},
					Rules:     rules,
				},
			}
			httpRoutes = append(httpRoutes, route)
			store[string(route.UID)] = route

			if params.policyTarget == "route" {
				routeTargetRef := gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1alpha2.Group(machinery.HTTPRouteGroupKind.Group),
						Kind:  gatewayapiv1alpha2.Kind(machinery.HTTPRouteGroupKind.Kind),
						Name:  gatewayapiv1alpha2.ObjectName(routeName),
					},
				}
				globalRouteIdx := gc*params.numRoutesPerGW + r

				if params.attachAuthPolicies {
					p := &kuadrantv1.AuthPolicy{
						TypeMeta:   metav1.TypeMeta{Kind: "AuthPolicy", APIVersion: kuadrantv1.GroupVersion.String()},
						ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("auth-%d", globalRouteIdx), Namespace: "default", UID: types.UID(fmt.Sprintf("uid-auth-%d", globalRouteIdx))},
						Spec: kuadrantv1.AuthPolicySpec{
							TargetRef: routeTargetRef,
							AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
								AuthScheme: &kuadrantv1.AuthSchemeSpec{
									Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
										fmt.Sprintf("apikey-%d", globalRouteIdx): {},
									},
								},
							},
						},
						Status: kuadrantv1.AuthPolicyStatus{Conditions: []metav1.Condition{acceptedCondition}},
					}
					policies = append(policies, p)
					store[string(p.UID)] = p
				}
				if params.attachRLPolicies {
					p := &kuadrantv1.RateLimitPolicy{
						TypeMeta:   metav1.TypeMeta{Kind: "RateLimitPolicy", APIVersion: kuadrantv1.GroupVersion.String()},
						ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("rlp-%d", globalRouteIdx), Namespace: "default", UID: types.UID(fmt.Sprintf("uid-rlp-%d", globalRouteIdx))},
						Spec: kuadrantv1.RateLimitPolicySpec{
							TargetRef:                 routeTargetRef,
							RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{},
						},
						Status: kuadrantv1.RateLimitPolicyStatus{Conditions: []metav1.Condition{acceptedCondition}},
					}
					policies = append(policies, p)
					store[string(p.UID)] = p
				}
				if params.attachTRLPolicies {
					p := &kuadrantv1alpha1.TokenRateLimitPolicy{
						TypeMeta:   metav1.TypeMeta{Kind: "TokenRateLimitPolicy", APIVersion: kuadrantv1alpha1.GroupVersion.String()},
						ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("trlp-%d", globalRouteIdx), Namespace: "default", UID: types.UID(fmt.Sprintf("uid-trlp-%d", globalRouteIdx))},
						Spec: kuadrantv1alpha1.TokenRateLimitPolicySpec{
							TargetRef:                      routeTargetRef,
							TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{},
						},
						Status: kuadrantv1alpha1.TokenRateLimitPolicyStatus{Conditions: []metav1.Condition{acceptedCondition}},
					}
					policies = append(policies, p)
					store[string(p.UID)] = p
				}
			}
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

// ---------------------------------------------------------------------------
// Original benchmarks: individual reconciler hot paths
// ---------------------------------------------------------------------------

// BenchmarkCalculateEffectiveAuthPolicies measures the O(GatewayClasses × RouteRules) loop
// that calls Paths() for each pair — the most expensive reconciliation path.
func BenchmarkCalculateEffectiveAuthPolicies(b *testing.B) {
	cases := []struct {
		name string
		p    benchTopologyParams
	}{
		{"1gc-10routes", benchTopologyParams{numGatewayClasses: 1, numRoutesPerGW: 10, numRulesPerRoute: 2, attachAuthPolicies: true}},
		{"1gc-100routes", benchTopologyParams{numGatewayClasses: 1, numRoutesPerGW: 100, numRulesPerRoute: 2, attachAuthPolicies: true}},
		{"1gc-300routes", benchTopologyParams{numGatewayClasses: 1, numRoutesPerGW: 300, numRulesPerRoute: 2, attachAuthPolicies: true}},
		{"3gc-100routes", benchTopologyParams{numGatewayClasses: 3, numRoutesPerGW: 100, numRulesPerRoute: 2, attachAuthPolicies: true}},
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
			topology, _, _ := buildBenchTopology(b, benchTopologyParams{numGatewayClasses: 1, numRoutesPerGW: tc.routes, numRulesPerRoute: tc.rules})
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
			topology, kuadrant, state := buildBenchTopology(b, benchTopologyParams{numGatewayClasses: 1, numRoutesPerGW: 1, numRulesPerRoute: tc.rules, attachAuthPolicies: true})
			ctx := context.Background()

			effectivePolicies := CalculateEffectiveAuthPolicies(ctx, topology, kuadrant, state)

			type authConfigInput struct {
				effectivePolicy  EffectiveAuthPolicy
				name             string
				annotationKey    string
				routeRuleLocator string
			}
			var inputs []authConfigInput
			for pathID, ep := range effectivePolicies {
				inputs = append(inputs, authConfigInput{
					effectivePolicy:  ep,
					name:             AuthConfigNameForPath(pathID),
					annotationKey:    kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation,
					routeRuleLocator: ep.Path[len(ep.Path)-1].GetLocator(),
				})
			}

			r := &AuthConfigsReconciler{client: nil}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, in := range inputs {
					r.buildDesiredAuthConfig(ctx, in.effectivePolicy, in.name, "kuadrant-system", in.annotationKey, in.routeRuleLocator)
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
			topology, kuadrant, state := buildBenchTopology(b, benchTopologyParams{numGatewayClasses: 1, numRoutesPerGW: tc.routes, numRulesPerRoute: 2, attachRLPolicies: true})
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
		name string
		p    benchTopologyParams
	}{
		{"shallow-narrow", benchTopologyParams{numGatewayClasses: 1, numRoutesPerGW: 5, numRulesPerRoute: 2}},
		{"shallow-wide", benchTopologyParams{numGatewayClasses: 1, numRoutesPerGW: 100, numRulesPerRoute: 2}},
		{"deep-wide", benchTopologyParams{numGatewayClasses: 3, numRoutesPerGW: 100, numRulesPerRoute: 5}},
		{"stress", benchTopologyParams{numGatewayClasses: 1, numRoutesPerGW: 300, numRulesPerRoute: 2}},
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

// ---------------------------------------------------------------------------
// Benchmarks from Mike Nairn's PR #2031: scaling dimensions and composite cycles
// ---------------------------------------------------------------------------

// BenchmarkEffectiveRateLimitPolicies measures the RateLimitPolicy effective
// policy calculation via Reconcile() (calculateEffectivePolicies is unexported).
func BenchmarkEffectiveRateLimitPolicies(b *testing.B) {
	cases := []benchTopologyParams{
		{numGatewayClasses: 1, numRoutesPerGW: 10, numRulesPerRoute: 1, attachRLPolicies: true, policyTarget: "route"},
		{numGatewayClasses: 1, numRoutesPerGW: 100, numRulesPerRoute: 1, attachRLPolicies: true, policyTarget: "route"},
		{numGatewayClasses: 1, numRoutesPerGW: 300, numRulesPerRoute: 1, attachRLPolicies: true, policyTarget: "route"},
	}
	reconciler := &EffectiveRateLimitPolicyReconciler{}
	for _, tc := range cases {
		b.Run(tc.label(), func(b *testing.B) {
			topology, _, _ := buildBenchTopology(b, tc)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := &sync.Map{}
				reconciler.Reconcile(context.Background(), nil, topology, nil, state)
			}
		})
	}
}

// BenchmarkEffectiveAuthPoliciesListenerFanout measures the impact of increasing
// listeners per gateway. More listeners = more paths in the DFS traversal.
// Reproduces the scaling pattern from #1085 where 63 listeners caused 30-minute reconciliation.
func BenchmarkEffectiveAuthPoliciesListenerFanout(b *testing.B) {
	cases := []benchTopologyParams{
		{numGatewayClasses: 1, numRoutesPerGW: 100, numRulesPerRoute: 1, listenersPerGW: 1, attachAuthPolicies: true, policyTarget: "route"},
		{numGatewayClasses: 1, numRoutesPerGW: 100, numRulesPerRoute: 1, listenersPerGW: 4, attachAuthPolicies: true, policyTarget: "route"},
		{numGatewayClasses: 1, numRoutesPerGW: 100, numRulesPerRoute: 1, listenersPerGW: 16, attachAuthPolicies: true, policyTarget: "route"},
		{numGatewayClasses: 1, numRoutesPerGW: 100, numRulesPerRoute: 1, listenersPerGW: 32, attachAuthPolicies: true, policyTarget: "route"},
	}
	for _, tc := range cases {
		topology, kuadrant, _ := buildBenchTopology(b, tc)
		b.Run(tc.label(), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := &sync.Map{}
				CalculateEffectiveAuthPolicies(context.Background(), topology, kuadrant, state)
			}
		})
	}
}

// BenchmarkEffectiveAuthPoliciesMultiGateway measures the impact of
// multiple gateways with routes distributed evenly across them.
func BenchmarkEffectiveAuthPoliciesMultiGateway(b *testing.B) {
	cases := []benchTopologyParams{
		{numGatewayClasses: 3, numRoutesPerGW: 100, numRulesPerRoute: 1, attachAuthPolicies: true, policyTarget: "route"},
		{numGatewayClasses: 10, numRoutesPerGW: 30, numRulesPerRoute: 1, attachAuthPolicies: true, policyTarget: "route"},
	}
	for _, tc := range cases {
		topology, kuadrant, _ := buildBenchTopology(b, tc)
		b.Run(tc.label(), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := &sync.Map{}
				CalculateEffectiveAuthPolicies(context.Background(), topology, kuadrant, state)
			}
		})
	}
}

// BenchmarkGetKuadrantFromTopology measures the cost of the Kuadrant CR lookup.
// Called ~28 times per reconciliation cycle across all reconcilers.
func BenchmarkGetKuadrantFromTopology(b *testing.B) {
	cases := []benchTopologyParams{
		{numGatewayClasses: 1, numRoutesPerGW: 10, numRulesPerRoute: 1},
		{numGatewayClasses: 1, numRoutesPerGW: 100, numRulesPerRoute: 1},
		{numGatewayClasses: 1, numRoutesPerGW: 300, numRulesPerRoute: 1},
	}
	for _, tc := range cases {
		topology, _, _ := buildBenchTopology(b, tc)
		b.Run(tc.label(), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				GetKuadrantFromTopology(topology, &sync.Map{})
			}
		})
	}
}

// BenchmarkTopologyBuild measures the cost of constructing the GatewayAPI
// topology itself, which happens on every reconciliation cycle.
func BenchmarkTopologyBuild(b *testing.B) {
	cases := []benchTopologyParams{
		{numGatewayClasses: 1, numRoutesPerGW: 10, numRulesPerRoute: 1},
		{numGatewayClasses: 1, numRoutesPerGW: 100, numRulesPerRoute: 1},
		{numGatewayClasses: 1, numRoutesPerGW: 300, numRulesPerRoute: 1},
		{numGatewayClasses: 1, numRoutesPerGW: 300, numRulesPerRoute: 1, listenersPerGW: 32},
		{numGatewayClasses: 3, numRoutesPerGW: 100, numRulesPerRoute: 1, listenersPerGW: 16},
	}
	for _, tc := range cases {
		b.Run(tc.label(), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buildBenchTopology(b, tc)
			}
		})
	}
}

// BenchmarkFullReconciliationCycle simulates a full reconciliation cycle:
// build topology, look up Kuadrant CR (as each reconciler does), then
// calculate effective policies for all three policy types.
func BenchmarkFullReconciliationCycle(b *testing.B) {
	cases := []benchTopologyParams{
		{numGatewayClasses: 1, numRoutesPerGW: 10, numRulesPerRoute: 1, attachAuthPolicies: true, attachRLPolicies: true, attachTRLPolicies: true, policyTarget: "route"},
		{numGatewayClasses: 1, numRoutesPerGW: 100, numRulesPerRoute: 1, attachAuthPolicies: true, attachRLPolicies: true, attachTRLPolicies: true, policyTarget: "route"},
		{numGatewayClasses: 1, numRoutesPerGW: 300, numRulesPerRoute: 1, attachAuthPolicies: true, attachRLPolicies: true, attachTRLPolicies: true, policyTarget: "route"},
	}

	authReconciler := &EffectiveAuthPolicyReconciler{}
	rlpReconciler := &EffectiveRateLimitPolicyReconciler{}
	trlpReconciler := &EffectiveTokenRateLimitPolicyReconciler{}

	for _, tc := range cases {
		b.Run(tc.label(), func(b *testing.B) {
			ctx := context.Background()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				topology, _, _ := buildBenchTopology(b, tc)
				state := &sync.Map{}
				for j := 0; j < 28; j++ {
					GetKuadrantFromTopology(topology, state)
				}
				authReconciler.Reconcile(ctx, nil, topology, nil, state)
				rlpReconciler.Reconcile(ctx, nil, topology, nil, state)
				trlpReconciler.Reconcile(ctx, nil, topology, nil, state)
			}
		})
	}
}
