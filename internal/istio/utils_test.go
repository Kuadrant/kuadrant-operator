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
	istiotypev1beta1 "istio.io/api/type/v1beta1"
	istioclientgonetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiosecurityv1 "istio.io/client-go/pkg/apis/security/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
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
			WorkloadSelector: &istioapinetworkingv1alpha3.WorkloadSelector{
				Labels: map[string]string{
					GatewayNameLabel: "gw1",
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

	t.Run("different workload selector", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)
		b := testBasicEnvoyFilter(subT)
		b.Spec.WorkloadSelector = &istioapinetworkingv1alpha3.WorkloadSelector{
			Labels: map[string]string{
				GatewayNameLabel: "othergw",
			},
		}
		assert.Assert(subT, !EqualEnvoyFilters(a, b))
	})

	t.Run("detects upgrade from targetRefs to workloadSelector", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)
		a.Spec.WorkloadSelector = nil
		a.Spec.TargetRefs = []*istiotypev1beta1.PolicyTargetReference{{
			Group: "gateway.networking.k8s.io",
			Kind:  "Gateway",
			Name:  "gw1",
		}}
		b := testBasicEnvoyFilter(subT)
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

	t.Run("equal patch values with maps in different order", func(subT *testing.T) {
		a := testBasicEnvoyFilter(subT)
		b := testBasicEnvoyFilter(subT)

		// Create identical data but from separate map instances (which could marshal differently)
		mapData := map[string]any{
			"actionSets": []any{
				map[string]any{
					"name": "route-0",
					"routeRuleConditions": map[string]any{
						"hostnames": []any{"*.example.com"},
					},
					"actions": []any{
						map[string]any{
							"service": "ratelimit",
							"scope":   "route-0-rlp",
							"conditionalData": []any{
								map[string]any{
									"predicates": []string{"auth.identity.bob"},
									"data": []any{
										map[string]any{
											"static": map[string]any{
												"key":   "limit.route-0__1234",
												"value": "1",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		// Marshal to JSON and unmarshal into structpb.Struct for both a and b
		aJSON, _ := json.Marshal(mapData)
		aPatchValue := &_struct.Struct{}
		assert.NilError(subT, aPatchValue.UnmarshalJSON(aJSON))
		a.Spec.ConfigPatches[0].Patch.Value = aPatchValue

		bJSON, _ := json.Marshal(mapData)
		bPatchValue := &_struct.Struct{}
		assert.NilError(subT, bPatchValue.UnmarshalJSON(bJSON))
		b.Spec.ConfigPatches[0].Patch.Value = bPatchValue

		// With proto.Equal, these should be equal even if JSON string representations differ
		assert.Assert(subT, EqualEnvoyFilters(a, b),
			"EnvoyFilters with semantically identical patch values should be equal")
	})

	t.Run("equal HTTP_FILTER match with complex listener structure", func(subT *testing.T) {
		// This test validates proto.Equal is used for Match comparison (another fix for #2033)
		a := testBasicEnvoyFilter(subT)
		a.Spec.ConfigPatches[0].ApplyTo = istioapinetworkingv1alpha3.EnvoyFilter_HTTP_FILTER
		a.Spec.ConfigPatches[0].Match = &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istioapinetworkingv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
				Listener: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch{
					FilterChain: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
						Filter: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
							Name: "envoy.filters.network.http_connection_manager",
							SubFilter: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_SubFilterMatch{
								Name: "envoy.filters.http.router",
							},
						},
					},
				},
			},
		}

		b := testBasicEnvoyFilter(subT)
		b.Spec.ConfigPatches[0].ApplyTo = istioapinetworkingv1alpha3.EnvoyFilter_HTTP_FILTER
		b.Spec.ConfigPatches[0].Match = &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istioapinetworkingv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
				Listener: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch{
					FilterChain: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
						Filter: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
							Name: "envoy.filters.network.http_connection_manager",
							SubFilter: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_SubFilterMatch{
								Name: "envoy.filters.http.router",
							},
						},
					},
				},
			},
		}

		// With proto.Equal, these identical Match structures should be equal
		assert.Assert(subT, EqualEnvoyFilters(a, b),
			"EnvoyFilters with identical HTTP_FILTER Match structures should be equal")
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

// TestBuildWasmFilterConfigVmID guards the fix for kuadrant-operator#2351: the
// wasm vm_config.vm_id must be set to the module content SHA. Envoy keys Wasm VM
// reuse on vm_key = hash(vm_id, configuration, code); with an empty vm_id a remote
// code fetch that misses the cache (code == "") collides with the already-running
// VM and the new module is never loaded (gateway keeps the old wasm until a pod
// restart). Deriving vm_id from the SHA changes the key synchronously on a content
// change, forcing a new VM even before the fetch completes.
func TestBuildWasmFilterConfigVmID(t *testing.T) {
	const sha = "fd5039db79d000f31669de1fde37ea175d3ad05f2b14341d6936fb564cab5553"

	filterConfig, err := buildWasmFilterConfig("http://example.com/plugin.wasm", "", sha, "kuadrant-operator-wasm", nil)
	assert.NilError(t, err)

	config, ok := filterConfig["config"].(map[string]any)
	assert.Assert(t, ok, "config should be a map")

	vmConfig, ok := config["vm_config"].(map[string]any)
	assert.Assert(t, ok, "vm_config should be a map")

	assert.Equal(t, vmConfig["vm_id"], sha)

	// Also check the remote data source still pins the same SHA.
	code, ok := vmConfig["code"].(map[string]any)
	assert.Assert(t, ok, "code should be a map")
	remote, ok := code["remote"].(map[string]any)
	assert.Assert(t, ok, "code.remote should be a map")
	assert.Equal(t, remote["sha256"], sha)
}

// TestEqualEnvoyFiltersWasmConfigEncoding verifies that EqualEnvoyFilters compares
// the embedded wasm-shim config semantically, so that the same config encoded as
// plain JSON (as written by a pre-upgrade operator) and as the gzip+base64 envelope
// (as written by the current operator) are considered equal. This is what keeps an
// operator upgrade from rewriting every EnvoyFilter purely because the config
// encoding changed.
func TestEqualEnvoyFiltersWasmConfigEncoding(t *testing.T) {
	const sha = "fd5039db79d000f31669de1fde37ea175d3ad05f2b14341d6936fb564cab5553"

	cfg := &wasm.Config{
		Services: map[string]wasm.Service{
			"ratelimit-service": {
				Endpoint:    "ratelimit-cluster",
				Type:        wasm.DynamicServiceType,
				FailureMode: wasm.FailureModeAllow,
			},
		},
		ActionSets: []wasm.ActionSet{},
	}

	// Envelope struct (gzip+base64), as produced by the current operator.
	envelopeStruct, err := cfg.ToStruct()
	assert.NilError(t, err)

	// Plain-JSON struct, as produced by a pre-upgrade operator.
	plainJSON, err := json.Marshal(cfg)
	assert.NilError(t, err)
	plainStruct := &_struct.Struct{}
	assert.NilError(t, plainStruct.UnmarshalJSON(plainJSON))

	buildEnvoyFilter := func(subT *testing.T, imageSHA string, pluginConfig *_struct.Struct) *istioclientgonetworkingv1alpha3.EnvoyFilter {
		patches, err := BuildEnvoyFilterWasmPatch("http://example.com/plugin.wasm", "", imageSHA, "kuadrant-operator-wasm", pluginConfig)
		assert.NilError(subT, err)
		return &istioclientgonetworkingv1alpha3.EnvoyFilter{
			Spec: istioapinetworkingv1alpha3.EnvoyFilter{ConfigPatches: patches},
		}
	}

	t.Run("plain-JSON config equals gzip+base64 envelope of the same config", func(subT *testing.T) {
		existing := buildEnvoyFilter(subT, sha, plainStruct)   // pre-upgrade
		desired := buildEnvoyFilter(subT, sha, envelopeStruct) // post-upgrade
		assert.Assert(subT, EqualEnvoyFilters(existing, desired))
	})

	t.Run("semantically different configs are not equal", func(subT *testing.T) {
		other := &wasm.Config{
			Services: map[string]wasm.Service{
				"ratelimit-service": {
					Endpoint:    "ratelimit-cluster",
					Type:        wasm.DynamicServiceType,
					FailureMode: wasm.FailureModeDeny, // changed
				},
			},
			ActionSets: []wasm.ActionSet{},
		}
		otherStruct, err := other.ToStruct()
		assert.NilError(subT, err)
		assert.Assert(subT, !EqualEnvoyFilters(buildEnvoyFilter(subT, sha, plainStruct), buildEnvoyFilter(subT, sha, otherStruct)))
	})

	t.Run("same config but different wasm SHA is not equal", func(subT *testing.T) {
		const otherSHA = "aaaa39db79d000f31669de1fde37ea175d3ad05f2b14341d6936fb564cab5553"
		assert.Assert(subT, !EqualEnvoyFilters(buildEnvoyFilter(subT, sha, plainStruct), buildEnvoyFilter(subT, otherSHA, envelopeStruct)))
	})

	t.Run("two HTTP_FILTER patches without an embedded config are equal", func(subT *testing.T) {
		// A nil plugin config produces an HTTP_FILTER patch with no configuration
		// field. Both sides having no embedded config must compare equal (falling
		// through to the structural comparison of the rest of the patch).
		existing := buildEnvoyFilter(subT, sha, nil)
		desired := buildEnvoyFilter(subT, sha, nil)
		assert.Assert(subT, EqualEnvoyFilters(existing, desired))
	})

	t.Run("embedded config present on only one side is not equal", func(subT *testing.T) {
		withoutConfig := buildEnvoyFilter(subT, sha, nil)
		withConfig := buildEnvoyFilter(subT, sha, envelopeStruct)
		assert.Assert(subT, !EqualEnvoyFilters(withoutConfig, withConfig))
	})
}

// TestSplitWasmPluginConfig verifies that extracting the embedded config string is
// nil-safe at every level of the typed_config.value.config.configuration.value
// path: a patch value missing any level (or nil) yields an empty config string and
// never panics.
func TestSplitWasmPluginConfig(t *testing.T) {
	mustStruct := func(subT *testing.T, raw string) *_struct.Struct {
		s := &_struct.Struct{}
		assert.NilError(subT, s.UnmarshalJSON([]byte(raw)))
		return s
	}

	t.Run("nil patch value", func(subT *testing.T) {
		cfg, rest := splitWasmPluginConfig(nil)
		assert.Equal(subT, cfg, "")
		assert.Assert(subT, rest == nil)
	})

	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"missing typed_config", `{"foo":"bar"}`},
		{"missing config", `{"typed_config":{"value":{}}}`},
		{"missing configuration", `{"typed_config":{"value":{"config":{}}}}`},
		{"missing inner value", `{"typed_config":{"value":{"config":{"configuration":{}}}}}`},
	} {
		t.Run(tc.name, func(subT *testing.T) {
			cfg, rest := splitWasmPluginConfig(mustStruct(subT, tc.raw))
			assert.Equal(subT, cfg, "")
			assert.Assert(subT, rest != nil)
		})
	}

	t.Run("full path returns the config string and blanks it in the clone", func(subT *testing.T) {
		in := mustStruct(subT, `{"typed_config":{"value":{"config":{"configuration":{"value":"hello"}}}}}`)
		cfg, rest := splitWasmPluginConfig(in)
		assert.Equal(subT, cfg, "hello")

		// The clone has the config string blanked, and the input is left untouched.
		blanked := rest.GetFields()["typed_config"].GetStructValue().
			GetFields()["value"].GetStructValue().
			GetFields()["config"].GetStructValue().
			GetFields()["configuration"].GetStructValue().
			GetFields()["value"].GetStringValue()
		assert.Equal(subT, blanked, "")

		original := in.GetFields()["typed_config"].GetStructValue().
			GetFields()["value"].GetStructValue().
			GetFields()["config"].GetStructValue().
			GetFields()["configuration"].GetStructValue().
			GetFields()["value"].GetStringValue()
		assert.Equal(subT, original, "hello")
	})
}
