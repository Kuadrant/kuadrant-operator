INTEGRATION_COVER_PKGS = ./pkg/...,./internal/...,./api/...
INTEGRATION_TESTS_EXTRA_ARGS ?=
INTEGRATION_TEST_PACKAGES ?= tests/common/...

##@ Integration tests

.PHONY: test-bare-k8s-integration
test-bare-k8s-integration: clean-cov generate fmt vet ginkgo ## Requires only bare kubernetes cluster.
	mkdir -p $(PROJECT_PATH)/coverage/bare-k8s-integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
# Run in series as limit of 1 Kuadrant CR (procs=1)
	OPERATOR_NAMESPACE=kuadrant-system WITH_EXTENSIONS=false $(GINKGO) \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/bare-k8s-integration \
		--coverprofile cover.out \
		-tags integration \
		-p \
		--randomize-all \
		--randomize-suites \
		--fail-on-pending \
		--keep-going \
		--trace \
		--race \
		--output-interceptor-mode=none \
		$(INTEGRATION_TESTS_EXTRA_ARGS) ./tests/bare_k8s/...

.PHONY: test-gatewayapi-env-integration
test-gatewayapi-env-integration: clean-cov generate fmt vet ginkgo ## Requires kubernetes cluster with GatewayAPI installed.
	mkdir -p $(PROJECT_PATH)/coverage/gatewayapi-integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
	OPERATOR_NAMESPACE=kuadrant-system WITH_EXTENSIONS=false $(GINKGO) \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/gatewayapi-integration \
		--coverprofile cover.out \
		-tags integration \
		-p \
		--randomize-all \
		--randomize-suites \
		--fail-on-pending \
		--keep-going \
		--trace \
		--race \
		--output-interceptor-mode=none \
		$(INTEGRATION_TESTS_EXTRA_ARGS) ./tests/gatewayapi/...

.PHONY: test-istio-env-integration
test-istio-env-integration: clean-cov generate fmt vet ginkgo ## Requires kubernetes cluster with GatewayAPI and Istio installed.
	mkdir -p $(PROJECT_PATH)/coverage/istio-integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
	GATEWAYAPI_PROVIDER=istio OPERATOR_NAMESPACE=kuadrant-system WITH_EXTENSIONS=false $(GINKGO) \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/istio-integration \
		--coverprofile cover.out \
		-tags integration \
		-p \
		--randomize-all \
		--randomize-suites \
		--fail-on-pending \
		--keep-going \
		--trace \
		--race \
		--output-interceptor-mode=none \
		$(INTEGRATION_TESTS_EXTRA_ARGS) tests/istio/...

test-envoygateway-env-integration: clean-cov generate fmt vet ginkgo ## Requires kubernetes cluster with GatewayAPI and EnvoyGateway installed.
	mkdir -p $(PROJECT_PATH)/coverage/envoygateway-integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
	GATEWAYAPI_PROVIDER=envoygateway OPERATOR_NAMESPACE=kuadrant-system WITH_EXTENSIONS=false $(GINKGO) \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/envoygateway-integration \
		--coverprofile cover.out \
		-tags integration \
		-p \
		--randomize-all \
		--randomize-suites \
		--fail-on-pending \
		--keep-going \
		--trace \
		--race \
		--output-interceptor-mode=none \
		$(INTEGRATION_TESTS_EXTRA_ARGS) tests/envoygateway/...

.PHONY: test-integration
test-integration: clean-cov generate fmt vet ginkgo ## Requires kubernetes cluster with at least one GatewayAPI provider installed.
	mkdir -p $(PROJECT_PATH)/coverage/integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
	GATEWAYAPI_PROVIDER=$(GATEWAYAPI_PROVIDER) OPERATOR_NAMESPACE=kuadrant-system WITH_EXTENSIONS=false $(GINKGO) \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/integration \
		--coverprofile cover.out \
		-tags integration \
		-p \
		--randomize-all \
		--randomize-suites \
		--fail-on-pending \
		--keep-going \
		--trace \
		--race \
		--output-interceptor-mode=none \
		$(INTEGRATION_TESTS_EXTRA_ARGS) $(INTEGRATION_TEST_PACKAGES)
