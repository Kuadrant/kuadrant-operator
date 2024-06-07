
##@ Envoy Gateway

## Targets to help install and configure EG

EG_CONFIG_DIR = config/dependencies/envoy-gateway
EG_NAMESPACE = envoy-gateway-system

# egctl tool
EGCTL=$(PROJECT_PATH)/bin/egctl
EGCTL_VERSION ?= v1.0.0

ifeq ($(ARCH),x86_64)
	EG_ARCH = amd64
endif
ifeq ($(ARCH),aarch64)
	EG_ARCH = arm64
endif
ifneq ($(filter armv5%,$(ARCH)),)
	EG_ARCH = armv5
endif
ifneq ($(filter armv6%,$(ARCH)),)
	EG_ARCH = armv6
endif
ifneq ($(filter armv7%,$(ARCH)),)
	EG_ARCH = arm
endif

$(EGCTL):
	mkdir -p $(PROJECT_PATH)/bin
	## get-egctl.sh requires sudo and does not allow installing in a custom location. Fails if not in the PATH as well
	# curl -sSL https://gateway.envoyproxy.io/get-egctl.sh | EGCTL_INSTALL_DIR=$(PROJECT_PATH)/bin  VERSION=$(EGCTL_VERSION) bash
	$(eval TMP := $(shell mktemp -d))
	cd $(TMP); curl -sSL https://github.com/envoyproxy/gateway/releases/download/$(EGCTL_VERSION)/egctl_$(EGCTL_VERSION)_$(OS)_$(EG_ARCH).tar.gz -o egctl.tar.gz
	tar xf $(TMP)/egctl.tar.gz -C $(TMP)
	cp $(TMP)/bin/$(OS)/$(EG_ARCH)/egctl $(EGCTL)
	-rm -rf $(TMP)

.PHONY: egctl
egctl: $(EGCTL) ## Download egctl locally if necessary.

EG_VERSION ?= v1.0.1
.PHONY: envoy-gateway-install
envoy-gateway-install: kustomize $(HELM)
	$(HELM) install eg oci://docker.io/envoyproxy/gateway-helm --version $(EG_VERSION) -n envoy-gateway-system --create-namespace
	kubectl wait --timeout=5m -n envoy-gateway-system deployment/envoy-gateway --for=condition=Available

.PHONY: deploy-eg-gateway
deploy-eg-gateway: kustomize ## Deploy Gateway API gateway
	$(KUSTOMIZE) build $(EG_CONFIG_DIR)/gateway | kubectl apply -f -
	kubectl wait --timeout=5m -n envoy-gateway-system gateway/eg --for=condition=Programmed
	@echo
	@echo "-- Linux only -- Ingress gateway is exported using loadbalancer service in port 80"
	@echo "export INGRESS_HOST=\$$(kubectl get gtw eg -n envoy-gateway-system -o jsonpath='{.status.addresses[0].value}')"
	@echo "export INGRESS_PORT=\$$(kubectl get gtw eg -n envoy-gateway-system -o jsonpath='{.spec.listeners[?(@.name==\"http\")].port}')"
	@echo "Now you can hit the gateway:"
	@echo "curl --verbose --resolve www.example.com:\$${INGRESS_PORT}:\$${INGRESS_HOST} http://www.example.com:\$${INGRESS_PORT}/get"
