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

// SetConfig stores the last-computed wasm config JSON for a gateway.
func SetConfig(gatewayLocator string, configJSON []byte) {
	sum := sha256.Sum256(configJSON)
	configStore.Lock()
	defer configStore.Unlock()
	configStore.byGateway[gatewayLocator] = CachedConfig{
		ConfigJSON: configJSON,
		SHA256:     hex.EncodeToString(sum[:]),
	}
}

// GetConfig returns the last-computed wasm config JSON for a gateway, if any.
func GetConfig(gatewayLocator string) (CachedConfig, bool) {
	configStore.RLock()
	defer configStore.RUnlock()
	cached, ok := configStore.byGateway[gatewayLocator]
	return cached, ok
}
