##@ Cert Manager resources

.POHNY: install-cert-manager
install-cert-manager: kustomize ## Install Certificate Manager
	$(KUSTOMIZE) build config/dependencies/cert-manager | kubectl apply -f -
	kubectl -n cert-manager wait --timeout=300s --for=condition=Available deployments --all

