//go:build unit

package policymachinery

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
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
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 0"),
		},
		{
			name:          "empty path",
			path:          []machinery.Targetable{},
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 0"),
		},
		{
			name:          "path too short - 1 element",
			path:          []machinery.Targetable{gc},
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 1"),
		},
		{
			name:          "path too short - 2 elements",
			path:          []machinery.Targetable{gc, g},
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 2"),
		},
		{
			name:          "path too short - 3 elements",
			path:          []machinery.Targetable{gc, g, l},
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 3"),
		},
		{
			name:          "path too short - 4 elements",
			path:          []machinery.Targetable{gc, g, l, r},
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 4"),
		},
		{
			name:          "path too long - 6 elements",
			path:          []machinery.Targetable{gc, g, l, r, rr, gc},
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 6"),
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
		{
			name:          "typed nil gateway class",
			path:          []machinery.Targetable{(*machinery.GatewayClass)(nil), g, l, r, rr},
			expectedError: NewErrInvalidPath("gateway class is a typed nil"),
		},
		{
			name:                 "typed nil gateway",
			path:                 []machinery.Targetable{gc, (*machinery.Gateway)(nil), l, r, rr},
			expectedError:        NewErrInvalidPath("gateway is a typed nil"),
			expectedGatewayClass: gc,
		},
		{
			name:                 "typed nil listener",
			path:                 []machinery.Targetable{gc, g, (*machinery.Listener)(nil), r, rr},
			expectedError:        NewErrInvalidPath("listener is a typed nil"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
		},
		{
			name:                 "typed nil http route",
			path:                 []machinery.Targetable{gc, g, l, (*machinery.HTTPRoute)(nil), rr},
			expectedError:        NewErrInvalidPath("route is a typed nil"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
			expectedListener:     l,
		},
		{
			name:                 "typed nil http route rule",
			path:                 []machinery.Targetable{gc, g, l, r, (*machinery.HTTPRouteRule)(nil)},
			expectedError:        NewErrInvalidPath("route rule is a typed nil"),
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

func TestObjectsInGRPCRequestPath(t *testing.T) {
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
					Name:      "kuadrant",
					Namespace: "default",
				},
				Spec: gatewayapiv1.GatewaySpec{
					GatewayClassName: gatewayapiv1.ObjectName(gc.GetName()),
					Listeners: []gatewayapiv1.Listener{
						{
							Name:     "example",
							Hostname: ptr.To(gatewayapiv1.Hostname("*.example.com")),
						},
						{
							Name: "wildcard",
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
			Gateway:  g,
			Listener: &g.Spec.Listeners[0],
		}
		for _, m := range mutate {
			m(o)
		}
		return o
	}

	grpcRoute := func(l *machinery.Listener, mutate ...func(*machinery.GRPCRoute)) *machinery.GRPCRoute {
		parentRef := gatewayapiv1.ParentReference{
			Name:      gatewayapiv1.ObjectName(l.Gateway.GetName()),
			Namespace: ptr.To(gatewayapiv1.Namespace(l.Gateway.GetNamespace())),
		}
		if l.Name != "" {
			parentRef.SectionName = ptr.To(gatewayapiv1.SectionName(l.Name))
		}
		o := &machinery.GRPCRoute{
			GRPCRoute: &gatewayapiv1.GRPCRoute{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayapiv1.SchemeGroupVersion.String(),
					Kind:       machinery.GRPCRouteGroupKind.Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example",
					Namespace: "default",
				},
				Spec: gatewayapiv1.GRPCRouteSpec{
					CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
						ParentRefs: []gatewayapiv1.ParentReference{parentRef},
					},
					Hostnames: []gatewayapiv1.Hostname{"*.example.com"},
					Rules: []gatewayapiv1.GRPCRouteRule{
						{
							Matches: []gatewayapiv1.GRPCRouteMatch{
								{
									Method: &gatewayapiv1.GRPCMethodMatch{
										Service: ptr.To("com.example.FooService"),
										Method:  ptr.To("GetFoo"),
									},
								},
							},
							BackendRefs: []gatewayapiv1.GRPCBackendRef{
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
							Matches: []gatewayapiv1.GRPCRouteMatch{
								{
									Method: &gatewayapiv1.GRPCMethodMatch{
										Service: ptr.To("com.example.BarService"),
										Method:  ptr.To("GetBar"),
									},
								},
							},
							BackendRefs: []gatewayapiv1.GRPCBackendRef{
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

	grpcRouteRule := func(r *machinery.GRPCRoute, mutate ...func(*machinery.GRPCRouteRule)) *machinery.GRPCRouteRule {
		if r == nil {
			r = grpcRoute(nil)
		}
		o := &machinery.GRPCRouteRule{
			Name:          "rule-1",
			GRPCRoute:     r,
			GRPCRouteRule: &r.Spec.Rules[0],
		}
		for _, m := range mutate {
			m(o)
		}
		return o
	}

	gc := gatewayClass()
	g := gateway(gc)
	l := listener(g)
	r := grpcRoute(l)
	rr := grpcRouteRule(r)

	routeWithSectionName := grpcRoute(l)
	routeRuleWithSectionName := grpcRouteRule(routeWithSectionName)

	otherGateway := gateway(nil, func(g *machinery.Gateway) {
		g.ObjectMeta.Name = "other"
		g.Spec.GatewayClassName = "other"
	})
	otherListener := listener(otherGateway)
	otherRoute := grpcRoute(otherListener, func(r *machinery.GRPCRoute) {
		r.ObjectMeta.Name = "other"
	})
	otherRouteRule := grpcRouteRule(otherRoute)

	routeWithGatewayMatchingHostname := grpcRoute(l, func(r *machinery.GRPCRoute) {
		r.Spec.Hostnames = []gatewayapiv1.Hostname{"foo.example.com"}
	})
	routeRuleWithGatewayMatchingHostname := grpcRouteRule(routeWithGatewayMatchingHostname)

	routeWithStrictListenerMatchingHostname := grpcRoute(l, func(r *machinery.GRPCRoute) {
		r.Spec.Hostnames = []gatewayapiv1.Hostname{"foo.example.com"}
	})
	routeRuleWithStrictListenerMatchingHostname := grpcRouteRule(routeWithStrictListenerMatchingHostname)

	permissiveListener := listener(g, func(l *machinery.Listener) {
		l.Listener = &g.Spec.Listeners[1]
	})
	routeWithPermissiveListenerMatchingHostname := grpcRoute(permissiveListener, func(r *machinery.GRPCRoute) {
		r.Spec.Hostnames = []gatewayapiv1.Hostname{"other.org"}
	})
	routeRuleWithPermissiveListenerMatchingHostname := grpcRouteRule(routeWithPermissiveListenerMatchingHostname)

	routeWithUnmatchingHostname := grpcRoute(l, func(r *machinery.GRPCRoute) {
		r.Spec.Hostnames = []gatewayapiv1.Hostname{"other.org"}
	})

	testCase := []struct {
		name                  string
		path                  []machinery.Targetable
		expectedGatewayClass  *machinery.GatewayClass
		expectedGateway       *machinery.Gateway
		expectedListener      *machinery.Listener
		expectedGRPCRoute     *machinery.GRPCRoute
		expectedGRPCRouteRule *machinery.GRPCRouteRule
		expectedError         error
	}{
		{
			name:          "nil path",
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 0"),
		},
		{
			name:          "empty path",
			path:          []machinery.Targetable{},
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 0"),
		},
		{
			name:          "path too short - 1 element",
			path:          []machinery.Targetable{gc},
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 1"),
		},
		{
			name:          "path too short - 2 elements",
			path:          []machinery.Targetable{gc, g},
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 2"),
		},
		{
			name:          "path too short - 3 elements",
			path:          []machinery.Targetable{gc, g, l},
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 3"),
		},
		{
			name:          "path too short - 4 elements",
			path:          []machinery.Targetable{gc, g, l, r},
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 4"),
		},
		{
			name:          "path too long - 6 elements",
			path:          []machinery.Targetable{gc, g, l, r, rr, gc},
			expectedError: NewErrInvalidPath("path must have exactly 5 elements, got 6"),
		},
		{
			name:                  "valid path",
			path:                  []machinery.Targetable{gc, g, l, r, rr},
			expectedGatewayClass:  gc,
			expectedGateway:       g,
			expectedListener:      l,
			expectedGRPCRoute:     r,
			expectedGRPCRouteRule: rr,
		},
		{
			name:                  "valid path with route with section name",
			path:                  []machinery.Targetable{gc, g, l, routeWithSectionName, routeRuleWithSectionName},
			expectedGatewayClass:  gc,
			expectedGateway:       g,
			expectedListener:      l,
			expectedGRPCRoute:     routeWithSectionName,
			expectedGRPCRouteRule: routeRuleWithSectionName,
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
			name:                 "grpc route does not belong to the listener",
			path:                 []machinery.Targetable{gc, g, l, otherRoute, otherRouteRule},
			expectedError:        NewErrInvalidPath("grpc route does not belong to the listener"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
			expectedListener:     l,
			expectedGRPCRoute:    otherRoute,
		},
		{
			name:                  "route with gateway matching hostname",
			path:                  []machinery.Targetable{gc, g, l, routeWithGatewayMatchingHostname, routeRuleWithGatewayMatchingHostname},
			expectedGatewayClass:  gc,
			expectedGateway:       g,
			expectedListener:      l,
			expectedGRPCRoute:     routeWithGatewayMatchingHostname,
			expectedGRPCRouteRule: routeRuleWithGatewayMatchingHostname,
		},
		{
			name:                  "route with strict listener matching hostname",
			path:                  []machinery.Targetable{gc, g, l, routeWithStrictListenerMatchingHostname, routeRuleWithStrictListenerMatchingHostname},
			expectedGatewayClass:  gc,
			expectedGateway:       g,
			expectedListener:      l,
			expectedGRPCRoute:     routeWithStrictListenerMatchingHostname,
			expectedGRPCRouteRule: routeRuleWithStrictListenerMatchingHostname,
		},
		{
			name:                  "route with permissive listener matching hostname",
			path:                  []machinery.Targetable{gc, g, permissiveListener, routeWithPermissiveListenerMatchingHostname, routeRuleWithPermissiveListenerMatchingHostname},
			expectedGatewayClass:  gc,
			expectedGateway:       g,
			expectedListener:      permissiveListener,
			expectedGRPCRoute:     routeWithPermissiveListenerMatchingHostname,
			expectedGRPCRouteRule: routeRuleWithPermissiveListenerMatchingHostname,
		},
		{
			name:                 "route with unmatching hostname",
			path:                 []machinery.Targetable{gc, g, l, routeWithUnmatchingHostname, rr},
			expectedError:        NewErrInvalidPath("grpc route does not belong to the listener"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
			expectedListener:     l,
			expectedGRPCRoute:    routeWithUnmatchingHostname,
		},
		{
			name:                  "grpc route rule does not belong to the grpc route",
			path:                  []machinery.Targetable{gc, g, l, r, otherRouteRule},
			expectedError:         NewErrInvalidPath("grpc route rule does not belong to the grpc route"),
			expectedGatewayClass:  gc,
			expectedGateway:       g,
			expectedListener:      l,
			expectedGRPCRoute:     r,
			expectedGRPCRouteRule: otherRouteRule,
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
			name:                 "invalid grpc route",
			path:                 []machinery.Targetable{gc, g, l, rr, rr},
			expectedError:        NewErrInvalidPath("index 3 is not a GRPCRoute"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
			expectedListener:     l,
		},
		{
			name:                 "invalid grpc route rule",
			path:                 []machinery.Targetable{gc, g, l, r, gc},
			expectedError:        NewErrInvalidPath("index 4 is not a GRPCRouteRule"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
			expectedListener:     l,
			expectedGRPCRoute:    r,
		},
		{
			name:          "typed nil gateway class",
			path:          []machinery.Targetable{(*machinery.GatewayClass)(nil), g, l, r, rr},
			expectedError: NewErrInvalidPath("gateway class is a typed nil"),
		},
		{
			name:                 "typed nil gateway",
			path:                 []machinery.Targetable{gc, (*machinery.Gateway)(nil), l, r, rr},
			expectedError:        NewErrInvalidPath("gateway is a typed nil"),
			expectedGatewayClass: gc,
		},
		{
			name:                 "typed nil listener",
			path:                 []machinery.Targetable{gc, g, (*machinery.Listener)(nil), r, rr},
			expectedError:        NewErrInvalidPath("listener is a typed nil"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
		},
		{
			name:                 "typed nil grpc route",
			path:                 []machinery.Targetable{gc, g, l, (*machinery.GRPCRoute)(nil), rr},
			expectedError:        NewErrInvalidPath("route is a typed nil"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
			expectedListener:     l,
		},
		{
			name:                 "typed nil grpc route rule",
			path:                 []machinery.Targetable{gc, g, l, r, (*machinery.GRPCRouteRule)(nil)},
			expectedError:        NewErrInvalidPath("route rule is a typed nil"),
			expectedGatewayClass: gc,
			expectedGateway:      g,
			expectedListener:     l,
			expectedGRPCRoute:    r,
		},
	}
	for _, tc := range testCase {
		t.Run(tc.name, func(subT *testing.T) {
			gatewayClass, gateway, listener, grpcRoute, grpcRouteRule, err := ObjectsInGRPCRequestPath(tc.path)
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
			if grpcRoute != tc.expectedGRPCRoute {
				t.Errorf("expected grpcRoute %v, got %v", tc.expectedGRPCRoute, grpcRoute)
			}
			if grpcRouteRule != tc.expectedGRPCRouteRule {
				t.Errorf("expected grpcRouteRule %v, got %v", tc.expectedGRPCRouteRule, grpcRouteRule)
			}
		})
	}
}

func TestRouteType_String(t *testing.T) {
	tests := []struct {
		name     string
		rt       RouteType
		expected string
	}{
		{"HTTP route type", RouteTypeHTTP, "HTTP"},
		{"gRPC route type", RouteTypeGRPC, "gRPC"},
		{"Unknown route type", RouteTypeUnknown, "Unknown"},
		{"Invalid route type value", RouteType(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.rt.String()
			if result != tt.expected {
				t.Errorf("RouteType.String() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDetectRouteType(t *testing.T) {
	gc := &machinery.GatewayClass{GatewayClass: &gatewayapiv1.GatewayClass{}}
	g := &machinery.Gateway{Gateway: &gatewayapiv1.Gateway{}}
	l := &machinery.Listener{Gateway: g}
	httpRoute := &machinery.HTTPRoute{HTTPRoute: &gatewayapiv1.HTTPRoute{}}
	grpcRoute := &machinery.GRPCRoute{GRPCRoute: &gatewayapiv1.GRPCRoute{}}
	httpRouteRule := &machinery.HTTPRouteRule{}

	tests := []struct {
		name     string
		path     []machinery.Targetable
		expected RouteType
	}{
		{
			name:     "HTTP route at index 3",
			path:     []machinery.Targetable{gc, g, l, httpRoute, httpRouteRule},
			expected: RouteTypeHTTP,
		},
		{
			name:     "gRPC route at index 3",
			path:     []machinery.Targetable{gc, g, l, grpcRoute, httpRouteRule},
			expected: RouteTypeGRPC,
		},
		{
			name:     "unknown type at index 3",
			path:     []machinery.Targetable{gc, g, l, gc, httpRouteRule},
			expected: RouteTypeUnknown,
		},
		{
			name:     "path with length < 4",
			path:     []machinery.Targetable{gc, g, l},
			expected: RouteTypeUnknown,
		},
		{
			name:     "empty path",
			path:     []machinery.Targetable{},
			expected: RouteTypeUnknown,
		},
		{
			name:     "nil path",
			path:     nil,
			expected: RouteTypeUnknown,
		},
		{
			name:     "exactly 4 elements with HTTP route",
			path:     []machinery.Targetable{gc, g, l, httpRoute},
			expected: RouteTypeHTTP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectRouteType(tt.path)
			if result != tt.expected {
				t.Errorf("DetectRouteType() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseTopologyPath(t *testing.T) {
	gc := &machinery.GatewayClass{GatewayClass: &gatewayapiv1.GatewayClass{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gatewayapiv1.SchemeGroupVersion.String(),
			Kind:       machinery.GatewayClassGroupKind.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{Name: "test-gc"},
	}}
	g := &machinery.Gateway{Gateway: &gatewayapiv1.Gateway{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gatewayapiv1.SchemeGroupVersion.String(),
			Kind:       machinery.GatewayGroupKind.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "test-ns"},
		Spec: gatewayapiv1.GatewaySpec{
			GatewayClassName: "test-gc",
			Listeners: []gatewayapiv1.Listener{
				{
					Name:     "test-listener",
					Protocol: gatewayapiv1.ProtocolType("HTTP"),
					Port:     gatewayapiv1.PortNumber(80),
				},
			},
		},
	}}
	l := &machinery.Listener{
		Listener: &g.Spec.Listeners[0],
		Gateway:  g,
	}
	httpRoute := &machinery.HTTPRoute{HTTPRoute: &gatewayapiv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gatewayapiv1.SchemeGroupVersion.String(),
			Kind:       machinery.HTTPRouteGroupKind.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{Name: "test-http-route", Namespace: "test-ns"},
		Spec: gatewayapiv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1.ParentReference{
					{
						Name:      "test-gw",
						Namespace: ptr.To(gatewayapiv1.Namespace("test-ns")),
					},
				},
			},
		},
	}}
	httpRouteRule := &machinery.HTTPRouteRule{
		Name:      "rule-1",
		HTTPRoute: httpRoute,
	}
	grpcRoute := &machinery.GRPCRoute{GRPCRoute: &gatewayapiv1.GRPCRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gatewayapiv1.SchemeGroupVersion.String(),
			Kind:       machinery.GRPCRouteGroupKind.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{Name: "test-grpc-route", Namespace: "test-ns"},
		Spec: gatewayapiv1.GRPCRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1.ParentReference{
					{
						Name:      "test-gw",
						Namespace: ptr.To(gatewayapiv1.Namespace("test-ns")),
					},
				},
			},
		},
	}}
	grpcRouteRule := &machinery.GRPCRouteRule{
		Name:      "grpc-rule-1",
		GRPCRoute: grpcRoute,
	}

	tests := []struct {
		name        string
		path        []machinery.Targetable
		expectError bool
		errorSubstr string
		validate    func(*testing.T, *ParsedTopologyPath)
	}{
		{
			name:        "valid HTTP path",
			path:        []machinery.Targetable{gc, g, l, httpRoute, httpRouteRule},
			expectError: false,
			validate: func(t *testing.T, p *ParsedTopologyPath) {
				if p.RouteType != RouteTypeHTTP {
					t.Errorf("expected RouteType HTTP, got %v", p.RouteType)
				}
				if p.HTTPRoute != httpRoute {
					t.Errorf("expected HTTPRoute to be set")
				}
				if p.HTTPRouteRule != httpRouteRule {
					t.Errorf("expected HTTPRouteRule to be set")
				}
				if p.GRPCRoute != nil || p.GRPCRouteRule != nil {
					t.Errorf("expected gRPC fields to be nil")
				}
				if p.GatewayClass != gc || p.Gateway != g || p.Listener != l {
					t.Errorf("expected common fields to be populated")
				}
			},
		},
		{
			name:        "valid gRPC path",
			path:        []machinery.Targetable{gc, g, l, grpcRoute, grpcRouteRule},
			expectError: false,
			validate: func(t *testing.T, p *ParsedTopologyPath) {
				if p.RouteType != RouteTypeGRPC {
					t.Errorf("expected RouteType gRPC, got %v", p.RouteType)
				}
				if p.GRPCRoute != grpcRoute {
					t.Errorf("expected GRPCRoute to be set")
				}
				if p.GRPCRouteRule != grpcRouteRule {
					t.Errorf("expected GRPCRouteRule to be set")
				}
				if p.HTTPRoute != nil || p.HTTPRouteRule != nil {
					t.Errorf("expected HTTP fields to be nil")
				}
				if p.GatewayClass != gc || p.Gateway != g || p.Listener != l {
					t.Errorf("expected common fields to be populated")
				}
			},
		},
		{
			name:        "unknown route type",
			path:        []machinery.Targetable{gc, g, l, gc, httpRouteRule},
			expectError: true,
			errorSubstr: "unsupported route type",
		},
		{
			name:        "path too short",
			path:        []machinery.Targetable{gc, g, l},
			expectError: true,
			errorSubstr: "unsupported route type",
		},
		{
			name:        "empty path",
			path:        []machinery.Targetable{},
			expectError: true,
			errorSubstr: "unsupported route type",
		},
		{
			name:        "invalid HTTP path structure",
			path:        []machinery.Targetable{gc, g, l, httpRoute, gc},
			expectError: true,
			errorSubstr: "index 4 is not a HTTPRouteRule",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTopologyPath(tt.path)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if tt.errorSubstr != "" && !strings.Contains(err.Error(), tt.errorSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errorSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Errorf("expected non-nil result")
				} else if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}

func TestParsedTopologyPath_Helpers(t *testing.T) {
	httpRoute := &machinery.HTTPRoute{HTTPRoute: &gatewayapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "test-http", Namespace: "test-ns"},
	}}
	httpRouteRule := &machinery.HTTPRouteRule{
		Name:      "http-rule",
		HTTPRoute: httpRoute,
	}
	grpcRoute := &machinery.GRPCRoute{GRPCRoute: &gatewayapiv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "test-grpc", Namespace: "grpc-ns"},
	}}
	grpcRouteRule := &machinery.GRPCRouteRule{
		Name:      "grpc-rule",
		GRPCRoute: grpcRoute,
	}

	tests := []struct {
		name   string
		parsed *ParsedTopologyPath
	}{
		{
			name: "HTTP route",
			parsed: &ParsedTopologyPath{
				RouteType:     RouteTypeHTTP,
				HTTPRoute:     httpRoute,
				HTTPRouteRule: httpRouteRule,
			},
		},
		{
			name: "gRPC route",
			parsed: &ParsedTopologyPath{
				RouteType:     RouteTypeGRPC,
				GRPCRoute:     grpcRoute,
				GRPCRouteRule: grpcRouteRule,
			},
		},
		{
			name: "empty route",
			parsed: &ParsedTopologyPath{
				RouteType: RouteTypeUnknown,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("GetRouteName", func(t *testing.T) {
				result := tt.parsed.GetRouteName()
				var expected string
				if tt.parsed.HTTPRoute != nil {
					expected = tt.parsed.HTTPRoute.GetName()
				} else if tt.parsed.GRPCRoute != nil {
					expected = tt.parsed.GRPCRoute.GetName()
				}
				if result != expected {
					t.Errorf("GetRouteName() = %v, want %v", result, expected)
				}
			})

			t.Run("GetRouteNamespace", func(t *testing.T) {
				result := tt.parsed.GetRouteNamespace()
				var expected string
				if tt.parsed.HTTPRoute != nil {
					expected = tt.parsed.HTTPRoute.GetNamespace()
				} else if tt.parsed.GRPCRoute != nil {
					expected = tt.parsed.GRPCRoute.GetNamespace()
				}
				if result != expected {
					t.Errorf("GetRouteNamespace() = %v, want %v", result, expected)
				}
			})

			t.Run("GetRouteNamespacedName", func(t *testing.T) {
				result := tt.parsed.GetRouteNamespacedName()
				expected := k8stypes.NamespacedName{
					Name:      tt.parsed.GetRouteName(),
					Namespace: tt.parsed.GetRouteNamespace(),
				}
				if result != expected {
					t.Errorf("GetRouteNamespacedName() = %v, want %v", result, expected)
				}
			})

			t.Run("GetRouteRuleName", func(t *testing.T) {
				result := tt.parsed.GetRouteRuleName()
				var expected string
				if tt.parsed.HTTPRouteRule != nil {
					expected = string(tt.parsed.HTTPRouteRule.Name)
				} else if tt.parsed.GRPCRouteRule != nil {
					expected = string(tt.parsed.GRPCRouteRule.Name)
				}
				if result != expected {
					t.Errorf("GetRouteRuleName() = %v, want %v", result, expected)
				}
			})

			t.Run("GetRouteRule", func(t *testing.T) {
				result := tt.parsed.GetRouteRule()
				var expected machinery.Object
				if tt.parsed.HTTPRouteRule != nil {
					expected = tt.parsed.HTTPRouteRule
				} else if tt.parsed.GRPCRouteRule != nil {
					expected = tt.parsed.GRPCRouteRule
				}
				if result != expected {
					t.Errorf("GetRouteRule() = %v, want %v", result, expected)
				}
			})

			t.Run("GetRouteRuleLocator", func(t *testing.T) {
				result := tt.parsed.GetRouteRuleLocator()
				var expected string
				if tt.parsed.HTTPRouteRule != nil {
					expected = tt.parsed.HTTPRouteRule.GetLocator()
				} else if tt.parsed.GRPCRouteRule != nil {
					expected = tt.parsed.GRPCRouteRule.GetLocator()
				}
				if result != expected {
					t.Errorf("GetRouteRuleLocator() = %v, want %v", result, expected)
				}
			})
		})
	}
}

func TestIsNil(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected bool
	}{
		{
			name:     "nil interface",
			value:    nil,
			expected: true,
		},
		{
			name:     "typed nil pointer",
			value:    (*machinery.HTTPRoute)(nil),
			expected: true,
		},
		{
			name:     "typed nil slice",
			value:    ([]string)(nil),
			expected: true,
		},
		{
			name:     "typed nil map",
			value:    (map[string]string)(nil),
			expected: true,
		},
		{
			name:     "typed nil channel",
			value:    (chan int)(nil),
			expected: true,
		},
		{
			name:     "typed nil func",
			value:    (func())(nil),
			expected: true,
		},
		{
			name:     "non-nil pointer",
			value:    &machinery.HTTPRoute{},
			expected: false,
		},
		{
			name:     "non-nil slice",
			value:    []string{},
			expected: false,
		},
		{
			name:     "non-nil map",
			value:    map[string]string{},
			expected: false,
		},
		{
			name:     "non-nil value",
			value:    "test",
			expected: false,
		},
		{
			name:     "zero value struct",
			value:    machinery.HTTPRoute{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNil(tt.value)
			if result != tt.expected {
				t.Errorf("isNil() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// BenchmarkIsNil measures the performance overhead of reflection-based nil checking.
// Results show 3-6ns per call with zero allocations, which is negligible in
// controller reconciliation context.
func BenchmarkIsNil(b *testing.B) {
	benchmarks := []struct {
		name  string
		value any
	}{
		{
			name:  "nil interface",
			value: nil,
		},
		{
			name:  "typed nil pointer",
			value: (*machinery.HTTPRoute)(nil),
		},
		{
			name:  "non-nil pointer",
			value: &machinery.HTTPRoute{},
		},
		{
			name:  "nil slice",
			value: ([]string)(nil),
		},
		{
			name:  "non-nil value",
			value: "test",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = isNil(bm.value)
			}
		})
	}
}
