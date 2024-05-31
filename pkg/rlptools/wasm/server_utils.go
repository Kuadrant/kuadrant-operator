package wasm

import "k8s.io/utils/env"

const (
	SERVER_SERVICE_NAME = "kuadrant-operator-controller-manager-ratelimit-wasm-service"
)

var (
	WASM_SHIM_SHA256 = "not injected"
)

func ServerServiceName() string {
	return SERVER_SERVICE_NAME
}

func ServerServicePort() int {
	return 8082
}

func ServerServiceNamespace() string {
	return env.GetString("POD_NAMESPACE", "POD_NAMESPACE_MISSING")
}

func SHA256() string {
	return WASM_SHIM_SHA256
}
