//go:build unit

package v1beta1

import (
	"testing"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLinkLimitadorToDeployment(t *testing.T) {
	t.Run("empty store", func(subT *testing.T) {
		link := LinkLimitadorToDeployment(controller.Store{})
		assert.Equal(subT, link.From, LimitadorGroupKind)
		assert.Equal(subT, link.To, DeploymentGroupKind)
		assert.Assert(subT, is.Len(link.Func(testDeployment("ns1", "foo")), 0))
	})

	t.Run("basic", func(subT *testing.T) {
		store := controller.Store{}
		store["limitador1"] = testLimitador("ns1", "limitador1")
		store["limitador2"] = testLimitador("ns2", "limitador2")
		link := LinkLimitadorToDeployment(store)
		parents := link.Func(testDeployment("ns1", "limitador-limitador"))
		assert.Assert(subT, is.Len(parents, 1))
		assert.Equal(subT, parents[0].GetName(), "limitador1")
		assert.Equal(subT, parents[0].GetNamespace(), "ns1")
		parents = link.Func(testDeployment("ns1", "foo"))
		assert.Assert(subT, is.Len(parents, 0))
		parents = link.Func(testDeployment("ns2", "limitador-limitador"))
		assert.Assert(subT, is.Len(parents, 1))
		assert.Equal(subT, parents[0].GetName(), "limitador2")
		assert.Equal(subT, parents[0].GetNamespace(), "ns2")
	})
}

func testDeployment(ns, name string) machinery.Object {
	return &controller.RuntimeObject{
		Object: &appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				Kind:       DeploymentGroupKind.Kind,
				APIVersion: appsv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
		},
	}
}

func testLimitador(ns, name string) controller.Object {
	return &limitadorv1alpha1.Limitador{
		TypeMeta: metav1.TypeMeta{
			Kind:       LimitadorGroupKind.Kind,
			APIVersion: limitadorv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}
}
