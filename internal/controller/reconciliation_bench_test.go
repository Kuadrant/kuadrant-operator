//go:build unit

package controllers

import (
	"context"
	"fmt"
	"sync"
	"testing"

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
)

type topologyParams struct {
	numGateways          int
	listenersPerGateway  int
	numRoutes            int
}

func (p topologyParams) label() string {
	return fmt.Sprintf("gw=%d_listen=%d_routes=%d", p.numGateways, p.listenersPerGateway, p.numRoutes)
}

// buildScaleTopology constructs a realistic Kuadrant topology at a given scale:
//   - 1 Kuadrant CR, 1 GatewayClass
//   - G Gateways, each with L Listeners
//   - N HTTPRoutes (each with 1 rule, 1 AuthPolicy attached)
//
// Routes are distributed evenly across gateways. Each route references
// its parent gateway by name (not a specific listener section), which
// means the Gateway API topology expands the route to all listeners —
// creating the fan-out that multiplies DFS paths.
func buildScaleTopology(b testing.TB, params topologyParams) (*machinery.Topology, *kuadrantv1beta1.Kuadrant) {
	b.Helper()

	const (
		namespace        = "default"
		gatewayClassName = "kuadrant-gateway-class"
	)

	kuadrant := &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantv1beta1.KuadrantGroupKind.Kind,
			APIVersion: kuadrantv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kuadrant",
			Namespace: namespace,
			UID:       types.UID("kuadrant-uid"),
		},
	}

	gatewayClass := &gatewayapiv1.GatewayClass{
		TypeMeta: metav1.TypeMeta{
			Kind:       machinery.GatewayClassGroupKind.Kind,
			APIVersion: gatewayapiv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassName,
			UID:  types.UID("gwclass-uid"),
		},
		Spec: gatewayapiv1.GatewayClassSpec{
			ControllerName: "kuadrant.io/policy-controller",
		},
	}

	store := make(controller.Store)
	store[string(kuadrant.UID)] = kuadrant
	store[string(gatewayClass.UID)] = gatewayClass

	gateways := make([]*gatewayapiv1.Gateway, params.numGateways)
	for g := range params.numGateways {
		gwName := fmt.Sprintf("gateway-%d", g)
		gwUID := types.UID(fmt.Sprintf("gw-uid-%d", g))

		listeners := make([]gatewayapiv1.Listener, params.listenersPerGateway)
		for l := range params.listenersPerGateway {
			listeners[l] = gatewayapiv1.Listener{
				Name:     gatewayapiv1.SectionName(fmt.Sprintf("listener-%d", l)),
				Hostname: ptr.To(gatewayapiv1.Hostname(fmt.Sprintf("*.gw%d-l%d.example.com", g, l))),
			}
		}

		gateways[g] = &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       machinery.GatewayGroupKind.Kind,
				APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      gwName,
				Namespace: namespace,
				UID:       gwUID,
			},
			Spec: gatewayapiv1.GatewaySpec{
				GatewayClassName: gatewayClassName,
				Listeners:        listeners,
			},
		}
		store[string(gwUID)] = gateways[g]
	}

	httpRoutes := make([]*gatewayapiv1.HTTPRoute, params.numRoutes)
	allPolicies := make([]machinery.Policy, 0, params.numRoutes*3)

	for i := range params.numRoutes {
		gwIndex := i % params.numGateways
		gwName := fmt.Sprintf("gateway-%d", gwIndex)
		routeName := fmt.Sprintf("route-%d", i)
		routeUID := types.UID(fmt.Sprintf("route-uid-%d", i))

		httpRoutes[i] = &gatewayapiv1.HTTPRoute{
			TypeMeta: metav1.TypeMeta{
				Kind:       machinery.HTTPRouteGroupKind.Kind,
				APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      routeName,
				Namespace: namespace,
				UID:       routeUID,
			},
			Spec: gatewayapiv1.HTTPRouteSpec{
				CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
					ParentRefs: []gatewayapiv1.ParentReference{
						{Name: gatewayapiv1.ObjectName(gwName)},
					},
				},
				Hostnames: []gatewayapiv1.Hostname{
					gatewayapiv1.Hostname(fmt.Sprintf("app-%d.example.com", i)),
				},
				Rules: []gatewayapiv1.HTTPRouteRule{
					{
						Name: ptr.To(gatewayapiv1.SectionName("rule-0")),
						Matches: []gatewayapiv1.HTTPRouteMatch{
							{Path: &gatewayapiv1.HTTPPathMatch{
								Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
								Value: ptr.To("/"),
							}},
						},
						BackendRefs: []gatewayapiv1.HTTPBackendRef{
							{BackendRef: gatewayapiv1.BackendRef{
								BackendObjectReference: gatewayapiv1.BackendObjectReference{
									Name: "backend",
								},
							}},
						},
					},
				},
			},
		}
		store[string(routeUID)] = httpRoutes[i]

		targetRef := gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: gatewayapiv1alpha2.Group(machinery.HTTPRouteGroupKind.Group),
				Kind:  gatewayapiv1alpha2.Kind(machinery.HTTPRouteGroupKind.Kind),
				Name:  gatewayapiv1alpha2.ObjectName(routeName),
			},
		}
		acceptedCondition := metav1.Condition{
			Type:   string(gatewayapiv1alpha2.PolicyConditionAccepted),
			Status: metav1.ConditionTrue,
		}

		// AuthPolicy
		authUID := types.UID(fmt.Sprintf("auth-uid-%d", i))
		authPolicy := &kuadrantv1.AuthPolicy{
			TypeMeta:   metav1.TypeMeta{Kind: "AuthPolicy", APIVersion: kuadrantv1.GroupVersion.String()},
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("auth-%d", i), Namespace: namespace, UID: authUID},
			Spec: kuadrantv1.AuthPolicySpec{
				TargetRef: targetRef,
				AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
					AuthScheme: &kuadrantv1.AuthSchemeSpec{
						Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
							fmt.Sprintf("apikey-%d", i): {},
						},
					},
				},
			},
			Status: kuadrantv1.AuthPolicyStatus{Conditions: []metav1.Condition{acceptedCondition}},
		}
		store[string(authUID)] = authPolicy
		allPolicies = append(allPolicies, authPolicy)

		// RateLimitPolicy
		rlpUID := types.UID(fmt.Sprintf("rlp-uid-%d", i))
		rlpPolicy := &kuadrantv1.RateLimitPolicy{
			TypeMeta:   metav1.TypeMeta{Kind: "RateLimitPolicy", APIVersion: kuadrantv1.GroupVersion.String()},
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("rlp-%d", i), Namespace: namespace, UID: rlpUID},
			Spec: kuadrantv1.RateLimitPolicySpec{
				TargetRef:                targetRef,
				RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{},
			},
			Status: kuadrantv1.RateLimitPolicyStatus{Conditions: []metav1.Condition{acceptedCondition}},
		}
		store[string(rlpUID)] = rlpPolicy
		allPolicies = append(allPolicies, rlpPolicy)

		// TokenRateLimitPolicy
		trlpUID := types.UID(fmt.Sprintf("trlp-uid-%d", i))
		trlpPolicy := &kuadrantv1alpha1.TokenRateLimitPolicy{
			TypeMeta:   metav1.TypeMeta{Kind: "TokenRateLimitPolicy", APIVersion: kuadrantv1alpha1.GroupVersion.String()},
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("trlp-%d", i), Namespace: namespace, UID: trlpUID},
			Spec: kuadrantv1alpha1.TokenRateLimitPolicySpec{
				TargetRef:                     targetRef,
				TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{},
			},
			Status: kuadrantv1alpha1.TokenRateLimitPolicyStatus{Conditions: []metav1.Condition{acceptedCondition}},
		}
		store[string(trlpUID)] = trlpPolicy
		allPolicies = append(allPolicies, trlpPolicy)
	}

	topology, err := machinery.NewGatewayAPITopology(
		machinery.WithGatewayClasses(gatewayClass),
		machinery.WithGateways(gateways...),
		machinery.ExpandGatewayListeners(),
		machinery.WithHTTPRoutes(httpRoutes...),
		machinery.ExpandHTTPRouteRules(),
		machinery.WithGatewayAPITopologyPolicies(allPolicies...),
		machinery.WithGatewayAPITopologyObjects(kuadrant),
		machinery.WithGatewayAPITopologyLinks(
			kuadrantv1beta1.LinkKuadrantToGatewayClasses(store),
		),
	)
	if err != nil {
		b.Fatalf("failed to create topology: %v", err)
	}

	return topology, kuadrant
}

// BenchmarkCalculateEffectiveAuthPolicies measures the full effective policy
// calculation at varying scale. This is the hot path identified in CONNLINK-895.
//
// Run with pprof:
//
//	go test -tags=unit -bench=BenchmarkCalculateEffectiveAuthPolicies -cpuprofile=cpu.prof -memprofile=mem.prof ./internal/controller/
//	go tool pprof -http=:8080 cpu.prof
func BenchmarkCalculateEffectiveAuthPolicies(b *testing.B) {
	benchCases := []topologyParams{
		// Vary routes with fixed 1 gw / 1 listener (CONNLINK-895 scenario)
		{1, 1, 10},
		{1, 1, 50},
		{1, 1, 100},
		{1, 1, 300},
		{1, 1, 600},
		{1, 1, 1000},
	}

	for _, bc := range benchCases {
		topology, kuadrant := buildScaleTopology(b, bc)

		b.Run(bc.label(), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				state := &sync.Map{}
				CalculateEffectiveAuthPolicies(context.Background(), topology, kuadrant, state)
			}
		})
	}
}

// BenchmarkEffectiveRateLimitPolicies measures the RateLimitPolicy effective
// policy calculation. Uses Reconcile() since calculateEffectivePolicies is unexported.
func BenchmarkEffectiveRateLimitPolicies(b *testing.B) {
	benchCases := []topologyParams{
		{1, 1, 10},
		{1, 1, 100},
		{1, 1, 300},
		{1, 1, 600},
		{1, 1, 1000},
	}

	reconciler := &EffectiveRateLimitPolicyReconciler{}

	for _, bc := range benchCases {
		topology, _ := buildScaleTopology(b, bc)

		b.Run(bc.label(), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				state := &sync.Map{}
				_ = reconciler.Reconcile(context.Background(), nil, topology, nil, state)
			}
		})
	}
}

// BenchmarkEffectiveTokenRateLimitPolicies measures the TokenRateLimitPolicy
// effective policy calculation.
func BenchmarkEffectiveTokenRateLimitPolicies(b *testing.B) {
	benchCases := []topologyParams{
		{1, 1, 10},
		{1, 1, 100},
		{1, 1, 300},
		{1, 1, 600},
		{1, 1, 1000},
	}

	reconciler := &EffectiveTokenRateLimitPolicyReconciler{}

	for _, bc := range benchCases {
		topology, _ := buildScaleTopology(b, bc)

		b.Run(bc.label(), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				state := &sync.Map{}
				_ = reconciler.Reconcile(context.Background(), nil, topology, nil, state)
			}
		})
	}
}

// BenchmarkEffectiveAuthPoliciesListenerFanout measures the impact of
// increasing listeners per gateway. More listeners = more paths in the
// DFS traversal. This reproduces the scaling pattern from GH #1085
// where 63 listeners caused 30-minute reconciliation.
func BenchmarkEffectiveAuthPoliciesListenerFanout(b *testing.B) {
	benchCases := []topologyParams{
		{1, 1, 100},
		{1, 4, 100},
		{1, 16, 100},
		{1, 32, 100},
		{1, 64, 100},
	}

	for _, bc := range benchCases {
		topology, kuadrant := buildScaleTopology(b, bc)

		b.Run(bc.label(), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				state := &sync.Map{}
				CalculateEffectiveAuthPolicies(context.Background(), topology, kuadrant, state)
			}
		})
	}
}

// BenchmarkEffectiveAuthPoliciesMultiGateway measures the impact of
// multiple gateways. Routes are distributed evenly across gateways.
func BenchmarkEffectiveAuthPoliciesMultiGateway(b *testing.B) {
	benchCases := []topologyParams{
		{1, 1, 300},
		{3, 1, 300},
		{10, 1, 300},
		{3, 16, 300},
	}

	for _, bc := range benchCases {
		topology, kuadrant := buildScaleTopology(b, bc)

		b.Run(bc.label(), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				state := &sync.Map{}
				CalculateEffectiveAuthPolicies(context.Background(), topology, kuadrant, state)
			}
		})
	}
}

// BenchmarkGetKuadrantFromTopology measures the cost of the Kuadrant CR lookup.
// This is called ~28 times per reconciliation cycle across all reconcilers.
// PR #1957 adds caching via sync.Map state — this benchmark quantifies the
// uncached cost to justify that optimisation.
func BenchmarkGetKuadrantFromTopology(b *testing.B) {
	benchCases := []topologyParams{
		{1, 1, 10},
		{1, 1, 100},
		{1, 1, 300},
		{1, 1, 600},
		{1, 1, 1000},
	}

	for _, bc := range benchCases {
		topology, _ := buildScaleTopology(b, bc)

		b.Run(bc.label(), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				GetKuadrantFromTopology(topology)
			}
		})
	}
}

// BenchmarkTopologyBuild measures the cost of constructing the GatewayAPI
// topology itself, which happens on every reconciliation cycle.
func BenchmarkTopologyBuild(b *testing.B) {
	benchCases := []topologyParams{
		{1, 1, 10},
		{1, 1, 100},
		{1, 1, 300},
		{1, 1, 1000},
		{1, 32, 300},
		{3, 16, 300},
	}

	for _, bc := range benchCases {
		b.Run(bc.label(), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				buildScaleTopology(b, bc)
			}
		})
	}
}

// BenchmarkFullReconciliationCycle simulates what happens in a single
// reconciliation cycle: build topology, look up Kuadrant CR multiple times
// (as each reconciler does), then calculate effective policies.
func BenchmarkFullReconciliationCycle(b *testing.B) {
	benchCases := []topologyParams{
		{1, 1, 10},
		{1, 1, 100},
		{1, 1, 300},
		{1, 1, 1000},
		{1, 32, 100},
		{3, 16, 300},
	}

	for _, bc := range benchCases {
		b.Run(bc.label(), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				topology, kuadrant := buildScaleTopology(b, bc)
				state := &sync.Map{}

				for range 28 {
					GetKuadrantFromTopology(topology)
				}

				CalculateEffectiveAuthPolicies(context.Background(), topology, kuadrant, state)
			}
		})
	}
}

// BenchmarkMultiReconcilerCycle simulates a realistic reconciliation cycle
// where all three effective policy reconcilers run against the same topology
// and shared state. This is the pattern that PR #1957's GetKuadrantFromTopology
// caching targets — each reconciler calls GetKuadrantFromTopology internally,
// so caching benefits compound across reconcilers.
func BenchmarkMultiReconcilerCycle(b *testing.B) {
	benchCases := []topologyParams{
		{1, 1, 10},
		{1, 1, 100},
		{1, 1, 300},
		{1, 1, 600},
		{1, 32, 100},
	}

	authReconciler := &EffectiveAuthPolicyReconciler{}
	rlpReconciler := &EffectiveRateLimitPolicyReconciler{}
	trlpReconciler := &EffectiveTokenRateLimitPolicyReconciler{}

	for _, bc := range benchCases {
		topology, _ := buildScaleTopology(b, bc)

		b.Run(bc.label(), func(b *testing.B) {
			b.ReportAllocs()
			ctx := context.Background()
			for range b.N {
				state := &sync.Map{}
				_ = authReconciler.Reconcile(ctx, nil, topology, nil, state)
				_ = rlpReconciler.Reconcile(ctx, nil, topology, nil, state)
				_ = trlpReconciler.Reconcile(ctx, nil, topology, nil, state)
			}
		})
	}
}
