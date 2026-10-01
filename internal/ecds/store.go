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

// fixedNodeKey is the single cache key used for every connected Envoy in
// this PoC. Production use would key by something Gateway-identifying
// derived from node.Metadata/node.Cluster instead (see
// poc/ecds-server/README.md for why this is explicitly scoped out of this
// first pass, not a discovered limitation to silently fix later).
const fixedNodeKey = "kuadrant-ecds"

// fixedNodeHash maps every connected Envoy node to the same cache key, so
// every Envoy gets the same ECDS snapshot regardless of its node.id.
type fixedNodeHash struct{}

func (fixedNodeHash) ID(*corev3.Node) string { return fixedNodeKey }

// Store wraps a go-control-plane SnapshotCache with a small Push API, so
// callers don't need to know about snapshot/version bookkeeping directly -
// mirrors the shape of the poc-extensions-endpoint branch's
// wasm.SetConfig/GetConfig cache, but xDS-flavored.
type Store struct {
	cache cachev3.SnapshotCache
}

// NewStore constructs a Store backed by a fresh, ADS-disabled (ads=false -
// see plan: no cross-type LDS/CDS/RDS consistency needed, we only ever
// serve one resource type) SnapshotCache using the fixed node key above.
func NewStore() *Store {
	return &Store{
		cache: cachev3.NewSnapshotCache(false, fixedNodeHash{}, nil),
	}
}

// Cache exposes the underlying SnapshotCache for wiring into the xDS server.
func (s *Store) Cache() cachev3.SnapshotCache {
	return s.cache
}

// Push publishes a new ExtensionConfig resource named `name`, replacing any
// previously-pushed resource(s) for the fixed node key. The snapshot
// version is a content hash of the marshaled resource, so redundant pushes
// (same content) are naturally deduped by the snapshot cache's own
// version comparison in SetSnapshot - no separate change-detection needed.
//
// NOTE: SetSnapshot replaces the *entire* per-type resource set for the
// node, not just the named resource being pushed. This PoC only ever
// serves one named resource ("kuadrant-wasm-shim-ecds"), so that's fine
// here; serving more than one named ECDS resource per node would require
// Push to track and resend all currently-known resources, not just the one
// being updated.
func (s *Store) Push(ctx context.Context, name string, typedConfig *anypb.Any) error {
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

	if err := s.cache.SetSnapshot(ctx, fixedNodeKey, snapshot); err != nil {
		return fmt.Errorf("failed to set snapshot for %q: %w", name, err)
	}

	return nil
}
