
.PHONY: deploy-observability
deploy-observability: kustomize
	$(KUSTOMIZE) build config/observability | docker run --rm -i docker.io/ryane/kfilt -i kind=CustomResourceDefinition | kubectl apply --server-side -f -
	$(KUSTOMIZE) build config/observability | docker run --rm -i docker.io/ryane/kfilt -x kind=CustomResourceDefinition | kubectl apply -f -

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

##@ Jaeger Tracing

JAEGER_NAMESPACE ?= observability
JAEGER_RELEASE_NAME ?= jaeger

.PHONY: install-jaeger
install-jaeger: helm ## Install Jaeger v2 using Helm for distributed tracing
	@echo "Installing Jaeger v2 in namespace $(JAEGER_NAMESPACE)..."
	kubectl create namespace $(JAEGER_NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	$(HELM) repo add jaegertracing https://jaegertracing.github.io/helm-charts || true
	$(HELM) repo update jaegertracing
	$(HELM) upgrade --install $(JAEGER_RELEASE_NAME) jaegertracing/jaeger \
		--namespace $(JAEGER_NAMESPACE) \
		--wait
	@echo "✅ Jaeger v2 installed successfully!"
	@echo "   Service: $(JAEGER_RELEASE_NAME).$(JAEGER_NAMESPACE).svc.cluster.local:4317 (OTLP gRPC)"
	@echo "   Query UI: make jaeger-port-forward (then open http://localhost:16686)"

.PHONY: uninstall-jaeger
uninstall-jaeger: helm ## Uninstall Jaeger from the cluster
	@echo "Uninstalling Jaeger..."
	$(HELM) uninstall $(JAEGER_RELEASE_NAME) -n $(JAEGER_NAMESPACE) || true
	@echo "✅ Jaeger uninstalled"

.PHONY: jaeger-port-forward
jaeger-port-forward: ## Port-forward to Jaeger Query UI (http://localhost:16686)
	@echo "Port-forwarding to Jaeger UI at http://localhost:16686"
	@echo "Press Ctrl+C to stop"
	kubectl port-forward -n $(JAEGER_NAMESPACE) svc/$(JAEGER_RELEASE_NAME) 16686:16686

.PHONY: deploy-tracing
deploy-tracing: kustomize ## Apply tracing configurations for Istio and Kuadrant components
	@echo "Deploying tracing configurations via kustomize..."
	$(KUSTOMIZE) build config/observability/tracing | kubectl apply -f -
	@echo "✅ Tracing configurations applied"
	@echo "   Note: Operator patches require manual kubectl patch (see config/observability/tracing/README.md)"
	@echo "   Open Jaeger UI: make jaeger-port-forward"
