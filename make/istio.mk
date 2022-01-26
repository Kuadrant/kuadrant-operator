
##@ Istio

## Targets to help install and configure istio

ISTIO_PATCHES_DIR = config/dependencies/istio/patches
ISTIO_NAMESPACE = istio-system
ISTIO_INSTALL_OPTIONS ?= --set profile=default \
	--set values.gateways.istio-ingressgateway.autoscaleEnabled=false \
	--set values.pilot.autoscaleEnabled=false \
	--set values.global.istioNamespace=$(ISTIO_NAMESPACE)

# istioctl tool
ISTIOCTL=$(shell pwd)/bin/istioctl
ISTIOVERSION = 1.12.1
$(ISTIOCTL):
	mkdir -p $(PROJECT_PATH)/bin
	$(eval TMP := $(shell mktemp -d))
	cd $(TMP); curl -sSL https://istio.io/downloadIstio | ISTIO_VERSION=$(ISTIOVERSION) sh -
	cp $(TMP)/istio-$(ISTIOVERSION)/bin/istioctl ${ISTIOCTL}
	-rm -rf $(TMP)

.PHONY: istioctl
istioctl: $(ISTIOCTL) ## Download istioctl locally if necessary.

.PHONY: istio-install
istio-install: istioctl ## Install istio.
	$(ISTIOCTL) install -y $(ISTIO_INSTALL_OPTIONS)

#Note: This target is here temporarily to aid dev/test of the operator. Eventually it will be the responsibility of the
# operator itself to configure istio as part of the reconciliation of a kuadrant CR.
.PHONY: istio-install-with-patch
istio-install-with-patch: istioctl ## Install istio with patch to add authorino auth extension.
	$(ISTIOCTL) install -y $(ISTIO_INSTALL_OPTIONS) -f $(ISTIO_PATCHES_DIR)/istio-externalProvider.yaml

.PHONY: istio-uninstall
istio-uninstall: istioctl ## Uninstall istio.
	$(ISTIOCTL) x uninstall -y --purge

.PHONY: istio-verify-install
istio-verify-install: istioctl ## Verify istio installation.
	$(ISTIOCTL) verify-install -i $(ISTIO_NAMESPACE)
