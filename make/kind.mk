
##@ Kind

## Targets to help install and use kind for development https://kind.sigs.k8s.io

KIND = $(shell pwd)/bin/kind
kind: ## Download kind locally if necessary.
	$(call go-get-tool,$(KIND),sigs.k8s.io/kind@v0.11.1)

KIND_CLUSTER_NAME = kuadrant-local

.PHONY: kind-create-cluster
kind-create-cluster: kind ## Create the "kuadrant-local" kind cluster.
	$(KIND) create cluster --name $(KIND_CLUSTER_NAME) --config utils/kind-cluster.yaml

.PHONY: kind-delete-cluster
kind-delete-cluster: kind ## Delete the "kuadrant-local" kind cluster.
	- $(KIND) delete cluster --name $(KIND_CLUSTER_NAME)

.PHONY: kind-create-kuadrant-cluster
kind-create-kuadrant-cluster: export IMG := quay.io/kuadrant/kuadrant-operator:dev
kind-create-kuadrant-cluster: ## Create a kind cluster with kuadrant deployed.
	$(MAKE) kind-delete-cluster
	$(MAKE) kind-create-cluster
	$(MAKE) gateway-api-install
	$(MAKE) istio-install
	$(MAKE) deploy-gateway
	$(MAKE) docker-build
	$(KIND) load docker-image $(IMG) --name $(KIND_CLUSTER_NAME)
	$(MAKE) install
	$(MAKE) deploy
	kubectl -n kuadrant-system wait --timeout=300s --for=condition=Available deployments --all
