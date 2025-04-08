//go:build unit

package istio

import (
	"encoding/json"
	"testing"

	_struct "github.com/golang/protobuf/ptypes/struct"
	"gotest.tools/assert"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istiov1beta1 "istio.io/api/type/v1beta1"
	istioclientgonetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
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
