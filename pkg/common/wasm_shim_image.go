package common

const (
	DEFAULT_WASMSHIM_IMAGE_VERSION = "oci://quay.io/kuadrant/wasm-shim:latest"
	WASM_SHIM_IMAGE_ENV_NAME       = "RELATED_IMAGE_WASMSHIM"
)

func GetWASMShimImageVersion() string {
	return FetchEnv(WASM_SHIM_IMAGE_ENV_NAME, DEFAULT_WASMSHIM_IMAGE_VERSION)
}
