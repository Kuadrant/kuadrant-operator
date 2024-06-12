##@ Operator Catalog


# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:$(IMAGE_TAG)

CATALOG_FILE = $(PROJECT_PATH)/catalog/kuadrant-operator-catalog/operator.yaml
CATALOG_DOCKERFILE = $(PROJECT_PATH)/catalog/kuadrant-operator-catalog.Dockerfile

$(CATALOG_DOCKERFILE): $(OPM)
	-mkdir -p $(PROJECT_PATH)/catalog/kuadrant-operator-catalog
	cd $(PROJECT_PATH)/catalog && $(OPM) generate dockerfile kuadrant-operator-catalog
catalog-dockerfile: $(CATALOG_DOCKERFILE) ## Generate catalog dockerfile.

CHANNELS ?= preview

$(CATALOG_FILE): $(OPM) $(YQ)
	@echo "************************************************************"
	@echo Build kuadrant operator catalog
	@echo
	@echo BUNDLE_IMG                     = $(BUNDLE_IMG)
	@echo REPLACES_VERSION               = $(REPLACES_VERSION)
	@echo LIMITADOR_OPERATOR_BUNDLE_IMG  = $(LIMITADOR_OPERATOR_BUNDLE_IMG)
	@echo AUTHORINO_OPERATOR_BUNDLE_IMG  = $(AUTHORINO_OPERATOR_BUNDLE_IMG)
	@echo DNS_OPERATOR_BUNDLE_IMG  		 = $(DNS_OPERATOR_BUNDLE_IMG)
	@echo CHANNELS  					 = $(CHANNELS)
	@echo CATALOG_FILE                   = $@
	@echo "************************************************************"
	@echo
	@echo Please check this matches your expectations and override variables if needed.
	@echo
	$(PROJECT_PATH)/utils/generate-catalog.sh $(OPM) $(YQ) $(BUNDLE_IMG) $(REPLACES_VERSION)\
			$(LIMITADOR_OPERATOR_BUNDLE_IMG) $(AUTHORINO_OPERATOR_BUNDLE_IMG) $(DNS_OPERATOR_BUNDLE_IMG) $(CHANNELS) $@

.PHONY: catalog
catalog: $(OPM) ## Generate catalog content and validate.
	# Initializing the Catalog
	-rm -rf $(PROJECT_PATH)/catalog/kuadrant-operator-catalog
	-rm -rf $(PROJECT_PATH)/catalog/kuadrant-operator-catalog.Dockerfile
	$(MAKE) $(CATALOG_DOCKERFILE)
	$(MAKE) $(CATALOG_FILE) LIMITADOR_OPERATOR_BUNDLE_IMG=$(LIMITADOR_OPERATOR_BUNDLE_IMG) \
		AUTHORINO_OPERATOR_BUNDLE_IMG=$(AUTHORINO_OPERATOR_BUNDLE_IMG) \
		BUNDLE_IMG=$(BUNDLE_IMG) \
		REPLACES_VERSION=$(REPLACES_VERSION)
	cd $(PROJECT_PATH)/catalog && $(OPM) validate kuadrant-operator-catalog

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# Ref https://olm.operatorframework.io/docs/tasks/creating-a-catalog/#catalog-creation-with-raw-file-based-catalogs
.PHONY: catalog-build
catalog-build: ## Build a catalog image.
	# Build the Catalog
	docker build $(PROJECT_PATH)/catalog -f $(PROJECT_PATH)/catalog/kuadrant-operator-catalog.Dockerfile -t $(CATALOG_IMG)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

.PHONY: deploy-catalog
deploy-catalog: $(KUSTOMIZE) $(YQ) ## Deploy operator to the K8s cluster specified in ~/.kube/config using OLM catalog image.
	V="$(CATALOG_IMG)" $(YQ) eval '.spec.image = strenv(V)' -i config/deploy/olm/catalogsource.yaml
	$(KUSTOMIZE) build config/deploy/olm | kubectl apply -f -

.PHONY: undeploy-catalog
undeploy-catalog: $(KUSTOMIZE) ## Undeploy controller from the K8s cluster specified in ~/.kube/config using OLM catalog image.
	$(KUSTOMIZE) build config/deploy/olm | kubectl delete -f -
