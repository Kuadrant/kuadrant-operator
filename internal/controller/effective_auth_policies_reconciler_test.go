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
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

func TestCalculateEffectiveAuthPolicies(t *testing.T) {
	const (
		namespace        = "default"
		gatewayClassName = "kuadrant-gateway-class"
		gatewayName      = "kuadrant-gateway"
		listenerName     = "http"
		routeName        = "http-route"
	)

	kuadrant := &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantv1beta1.KuadrantGroupKind.Kind,
			APIVersion: kuadrantv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kuadrant",
			Namespace: namespace,
			UID:       types.UID(rand.String(9)),
		},
		Spec: kuadrantv1beta1.KuadrantSpec{},
	}

	gatewayClass := &gatewayapiv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassName,
			UID:  types.UID(rand.String(9)),
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       machinery.GatewayClassGroupKind.Kind,
			APIVersion: gatewayapiv1.GroupVersion.String(),
		},
		Spec: gatewayapiv1.GatewayClassSpec{
			ControllerName: gatewayapiv1.GatewayController("kuadrant.io/policy-controller"),
		},
	}

	gateway := &gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayName,
			Namespace: namespace,
			UID:       types.UID(rand.String(9)),
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       machinery.GatewayGroupKind.Kind,
			APIVersion: gatewayapiv1.GroupVersion.String(),
		},
		Spec: gatewayapiv1.GatewaySpec{
			GatewayClassName: gatewayClassName,
			Listeners: []gatewayapiv1.Listener{
				{
					Name:     listenerName,
					Hostname: ptr.To[gatewayapiv1.Hostname]("localhost"),
				},
			},
		},
	}

	httpRoute := &gatewayapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: namespace,
			UID:       types.UID(rand.String(9)),
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       machinery.HTTPRouteGroupKind.Kind,
			APIVersion: gatewayapiv1.GroupVersion.String(),
		},
		Spec: gatewayapiv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1.ParentReference{
					{
						Name: gatewayName,
					},
				},
			},
			Rules: []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
						},
					},
					BackendRefs: []gatewayapiv1.HTTPBackendRef{
						{
							BackendRef: gatewayapiv1.BackendRef{
								BackendObjectReference: gatewayapiv1.BackendObjectReference{
									Name: gatewayapiv1.ObjectName("backend-ro"),
								},
							},
						},
					},
				},
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Method: ptr.To(gatewayapiv1.HTTPMethod("POST")),
						},
					},
					BackendRefs: []gatewayapiv1.HTTPBackendRef{
						{
							BackendRef: gatewayapiv1.BackendRef{
								BackendObjectReference: gatewayapiv1.BackendObjectReference{
									Name: gatewayapiv1.ObjectName("backend-rw"),
								},
							},
						},
					},
				},
			},
		},
	}

	policyFactory := func(f ...func(p *kuadrantv1.AuthPolicy)) *kuadrantv1.AuthPolicy {
		policy := &kuadrantv1.AuthPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gw-auth",
				Namespace: namespace,
				UID:       types.UID(rand.String(9)),
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "AuthPolicy",
				APIVersion: kuadrantv1.GroupVersion.String(),
			},
			Spec: kuadrantv1.AuthPolicySpec{},
			Status: kuadrantv1.AuthPolicyStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(gatewayapiv1alpha2.PolicyConditionAccepted),
						Status: metav1.ConditionTrue,
					},
				},
			},
		}
		for _, fn := range f {
			fn(policy)
		}
		return policy
	}

	gatewayPolicy := policyFactory(func(p *kuadrantv1.AuthPolicy) {
		p.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: gatewayapiv1alpha2.Group(machinery.GatewayGroupKind.Group),
				Kind:  gatewayapiv1.Kind(machinery.GatewayGroupKind.Kind),
				Name:  gatewayName,
			},
		}
		p.Spec.Overrides = &kuadrantv1.MergeableAuthPolicySpec{
			Strategy: "merge",
			AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
				AuthScheme: &kuadrantv1.AuthSchemeSpec{
					Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
						"jwt": {
							AuthenticationSpec: authorinov1beta3.AuthenticationSpec{
								AuthenticationMethodSpec: authorinov1beta3.AuthenticationMethodSpec{
									Jwt: &authorinov1beta3.JwtAuthenticationSpec{
										IssuerUrl: "http://my-auth-server/auth",
									},
								},
							},
						},
					},
				},
			},
		}
	})
	routePolicy := policyFactory(func(p *kuadrantv1.AuthPolicy) {
		p.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: gatewayapiv1alpha2.Group(machinery.HTTPRouteGroupKind.Group),
				Kind:  gatewayapiv1alpha2.Kind(machinery.HTTPRouteGroupKind.Kind),
				Name:  routeName,
			},
		}
		p.Spec.AuthPolicySpecProper = kuadrantv1.AuthPolicySpecProper{
			AuthScheme: &kuadrantv1.AuthSchemeSpec{
				Authorization: map[string]kuadrantv1.MergeableAuthorizationSpec{
					"admins-only": {
						AuthorizationSpec: authorinov1beta3.AuthorizationSpec{
							AuthorizationMethodSpec: authorinov1beta3.AuthorizationMethodSpec{
								PatternMatching: &authorinov1beta3.PatternMatchingAuthorizationSpec{
									Patterns: []authorinov1beta3.PatternExpressionOrRef{
										{
											CelPredicate: authorinov1beta3.CelPredicate{
												Predicate: "'admin' in auth.identity.groups",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}
	})
	routeRulePolicy := policyFactory(func(p *kuadrantv1.AuthPolicy) {
		p.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: gatewayapiv1alpha2.Group(machinery.HTTPRouteGroupKind.Group),
				Kind:  gatewayapiv1alpha2.Kind(machinery.HTTPRouteGroupKind.Kind),
				Name:  routeName,
			},
			SectionName: ptr.To(gatewayapiv1.SectionName("rule-1")),
		}
		p.Spec.AuthPolicySpecProper = kuadrantv1.AuthPolicySpecProper{
			AuthScheme: &kuadrantv1.AuthSchemeSpec{
				Authorization: map[string]kuadrantv1.MergeableAuthorizationSpec{
					"admins-or-privileged": {
						AuthorizationSpec: authorinov1beta3.AuthorizationSpec{
							AuthorizationMethodSpec: authorinov1beta3.AuthorizationMethodSpec{
								PatternMatching: &authorinov1beta3.PatternMatchingAuthorizationSpec{
									Patterns: []authorinov1beta3.PatternExpressionOrRef{
										{
											CelPredicate: authorinov1beta3.CelPredicate{
												Predicate: "'admin' in auth.identity.groups || 'privileged' in auth.identity.groups",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}
	})

	store := make(controller.Store)
	store[string(kuadrant.UID)] = kuadrant
	store[string(gatewayClass.UID)] = gatewayClass
	store[string(gateway.UID)] = gateway
	store[string(httpRoute.UID)] = httpRoute
	store[string(gatewayPolicy.UID)] = gatewayPolicy
	store[string(routePolicy.UID)] = routePolicy
	store[string(routeRulePolicy.UID)] = routeRulePolicy

	topology, err := machinery.NewGatewayAPITopology(
		machinery.WithGatewayClasses(gatewayClass),
		machinery.WithGateways(gateway),
		machinery.ExpandGatewayListeners(),
		machinery.WithHTTPRoutes(httpRoute),
		machinery.ExpandHTTPRouteRules(),
		machinery.WithGatewayAPITopologyPolicies(gatewayPolicy, routePolicy, routeRulePolicy),
		machinery.WithGatewayAPITopologyObjects(kuadrant),
		machinery.WithGatewayAPITopologyLinks(
			kuadrantv1beta1.LinkKuadrantToGatewayClasses(store),
		),
	)
	if err != nil {
		t.Fatalf("failed to create topology: %v", err)
	}

	effectiveAuthPolicies := CalculateEffectiveAuthPolicies(context.TODO(), topology, kuadrant, &sync.Map{})

	if len(effectiveAuthPolicies) != 2 {
		t.Fatalf("expected 2 effective auth policies, got %d", len(effectiveAuthPolicies))
	}

	// rule-1
	effectivePolicy, found := lo.Find(lo.Values(effectiveAuthPolicies), func(ep EffectiveAuthPolicy) bool {
		return len(ep.Path) > 0 && ep.Path[len(ep.Path)-1].GetName() == fmt.Sprintf("%s#rule-1", routeName)
	})
	if !found {
		t.Fatalf("expected effective policy for rule-1, got none")
	}
	if authn := effectivePolicy.Spec.Spec.Proper().AuthScheme.Authentication; len(authn) == 0 || !lo.HasKey(authn, "jwt") {
		t.Fatalf("expected effective policy for rule-1 to have authentication 'jwt'")
	}
	if authz := effectivePolicy.Spec.Spec.Proper().AuthScheme.Authorization; len(authz) == 0 || !lo.HasKey(authz, "admins-or-privileged") {
		t.Fatalf("expected effective policy for rule-1 to have authorization 'admins-or-privileged'")
	}

	// rule-2
	effectivePolicy, found = lo.Find(lo.Values(effectiveAuthPolicies), func(ep EffectiveAuthPolicy) bool {
		return len(ep.Path) > 0 && ep.Path[len(ep.Path)-1].GetName() == fmt.Sprintf("%s#rule-2", routeName)
	})
	if !found {
		t.Fatalf("expected effective policy for rule-2, got none")
	}
	if authn := effectivePolicy.Spec.Spec.Proper().AuthScheme.Authentication; len(authn) == 0 || !lo.HasKey(authn, "jwt") {
		t.Fatalf("expected effective policy for rule-2 to have authentication 'jwt'")
	}
	if authz := effectivePolicy.Spec.Spec.Proper().AuthScheme.Authorization; len(authz) == 0 || !lo.HasKey(authz, "admins-only") {
		t.Fatalf("expected effective policy for rule-2 to have authorization 'admins-only'")
	}
}
