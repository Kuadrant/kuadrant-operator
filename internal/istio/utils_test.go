//go:build unit

package istio

import (
	"testing"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
	istiosecurityv1 "istio.io/client-go/pkg/apis/security/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

func TestLinkKuadrantToPeerAuthentication(t *testing.T) {
	t.Run("empty store", func(subT *testing.T) {
		link := LinkKuadrantToPeerAuthentication(controller.Store{})
		assert.Equal(subT, link.From, kuadrantv1beta1.KuadrantGroupKind)
		assert.Equal(subT, link.To, PeerAuthenticationGroupKind)
		assert.Assert(subT, is.Len(link.Func(testPeerAuthentication("ns1", "foo")), 0))
	})

	t.Run("basic", func(subT *testing.T) {
		store := controller.Store{}
		store["kuad1"] = testKuadrantObj("ns1", "kuadrant1")
		store["kuad2"] = testKuadrantObj("ns2", "kuadrant2")
		link := LinkKuadrantToPeerAuthentication(store)
		parents := link.Func(testPeerAuthentication("ns1", "default"))
		assert.Assert(subT, is.Len(parents, 1))
		assert.Equal(subT, parents[0].GetName(), "kuadrant1")
		assert.Equal(subT, parents[0].GetNamespace(), "ns1")
		parents = link.Func(testPeerAuthentication("ns1", "foo"))
		assert.Assert(subT, is.Len(parents, 0))
		parents = link.Func(testPeerAuthentication("ns2", "default"))
		assert.Assert(subT, is.Len(parents, 1))
		assert.Equal(subT, parents[0].GetName(), "kuadrant2")
		assert.Equal(subT, parents[0].GetNamespace(), "ns2")
	})
}

func testPeerAuthentication(ns, name string) machinery.Object {
	return &controller.RuntimeObject{
		Object: &istiosecurityv1.PeerAuthentication{
			TypeMeta: metav1.TypeMeta{
				Kind:       PeerAuthenticationGroupKind.Kind,
				APIVersion: istiosecurityv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
		},
	}
}

func testKuadrantObj(ns, name string) controller.Object {
	return &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantv1beta1.KuadrantGroupKind.Kind,
			APIVersion: kuadrantv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}
}
