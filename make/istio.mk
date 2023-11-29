
##@ Istio

## Targets to help install and configure istio

ISTIO_INSTALL_DIR = config/dependencies/istio
ISTIO_NAMESPACE = istio-system

# istioctl tool
ISTIOCTL=$(shell pwd)/bin/istioctl
ISTIOVERSION = 1.20.0
$(ISTIOCTL):
	mkdir -p $(shell pwd)/bin
	$(eval TMP := $(shell mktemp -d))
	cd $(TMP); curl -sSL https://istio.io/downloadIstio | ISTIO_VERSION=$(ISTIOVERSION) sh -
	cp $(TMP)/istio-$(ISTIOVERSION)/bin/istioctl ${ISTIOCTL}
	-rm -rf $(TMP)

.PHONY: istioctl
istioctl: $(ISTIOCTL) ## Download istioctl locally if necessary.

.PHONY: istio-install
istio-install: istioctl ## Install istio.
	$(ISTIOCTL) operator init
	kubectl apply -f $(ISTIO_INSTALL_DIR)/istio-operator.yaml

.PHONY: istio-uninstall
istio-uninstall: istioctl ## Uninstall istio.
	$(ISTIOCTL) x uninstall -y --purge

.PHONY: istio-verify-install
istio-verify-install: istioctl ## Verify istio installation.
	$(ISTIOCTL) verify-install -i $(ISTIO_NAMESPACE)

.PHONY: sail-install
sail-install: kustomize
	$(KUSTOMIZE) build $(ISTIO_INSTALL_DIR)/sail | kubectl apply -f -
	kubectl -n istio-system wait --for=condition=Available deployment istio-operator --timeout=300s
	kubectl apply -f $(ISTIO_INSTALL_DIR)/sail/istio.yaml

.PHONY: sail-uninstall
sail-uninstall: kustomize
	kubectl delete -f $(ISTIO_INSTALL_DIR)/sail/istio.yaml
	$(KUSTOMIZE) build $(ISTIO_INSTALL_DIR)/sail | kubectl delete -f -
