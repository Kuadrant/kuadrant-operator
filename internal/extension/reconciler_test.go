package extension

import (
	"sync"
	"testing"
	"time"

	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	v0 "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v0"
)

func TestStateAwareDAG(t *testing.T) {
	t.Run("findGateways()", func(t *testing.T) {
		resources := BuildComplexGatewayAPITopology()

		gatewayClasses := lo.Map(resources.GatewayClasses, func(gatewayClass *gwapiv1.GatewayClass, _ int) *machinery.GatewayClass {
			return &machinery.GatewayClass{GatewayClass: gatewayClass}
		})
		gateways := lo.Map(resources.Gateways, func(gateway *gwapiv1.Gateway, _ int) *machinery.Gateway { return &machinery.Gateway{Gateway: gateway} })
		httpRoutes := lo.Map(resources.HTTPRoutes, func(httpRoute *gwapiv1.HTTPRoute, _ int) *machinery.HTTPRoute {
			return &machinery.HTTPRoute{HTTPRoute: httpRoute}
		})
		grpcRoutes := lo.Map(resources.GRPCRoutes, func(grpcRoute *gwapiv1.GRPCRoute, _ int) *machinery.GRPCRoute {
			return &machinery.GRPCRoute{GRPCRoute: grpcRoute}
		})
		tcpRoutes := lo.Map(resources.TCPRoutes, func(tcpRoute *gwapiv1alpha2.TCPRoute, _ int) *machinery.TCPRoute {
			return &machinery.TCPRoute{TCPRoute: tcpRoute}
		})
		tlsRoutes := lo.Map(resources.TLSRoutes, func(tlsRoute *gwapiv1alpha2.TLSRoute, _ int) *machinery.TLSRoute {
			return &machinery.TLSRoute{TLSRoute: tlsRoute}
		})
		udpRoutes := lo.Map(resources.UDPRoutes, func(updRoute *gwapiv1alpha2.UDPRoute, _ int) *machinery.UDPRoute {
			return &machinery.UDPRoute{UDPRoute: updRoute}
		})
		services := lo.Map(resources.Services, func(service *core.Service, _ int) *machinery.Service { return &machinery.Service{Service: service} })

		topology, err := machinery.NewTopology(
			machinery.WithTargetables(gatewayClasses...),
			machinery.WithTargetables(gateways...),
			machinery.WithTargetables(httpRoutes...),
			machinery.WithTargetables(services...),
			machinery.WithTargetables(grpcRoutes...),
			machinery.WithTargetables(tcpRoutes...),
			machinery.WithTargetables(tlsRoutes...),
			machinery.WithTargetables(udpRoutes...),
			machinery.WithLinks(
				machinery.LinkGatewayClassToGatewayFunc(gatewayClasses),
				machinery.LinkGatewayToHTTPRouteFunc(gateways),
				machinery.LinkGatewayToGRPCRouteFunc(gateways),
				machinery.LinkGatewayToTCPRouteFunc(gateways),
				machinery.LinkGatewayToTLSRouteFunc(gateways),
				machinery.LinkGatewayToUDPRouteFunc(gateways),
				machinery.LinkHTTPRouteToServiceFunc(httpRoutes, false),
				machinery.LinkGRPCRouteToServiceFunc(grpcRoutes, false),
				machinery.LinkTCPRouteToServiceFunc(tcpRoutes, false),
				machinery.LinkTLSRouteToServiceFunc(tlsRoutes, false),
				machinery.LinkUDPRouteToServiceFunc(udpRoutes, false),
			),
		)

		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		dag := StateAwareDAG{
			topology,
			nil,
		}

		gws, err := dag.FindGatewaysFor([]*v0.TargetRef{{Kind: "Service", Name: "service-1"}})
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		if len(gws) != 1 {
			t.Fatalf("Expected exactly 1 gateway, got %#v", gws)
		}
		if gws[0].GetMetadata().GetName() != "gateway-1" {
			t.Fatalf("Expected gateway-1, got %s", gws[0].GetMetadata().GetName())
		}

		gws, err = dag.FindGatewaysFor([]*v0.TargetRef{{Kind: "TLSRoute", Name: "tls-route-1"}})
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		if len(gws) != 2 {
			t.Fatalf("Expected exactly 2 gateways, got %#v", gws)
		}
		if gws[0].GetMetadata().GetName() != "gateway-3" && gws[1].GetMetadata().GetName() != "gateway-3" {
			t.Fatalf("Expected gateway-3")
		}
		if gws[0].GetMetadata().GetName() != "gateway-4" && gws[1].GetMetadata().GetName() != "gateway-4" {
			t.Fatalf("Expected gateway-4")
		}

		gws, err = dag.FindGatewaysFor([]*v0.TargetRef{{Kind: "Service", Name: "service-3"}})
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		if len(gws) != 3 {
			t.Fatalf("Expected exactly 3 gateways, got %#v", gws)
		}
		if gws[0].GetMetadata().GetName() != "gateway-1" && gws[1].GetMetadata().GetName() != "gateway-1" && gws[2].GetMetadata().GetName() != "gateway-1" {
			t.Fatalf("Expected gateway-1, got %#v", gws[0].GetMetadata().GetName())
		}
		if gws[0].GetMetadata().GetName() != "gateway-2" && gws[1].GetMetadata().GetName() != "gateway-2" && gws[2].GetMetadata().GetName() != "gateway-2" {
			t.Fatalf("Expected gateway-2, got %#v", gws[0].GetMetadata().GetName())
		}
		if gws[0].GetMetadata().GetName() != "gateway-3" && gws[1].GetMetadata().GetName() != "gateway-3" && gws[2].GetMetadata().GetName() != "gateway-3" {
			t.Fatalf("Expected gateway-3, got %#v", gws)
		}
	})
}

func TestNilGuardedPointer(t *testing.T) {
	t.Run("set and get", func(t *testing.T) {
		ptr := newNilGuardedPointer[string]()

		if ptr.get() != nil {
			t.Errorf("Expected initial value to be nil, got %v", ptr.get())
		}

		value := "test"
		ptr.set(value)

		loaded := ptr.get()
		if loaded == nil {
			t.Error("Expected loaded value to be non-nil")
		} else if *loaded != value {
			t.Errorf("Expected loaded value to be %s, got %s", value, *loaded)
		}
	})

	t.Run("getWait blocks until value is set", func(t *testing.T) {
		ptr := newNilGuardedPointer[string]()

		done := make(chan struct{})
		var loaded string

		go func() {
			loaded = ptr.getWait()
			close(done)
		}()

		time.Sleep(100 * time.Millisecond)

		value := "test"
		ptr.set(value)

		select {
		case <-done:
			if loaded != value {
				t.Errorf("Expected loaded value to be %s, got %s", value, loaded)
			}
		case <-time.After(1 * time.Second):
			t.Error("Timed out waiting for getWait to return")
		}
	})

	t.Run("getWait returns immediately if value is already set", func(t *testing.T) {
		ptr := newNilGuardedPointer[string]()

		value := "test"
		ptr.set(value)

		start := time.Now()
		loaded := ptr.getWait()
		elapsed := time.Since(start)

		if elapsed > 100*time.Millisecond {
			t.Errorf("Expected getWait to return immediately, took %v", elapsed)
		}

		if loaded != value {
			t.Errorf("Expected loaded value to be %s, got %s", value, loaded)
		}
	})

	t.Run("getWaitWithTimeout returns false on timeout", func(t *testing.T) {
		ptr := newNilGuardedPointer[string]()

		start := time.Now()
		_, success := ptr.getWaitWithTimeout(100 * time.Millisecond)
		elapsed := time.Since(start)

		if elapsed < 100*time.Millisecond {
			t.Errorf("Expected getWaitWithTimeout to wait for at least the timeout duration, took %v", elapsed)
		}

		if success {
			t.Error("Expected success to be false on timeout")
		}
	})

	t.Run("getWaitWithTimeout returns true when value is set before timeout", func(t *testing.T) {
		ptr := newNilGuardedPointer[string]()

		done := make(chan bool)
		var loaded string

		go func() {
			var success bool
			l, success := ptr.getWaitWithTimeout(1 * time.Second)
			loaded = *l
			done <- success
		}()

		time.Sleep(100 * time.Millisecond)

		value := "test"
		ptr.set(value)

		select {
		case success := <-done:
			if !success {
				t.Error("Expected success to be true when value is set before timeout")
			}
			if loaded != value {
				t.Errorf("Expected loaded value to be %s, got %s", value, loaded)
			}
		case <-time.After(2 * time.Second):
			t.Error("Timed out waiting for getWaitWithTimeout to return")
		}
	})

	t.Run("BlockingDAG variable", func(t *testing.T) {
		if BlockingDAG.get() != nil {
			t.Error("Expected initial BlockingDAG to be nil")
		}

		dag := StateAwareDAG{
			topology: nil,
			state:    &sync.Map{},
		}

		BlockingDAG.set(dag)

		loaded := BlockingDAG.get()
		if loaded == nil {
			t.Error("Expected loaded BlockingDAG to be non-nil")
		}
	})
}

func BuildGatewayClass(f ...func(*gwapiv1.GatewayClass)) *gwapiv1.GatewayClass {
	gc := &gwapiv1.GatewayClass{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gwapiv1.GroupVersion.String(),
			Kind:       "GatewayClass",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-gateway-class",
		},
		Spec: gwapiv1.GatewayClassSpec{
			ControllerName: gwapiv1.GatewayController("my-gateway-controller"),
		},
	}
	for _, fn := range f {
		fn(gc)
	}
	return gc
}

func BuildGateway(f ...func(*gwapiv1.Gateway)) *gwapiv1.Gateway {
	g := &gwapiv1.Gateway{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gwapiv1.GroupVersion.String(),
			Kind:       "Gateway",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-gateway",
			Namespace: "my-namespace",
		},
		Spec: gwapiv1.GatewaySpec{
			GatewayClassName: "my-gateway-class",
			Listeners: []gwapiv1.Listener{
				{
					Name:     "my-listener",
					Port:     80,
					Protocol: "HTTP",
				},
			},
		},
	}
	for _, fn := range f {
		fn(g)
	}
	return g
}

func BuildHTTPRoute(f ...func(*gwapiv1.HTTPRoute)) *gwapiv1.HTTPRoute {
	r := &gwapiv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gwapiv1.GroupVersion.String(),
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-http-route",
			Namespace: "my-namespace",
		},
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{
					{
						Name: "my-gateway",
					},
				},
			},
			Rules: []gwapiv1.HTTPRouteRule{
				{
					BackendRefs: []gwapiv1.HTTPBackendRef{BuildHTTPBackendRef()},
				},
			},
		},
	}
	for _, fn := range f {
		fn(r)
	}
	return r
}

func BuildHTTPBackendRef(f ...func(*gwapiv1.BackendObjectReference)) gwapiv1.HTTPBackendRef {
	return gwapiv1.HTTPBackendRef{
		BackendRef: BuildBackendRef(f...),
	}
}

func BuildService(f ...func(*core.Service)) *core.Service {
	s := &core.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: core.SchemeGroupVersion.String(),
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-service",
			Namespace: "my-namespace",
		},
		Spec: core.ServiceSpec{
			Ports: []core.ServicePort{
				{
					Name: "http",
					Port: 80,
				},
			},
			Selector: map[string]string{
				"app": "my-app",
			},
		},
	}
	for _, fn := range f {
		fn(s)
	}
	return s
}

func BuildGRPCRoute(f ...func(*gwapiv1.GRPCRoute)) *gwapiv1.GRPCRoute {
	r := &gwapiv1.GRPCRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gwapiv1.GroupVersion.String(),
			Kind:       "GRPCRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-grpc-route",
			Namespace: "my-namespace",
		},
		Spec: gwapiv1.GRPCRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{
					{
						Name: "my-gateway",
					},
				},
			},
			Rules: []gwapiv1.GRPCRouteRule{
				{
					BackendRefs: []gwapiv1.GRPCBackendRef{BuildGRPCBackendRef()},
				},
			},
		},
	}
	for _, fn := range f {
		fn(r)
	}

	return r
}

func BuildGRPCBackendRef(f ...func(*gwapiv1.BackendObjectReference)) gwapiv1.GRPCBackendRef {
	return gwapiv1.GRPCBackendRef{
		BackendRef: BuildBackendRef(f...),
	}
}

func BuildBackendRef(f ...func(*gwapiv1.BackendObjectReference)) gwapiv1.BackendRef {
	bor := &gwapiv1.BackendObjectReference{
		Name: "my-service",
	}
	for _, fn := range f {
		fn(bor)
	}
	return gwapiv1.BackendRef{
		BackendObjectReference: *bor,
	}
}

func BuildTCPRoute(f ...func(route *gwapiv1alpha2.TCPRoute)) *gwapiv1alpha2.TCPRoute {
	r := &gwapiv1alpha2.TCPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gwapiv1alpha2.GroupVersion.String(),
			Kind:       "TCPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-tcp-route",
			Namespace: "my-namespace",
		},
		Spec: gwapiv1alpha2.TCPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{
					{
						Name: "my-gateway",
					},
				},
			},
			Rules: []gwapiv1alpha2.TCPRouteRule{
				{
					BackendRefs: []gwapiv1.BackendRef{BuildBackendRef()},
				},
			},
		},
	}
	for _, fn := range f {
		fn(r)
	}

	return r
}

func BuildTLSRoute(f ...func(route *gwapiv1alpha2.TLSRoute)) *gwapiv1alpha2.TLSRoute {
	r := &gwapiv1alpha2.TLSRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gwapiv1alpha2.GroupVersion.String(),
			Kind:       "TLSRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-tls-route",
			Namespace: "my-namespace",
		},
		Spec: gwapiv1alpha2.TLSRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{
					{
						Name: "my-gateway",
					},
				},
			},
			Rules: []gwapiv1alpha2.TLSRouteRule{
				{
					BackendRefs: []gwapiv1.BackendRef{BuildBackendRef()},
				},
			},
		},
	}
	for _, fn := range f {
		fn(r)
	}

	return r
}

func BuildUDPRoute(f ...func(route *gwapiv1alpha2.UDPRoute)) *gwapiv1alpha2.UDPRoute {
	r := &gwapiv1alpha2.UDPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gwapiv1alpha2.GroupVersion.String(),
			Kind:       "UDPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-udp-route",
			Namespace: "my-namespace",
		},
		Spec: gwapiv1alpha2.UDPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{
					{
						Name: "my-gateway",
					},
				},
			},
			Rules: []gwapiv1alpha2.UDPRouteRule{
				{
					BackendRefs: []gwapiv1.BackendRef{BuildBackendRef()},
				},
			},
		},
	}
	for _, fn := range f {
		fn(r)
	}

	return r
}

type GatewayAPIResources struct {
	GatewayClasses []*gwapiv1.GatewayClass
	Gateways       []*gwapiv1.Gateway
	HTTPRoutes     []*gwapiv1.HTTPRoute
	GRPCRoutes     []*gwapiv1.GRPCRoute
	TCPRoutes      []*gwapiv1alpha2.TCPRoute
	TLSRoutes      []*gwapiv1alpha2.TLSRoute
	UDPRoutes      []*gwapiv1alpha2.UDPRoute
	Services       []*core.Service
}

// BuildComplexGatewayAPITopology returns a set of Gateway API resources organized :
//
//	                                          ┌────────────────┐                                                                        ┌────────────────┐
//	                                          │ gatewayclass-1 │                                                                        │ gatewayclass-2 │
//	                                          └────────────────┘                                                                        └────────────────┘
//	                                                  ▲                                                                                         ▲
//	                                                  │                                                                                         │
//	                        ┌─────────────────────────┼──────────────────────────┐                                                 ┌────────────┴─────────────┐
//	                        │                         │                          │                                                 │                          │
//	        ┌───────────────┴───────────────┐ ┌───────┴────────┐ ┌───────────────┴───────────────┐                  ┌──────────────┴────────────────┐ ┌───────┴────────┐
//	        │           gateway-1           │ │   gateway-2    │ │           gateway-3           │                  │           gateway-4           │ │   gateway-5    │
//	        │                               │ │                │ │                               │                  │                               │ │                │
//	        │ ┌────────────┐ ┌────────────┐ │ │ ┌────────────┐ │ │ ┌────────────┐ ┌────────────┐ │                  │ ┌────────────┐ ┌────────────┐ │ │ ┌────────────┐ │
//	        │ │ listener-1 │ │ listener-2 │ │ │ │ listener-1 │ │ │ │ listener-1 │ │ listener-2 │ │                  │ │ listener-1 │ │ listener-2 │ │ │ │ listener-1 │ │
//	        │ └────────────┘ └────────────┘ │ │ └────────────┘ │ │ └────────────┘ └────────────┘ │                  │ └────────────┘ └────────────┘ │ │ └────────────┘ │
//	        │                        ▲      │ │      ▲         │ │                               │                  │                               │ │                │
//	        └────────────────────────┬──────┘ └──────┬─────────┘ └───────────────────────────────┘                  └───────────────────────────────┘ └────────────────┘
//	                    ▲            │               │       ▲                    ▲            ▲                            ▲           ▲                        ▲
//	                    │            │               │       │                    │            │                            │           │                        │
//	                    │            └───────┬───────┘       │                    │            └──────────────┬─────────────┘           │                        │
//	                    │                    │               │                    │                           │                         │                        │
//	        ┌───────────┴───────────┐ ┌──────┴───────┐ ┌─────┴────────┐ ┌─────────┴─────────────┐ ┌───────────┴───────────┐ ┌───────────┴───────────┐      ┌─────┴────────┐
//	        │     http-route-1      │ │ http-route-2 │ │ http-route-3 │ │     udp-route-1       │ │      tls-route-1      │ │     tcp-route-1       │      │ grpc-route-1 │
//	        │                       │ │              │ │              │ │                       │ │                       │ │                       │      │              │
//	        │ ┌────────┐ ┌────────┐ │ │ ┌────────┐   │ │  ┌────────┐  │ │ ┌────────┐ ┌────────┐ │ │ ┌────────┐ ┌────────┐ │ │ ┌────────┐ ┌────────┐ │      │ ┌────────┐   │
//	        │ │ rule-1 │ │ rule-2 │ │ │ │ rule-1 │   │ │  │ rule-1 │  │ │ │ rule-1 │ │ rule-2 │ │ │ │ rule-1 │ │ rule-2 │ │ │ │ rule-1 │ │ rule-2 │ │      │ │ rule-1 │   │
//	        │ └────┬───┘ └─────┬──┘ │ │ └────┬───┘   │ │  └───┬────┘  │ │ └─┬──────┘ └───┬────┘ │ │ └───┬────┘ └────┬───┘ │ │ └─┬────┬─┘ └────┬───┘ │      │ └────┬───┘   │
//	        │      │           │    │ │      │       │ │      │       │ │   │            │      │ │     │           │     │ │   │    │        │     │      │      │       │
//	        └──────┼───────────┼────┘ └──────┼───────┘ └──────┼───────┘ └───┼────────────┼──────┘ └─────┼───────────┼─────┘ └───┼────┼────────┼─────┘      └──────┼───────┘
//	               │           │             │                │             │            │              │           │           │    │        │                   │
//	               │           │             └────────────────┤             │            │              └───────────┴───────────┘    │        │                   │
//	               ▼           ▼                              │             │            │                          ▼                ▼        │                   ▼
//	┌───────────────────────┐ ┌────────────┐          ┌───────┴─────────────┴───┐  ┌─────┴──────┐             ┌────────────┐        ┌─────────┴──┐          ┌────────────┐
//	│                       │ │            │          │       ▼             ▼   │  │     ▼      │             │            │        │         ▼  │          │            │
//	│ ┌────────┐ ┌────────┐ │ │ ┌────────┐ │          │   ┌────────┐ ┌────────┐ │  │ ┌────────┐ │             │ ┌────────┐ │        │ ┌────────┐ │          │ ┌────────┐ │
//	│ │ port-1 │ │ port-2 │ │ │ │ port-1 │ │          │   │ port-1 │ │ port-2 │ │  │ │ port-1 │ │             │ │ port-1 │ │        │ │ port-1 │ │          │ │ port-1 │ │
//	│ └────────┘ └────────┘ │ │ └────────┘ │          │   └────────┘ └────────┘ │  │ └────────┘ │             │ └────────┘ │        │ └────────┘ │          │ └────────┘ │
//	│                       │ │            │          │                         │  │            │             │            │        │            │          │            │
//	│       service-1       │ │  service-2 │          │         service-3       │  │  service-4 │             │  service-5 │        │  service-6 │          │  service-7 │
//	└───────────────────────┘ └────────────┘          └─────────────────────────┘  └────────────┘             └────────────┘        └────────────┘          └────────────┘
func BuildComplexGatewayAPITopology(funcs ...func(*GatewayAPIResources)) GatewayAPIResources {
	t := GatewayAPIResources{
		GatewayClasses: []*gwapiv1.GatewayClass{
			BuildGatewayClass(func(gc *gwapiv1.GatewayClass) { gc.Name = "gatewayclass-1" }),
			BuildGatewayClass(func(gc *gwapiv1.GatewayClass) { gc.Name = "gatewayclass-2" }),
		},
		Gateways: []*gwapiv1.Gateway{
			BuildGateway(func(g *gwapiv1.Gateway) {
				g.Name = "gateway-1"
				g.Spec.GatewayClassName = "gatewayclass-1"
				g.Spec.Listeners[0].Name = "listener-1"
				g.Spec.Listeners = append(g.Spec.Listeners, gwapiv1.Listener{
					Name:     "listener-2",
					Port:     443,
					Protocol: "HTTPS",
				})
			}),
			BuildGateway(func(g *gwapiv1.Gateway) {
				g.Name = "gateway-2"
				g.Spec.GatewayClassName = "gatewayclass-1"
				g.Spec.Listeners[0].Name = "listener-1"
			}),
			BuildGateway(func(g *gwapiv1.Gateway) {
				g.Name = "gateway-3"
				g.Spec.GatewayClassName = "gatewayclass-1"
				g.Spec.Listeners[0].Name = "listener-1"
				g.Spec.Listeners = append(g.Spec.Listeners, gwapiv1.Listener{
					Name:     "listener-2",
					Port:     443,
					Protocol: "HTTPS",
				})
			}),
			BuildGateway(func(g *gwapiv1.Gateway) {
				g.Name = "gateway-4"
				g.Spec.GatewayClassName = "gatewayclass-2"
				g.Spec.Listeners[0].Name = "listener-1"
				g.Spec.Listeners = append(g.Spec.Listeners, gwapiv1.Listener{
					Name:     "listener-2",
					Port:     443,
					Protocol: "HTTPS",
				})
			}),
			BuildGateway(func(g *gwapiv1.Gateway) {
				g.Name = "gateway-5"
				g.Spec.GatewayClassName = "gatewayclass-2"
				g.Spec.Listeners[0].Name = "listener-1"
			}),
		},
		HTTPRoutes: []*gwapiv1.HTTPRoute{
			BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
				r.Name = "http-route-1"
				r.Spec.ParentRefs[0].Name = "gateway-1"
				r.Spec.Rules = []gwapiv1.HTTPRouteRule{
					{ // rule-1
						BackendRefs: []gwapiv1.HTTPBackendRef{BuildHTTPBackendRef(func(backendRef *gwapiv1.BackendObjectReference) {
							backendRef.Name = "service-1"
						})},
					},
					{ // rule-2
						BackendRefs: []gwapiv1.HTTPBackendRef{BuildHTTPBackendRef(func(backendRef *gwapiv1.BackendObjectReference) {
							backendRef.Name = "service-2"
						})},
					},
				}
			}),
			BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
				r.Name = "http-route-2"
				r.Spec.ParentRefs = []gwapiv1.ParentReference{
					{
						Name:        "gateway-1",
						SectionName: ptr.To(gwapiv1.SectionName("listener-2")),
					},
					{
						Name:        "gateway-2",
						SectionName: ptr.To(gwapiv1.SectionName("listener-1")),
					},
				}
				r.Spec.Rules[0].BackendRefs[0] = BuildHTTPBackendRef(func(backendRef *gwapiv1.BackendObjectReference) {
					backendRef.Name = "service-3"
					backendRef.Port = ptr.To(gwapiv1.PortNumber(80)) // port-1
				})
			}),
			BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
				r.Name = "http-route-3"
				r.Spec.ParentRefs[0].Name = "gateway-2"
				r.Spec.Rules[0].BackendRefs[0] = BuildHTTPBackendRef(func(backendRef *gwapiv1.BackendObjectReference) {
					backendRef.Name = "service-3"
					backendRef.Port = ptr.To(gwapiv1.PortNumber(80)) // port-1
				})
			}),
		},
		Services: []*core.Service{
			BuildService(func(s *core.Service) {
				s.Name = "service-1"
				s.Spec.Ports[0].Name = "port-1"
				s.Spec.Ports = append(s.Spec.Ports, core.ServicePort{
					Name: "port-2",
					Port: 443,
				})
			}),
			BuildService(func(s *core.Service) {
				s.Name = "service-2"
				s.Spec.Ports[0].Name = "port-1"
			}),
			BuildService(func(s *core.Service) {
				s.Name = "service-3"
				s.Spec.Ports[0].Name = "port-1"
				s.Spec.Ports = append(s.Spec.Ports, core.ServicePort{
					Name: "port-2",
					Port: 443,
				})
			}),
			BuildService(func(s *core.Service) {
				s.Name = "service-4"
				s.Spec.Ports[0].Name = "port-1"
			}),
			BuildService(func(s *core.Service) {
				s.Name = "service-5"
				s.Spec.Ports[0].Name = "port-1"
			}),
			BuildService(func(s *core.Service) {
				s.Name = "service-6"
				s.Spec.Ports[0].Name = "port-1"
			}),
			BuildService(func(s *core.Service) {
				s.Name = "service-7"
				s.Spec.Ports[0].Name = "port-1"
			}),
		},
		GRPCRoutes: []*gwapiv1.GRPCRoute{
			BuildGRPCRoute(func(r *gwapiv1.GRPCRoute) {
				r.Name = "grpc-route-1"
				r.Spec.ParentRefs[0].Name = "gateway-5"
				r.Spec.Rules[0].BackendRefs[0] = BuildGRPCBackendRef(func(backendRef *gwapiv1.BackendObjectReference) {
					backendRef.Name = "service-7"
				})
			}),
		},
		TCPRoutes: []*gwapiv1alpha2.TCPRoute{
			BuildTCPRoute(func(r *gwapiv1alpha2.TCPRoute) {
				r.Name = "tcp-route-1"
				r.Spec.ParentRefs[0].Name = "gateway-4"
				r.Spec.Rules = []gwapiv1alpha2.TCPRouteRule{
					{ // rule-1
						BackendRefs: []gwapiv1.BackendRef{
							BuildBackendRef(func(backendRef *gwapiv1.BackendObjectReference) {
								backendRef.Name = "service-5"
							}),
							BuildBackendRef(func(backendRef *gwapiv1.BackendObjectReference) {
								backendRef.Name = "service-6"
							}),
						},
					},
					{ // rule-2
						BackendRefs: []gwapiv1.BackendRef{BuildBackendRef(func(backendRef *gwapiv1.BackendObjectReference) {
							backendRef.Name = "service-6"
							backendRef.Port = ptr.To(gwapiv1.PortNumber(80)) // port-1
						})},
					},
				}
			}),
		},
		TLSRoutes: []*gwapiv1alpha2.TLSRoute{
			BuildTLSRoute(func(r *gwapiv1alpha2.TLSRoute) {
				r.Name = "tls-route-1"
				r.Spec.ParentRefs[0].Name = "gateway-3"
				r.Spec.ParentRefs = append(r.Spec.ParentRefs, gwapiv1.ParentReference{Name: "gateway-4"})
				r.Spec.Rules = []gwapiv1alpha2.TLSRouteRule{
					{ // rule-1
						BackendRefs: []gwapiv1.BackendRef{BuildBackendRef(func(backendRef *gwapiv1.BackendObjectReference) {
							backendRef.Name = "service-5"
						})},
					},
					{ // rule-2
						BackendRefs: []gwapiv1.BackendRef{BuildBackendRef(func(backendRef *gwapiv1.BackendObjectReference) {
							backendRef.Name = "service-5"
						})},
					},
				}
			}),
		},
		UDPRoutes: []*gwapiv1alpha2.UDPRoute{
			BuildUDPRoute(func(r *gwapiv1alpha2.UDPRoute) {
				r.Name = "udp-route-1"
				r.Spec.ParentRefs[0].Name = "gateway-3"
				r.Spec.Rules = []gwapiv1alpha2.UDPRouteRule{
					{ // rule-1
						BackendRefs: []gwapiv1.BackendRef{BuildBackendRef(func(backendRef *gwapiv1.BackendObjectReference) {
							backendRef.Name = "service-3"
							backendRef.Port = ptr.To(gwapiv1.PortNumber(443)) // port-2
						})},
					},
					{ // rule-2
						BackendRefs: []gwapiv1.BackendRef{BuildBackendRef(func(backendRef *gwapiv1.BackendObjectReference) {
							backendRef.Name = "service-4"
							backendRef.Port = ptr.To(gwapiv1.PortNumber(80)) // port-1
						})},
					},
				}
			}),
		},
	}
	for _, f := range funcs {
		f(&t)
	}
	return t
}

type TestPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TestPolicySpec `json:"spec"`
}

type TestPolicySpec struct {
	TargetRef gwapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`
}
