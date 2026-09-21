INTEGRATION_COVER_PKGS = ./pkg/...,./internal/...,./api/...
INTEGRATION_COVER_OUTPUT_DIR = $(PROJECT_PATH)/coverage/integration
INTEGRATION_TESTS_EXTRA_ARGS ?=
INTEGRATION_TEST_NUM_CORES ?= 4
INTEGRATION_TEST_NUM_PROCESSES ?= 10
INTEGRATION_TEST_PACKAGES ?= tests/common/...

##@ Integration tests

.PHONY: test-bare-k8s-integration
test-bare-k8s-integration: INTEGRATION_COVER_OUTPUT_DIR=$(PROJECT_PATH)/coverage/bare-k8s-integration
test-bare-k8s-integration: INTEGRATION_TEST_PACKAGES=tests/bare_k8s/...
test-bare-k8s-integration: INTEGRATION_TEST_NUM_PROCESSES=1
test-bare-k8s-integration: test-integration ## Tests Kuadrant on bare Kubernetes. Requires kubernetes cluster with No GatewayAPI CRDs installed.

.PHONY: test-controlplane-integration
test-controlplane-integration: INTEGRATION_COVER_OUTPUT_DIR=$(PROJECT_PATH)/coverage/controlplane-integration
test-controlplane-integration: INTEGRATION_TEST_PACKAGES=tests/controlplane/...
test-controlplane-integration: INTEGRATION_TEST_NUM_PROCESSES=1
test-controlplane-integration: test-integration ## Tests Kuadrant control plane. Requires kubernetes cluster with GatewayAPI CRDs installed.

.PHONY: test-gatewayapi-env-integration
test-gatewayapi-env-integration: INTEGRATION_COVER_OUTPUT_DIR=$(PROJECT_PATH)/coverage/gatewayapi-integration
test-gatewayapi-env-integration: INTEGRATION_TEST_PACKAGES=tests/gatewayapi/...
test-gatewayapi-env-integration: test-integration ## Tests Gateway API integration without a provider. Requires kubernetes cluster with GatewayAPI CRDs installed only (no Istio/Envoy Gateway).

.PHONY: test-istio-env-integration
test-istio-env-integration: INTEGRATION_COVER_OUTPUT_DIR=$(PROJECT_PATH)/coverage/istio-integration
test-istio-env-integration: INTEGRATION_TEST_PACKAGES=tests/common/... tests/istio/...
test-istio-env-integration: GATEWAYAPI_PROVIDER=istio
test-istio-env-integration: test-integration  ## Tests Kuadrant with Istio as the gateway provider. Requires kubernetes cluster with GatewayAPI and Istio installed.

.PHONY: test-envoygateway-env-integration
test-envoygateway-env-integration: INTEGRATION_COVER_OUTPUT_DIR=$(PROJECT_PATH)/coverage/envoygateway-integration
test-envoygateway-env-integration: INTEGRATION_TEST_PACKAGES=tests/common/... tests/envoygateway/...
test-envoygateway-env-integration: GATEWAYAPI_PROVIDER=envoygateway
test-envoygateway-env-integration: test-integration ## Tests Kuadrant with Envoy Gateway as the gateway provider. Requires kubernetes cluster with GatewayAPI and Envoy Gateway installed.

.PHONY: test-integration
test-integration: clean-cov generate fmt vet ginkgo ## Base integration test target. Runs ginkgo with configured packages and settings. Requires kubernetes cluster with appropriate CRDs/providers installed.
	mkdir -p $(INTEGRATION_COVER_OUTPUT_DIR)
	GATEWAYAPI_PROVIDER=$(GATEWAYAPI_PROVIDER) $(GINKGO) \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(INTEGRATION_COVER_OUTPUT_DIR) \
		--coverprofile cover.out \
		-tags integration \
		--compilers=$(INTEGRATION_TEST_NUM_CORES) \
		--procs=$(INTEGRATION_TEST_NUM_PROCESSES) \
		--randomize-all \
		--randomize-suites \
		--fail-on-pending \
		--keep-going \
		--trace \
		--race \
		--output-interceptor-mode=none \
		$(INTEGRATION_TESTS_EXTRA_ARGS) $(INTEGRATION_TEST_PACKAGES)
