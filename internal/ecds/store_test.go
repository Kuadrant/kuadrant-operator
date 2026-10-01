//go:build unit

package ecds

import (
	"context"
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"gotest.tools/assert"
)

func nodeWithMetadata(fields map[string]any) *corev3.Node {
	metadata, err := structpb.NewStruct(fields)
	if err != nil {
		panic(err)
	}
	return &corev3.Node{Metadata: metadata}
}

func TestGatewayNodeHash(t *testing.T) {
	hash := gatewayNodeHash{}

	t.Run("nil node returns empty key", func(t *testing.T) {
		assert.Equal(t, hash.ID(nil), "")
	})

	t.Run("node with no metadata returns empty key", func(t *testing.T) {
		assert.Equal(t, hash.ID(&corev3.Node{}), "")
	})

	t.Run("missing NAMESPACE field returns empty key", func(t *testing.T) {
		node := nodeWithMetadata(map[string]any{
			"LABELS": map[string]any{gatewayNameLabel: "my-gateway"},
		})
		assert.Equal(t, hash.ID(node), "")
	})

	t.Run("missing LABELS field returns empty key", func(t *testing.T) {
		node := nodeWithMetadata(map[string]any{
			"NAMESPACE": "gateway-system",
		})
		assert.Equal(t, hash.ID(node), "")
	})

	t.Run("LABELS without gateway-name label returns empty key", func(t *testing.T) {
		node := nodeWithMetadata(map[string]any{
			"NAMESPACE": "gateway-system",
			"LABELS":    map[string]any{"istio": "ingressgateway"},
		})
		assert.Equal(t, hash.ID(node), "")
	})

	t.Run("real Istio Gateway API node metadata shape resolves to namespace/gateway-name", func(t *testing.T) {
		// Shape confirmed live against Istio 1.29.1 (see poc/ecds-server/README.md).
		node := nodeWithMetadata(map[string]any{
			"NAMESPACE": "gateway-system",
			"LABELS": map[string]any{
				"gateway.istio.io/managed":                     "istio.io-gateway-controller",
				"gateway.networking.k8s.io/gateway-class-name": "istio",
				"gateway.networking.k8s.io/gateway-name":       "kuadrant-ingressgateway",
				"istio":                                        "ingressgateway",
			},
		})
		assert.Equal(t, hash.ID(node), "gateway-system/kuadrant-ingressgateway")
	})
}

func TestStorePushIsolatesPerGateway(t *testing.T) {
	store := NewStore()
	ctx := context.Background()

	anyA, err := anypb.New(&corev3.Node{Id: "config-for-gw-a"})
	assert.NilError(t, err)
	anyB, err := anypb.New(&corev3.Node{Id: "config-for-gw-b"})
	assert.NilError(t, err)

	keyA := GatewayNodeKey("ns-a", "gw-a")
	keyB := GatewayNodeKey("ns-b", "gw-b")

	assert.NilError(t, store.Push(ctx, keyA, "kuadrant-wasm-shim-ecds", anyA))
	assert.NilError(t, store.Push(ctx, keyB, "kuadrant-wasm-shim-ecds", anyB))

	snapA, err := store.Cache().GetSnapshot(keyA)
	assert.NilError(t, err)
	snapB, err := store.Cache().GetSnapshot(keyB)
	assert.NilError(t, err)

	assert.Assert(t, snapA.GetVersion("type.googleapis.com/envoy.config.core.v3.TypedExtensionConfig") !=
		snapB.GetVersion("type.googleapis.com/envoy.config.core.v3.TypedExtensionConfig"),
		"pushing a different config for gw-b must not change gw-a's snapshot version")

	// gw-a's snapshot must still reflect gw-a's own content, not gw-b's.
	resourcesA := snapA.GetResources("type.googleapis.com/envoy.config.core.v3.TypedExtensionConfig")
	cfgA, ok := resourcesA["kuadrant-wasm-shim-ecds"].(*corev3.TypedExtensionConfig)
	assert.Assert(t, ok)
	decodedA := &corev3.Node{}
	assert.NilError(t, cfgA.GetTypedConfig().UnmarshalTo(decodedA))
	assert.Equal(t, decodedA.GetId(), "config-for-gw-a")
}
