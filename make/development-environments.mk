##@ Deployment

GATEWAYAPI_PROVIDER = istio

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	# Use server side apply, otherwise will hit into this issue https://medium.com/pareture/kubectl-install-crd-failed-annotations-too-long-2ebc91b40c7d
	$(KUSTOMIZE) build config/crd | kubectl apply --server-side -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

.PHONY: install-metallb
install-metallb: SUBNET_OFFSET=1
install-metallb: kustomize yq ## Installs the metallb load balancer allowing use of an LoadBalancer type with a gateway
	$(KUSTOMIZE) build config/metallb | kubectl apply -f -
	kubectl -n metallb-system wait --for=condition=Available deployments controller --timeout=300s
	kubectl -n metallb-system wait --for=condition=ready pod --selector=app=metallb --timeout=60s
	./utils/docker-network-ipaddresspool.sh kind $(YQ) ${SUBNET_OFFSET} | kubectl apply -n metallb-system -f -

.PHONY: uninstall-metallb
uninstall-metallb: $(KUSTOMIZE)
	$(KUSTOMIZE) build config/metallb | kubectl delete -f -

.PHONY: install-olm
install-olm: $(OPERATOR_SDK)
	$(OPERATOR_SDK) olm install

.PHONY: uninstall-olm
uninstall-olm:
	$(OPERATOR_SDK) olm uninstall

deploy-dependencies: kustomize dependencies-manifests ## Deploy dependencies to the K8s cluster specified in ~/.kube/config.
	$(MAKE) namespace
	$(KUSTOMIZE) build config/dependencies | kubectl apply -f -
	kubectl -n "$(KUADRANT_NAMESPACE)" wait --timeout=300s --for=condition=Available deployments --all

deploy: manifests dependencies-manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/deploy | kubectl apply --server-side -f -
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMAGE_TAG_BASE):latest

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/deploy | kubectl delete -f -

.PHONY: namespace
namespace: ## Creates a namespace where to deploy Kuadrant Operator
	kubectl create namespace $(KUADRANT_NAMESPACE)

.PHONY: local-deploy
local-deploy: ## Deploy Kuadrant Operator from the current code
	$(MAKE) docker-build IMG=$(IMAGE_TAG_BASE):dev
	$(MAKE) kind-load-image IMG=$(IMAGE_TAG_BASE):dev

	$(MAKE) deploy IMG=$(IMAGE_TAG_BASE):dev
	kubectl -n $(KUADRANT_NAMESPACE) wait --timeout=300s --for=condition=Available deployments --all

.PHONY: env-setup
env-setup: ## Install deploy kuadrant dependencies and configured gatewayapi provider
	$(MAKE) $(GATEWAYAPI_PROVIDER)-env-setup ISTIO_INSTALL_SAIL=$(ISTIO_INSTALL_SAIL)

.PHONY: local-env-setup
local-env-setup: ## env-setup based on kind cluster
	$(MAKE) kind-delete-cluster
	$(MAKE) kind-create-cluster
	$(MAKE) env-setup GATEWAYAPI_PROVIDER=$(GATEWAYAPI_PROVIDER)

.PHONY: local-setup
local-setup: $(KIND) ## Run local Kubernetes cluster and deploy kuadrant operator (and *all* dependencies)
	$(MAKE) local-env-setup GATEWAYAPI_PROVIDER=$(GATEWAYAPI_PROVIDER)
	$(MAKE) local-deploy

.PHONY: local-cleanup
local-cleanup: ## Delete local cluster
	$(MAKE) kind-delete-cluster

##@ Development Environment: bare kubernetes

.PHONY: k8s-env-setup
k8s-env-setup: ## Install Kuadrant CRDs and dependencies.
	$(MAKE) deploy-metrics-server
	$(MAKE) install-metallb
	$(MAKE) install-cert-manager
	$(MAKE) deploy-dependencies
	$(MAKE) install

.PHONY: local-k8s-env-setup
local-k8s-env-setup: ## k8s-env-setup based on Kind cluster
	$(MAKE) kind-delete-cluster
	$(MAKE) kind-create-cluster
	$(MAKE) k8s-env-setup

##@ Development Environment: Kubernetes with GatewayAPI

.PHONY: gatewayapi-env-setup
gatewayapi-env-setup: ## Install GatewayAPI CRDs and k8s-env-setup
	$(MAKE) k8s-env-setup
	$(MAKE) gateway-api-install

.PHONY: local-k8s-env-setup
local-gatewayapi-env-setup: ## gatewayapi-env-setup based on Kind cluster
	$(MAKE) kind-delete-cluster
	$(MAKE) kind-create-cluster
	$(MAKE) gatewayapi-env-setup

##@ Development Environment: Kubernetes with GatewayAPI and Istio installed

.PHONY: istio-env-setup
istio-env-setup: ## Install Istio, istio gateway and gatewayapi-env-setup
	$(MAKE) gatewayapi-env-setup
	$(MAKE) istio-install ISTIO_INSTALL_SAIL=$(ISTIO_INSTALL_SAIL)
	$(MAKE) deploy-istio-gateway
	@echo
	@echo "Now you can open local access to the istio gateway by doing:"
	@echo "kubectl port-forward -n istio-system service/istio-ingressgateway-istio 9080:80 &"
	@echo "export GATEWAY_URL=localhost:9080"
	@echo "after that, you can curl -H \"Host: myhost.com\" \$$GATEWAY_URL"
	@echo "-- Linux only -- Ingress gateway is exported using loadbalancer service in port 80"
	@echo "export INGRESS_HOST=\$$(kubectl get gtw istio-ingressgateway -n istio-system -o jsonpath='{.status.addresses[0].value}')"
	@echo "export INGRESS_PORT=\$$(kubectl get gtw istio-ingressgateway -n istio-system -o jsonpath='{.spec.listeners[?(@.name==\"http\")].port}')"
	@echo "export GATEWAY_URL=\$$INGRESS_HOST:\$$INGRESS_PORT"
	@echo "curl -H \"Host: myhost.com\" \$$GATEWAY_URL"
	@echo
