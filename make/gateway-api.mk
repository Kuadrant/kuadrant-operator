
##@ Gateway API resources

.PHONY: deploy-gateway
deploy-gateway: kustomize ## Deploy Gateway API gateway
	$(KUSTOMIZE) build config/dependencies/gateway-api/gateway | kubectl apply -f -

.PHONY: gateway-api-install
gateway-api-install: kustomize ## Install Gateway API CRDs
	$(KUSTOMIZE) build config/dependencies/gateway-api | kubectl apply -f -
