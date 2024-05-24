INTEGRATION_COVER_PKGS = ./pkg/...,./controllers/...,./api/...

##@ Integration tests

.PHONY: test-bare-k8s-integration
test-bare-k8s-integration: clean-cov generate fmt vet ginkgo ## Requires only bare kubernetes cluster.
	mkdir -p $(PROJECT_PATH)/coverage/bare-k8s-integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
	$(GINKGO) \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/bare-k8s-integration \
		--coverprofile cover.out \
		-tags integration \
		./tests/bare_k8s/...

.PHONY: test-gatewayapi-env-integration
test-gatewayapi-env-integration: clean-cov generate fmt vet ginkgo ## Requires kubernetes cluster with GatewayAPI installed.
	mkdir -p $(PROJECT_PATH)/coverage/gatewayapi-integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
	$(GINKGO) \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/gatewayapi-integration \
		--coverprofile cover.out \
		-tags integration \
		./tests/gatewayapi/...

.PHONY: test-istio-env-integration
test-istio-env-integration: clean-cov generate fmt vet ginkgo ## Requires kubernetes cluster with GatewayAPI and Istio installed.
	mkdir -p $(PROJECT_PATH)/coverage/istio-integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
	$(GINKGO) \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/istio-integration \
		--coverprofile cover.out \
		-tags integration \
		tests/istio/...

.PHONY: test-integration
test-integration: clean-cov generate fmt vet ginkgo ## Requires kubernetes cluster with at least one GatewayAPI provider installed.
	mkdir -p $(PROJECT_PATH)/coverage/integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
	$(GINKGO) \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/integration \
		--coverprofile cover.out \
		-tags integration \
		./controllers/...
