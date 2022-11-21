.PHONY: opm
OPM = $(PROJECT_PATH)/bin/opm
OPM_VERSION = v1.26.2
$(OPM):
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/$(OPM_VERSION)/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}

opm: $(OPM) ## Download opm locally if necessary.

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:$(IMAGE_TAG)

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# Ref https://olm.operatorframework.io/docs/tasks/creating-a-catalog/#catalog-creation-with-raw-file-based-catalogs
.PHONY: catalog-build
catalog-build: export BUNDLE_VERSION := $(VERSION)
catalog-build: export AUTHORINO_OPERATOR_BUNDLE_VERSION := $(AUTHORINO_OPERATOR_BUNDLE_VERSION)
catalog-build: export LIMITADOR_OPERATOR_BUNDLE_VERSION := $(LIMITADOR_OPERATOR_BUNDLE_VERSION)
catalog-build: opm ## Build a catalog image.
	# Initializing the Catalog
	-rm -rf $(PROJECT_PATH)/catalog/kuadrant-operator-catalog
	-rm -rf $(PROJECT_PATH)/catalog/kuadrant-operator-catalog.Dockerfile
	mkdir -p $(PROJECT_PATH)/catalog/kuadrant-operator-catalog
	cd $(PROJECT_PATH)/catalog && $(OPM) generate dockerfile kuadrant-operator-catalog
	###
	# Limitador Operator
	###
	# Add the package
	cd $(PROJECT_PATH)/catalog && $(OPM) init limitador-operator --default-channel=preview --output yaml >> $(PROJECT_PATH)/catalog/kuadrant-operator-catalog/operator.yaml
	# Add a bundles to the Catalog
	$(OPM) render $(LIMITADOR_OPERATOR_BUNDLE_IMG) --output=yaml >> $(PROJECT_PATH)/catalog/kuadrant-operator-catalog/operator.yaml
	# Add a channel entry for the bundle
	envsubst < $(PROJECT_PATH)/catalog/limitador-operator-channel-entry.template.yaml >> $(PROJECT_PATH)/catalog/kuadrant-operator-catalog/operator.yaml
	###
	# Authorino Operator
	###
	# Add the package
	cd $(PROJECT_PATH)/catalog && $(OPM) init authorino-operator --default-channel=preview --output yaml >> $(PROJECT_PATH)/catalog/kuadrant-operator-catalog/operator.yaml
	# Add a bundles to the Catalog
	$(OPM) render $(AUTHORINO_OPERATOR_BUNDLE_IMG) --output=yaml >> $(PROJECT_PATH)/catalog/kuadrant-operator-catalog/operator.yaml
	# Add a channel entry for the bundle
	envsubst < $(PROJECT_PATH)/catalog/authorino-operator-channel-entry.template.yaml >> $(PROJECT_PATH)/catalog/kuadrant-operator-catalog/operator.yaml
	###
	# Kuadrant Operator
	###
	cd $(PROJECT_PATH)/catalog && $(OPM) init kuadrant-operator --default-channel=preview --description=$(PROJECT_PATH)/catalog/README.md --icon=$(PROJECT_PATH)/catalog/icon.png --output yaml >> $(PROJECT_PATH)/catalog/kuadrant-operator-catalog/operator.yaml
	# Add a bundles to the Catalog: kuadrant-operator
	$(OPM) render $(BUNDLE_IMG) --output=yaml >> $(PROJECT_PATH)/catalog/kuadrant-operator-catalog/operator.yaml
	# Add a channel entry for the bundle
	envsubst < $(PROJECT_PATH)/catalog/kuadrant-operator-channel-entry.template.yaml >> $(PROJECT_PATH)/catalog/kuadrant-operator-catalog/operator.yaml
	# Validate the Catalog
	cd $(PROJECT_PATH)/catalog && $(OPM) validate kuadrant-operator-catalog
	# Build the Catalog
	docker build $(PROJECT_PATH)/catalog -f $(PROJECT_PATH)/catalog/kuadrant-operator-catalog.Dockerfile -t $(CATALOG_IMG)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)
