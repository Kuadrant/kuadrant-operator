//go:build unit

package kuadrant

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

var _ Referrer = &PolicyKindStub{}

type PolicyKindStub struct{}

func (tpk *PolicyKindStub) Kind() string {
	return "TestPolicy"
}

func (tpk *PolicyKindStub) BackReferenceAnnotationName() string {
	return "kuadrant.io/testpolicies"
}

func (tpk *PolicyKindStub) DirectReferenceAnnotationName() string {
	return "kuadrant.io/testpolicy"
}
const (
	NS = "nsA"
)

type FakePolicy struct {
	client.Object
	Hosts     []string
	targetRef gatewayapiv1alpha2.PolicyTargetReference
}

func (p *FakePolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return p.targetRef
}

func (p *FakePolicy) GetWrappedNamespace() gatewayapiv1.Namespace {
	return gatewayapiv1.Namespace(p.GetNamespace())
}

func (p *FakePolicy) GetRulesHostnames() []string {
	return p.Hosts
}

func (p *FakePolicy) Kind() string {
	return "FakePolicy"
}

type TestPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`
}

var (
	_ KuadrantPolicy   = &TestPolicy{}
	_ GatewayAPIPolicy = &TestPolicy{}
)

func (p *TestPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return p.TargetRef
}

func (p *TestPolicy) GetWrappedNamespace() gatewayapiv1beta1.Namespace {
	return gatewayapiv1beta1.Namespace(p.Namespace)
}

func (p *TestPolicy) GetRulesHostnames() []string {
	return nil
}

func (p *TestPolicy) Kind() string {
	return p.TypeMeta.Kind
}

func (p *TestPolicy) DeepCopyObject() runtime.Object {
	if c := p.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (p *TestPolicy) DeepCopy() *TestPolicy {
	if p == nil {
		return nil
	}
	out := new(TestPolicy)
	p.DeepCopyInto(out)
	return out
}

func (p *TestPolicy) DeepCopyInto(out *TestPolicy) {
	*out = *p
	out.TypeMeta = p.TypeMeta
	p.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	p.TargetRef.DeepCopyInto(&out.TargetRef)
}

func testBasicGateway(name, namespace string) *gatewayapiv1.Gateway {
	// Valid gateway
	return &gatewayapiv1.Gateway{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gatewayapiv1.GroupVersion.String(),
			Kind:       "Gateway",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: gatewayapiv1.GatewayStatus{
			Conditions: []metav1.Condition{
				{
					Type:   GatewayProgrammedConditionType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}
}

func testInvalidGateway(name, namespace string) *gatewayapiv1.Gateway {
	gw := testBasicGateway(name, namespace)
	// remove conditions to make it invalid
	gw.Status = gatewayapiv1.GatewayStatus{}

	return gw
}

func testBasicRoute(name, namespace string, parents ...*gatewayapiv1.Gateway) *gatewayapiv1.HTTPRoute {
	parentRefs := make([]gatewayapiv1.ParentReference, 0)
	for _, val := range parents {
		parentRefs = append(parentRefs, gatewayapiv1.ParentReference{
			Group:     ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
			Kind:      ptr.To(gatewayapiv1.Kind("Gateway")),
			Namespace: ptr.To(gatewayapiv1.Namespace(val.Namespace)),
			Name:      gatewayapiv1.ObjectName(val.Name),
		})
	}

	parentStatusRefs := Map(parentRefs, func(p gatewayapiv1.ParentReference) gatewayapiv1.RouteParentStatus {
		return gatewayapiv1.RouteParentStatus{
			ParentRef:  p,
			Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue}},
		}
	})

	return &gatewayapiv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gatewayapiv1.GroupVersion.String(),
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: gatewayapiv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
		},
		Status: gatewayapiv1.HTTPRouteStatus{
			RouteStatus: gatewayapiv1.RouteStatus{
				Parents: parentStatusRefs,
			},
		},
	}
}

func testBasicGatewayPolicy(name, namespace string, gateway *gatewayapiv1.Gateway) GatewayAPIPolicy {
	return &TestPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "example.com/v1",
			Kind:       "TestPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
			Group:     gatewayapiv1.Group(gatewayapiv1.GroupName),
			Kind:      gatewayapiv1.Kind("Gateway"),
			Namespace: ptr.To(gatewayapiv1.Namespace(gateway.Namespace)),
			Name:      gatewayapiv1.ObjectName(gateway.Name),
		},
	}
}

func testBasicRoutePolicy(name, namespace string, route *gatewayapiv1.HTTPRoute) GatewayAPIPolicy {
	return &TestPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "example.com/v1",
			Kind:       "TestPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
			Group:     gatewayapiv1.Group(gatewayapiv1.GroupName),
			Kind:      gatewayapiv1.Kind("HTTPRoute"),
			Namespace: ptr.To(gatewayapiv1.Namespace(route.Namespace)),
			Name:      gatewayapiv1.ObjectName(route.Name),
		},
	}
}
