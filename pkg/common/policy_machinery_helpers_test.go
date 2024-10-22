//go:build unit

package common

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/policy-machinery/machinery"
)

func TestObjectsInRequestPath(t *testing.T) {
	gatewayClass := func(mutate ...func(*machinery.GatewayClass)) *machinery.GatewayClass {
		o := &machinery.GatewayClass{
			GatewayClass: &gatewayapiv1.GatewayClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayapiv1.SchemeGroupVersion.String(),
					Kind:       machinery.GatewayClassGroupKind.Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "kuadrant",
				},
				Spec: gatewayapiv1.GatewayClassSpec{
					ControllerName: "kuadrant.io/policy-controller",
				},
			},
		}
		for _, m := range mutate {
			m(o)
		}
		return o
	}

	gateway := func(gc *machinery.GatewayClass, mutate ...func(*machinery.Gateway)) *machinery.Gateway {
		if gc == nil {
			gc = gatewayClass()
		}
		o := &machinery.Gateway{
			Gateway: &gatewayapiv1.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayapiv1.SchemeGroupVersion.String(),
					Kind:       machinery.GatewayGroupKind.Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "kuadrant",
				},
				Spec: gatewayapiv1.GatewaySpec{
					GatewayClassName: gatewayapiv1.ObjectName(gc.GetName()),
					Listeners: []gatewayapiv1.Listener{
						{
							Name:     "example",
							Hostname: ptr.To(gatewayapiv1.Hostname("*.example.com")),
							Protocol: gatewayapiv1.ProtocolType("HTTP"),
							Port:     gatewayapiv1.PortNumber(80),
						},
						{
							Name:     "catch-all",
							Protocol: gatewayapiv1.ProtocolType("HTTP"),
							Port:     gatewayapiv1.PortNumber(80),
						},
					},
				},
			},
		}
		for _, m := range mutate {
			m(o)
		}
		return o
	}

	listener := func(g *machinery.Gateway, mutate ...func(*machinery.Listener)) *machinery.Listener {
		if g == nil {
			g = gateway(nil)
		}
		o := &machinery.Listener{
			Listener: &g.Spec.Listeners[0],
			Gateway:  g,
		}
		for _, m := range mutate {
			m(o)
		}
		return o
	}

	httpRoute := func(parent machinery.Targetable, mutate ...func(*machinery.HTTPRoute)) *machinery.HTTPRoute {
		if parent == nil {
			parent = gateway(nil)
		}
		parentRef := gatewayapiv1.ParentReference{
			Name:      gatewayapiv1.ObjectName(parent.GetName()),
			Namespace: ptr.To(gatewayapiv1.Namespace(parent.GetNamespace())),
		}
		if l, ok := parent.(*machinery.Listener); ok {
			parentRef.Name = gatewayapiv1.ObjectName(l.Gateway.GetName())
			parentRef.Namespace = ptr.To(gatewayapiv1.Namespace(l.Gateway.GetNamespace()))
			parentRef.SectionName = ptr.To(gatewayapiv1.SectionName(l.Name))
		}
		o := &machinery.HTTPRoute{
			HTTPRoute: &gatewayapiv1.HTTPRoute{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayapiv1.SchemeGroupVersion.String(),
					Kind:       machinery.HTTPRouteGroupKind.Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example",
					Namespace: "default",
				},
				Spec: gatewayapiv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{parentRef},
					},
					Hostnames: []gatewayapiv1.Hostname{"*.example.com"},
					Rules: []gatewayapiv1.HTTPRouteRule{
						{
							Matches: []gatewayapiv1.HTTPRouteMatch{
								{
									Path: &gatewayapiv1.HTTPPathMatch{Value: ptr.To("/foo")},
								},
							},
							BackendRefs: []gatewayapiv1.HTTPBackendRef{
								{
									BackendRef: gatewayapiv1.BackendRef{
										BackendObjectReference: gatewayapiv1.BackendObjectReference{
											Name: "foo",
										},
									},
								},
							},
						},
						{
							Matches: []gatewayapiv1.HTTPRouteMatch{
								{
									Path: &gatewayapiv1.HTTPPathMatch{Value: ptr.To("/bar")},
								},
							},
							BackendRefs: []gatewayapiv1.HTTPBackendRef{
								{
									BackendRef: gatewayapiv1.BackendRef{
										BackendObjectReference: gatewayapiv1.BackendObjectReference{
											Name: "bar",
										},
									},
								},
							},
						},
					},
				},
			},
		}
		for _, m := range mutate {
			m(o)
		}
		return o
	}

	httpRouteRule := func(r *machinery.HTTPRoute, mutate ...func(*machinery.HTTPRouteRule)) *machinery.HTTPRouteRule {
		if r == nil {
			r = httpRoute(nil)
		}
		o := &machinery.HTTPRouteRule{
			Name:          "rule-1",
			HTTPRoute:     r,
			HTTPRouteRule: &r.Spec.Rules[0],
		}
		for _, m := range mutate {
			m(o)
		}
		return o
	}

	gc := gatewayClass()
	g := gateway(gc)
	l := listener(g)
	r := httpRoute(g)
	rr := httpRouteRule(r)

	routeWithSectionName := httpRoute(l)
	routeRuleWithSectionName := httpRouteRule(routeWithSectionName)

	otherGateway := gateway(nil, func(g *machinery.Gateway) {
		g.ObjectMeta.Name = "other"
		g.Spec.GatewayClassName = "other"
	})
	otherListener := listener(otherGateway)
	otherRoute := httpRoute(otherGateway, func(r *machinery.HTTPRoute) {
		r.ObjectMeta.Name = "other"
	})
	otherRouteRule := httpRouteRule(otherRoute)

	routeWithGatewayMatchingHostname := httpRoute(g, func(r *machinery.HTTPRoute) {
		r.Spec.Hostnames = []gatewayapiv1.Hostname{"foo.example.com"}
	})
	routeRuleWithGatewayMatchingHostname := httpRouteRule(routeWithGatewayMatchingHostname)
	routeWithStrictListenerMatchingHostname := httpRoute(l, func(r *machinery.HTTPRoute) {
		r.Spec.Hostnames = []gatewayapiv1.Hostname{"foo.example.com"}
	})
	routeRuleWithStrictListenerMatchingHostname := httpRouteRule(routeWithStrictListenerMatchingHostname)
	permissiveListener := listener(g, func(l *machinery.Listener) {
		l.Listener = &g.Spec.Listeners[1]
	})
	routeWithPermissiveListenerMatchingHostname := httpRoute(permissiveListener, func(r *machinery.HTTPRoute) {
		r.Spec.Hostnames = []gatewayapiv1.Hostname{"other.org"}
	})
	routeRuleWithPermissiveListenerMatchingHostname := httpRouteRule(routeWithPermissiveListenerMatchingHostname)
	routeWithUnmatchingHostname := httpRoute(l, func(r *machinery.HTTPRoute) {
		r.Spec.Hostnames = []gatewayapiv1.Hostname{"other.org"}
	})

	testCase := []struct {
		name                  string
		path                  []machinery.Targetable
		expectedGatewayClass  *machinery.GatewayClass
		expectedGateway       *machinery.Gateway
		expectedListener      *machinery.Listener
		expectedHTTPRoute     *machinery.HTTPRoute
		expectedHTTPRouteRule *machinery.HTTPRouteRule
		expectedError         error
	}{
		{
			name:          "nil path",
			expectedError: NewErrInvalidPath("empty path"),
		},
		{
			name:          "empty path",
			path:          []machinery.Targetable{},
			expectedError: NewErrInvalidPath("empty path"),
		},
		{
			name:                  "valid path",
			path:                  []machinery.Targetable{gc, g, l, r, rr},
			expectedGatewayClass:  gc,
			expectedGateway:       g,
			expectedListener:      l,
			expectedHTTPRoute:     r,
			expectedHTTPRouteRule: rr,
		},
		{
			name:                  "valid path with route with section name",
			path:                  []machinery.Targetable{gc, g, l, routeWithSectionName, routeRuleWithSectionName},
			expectedGatewayClass:  gc,
			expectedGateway:       g,
			expectedListener:      l,
			expectedHTTPRoute:     routeWithSectionName,
			expectedHTTPRouteRule: routeRuleWithSectionName,
		},
		{
			name:                 "gateway does not belong to the gateway class",
			path:                 []machinery.Targetable{gc, otherGateway, l, r, rr},
			expectedError:        NewErrInvalidPath("gateway does not belong to the gateway class"),
			expectedGatewayClass: gc,
			expectedGateway:      otherGateway,
		},
		{
			name:                 "listener does not belong to the gateway",
			path:                 []machinery.Targetable{gc, g, otherListener, r, rr},
			expectedError:        NewErrInvalidPath("listener does not belong to the gateway"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
			expectedListener:     otherListener,
		},
		{
			name:                 "http route does not belong to the listener",
			path:                 []machinery.Targetable{gc, g, l, otherRoute, otherRouteRule},
			expectedError:        NewErrInvalidPath("http route does not belong to the listener"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
			expectedListener:     l,
			expectedHTTPRoute:    otherRoute,
		},
		{
			name:                  "route with gateway matching hostname",
			path:                  []machinery.Targetable{gc, g, l, routeWithGatewayMatchingHostname, routeRuleWithGatewayMatchingHostname},
			expectedGatewayClass:  gc,
			expectedGateway:       g,
			expectedListener:      l,
			expectedHTTPRoute:     routeWithGatewayMatchingHostname,
			expectedHTTPRouteRule: routeRuleWithGatewayMatchingHostname,
		},
		{
			name:                  "route with strict listener matching hostname",
			path:                  []machinery.Targetable{gc, g, l, routeWithStrictListenerMatchingHostname, routeRuleWithStrictListenerMatchingHostname},
			expectedGatewayClass:  gc,
			expectedGateway:       g,
			expectedListener:      l,
			expectedHTTPRoute:     routeWithStrictListenerMatchingHostname,
			expectedHTTPRouteRule: routeRuleWithStrictListenerMatchingHostname,
		},
		{
			name:                  "route with permissive listener matching hostname",
			path:                  []machinery.Targetable{gc, g, permissiveListener, routeWithPermissiveListenerMatchingHostname, routeRuleWithPermissiveListenerMatchingHostname},
			expectedGatewayClass:  gc,
			expectedGateway:       g,
			expectedListener:      permissiveListener,
			expectedHTTPRoute:     routeWithPermissiveListenerMatchingHostname,
			expectedHTTPRouteRule: routeRuleWithPermissiveListenerMatchingHostname,
		},
		{
			name:                 "route with unmatching hostname",
			path:                 []machinery.Targetable{gc, g, l, routeWithUnmatchingHostname, rr},
			expectedError:        NewErrInvalidPath("http route does not belong to the listener"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
			expectedListener:     l,
			expectedHTTPRoute:    routeWithUnmatchingHostname,
		},
		{
			name:                  "http route rule does not belong to the http route",
			path:                  []machinery.Targetable{gc, g, l, r, otherRouteRule},
			expectedError:         NewErrInvalidPath("http route rule does not belong to the http route"),
			expectedGatewayClass:  gc,
			expectedGateway:       g,
			expectedListener:      l,
			expectedHTTPRoute:     r,
			expectedHTTPRouteRule: otherRouteRule,
		},
		{
			name:          "invalid gateway class",
			path:          []machinery.Targetable{rr, g, l, r, rr},
			expectedError: NewErrInvalidPath("index 0 is not a GatewayClass"),
		},
		{
			name:                 "invalid gateway",
			path:                 []machinery.Targetable{gc, rr, l, r, rr},
			expectedError:        NewErrInvalidPath("index 1 is not a Gateway"),
			expectedGatewayClass: gc,
		},
		{
			name:                 "invalid listener",
			path:                 []machinery.Targetable{gc, g, rr, r, rr},
			expectedError:        NewErrInvalidPath("index 2 is not a Listener"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
		},
		{
			name:                 "invalid http route",
			path:                 []machinery.Targetable{gc, g, l, rr, rr},
			expectedError:        NewErrInvalidPath("index 3 is not a HTTPRoute"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
			expectedListener:     l,
		},
		{
			name:                 "invalid http route rule",
			path:                 []machinery.Targetable{gc, g, l, r, gc},
			expectedError:        NewErrInvalidPath("index 4 is not a HTTPRouteRule"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
			expectedListener:     l,
			expectedHTTPRoute:    r,
		},
	}
	for _, tc := range testCase {
		t.Run(tc.name, func(subT *testing.T) {
			gatewayClass, gateway, listener, httpRoute, httpRouteRule, err := ObjectsInRequestPath(tc.path)
			if err != tc.expectedError {
				t.Errorf("expected error %v, got %v", tc.expectedError, err)
			}
			if gatewayClass != tc.expectedGatewayClass {
				t.Errorf("expected gatewayClass %v, got %v", tc.expectedGatewayClass, gatewayClass)
			}
			if gateway != tc.expectedGateway {
				t.Errorf("expected gateway %v, got %v", tc.expectedGateway, gateway)
			}
			if listener != tc.expectedListener {
				t.Errorf("expected listener %v, got %v", tc.expectedListener, listener)
			}
			if httpRoute != tc.expectedHTTPRoute {
				t.Errorf("expected httpRoute %v, got %v", tc.expectedHTTPRoute, httpRoute)
			}
			if httpRouteRule != tc.expectedHTTPRouteRule {
				t.Errorf("expected httpRouteRule %v, got %v", tc.expectedHTTPRouteRule, httpRouteRule)
			}
		})
	}
}
