
##@ Kustomize Overlay Generation

## Targets to help create deployment kustomizations (overlays)

CLUSTER_NAME ?= $(KIND_CLUSTER_NAME)

## Location to generate cluster overlays
CLUSTER_OVERLAY_DIR ?= $(shell pwd)/tmp/overlays
$(CLUSTER_OVERLAY_DIR):
	mkdir -p $(CLUSTER_OVERLAY_DIR)

USE_REMOTE_CONFIG ?= false
KUADRANT_OPERATOR_GITREF ?= main

config_path_for = $(shell if [ $(USE_REMOTE_CONFIG) = 'true' ]; then echo "github.com/kuadrant/kuadrant-operator/$(1)?ref=$(KUADRANT_OPERATOR_GITREF)"; else realpath -m --relative-to=$(2) $(shell pwd)/$(1); fi)

.PHONY: generate-cluster-overlay
generate-cluster-overlay: remove-cluster-overlay ## Generate a cluster overlay with deployment for the current cluster (CLUSTER_NAME)
	$(MAKE) -s generate-operator-deployment-overlay

.PHONY: remove-cluster-overlay
remove-cluster-overlay: ## Remove an existing cluster overlay for the current cluster (CLUSTER_NAME)
	rm -rf $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)

.PHONY: remove-all-cluster-overlays
remove-all-cluster-overlays: ## Remove all existing cluster overlays (kuadrant-dns-local*)
	rm -rf $(CLUSTER_OVERLAY_DIR)/kuadrant-local*

.PHONY: generate-operator-deployment-overlay
generate-operator-deployment-overlay: ## Generate a Kuadrant Operator deployment overlay for the current cluster (CLUSTER_NAME)
	# Generate cluster overlay with namespace resources
	mkdir -p $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)

	# Add dependencies and kuadrant components
	cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME) && \
	touch kustomization.yaml && \
	$(KUSTOMIZE) edit add resource $(call config_path_for,"config/dependencies/cert-manager",$(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)) && \
	$(KUSTOMIZE) edit add resource $(call config_path_for,"config/dependencies/gateway-api",$(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)) && \
	$(KUSTOMIZE) edit add resource $(call config_path_for,"config/dependencies/istio/sail/operator",$(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)) && \
	$(KUSTOMIZE) edit add resource $(call config_path_for,"config/dependencies/istio/sail",$(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)) && \
	$(KUSTOMIZE) edit add resource $(call config_path_for,"config/dependencies/istio/gateway",$(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)) && \
	$(KUSTOMIZE) edit add resource $(call config_path_for,"config/dependencies",$(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)) && \
	$(KUSTOMIZE) edit add resource $(call config_path_for,"config/default",$(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME))

	#Check it compiles
	$(KUSTOMIZE) build --enable-helm $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME) &> /dev/null
