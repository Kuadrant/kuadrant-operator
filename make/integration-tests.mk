INTEGRATION_COVER_PKGS = ./pkg/...,./controllers/...,./api/...
INTEGRATION_TESTS_EXTRA_ARGS =

##@ Integration tests

.PHONY: test-bare-k8s-integration
test-bare-k8s-integration: $(WASM_SHIM) clean-cov generate fmt vet ginkgo ## Requires only bare kubernetes cluster.
	mkdir -p $(PROJECT_PATH)/coverage/bare-k8s-integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
	WASM_SHIM_SHA256=$$(sha256sum $(WASM_SHIM) | awk '{print $$1}') \
	&& $(GINKGO) \
		-ldflags "-X github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm.WasmShimSha256=$${WASM_SHIM_SHA256}" \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/bare-k8s-integration \
		--coverprofile cover.out \
		-tags integration \
		$(INTEGRATION_TESTS_EXTRA_ARGS) ./tests/bare_k8s/...

.PHONY: test-gatewayapi-env-integration
test-gatewayapi-env-integration: $(WASM_SHIM) clean-cov generate fmt vet ginkgo ## Requires kubernetes cluster with GatewayAPI installed.
	mkdir -p $(PROJECT_PATH)/coverage/gatewayapi-integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
	WASM_SHIM_SHA256=$$(sha256sum $(WASM_SHIM) | awk '{print $$1}') \
	&& $(GINKGO) \
		-ldflags "-X github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm.WasmShimSha256=$${WASM_SHIM_SHA256}" \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/gatewayapi-integration \
		--coverprofile cover.out \
		-tags integration \
		$(INTEGRATION_TESTS_EXTRA_ARGS) ./tests/gatewayapi/...

.PHONY: test-istio-env-integration
test-istio-env-integration: $(WASM_SHIM) clean-cov generate fmt vet ginkgo ## Requires kubernetes cluster with GatewayAPI and Istio installed.
	mkdir -p $(PROJECT_PATH)/coverage/istio-integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
	WASM_SHIM_SHA256=$$(sha256sum $(WASM_SHIM) | awk '{print $$1}') \
	&& $(GINKGO) \
		-ldflags "-X github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm.WasmShimSha256=$${WASM_SHIM_SHA256}" \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/istio-integration \
		--coverprofile cover.out \
		-tags integration \
		$(INTEGRATION_TESTS_EXTRA_ARGS) tests/istio/...

.PHONY: test-integration
test-integration: $(WASM_SHIM) clean-cov generate fmt vet ginkgo ## Requires kubernetes cluster with at least one GatewayAPI provider installed.
	mkdir -p $(PROJECT_PATH)/coverage/integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
	WASM_SHIM_SHA256=$$(sha256sum $(WASM_SHIM) | awk '{print $$1}') \
	&& $(GINKGO) \
		-ldflags "-X github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm.WasmShimSha256=$${WASM_SHIM_SHA256}" \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/integration \
		--coverprofile cover.out \
		-tags integration \
		$(INTEGRATION_TESTS_EXTRA_ARGS) ./controllers/...
