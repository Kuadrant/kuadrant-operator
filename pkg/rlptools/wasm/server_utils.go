package wasm

import "k8s.io/utils/env"

const (
	ServerSvcName = "kuadrant-operator-controller-manager-ratelimit-wasm-service"
)

var (
	WasmShimSha256 = "not injected"
)

func ServerServiceName() string {
	return ServerSvcName
}

func ServerServicePort() int {
	return 8082
}

func ServerServiceNamespace() string {
	return env.GetString("POD_NAMESPACE", "POD_NAMESPACE_MISSING")
}

func SHA256() string {
	return WasmShimSha256
}
