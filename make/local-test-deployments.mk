##@ Local Testing Deployments

# Override these variables to customize your local testing
# LOCAL_TEST_ORG is required - must be set via environment variable or make argument
LOCAL_TEST_ORG ?=
LOCAL_TEST_IMG ?= quay.io/$(LOCAL_TEST_ORG)/kuadrant-operator:latest
LOCAL_TEST_BUNDLE_IMG ?= quay.io/$(LOCAL_TEST_ORG)/kuadrant-operator-bundle:latest
LOCAL_TEST_CATALOG_IMG ?= quay.io/$(LOCAL_TEST_ORG)/kuadrant-operator-catalog:latest
LOCAL_TEST_CHANNEL ?= preview
LOCAL_TEST_DEFAULT_CHANNEL ?= preview
LOCAL_TEST_NAMESPACE ?=
# Operand versions for Helm chart dependencies
# Set to "latest" to automatically fetch the latest released version from GitHub
# Or set to a specific version like "0.15.0"
LOCAL_TEST_LIMITADOR_VERSION ?= latest
LOCAL_TEST_AUTHORINO_VERSION ?= latest
LOCAL_TEST_DNS_VERSION ?= latest

# Helper function to fetch latest version from GitHub
# Usage: $(call get_latest_version,owner,repo)
define get_latest_version
$(shell curl -s https://api.github.com/repos/$(1)/$(2)/releases/latest | grep '"tag_name":' | sed -E 's/.*"v([^"]+)".*/\1/')
endef

# Resolve version - if set to "latest", fetch from GitHub, otherwise use specified version
ifeq (latest,$(LOCAL_TEST_LIMITADOR_VERSION))
RESOLVED_LIMITADOR_VERSION := $(call get_latest_version,Kuadrant,limitador-operator)
else
RESOLVED_LIMITADOR_VERSION := $(LOCAL_TEST_LIMITADOR_VERSION)
endif

ifeq (latest,$(LOCAL_TEST_AUTHORINO_VERSION))
RESOLVED_AUTHORINO_VERSION := $(call get_latest_version,Kuadrant,authorino-operator)
else
RESOLVED_AUTHORINO_VERSION := $(LOCAL_TEST_AUTHORINO_VERSION)
endif

ifeq (latest,$(LOCAL_TEST_DNS_VERSION))
RESOLVED_DNS_VERSION := $(call get_latest_version,Kuadrant,dns-operator)
else
RESOLVED_DNS_VERSION := $(LOCAL_TEST_DNS_VERSION)
endif

.PHONY: local-test-olm
local-test-olm: ## Full OLM deployment test: create cluster, deploy prerequisites, build/push images, build/push bundle, build/push catalog, deploy via OLM
ifndef LOCAL_TEST_ORG
	$(error LOCAL_TEST_ORG is not set. Please set it via: make local-test-olm LOCAL_TEST_ORG=yourorg)
endif
	@echo "🚀 Starting full OLM deployment test..."
	@echo "1️⃣  Creating local Kind cluster..."
	$(MAKE) kind-delete-cluster
	$(MAKE) kind-create-cluster
	@echo "2️⃣  Installing prerequisites (Gateway API, cert-manager, Istio, Observability CRDs)..."
	$(MAKE) gateway-api-install
	$(MAKE) install-cert-manager
	$(MAKE) install-observability-crds
	$(MAKE) istio-install
	$(MAKE) install-metallb
	@echo "3️⃣  Installing OLM..."
	$(MAKE) install-olm
	@echo "4️⃣  Building and pushing operator image..."
	$(MAKE) docker-build IMG=$(LOCAL_TEST_IMG)
	$(MAKE) docker-push IMG=$(LOCAL_TEST_IMG)
	@echo "5️⃣  Building and pushing bundle..."
	$(MAKE) bundle IMG=$(LOCAL_TEST_IMG) CHANNELS=$(LOCAL_TEST_CHANNEL) DEFAULT_CHANNEL=$(LOCAL_TEST_DEFAULT_CHANNEL)
	$(MAKE) bundle-build BUNDLE_IMG=$(LOCAL_TEST_BUNDLE_IMG)
	$(MAKE) bundle-push BUNDLE_IMG=$(LOCAL_TEST_BUNDLE_IMG)
	@echo "6️⃣  Building and pushing catalog..."
	$(MAKE) catalog BUNDLE_IMG=$(LOCAL_TEST_BUNDLE_IMG) CHANNEL=$(LOCAL_TEST_CHANNEL)
	$(MAKE) catalog-build CATALOG_IMG=$(LOCAL_TEST_CATALOG_IMG)
	$(MAKE) catalog-push CATALOG_IMG=$(LOCAL_TEST_CATALOG_IMG)
	@echo "7️⃣  Deploying via OLM catalog..."
ifdef LOCAL_TEST_NAMESPACE
	@echo "Customizing namespace to: $(LOCAL_TEST_NAMESPACE)"
	V="$(LOCAL_TEST_NAMESPACE)" $(YQ) eval '.namespace = strenv(V)' -i config/deploy/olm/kustomization.yaml
	V="$(LOCAL_TEST_NAMESPACE)" $(YQ) eval '.spec.sourceNamespace = strenv(V)' -i config/deploy/olm/subscription.yaml
endif
ifdef LOCAL_TEST_CHANNEL
	@echo "Customizing channel to: $(LOCAL_TEST_CHANNEL)"
	V="$(LOCAL_TEST_CHANNEL)" $(YQ) eval '.spec.channel = strenv(V)' -i config/deploy/olm/subscription.yaml
endif
	$(MAKE) deploy-catalog CATALOG_IMG=$(LOCAL_TEST_CATALOG_IMG)
	@echo "✅ OLM deployment complete!"
	@echo ""
	@echo "To verify deployment readiness, run:"
ifdef LOCAL_TEST_NAMESPACE
	@echo "  kubectl wait --for=jsonpath='{.status.state}'=AtLatestKnown subscription/kuadrant -n $(LOCAL_TEST_NAMESPACE) --timeout=600s"
	@echo "  kubectl wait --for=jsonpath='{.status.phase}'=Succeeded csv -l operators.coreos.com/kuadrant-operator.$(LOCAL_TEST_NAMESPACE) -n $(LOCAL_TEST_NAMESPACE) --timeout=300s"
else
	@echo "  kubectl wait --for=jsonpath='{.status.state}'=AtLatestKnown subscription/kuadrant -n kuadrant-system --timeout=600s"
	@echo "  kubectl wait --for=jsonpath='{.status.phase}'=Succeeded csv -l operators.coreos.com/kuadrant-operator.kuadrant-system -n kuadrant-system --timeout=300s"
endif

.PHONY: local-test-helm
local-test-helm: ## Full Helm deployment test: create cluster, deploy prerequisites, build/push image, build helm chart, deploy via Helm
ifndef LOCAL_TEST_ORG
	$(error LOCAL_TEST_ORG is not set. Please set it via: make local-test-helm LOCAL_TEST_ORG=yourorg)
endif
	@echo "🚀 Starting full Helm deployment test..."
	@echo "1️⃣  Creating local Kind cluster..."
	$(MAKE) kind-delete-cluster
	$(MAKE) kind-create-cluster
	@echo "2️⃣  Installing prerequisites (Gateway API, cert-manager, Istio, Observability CRDs)..."
	$(MAKE) gateway-api-install
	$(MAKE) install-cert-manager
	$(MAKE) install-observability-crds
	$(MAKE) istio-install
	$(MAKE) install-metallb
	@echo "3️⃣  Building and pushing operator image..."
	$(MAKE) docker-build IMG=$(LOCAL_TEST_IMG)
	$(MAKE) docker-push IMG=$(LOCAL_TEST_IMG)
	@echo "4️⃣  Building Helm chart..."
	@echo "   Using Limitador version: $(RESOLVED_LIMITADOR_VERSION)"
	@echo "   Using Authorino version: $(RESOLVED_AUTHORINO_VERSION)"
	@echo "   Using DNS Operator version: $(RESOLVED_DNS_VERSION)"
	$(MAKE) helm-build IMG=$(LOCAL_TEST_IMG) VERSION=latest \
		LIMITADOR_OPERATOR_VERSION=$(RESOLVED_LIMITADOR_VERSION) \
		AUTHORINO_OPERATOR_VERSION=$(RESOLVED_AUTHORINO_VERSION) \
		DNS_OPERATOR_VERSION=$(RESOLVED_DNS_VERSION)
	@echo "5️⃣  Packaging Helm chart..."
	$(MAKE) helm-package
	@echo "6️⃣  Installing Helm chart..."
ifdef LOCAL_TEST_NAMESPACE
	$(HELM) install $(CHART_NAME) $(CHART_DIRECTORY) -n $(LOCAL_TEST_NAMESPACE) --create-namespace
else
	$(HELM) install $(CHART_NAME) $(CHART_DIRECTORY) -n kuadrant-system --create-namespace
endif
	@echo "✅ Helm deployment complete!"

.PHONY: show-local-test-versions
show-local-test-versions: ## Display the resolved versions for local testing
	@echo "Operand versions for local testing:"
	@echo "  Limitador Operator: $(RESOLVED_LIMITADOR_VERSION)"
	@echo "  Authorino Operator: $(RESOLVED_AUTHORINO_VERSION)"
	@echo "  DNS Operator:       $(RESOLVED_DNS_VERSION)"
