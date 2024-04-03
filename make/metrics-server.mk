
##@ kubernetes metrics server
## https://github.com/kubernetes-sigs/metrics-server

.PHONY: deploy-metrics-server
deploy-metrics-server: kustomize ## Deploy Gateway API gateway
	$(KUSTOMIZE) build config/metrics-server | kubectl apply -f -
