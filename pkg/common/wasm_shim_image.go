package common

const (
	defaultWasmShimImageVersion = "oci://quay.io/kuadrant/wasm-shim:latest"
	wasmShimImageEnvName        = "RELATED_IMAGE_WASMSHIM"
)

func GetWASMShimImageVersion() string {
	return FetchEnv(wasmShimImageEnvName, defaultWasmShimImageVersion)
}
