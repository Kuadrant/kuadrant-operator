
.PHONY: deploy-observability
deploy-observability: kustomize
	$(KUSTOMIZE) build config/observability | docker run --rm -i ryane/kfilt -i kind=CustomResourceDefinition | kubectl apply --server-side -f -
	$(KUSTOMIZE) build config/observability | docker run --rm -i ryane/kfilt -x kind=CustomResourceDefinition | kubectl apply -f -
