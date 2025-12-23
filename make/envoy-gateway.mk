
##@ Envoy Gateway

## Targets to help install and configure EG

EG_CONFIG_DIR = config/dependencies/envoy-gateway
EG_NAMESPACE = envoy-gateway-system

EG_VERSION ?= v1.2.6

# egctl tool
EGCTL ?= $(LOCALBIN)/egctl
EGCTL_VERSION ?= $(EG_VERSION)
EGCTL_V_BINARY := $(LOCALBIN)/egctl-$(EGCTL_VERSION)

.PHONY: egctl
egctl: $(EGCTL_V_BINARY) ## Download egctl locally if necessary.
$(EGCTL_V_BINARY): $(LOCALBIN)
	$(call go-install-tool,$(EGCTL),github.com/envoyproxy/gateway/cmd/egctl,$(EGCTL_VERSION))

envoy-gateway-enable-envoypatchpolicy: yq
	$(eval TMP := $(shell mktemp -d))
	kubectl get configmap -n $(EG_NAMESPACE) envoy-gateway-config -o jsonpath='{.data.envoy-gateway\.yaml}' > $(TMP)/envoy-gateway.yaml
	yq e '.extensionApis.enableEnvoyPatchPolicy = true' -i $(TMP)/envoy-gateway.yaml
	kubectl create configmap -n $(EG_NAMESPACE) envoy-gateway-config --from-file=envoy-gateway.yaml=$(TMP)/envoy-gateway.yaml -o yaml --dry-run=client | kubectl replace -f -
	-rm -rf $(TMP)
	kubectl rollout restart deployment envoy-gateway -n $(EG_NAMESPACE)

.PHONY: envoy-gateway-install
envoy-gateway-install: kustomize helm
	$(HELM) install eg oci://docker.io/envoyproxy/gateway-helm --version $(EG_VERSION) -n $(EG_NAMESPACE) --create-namespace
	$(MAKE) envoy-gateway-enable-envoypatchpolicy
	kubectl wait --timeout=5m -n $(EG_NAMESPACE) deployment/envoy-gateway --for=condition=Available

.PHONY: deploy-eg-gateway
deploy-eg-gateway: kustomize ## Deploy Gateway API gateway
	$(KUSTOMIZE) build $(EG_CONFIG_DIR)/gateway | kubectl apply -f -
	kubectl wait --timeout=5m -n gateway-system gateway/kuadrant-ingressgateway --for=condition=Programmed
	@echo
	@echo "-- Linux only -- Ingress gateway is exported using loadbalancer service in port 80"
	@echo "export INGRESS_HOST=\$$(kubectl get gtw kuadrant-ingressgateway -n gateway-system-o jsonpath='{.status.addresses[0].value}')"
	@echo "export INGRESS_PORT=\$$(kubectl get gtw kuadrant-ingressgateway -n gateway-system -o jsonpath='{.spec.listeners[?(@.name==\"http\")].port}')"
	@echo "Now you can hit the gateway:"
	@echo "curl --verbose --resolve www.example.com:\$${INGRESS_PORT}:\$${INGRESS_HOST} http://www.example.com:\$${INGRESS_PORT}/get"
