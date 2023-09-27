package common

import (
	"testing"

	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestBackReferencesFromObject(t *testing.T) {
	obj := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "gw-ns",
			Name:        "gw-1",
			Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
		},
		Spec: corev1.ServiceSpec{},
	}

	policyKind := &PolicyKindStub{}

	refs := Map(BackReferencesFromObject(obj, policyKind), func(ref client.ObjectKey) string { return ref.String() })
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
