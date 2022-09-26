##@ Kind

## Targets to help install and use kind for development https://kind.sigs.k8s.io

KIND = $(shell pwd)/bin/kind
kind: ## Download kind locally if necessary.
	$(call go-get-tool,$(KIND),sigs.k8s.io/kind@v0.11.1)

.PHONY: kind-create-cluster
kind-create-cluster: kind ## Create the kind cluster.
	$(KIND) create cluster --name $(KIND_CLUSTER_NAME) --config utils/local-deployment/kind-cluster.yaml

.PHONY: kind-delete-cluster
kind-delete-cluster: kind ## Delete the "kuadrant-local" kind cluster.
	- $(KIND) delete cluster --name $(KIND_CLUSTER_NAME)
