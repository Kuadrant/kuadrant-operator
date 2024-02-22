package kuadrant

import (
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func TestGatewayWrapperKey(t *testing.T) {
	gw := GatewayWrapper{
		Gateway: &gatewayapiv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "gw-ns",
				Name:        "gw-1",
				Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
			},
		},
		Referrer: &PolicyKindStub{},
	}
	if gw.Key().Namespace != "gw-ns" || gw.Key().Name != "gw-1" {
		t.Fail()
	}
}

func TestGatewayWrapperPolicyRefs(t *testing.T) {
	gw := GatewayWrapper{
		Gateway: &gatewayapiv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "gw-ns",
				Name:        "gw-1",
				Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
			},
		},
		Referrer: &PolicyKindStub{},
	}
	refs := utils.Map(gw.PolicyRefs(), func(ref k8stypes.NamespacedName) string { return ref.String() })
	if !slices.Contains(refs, "app-ns/policy-1") {
		t.Error("GatewayWrapper.PolicyRefs() should contain app-ns/policy-1")
	}
	if !slices.Contains(refs, "app-ns/policy-2") {
		t.Error("GatewayWrapper.PolicyRefs() should contain app-ns/policy-2")
	}
	if len(refs) != 2 {
		t.Fail()
	}
}

func TestGatewayWrapperContainsPolicy(t *testing.T) {
	gw := GatewayWrapper{
		Gateway: &gatewayapiv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "gw-ns",
				Name:        "gw-1",
				Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
			},
		},
		Referrer: &PolicyKindStub{},
	}
	if !gw.ContainsPolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-1"}) {
		t.Error("GatewayWrapper.ContainsPolicy() should contain app-ns/policy-1")
	}
	if !gw.ContainsPolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-2"}) {
		t.Error("GatewayWrapper.ContainsPolicy() should contain app-ns/policy-1")
	}
	if gw.ContainsPolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-3"}) {
		t.Error("GatewayWrapper.ContainsPolicy() should not contain app-ns/policy-1")
	}
}

func TestGatewayWrapperAddPolicy(t *testing.T) {
	gateway := gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "gw-ns",
			Name:        "gw-1",
			Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
		},
	}
	gw := GatewayWrapper{
		Gateway:  &gateway,
		Referrer: &PolicyKindStub{},
	}
	if gw.AddPolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-1"}) {
		t.Error("GatewayWrapper.AddPolicy() expected to return false")
	}
	if !gw.AddPolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-3"}) {
		t.Error("GatewayWrapper.AddPolicy() expected to return true")
	}
	if gw.Annotations["kuadrant.io/testpolicies"] != `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"},{"Namespace":"app-ns","Name":"policy-3"}]` {
		t.Error("GatewayWrapper.AddPolicy() expected to have added policy ref to the annotations")
	}
}

func TestGatewayDeletePolicy(t *testing.T) {
	gateway := gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "gw-ns",
			Name:        "gw-1",
			Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
		},
	}
	gw := GatewayWrapper{
		Gateway:  &gateway,
		Referrer: &PolicyKindStub{},
	}
	if !gw.DeletePolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-1"}) {
		t.Error("GatewayWrapper.DeletePolicy() expected to return true")
	}
	if gw.DeletePolicy(client.ObjectKey{Namespace: "app-ns", Name: "policy-3"}) {
		t.Error("GatewayWrapper.DeletePolicy() expected to return false")
	}
	if gw.Annotations["kuadrant.io/testpolicies"] != `[{"Namespace":"app-ns","Name":"policy-2"}]` {
		t.Error("GatewayWrapper.DeletePolicy() expected to have deleted policy ref from the annotations")
	}
}

func TestGatewayHostnames(t *testing.T) {
	hostname := gatewayapiv1.Hostname("toystore.com")
	gateway := gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "gw-ns",
			Name:        "gw-1",
			Annotations: map[string]string{"kuadrant.io/ratelimitpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
		},
		Spec: gatewayapiv1.GatewaySpec{
			Listeners: []gatewayapiv1.Listener{
				{
					Name:     "my-listener",
					Hostname: &hostname,
				},
			},
		},
	}
	gw := GatewayWrapper{
		Gateway:  &gateway,
		Referrer: &PolicyKindStub{},
	}
	hostnames := gw.Hostnames()
	if !slices.Contains(hostnames, "toystore.com") {
		t.Error("GatewayWrapper.Hostnames() expected to contain 'toystore.com'")
	}
	if len(hostnames) != 1 {
		t.Fail()
	}
}

func TestBackReferencesFromGatewayWrapper(t *testing.T) {
	gw := GatewayWrapper{
		Gateway: &gatewayapiv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "gw-ns",
				Name:        "gw-1",
				Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
			},
		},
		Referrer: &PolicyKindStub{},
	}
	refs := utils.Map(BackReferencesFromObject(gw.Gateway, gw.Referrer), func(ref client.ObjectKey) string { return ref.String() })
	if !slices.Contains(refs, "app-ns/policy-1") {
		t.Error("GatewayWrapper.PolicyRefs() should contain app-ns/policy-1")
	}
	if !slices.Contains(refs, "app-ns/policy-2") {
		t.Error("GatewayWrapper.PolicyRefs() should contain app-ns/policy-2")
	}
	if len(refs) != 2 {
		t.Fail()
	}
}
