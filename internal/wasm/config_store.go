package wasm

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// CachedConfig is the last-computed wasm plugin configuration for a
// gateway, kept in memory so PluginConfigService.GetPluginConfig can serve
// it on demand without recomputing it.
type CachedConfig struct {
	ConfigJSON []byte
	SHA256     string
}

// configStore holds the last-computed wasm config per gateway locator.
// Populated at the end of every IstioExtensionReconciler reconcile cycle
// (internal/controller); read by PluginConfigService's gRPC handler
// (internal/extension), which runs independently of the reconcile loop.
// Lives here (a leaf package both already import) to avoid an import cycle
// between internal/controller and internal/extension.
var configStore = struct {
	sync.RWMutex
	byGateway map[string]CachedConfig
}{byGateway: make(map[string]CachedConfig)}

// SetConfig stores the last-computed wasm config JSON for a gateway and
// returns its SHA256 digest, so callers can embed that digest in the
// bootstrap stand-in EnvoyFilter config without duplicating the hashing
// logic (see wasm.RemoteConfigRef.Digest for why).
func SetConfig(gatewayLocator string, configJSON []byte) string {
	sum := sha256.Sum256(configJSON)
	digest := hex.EncodeToString(sum[:])
	configStore.Lock()
	defer configStore.Unlock()
	configStore.byGateway[gatewayLocator] = CachedConfig{
		ConfigJSON: configJSON,
		SHA256:     digest,
	}
	return digest
}

// GetConfig returns the last-computed wasm config JSON for a gateway, if any.
func GetConfig(gatewayLocator string) (CachedConfig, bool) {
	configStore.RLock()
	defer configStore.RUnlock()
	cached, ok := configStore.byGateway[gatewayLocator]
	return cached, ok
}
