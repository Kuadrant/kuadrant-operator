package ecds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	cachetypes "github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// gatewayNameLabel is the pod label Istio's Gateway API deployment controller
// sets to the owning Gateway's name (see internal/istio.GatewayNameLabel) -
// duplicated here as a literal to avoid an internal/istio <-> internal/ecds
// import for a single string constant.
const gatewayNameLabel = "gateway.networking.k8s.io/gateway-name"

// GatewayNodeKey returns the SnapshotCache node key for a given Gateway.
// Callers pushing config (internal/controller/istio_extension_reconciler.go)
// and gatewayNodeHash (deriving a key from the connecting Envoy's
// self-reported Node) must produce identical keys for the same Gateway, or
// pushes silently go to the wrong (or no) snapshot.
func GatewayNodeKey(namespace, name string) string {
	return namespace + "/" + name
}

// gatewayNodeHash keys the SnapshotCache by the Gateway each connecting Envoy
// belongs to, derived from the Node identity Istio-managed Gateway API
// proxies self-report over xDS - confirmed live (see
// poc/ecds-server/README.md "Per-gateway keying") that Istio populates
// node.metadata.NAMESPACE with the pod's namespace and
// node.metadata.LABELS with the pod's full label set, which for a Gateway
// API-managed gateway pod includes gatewayNameLabel set to the owning
// Gateway's name - both ultimately sourced from ISTIO_META_* env vars and
// the istio-podinfo downward-API volume baked into every such pod by Istio's
// own gateway deployment controller, not anything this operator adds.
type gatewayNodeHash struct{}

func (gatewayNodeHash) ID(node *corev3.Node) string {
	if node == nil || node.GetMetadata() == nil {
		return ""
	}
	fields := node.GetMetadata().GetFields()
	namespace := fields["NAMESPACE"].GetStringValue()
	labels := fields["LABELS"].GetStructValue()
	if namespace == "" || labels == nil {
		return ""
	}
	gatewayName := labels.GetFields()[gatewayNameLabel].GetStringValue()
	if gatewayName == "" {
		return ""
	}
	return GatewayNodeKey(namespace, gatewayName)
}

// Store wraps a go-control-plane SnapshotCache with a small Push API, so
// callers don't need to know about snapshot/version bookkeeping directly -
// mirrors the shape of the poc-extensions-endpoint branch's
// wasm.SetConfig/GetConfig cache, but xDS-flavored.
type Store struct {
	cache cachev3.SnapshotCache
}

// NewStore constructs a Store backed by a fresh, ADS-disabled (ads=false -
// see plan: no cross-type LDS/CDS/RDS consistency needed, we only ever
// serve one resource type) SnapshotCache keyed per-Gateway via
// gatewayNodeHash.
func NewStore() *Store {
	return &Store{
		cache: cachev3.NewSnapshotCache(false, gatewayNodeHash{}, nil),
	}
}

// Cache exposes the underlying SnapshotCache for wiring into the xDS server.
func (s *Store) Cache() cachev3.SnapshotCache {
	return s.cache
}

// Push publishes a new ExtensionConfig resource named `name` for the Gateway
// identified by nodeKey (see GatewayNodeKey), replacing any previously-pushed
// resource(s) for that Gateway only - other Gateways' snapshots are
// untouched. The snapshot version is a content hash of the marshaled
// resource, so redundant pushes (same content) are naturally deduped by the
// snapshot cache's own version comparison in SetSnapshot - no separate
// change-detection needed.
//
// NOTE: SetSnapshot replaces the *entire* per-type resource set for the
// node, not just the named resource being pushed. This PoC only ever
// serves one named resource per Gateway ("kuadrant-wasm-shim-ecds"), so
// that's fine here; serving more than one named ECDS resource per Gateway
// would require Push to track and resend all currently-known resources for
// that node, not just the one being updated.
func (s *Store) Push(ctx context.Context, nodeKey, name string, typedConfig *anypb.Any) error {
	resourceCfg := &corev3.TypedExtensionConfig{
		Name:        name,
		TypedConfig: typedConfig,
	}

	raw, err := proto.Marshal(resourceCfg)
	if err != nil {
		return fmt.Errorf("failed to marshal extension config %q: %w", name, err)
	}
	sum := sha256.Sum256(raw)
	version := hex.EncodeToString(sum[:])

	snapshot, err := cachev3.NewSnapshot(version, map[resourcev3.Type][]cachetypes.Resource{
		resourcev3.ExtensionConfigType: {resourceCfg},
	})
	if err != nil {
		return fmt.Errorf("failed to build snapshot for %q: %w", name, err)
	}

	if err := s.cache.SetSnapshot(ctx, nodeKey, snapshot); err != nil {
		return fmt.Errorf("failed to set snapshot for %q: %w", name, err)
	}

	return nil
}
