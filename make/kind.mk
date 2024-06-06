
##@ Kind

## Targets to help install and use kind for development https://kind.sigs.k8s.io

KIND_CLUSTER_NAME ?= kuadrant-local

.PHONY: kind-create-cluster
kind-create-cluster: kind ## Create the "kuadrant-local" kind cluster.
	KIND_EXPERIMENTAL_PROVIDER=$(CONTAINER_ENGINE) $(KIND) create cluster --name $(KIND_CLUSTER_NAME) --config utils/kind-cluster.yaml

.PHONY: kind-delete-cluster
kind-delete-cluster: kind ## Delete the "kuadrant-local" kind cluster.
	- KIND_EXPERIMENTAL_PROVIDER=$(CONTAINER_ENGINE) $(KIND) delete cluster --name $(KIND_CLUSTER_NAME)
