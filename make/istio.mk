
##@ Istio

## Targets to help install and configure istio

ISTIO_INSTALL_DIR = config/dependencies/istio
ISTIO_NAMESPACE = istio-system
## installs project sail vs istioctl install
ISTIO_INSTALL_SAIL ?= true
ifeq (false,$(ISTIO_INSTALL_SAIL))
INSTALL_COMMAND=istioctl-install
else
INSTALL_COMMAND=sail-install
endif

# istioctl tool
ISTIOCTL ?= $(LOCALBIN)/istioctl
ISTIOCTL_VERSION ?= 1.27.1
ISTIOCTL_V_BINARY := $(LOCALBIN)/istioctl-$(ISTIOCTL_VERSION)

.PHONY: istioctl
istioctl: $(ISTIOCTL_V_BINARY) ## Download istioctl locally if necessary.
$(ISTIOCTL_V_BINARY): $(LOCALBIN)
	$(call go-install-tool,$(ISTIOCTL),istio.io/istio/istioctl/cmd/istioctl,$(ISTIOCTL_VERSION))

.PHONY: istioctl-install
istioctl-install: istioctl ## Install istio.
	$(ISTIOCTL) install -f $(ISTIO_INSTALL_DIR)/istio-operator.yaml -y

.PHONY: istioctl-uninstall
istioctl-uninstall: istioctl ## Uninstall istio.
	$(ISTIOCTL) uninstall -y --purge

SAIL_VERSION = 1.27.1
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
deploy-istio-gateway: kustomize ## Deploy Gateway API gateway with gatewayclass set to Istio
	$(KUSTOMIZE) build config/dependencies/istio/gateway | kubectl apply -f -
