
.PHONY: deploy-observability
deploy-observability: kustomize
	$(KUSTOMIZE) build config/observability | docker run --rm -i ryane/kfilt -i kind=CustomResourceDefinition | kubectl apply --server-side -f -
	$(KUSTOMIZE) build config/observability | docker run --rm -i ryane/kfilt -x kind=CustomResourceDefinition | kubectl apply -f -

.PHONY: thanos-manifests
thanos-manifests: ./hack/thanos/thanos_build.sh ./hack/thanos/thanos.jsonnet
	./hack/thanos/thanos_build.sh

DASHBOARD_FILES := $(wildcard examples/dashboards/*.json)

.PHONY: dashboard-cleanup
dashboard-cleanup:
	@for file in $(DASHBOARD_FILES); do \
		echo "Processing $$file"; \
		./hack/universal-dashboard.sh $$file; \
	done
