
##@ Istio

## Targets to help install and configure istio

ISTIO_INSTALL_DIR = config/dependencies/istio
ISTIO_NAMESPACE = istio-system
## installs project sail vs istioctl install
ISTIO_INSTALL_SAIL ?= false
ifeq (true,$(ISTIO_INSTALL_SAIL))
INSTALL_COMMAND=sail-install
else
INSTALL_COMMAND=istioctl-install
endif

# istioctl tool
ISTIOCTL=$(shell pwd)/bin/istioctl
ISTIOVERSION = 1.22.5
$(ISTIOCTL):
	mkdir -p $(shell pwd)/bin
	$(eval TMP := $(shell mktemp -d))
	cd $(TMP); curl -sSL https://istio.io/downloadIstio | ISTIO_VERSION=$(ISTIOVERSION) sh -
	cp $(TMP)/istio-$(ISTIOVERSION)/bin/istioctl ${ISTIOCTL}
	-rm -rf $(TMP)

.PHONY: istioctl
istioctl: $(ISTIOCTL) ## Download istioctl locally if necessary.

.PHONY: istioctl-install
istioctl-install: istioctl ## Install istio.
	$(ISTIOCTL) operator init --operatorNamespace $(ISTIO_NAMESPACE) --watchedNamespaces $(ISTIO_NAMESPACE)
	kubectl apply -f $(ISTIO_INSTALL_DIR)/istio-operator.yaml

.PHONY: istioctl-uninstall
istioctl-uninstall: istioctl ## Uninstall istio.
	$(ISTIOCTL) x uninstall -y --purge

.PHONY: istioctl-verify-install
istioctl-verify-install: istioctl ## Verify istio installation.
	$(ISTIOCTL) verify-install -i $(ISTIO_NAMESPACE)

SAIL_VERSION = 0.1.0
.PHONY: sail-install
sail-install: helm
	$(HELM) install sail-operator \
		--create-namespace \
		--namespace $(ISTIO_NAMESPACE) \
		--wait \
		--timeout=300s \
		https://github.com/istio-ecosystem/sail-operator/releases/download/$(SAIL_VERSION)/sail-operator-$(SAIL_VERSION).tgz
	kubectl apply -f $(ISTIO_INSTALL_DIR)/sail/istio.yaml

.PHONY: sail-uninstall
sail-uninstall: helm
	kubectl delete -f $(ISTIO_INSTALL_DIR)/sail/istio.yaml
	$(HELM) uninstall sail-operator

.PHONY: istio-install
istio-install:
	$(MAKE) $(INSTALL_COMMAND)

.PHONY: deploy-istio-gateway
deploy-istio-gateway: $(KUSTOMIZE) ## Deploy Gateway API gateway with gatewayclass set to Istio
	$(KUSTOMIZE) build config/dependencies/istio/gateway | kubectl apply -f -
