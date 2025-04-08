//go:build unit

package istio

import (
	"encoding/json"
	"testing"

	_struct "github.com/golang/protobuf/ptypes/struct"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istiov1beta1 "istio.io/api/type/v1beta1"
	istioclientgonetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiosecurityv1 "istio.io/client-go/pkg/apis/security/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

func testBasicEnvoyFilter(t *testing.T) *istioclientgonetworkingv1alpha3.EnvoyFilter {
	patchValueRaw, _ := json.Marshal(
		map[string]any{
			"foo":   "bar",
			"alice": "bob",
		},
	)

	patchValue := &_struct.Struct{}
	assert.NilError(t, patchValue.UnmarshalJSON(patchValueRaw))

	return &istioclientgonetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       EnvoyFilterGroupKind.Kind,
			APIVersion: istioclientgonetworkingv1alpha3.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "ns1",
		},
		Spec: istioapinetworkingv1alpha3.EnvoyFilter{
			Priority: 1,
			TargetRefs: []*istiov1beta1.PolicyTargetReference{
				{
					Group: gwapiv1.SchemeGroupVersion.Group,
					Kind:  "Gateway",
					Name:  "gw1",
				},
			},
			ConfigPatches: []*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: istioapinetworkingv1alpha3.EnvoyFilter_CLUSTER,
					Match: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
							Cluster: &istioapinetworkingv1alpha3.EnvoyFilter_ClusterMatch{
								Service: "some_service",
							},
						},
					},
					Patch: &istioapinetworkingv1alpha3.EnvoyFilter_Patch{
						Operation: istioapinetworkingv1alpha3.EnvoyFilter_Patch_ADD,
						Value:     patchValue,
					},
				},
			},
		},
	}
}

func TestEqualEnvoyFilters(t *testing.T) {

	t.Run("equal envoy filters", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)
		b := testBasicEnvoyFilter(subT)

		assert.Assert(subT, EqualEnvoyFilters(a, b))
	})

	t.Run("different targetrefs", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)
		b := testBasicEnvoyFilter(subT)
		b.Spec.TargetRefs[0].Name = "othergw"
		assert.Assert(subT, !EqualEnvoyFilters(a, b))
	})

	t.Run("different priorities", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)
		b := testBasicEnvoyFilter(subT)
		b.Spec.Priority = b.Spec.Priority + 1
		assert.Assert(subT, !EqualEnvoyFilters(a, b))
	})

	t.Run("different number of configpatches", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)
		b := testBasicEnvoyFilter(subT)
		b.Spec.ConfigPatches = append(b.Spec.ConfigPatches, b.Spec.ConfigPatches[0])
		assert.Assert(subT, !EqualEnvoyFilters(a, b))
	})

	t.Run("nil configpatches are valid", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)
		b := testBasicEnvoyFilter(subT)
		a.Spec.ConfigPatches = []*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
			nil, nil, nil, nil, nil,
		}

		b.Spec.ConfigPatches = []*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
			nil, nil, nil, nil, nil,
		}
		assert.Assert(subT, EqualEnvoyFilters(a, b))
	})

	t.Run("different configpatch applyto", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)
		a.Spec.ConfigPatches[0].ApplyTo = istioapinetworkingv1alpha3.EnvoyFilter_HTTP_ROUTE

		b := testBasicEnvoyFilter(subT)
		b.Spec.ConfigPatches[0].ApplyTo = istioapinetworkingv1alpha3.EnvoyFilter_CLUSTER

		assert.Assert(subT, !EqualEnvoyFilters(a, b))
	})

	t.Run("different configpatch cluster service", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)
		a.Spec.ConfigPatches[0].Match = &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			ObjectTypes: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
				Cluster: &istioapinetworkingv1alpha3.EnvoyFilter_ClusterMatch{
					Service: "some_service_a",
				},
			},
		}

		b := testBasicEnvoyFilter(subT)
		b.Spec.ConfigPatches[0].Match = &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			ObjectTypes: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
				Cluster: &istioapinetworkingv1alpha3.EnvoyFilter_ClusterMatch{
					Service: "some_service_b",
				},
			},
		}

		assert.Assert(subT, !EqualEnvoyFilters(a, b))
	})

	t.Run("different configpatch cluster subset", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)
		a.Spec.ConfigPatches[0].Match = &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			ObjectTypes: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
				Cluster: &istioapinetworkingv1alpha3.EnvoyFilter_ClusterMatch{
					Subset: "subset_a",
				},
			},
		}

		b := testBasicEnvoyFilter(subT)
		b.Spec.ConfigPatches[0].Match = &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			ObjectTypes: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
				Cluster: &istioapinetworkingv1alpha3.EnvoyFilter_ClusterMatch{
					Subset: "subset_b",
				},
			},
		}

		assert.Assert(subT, !EqualEnvoyFilters(a, b))
	})

	t.Run("different configpatch patch operation", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)
		a.Spec.ConfigPatches[0].Patch = &istioapinetworkingv1alpha3.EnvoyFilter_Patch{
			Operation: istioapinetworkingv1alpha3.EnvoyFilter_Patch_MERGE,
		}

		b := testBasicEnvoyFilter(subT)
		b.Spec.ConfigPatches[0].Patch = &istioapinetworkingv1alpha3.EnvoyFilter_Patch{
			Operation: istioapinetworkingv1alpha3.EnvoyFilter_Patch_ADD,
		}

		assert.Assert(subT, !EqualEnvoyFilters(a, b))
	})

	t.Run("different configpatch patch filterclass", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)
		a.Spec.ConfigPatches[0].Patch.FilterClass = istioapinetworkingv1alpha3.EnvoyFilter_Patch_AUTHN

		b := testBasicEnvoyFilter(subT)
		b.Spec.ConfigPatches[0].Patch.FilterClass = istioapinetworkingv1alpha3.EnvoyFilter_Patch_STATS

		assert.Assert(subT, !EqualEnvoyFilters(a, b))
	})

	t.Run("different configpatch patch value", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)

		patchValueRaw, err := json.Marshal(map[string]any{"one": "two"})
		assert.NilError(t, err)
		patchValue := &_struct.Struct{}
		assert.NilError(t, patchValue.UnmarshalJSON(patchValueRaw))
		a.Spec.ConfigPatches[0].Patch.Value = patchValue

		b := testBasicEnvoyFilter(subT)
		patchValueRaw, err = json.Marshal(map[string]any{"three": "four"})
		assert.NilError(t, err)
		patchValue = &_struct.Struct{}
		assert.NilError(t, patchValue.UnmarshalJSON(patchValueRaw))
		b.Spec.ConfigPatches[0].Patch.Value = patchValue

		assert.Assert(subT, !EqualEnvoyFilters(a, b))
	})
}

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
