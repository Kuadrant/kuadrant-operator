
##@ CRC (OpenShift Local)

## Targets to deploy Kuadrant on a CRC cluster for development and network policy testing.
## Requires: crc installed and running (`./hack/start-crc.sh`), `eval $(crc oc-env)`.

CRC_REGISTRY ?= default-route-openshift-image-registry.apps-crc.testing
CRC_IMG ?= $(CRC_REGISTRY)/$(KUADRANT_NAMESPACE)/kuadrant-operator:dev

.PHONY: crc-check
crc-check: ## Verify CRC is running and reachable
	@command -v crc > /dev/null 2>&1 || { echo "[ERROR] crc not found. Install from: https://console.redhat.com/openshift/create/local"; exit 1; }
	@crc status 2>/dev/null | grep -q "Running" || { echo "[ERROR] CRC is not running. Start it with: ./hack/start-crc.sh"; exit 1; }
	@oc whoami > /dev/null 2>&1 || { echo "[ERROR] Cannot reach CRC. Run: eval \$$(crc oc-env)"; exit 1; }
	@echo "[INFO] CRC is running, logged in as $$(oc whoami)"

.PHONY: crc-registry-login
crc-registry-login: crc-check ## Log in to the CRC internal registry
	@oc registry login --insecure=true 2>/dev/null || true
	@$(CONTAINER_ENGINE) login --tls-verify=false -u $$(oc whoami) -p $$(oc whoami -t) $(CRC_REGISTRY)

TOYSTORE_NAMESPACE ?= toystore

.PHONY: crc-setup
crc-setup: ## Deploy Kuadrant operator, dependencies, observability, and sample app on CRC
	$(MAKE) crc-env-setup GATEWAYAPI_PROVIDER=$(GATEWAYAPI_PROVIDER)
	$(MAKE) crc-network-policies
	$(MAKE) crc-deploy
	$(MAKE) crc-deploy-observability
	$(MAKE) crc-deploy-sample

.PHONY: crc-network-policies
crc-network-policies: ## Apply network policices to lock down pod to pod connections
	kubectl apply -k ./config/network-policy


.PHONY: crc-env-setup
crc-env-setup: ## Install dependencies and gateway provider on CRC
	$(MAKE) crc-check
	$(MAKE) crc-$(GATEWAYAPI_PROVIDER)-env-setup ISTIO_INSTALL_SAIL=$(ISTIO_INSTALL_SAIL)

.PHONY: crc-k8s-env-setup
crc-k8s-env-setup: kustomize dependencies-manifests ## Install Kuadrant CRDs and dependencies on CRC (no MetalLB, no metrics-server, no observability CRDs)
	kubectl create namespace $(KUADRANT_NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	$(MAKE) install-cert-manager
	$(KUSTOMIZE) build config/dependencies | kubectl apply --server-side -f -
	kubectl -n "$(KUADRANT_NAMESPACE)" wait --timeout=300s --for=condition=Available deployments --all
	$(MAKE) install

.PHONY: crc-gatewayapi-env-setup
crc-gatewayapi-env-setup: ## Install GatewayAPI CRDs and crc-k8s-env-setup (skips CRD install — OpenShift manages them)
	$(MAKE) crc-k8s-env-setup

.PHONY: crc-istio-env-setup
crc-istio-env-setup: ## Install Istio, gateway, and crc-gatewayapi-env-setup
	$(MAKE) crc-gatewayapi-env-setup
	@if $(HELM) status sail-operator -n $(ISTIO_NAMESPACE) > /dev/null 2>&1; then \
		echo "[INFO] Sail operator already installed, skipping"; \
	else \
		$(MAKE) istio-install ISTIO_INSTALL_SAIL=$(ISTIO_INSTALL_SAIL); \
	fi
	$(MAKE) deploy-istio-gateway
	kubectl annotate gateway kuadrant-ingressgateway -n gateway-system \
		networking.istio.io/service-type=ClusterIP --overwrite

.PHONY: crc-envoygateway-env-setup
crc-envoygateway-env-setup: ## Install Envoy Gateway and crc-k8s-env-setup
	$(MAKE) crc-k8s-env-setup
	@if $(HELM) status eg -n $(EG_NAMESPACE) > /dev/null 2>&1; then \
		echo "[INFO] Envoy Gateway already installed, skipping"; \
	else \
		$(MAKE) envoy-gateway-install; \
	fi
	$(MAKE) deploy-eg-gateway

.PHONY: crc-deploy
crc-deploy: ## Build, push, and deploy the operator on CRC
	$(MAKE) docker-build IMG=$(CRC_IMG)
	$(MAKE) crc-registry-login
	$(MAKE) crc-push IMG=$(CRC_IMG)
	$(MAKE) deploy IMG=$(CRC_IMG)
	kubectl -n $(KUADRANT_NAMESPACE) wait --timeout=300s --for=condition=Available deployments --all
	kubectl apply -f config/install/configure/standard/kuadrant.yaml
	kubectl -n $(KUADRANT_NAMESPACE) wait --timeout=300s --for=condition=Ready kuadrant/kuadrant
	@echo "[INFO] Enabling Kuadrant console plugin..."
	@if oc get consoles.operator.openshift.io cluster -o jsonpath='{.spec.plugins}' 2>/dev/null | grep -q kuadrant-console-plugin; then \
		echo "[INFO] Console plugin already enabled"; \
	else \
		oc patch consoles.operator.openshift.io cluster --type json \
			--patch '[{"op":"add","path":"/spec/plugins/-","value":"kuadrant-console-plugin"}]'; \
	fi

.PHONY: crc-push
crc-push: ## Push operator image to CRC internal registry
	$(CONTAINER_ENGINE) push --tls-verify=false $(IMG)

.PHONY: crc-deploy-observability
crc-deploy-observability: kustomize helm ## Deploy observability stack on CRC (Grafana, Jaeger, tracing, dashboards)
	@echo "[INFO] Enabling user workload monitoring..."
	kubectl apply -f config/observability/crc/user-workload-monitoring.yaml
	kubectl create namespace monitoring --dry-run=client -o yaml | kubectl apply -f -
	@echo "[INFO] Deploying ServiceMonitors for Kuadrant operators..."
	kubectl apply -f config/observability/prometheus/monitors/operators.yaml
	@echo "[INFO] Generating custom-resource-state ConfigMap for kube-state-metrics..."
	$(KUSTOMIZE) build github.com/Kuadrant/gateway-api-state-metrics/config/kuadrant?ref=0.7.0 | kubectl apply -f -
	@echo "[INFO] Deploying kube-state-metrics for Kuadrant CRDs..."
	kubectl apply -f config/observability/openshift/kube-state-metrics.yaml
	@echo "[INFO] Deploying Istio telemetry for Prometheus metrics..."
	-kubectl apply -f config/observability/openshift/telemetry.yaml
	@echo "[INFO] Installing Grafana Operator..."
	kubectl apply -f config/observability/openshift/grafana/subscription.yaml
	@echo "[INFO] Waiting for OLM to install Grafana operator..."
	@for i in $$(seq 1 60); do \
		kubectl get crd grafanas.grafana.integreatly.org > /dev/null 2>&1 && break; \
		sleep 5; \
	done
	kubectl wait --for=condition=Established crd/grafanas.grafana.integreatly.org --timeout=300s
	kubectl wait --for=condition=Established crd/grafanadatasources.grafana.integreatly.org --timeout=60s
	kubectl wait --for=condition=Established crd/grafanadashboards.grafana.integreatly.org --timeout=60s
	@echo "[INFO] Deploying Grafana service account and RBAC..."
	kubectl apply -f config/observability/crc/grafana-sa.yaml
	@echo "[INFO] Deploying Grafana instance..."
	kubectl apply -n monitoring -f config/observability/openshift/grafana/grafana.yaml
	@echo "[INFO] Deploying Grafana datasource (CRC-local Thanos)..."
	kubectl apply -f config/observability/crc/grafana-datasource.yaml
	@echo "[INFO] Deploying dashboard ConfigMaps..."
	$(KUSTOMIZE) build examples/dashboards | kubectl apply -f -
	@echo "[INFO] Deploying GrafanaDashboard resources..."
	kubectl apply -n monitoring -f config/observability/openshift/grafana/dashboards.yaml
	@echo "[INFO] Installing Jaeger..."
	$(MAKE) install-jaeger JAEGER_HELM_EXTRA_ARGS="-f config/observability/openshift/jaeger-values.yaml"
	kubectl apply -f config/observability/openshift/jaeger-route.yaml
	@echo "[INFO] Deploying tracing configurations..."
	$(MAKE) deploy-tracing
	@echo "[INFO] Observability stack deployed successfully!"

.PHONY: crc-deploy-sample
crc-deploy-sample: ## Deploy toystore sample app with AuthPolicy and RateLimitPolicy
	@echo "[INFO] Deploying toystore sample application..."
	kubectl create namespace $(TOYSTORE_NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -n $(TOYSTORE_NAMESPACE) -f examples/toystore/toystore.yaml
	kubectl -n $(TOYSTORE_NAMESPACE) wait --timeout=120s --for=condition=Available deployment/toystore
	@echo "[INFO] Deploying API key secrets..."
	kubectl apply -n $(KUADRANT_NAMESPACE) -f examples/toystore/alice-api-key-secret.yaml
	kubectl apply -n $(KUADRANT_NAMESPACE) -f examples/toystore/bob-api-key-secret.yaml
	kubectl apply -n $(KUADRANT_NAMESPACE) -f examples/toystore/admin-key-secret.yaml
	@echo "[INFO] Deploying HTTPRoute (CRC)..."
	kubectl apply -n $(TOYSTORE_NAMESPACE) -f examples/toystore/crc/httproute.yaml
	@echo "[INFO] Deploying AuthPolicy..."
	kubectl apply -n $(TOYSTORE_NAMESPACE) -f examples/toystore/crc/authpolicy.yaml
	@echo "[INFO] Deploying RateLimitPolicy..."
	kubectl apply -n $(TOYSTORE_NAMESPACE) -f examples/toystore/ratelimitpolicy_httproute.yaml
	@echo "[INFO] Creating OpenShift Route for toystore..."
	kubectl apply -f examples/toystore/crc/route.yaml
	@echo ""
	@echo "=== CRC Setup Complete ==="
	@echo ""
	@echo "Dashboards:"
	@echo "  Console:  https://console-openshift-console.apps-crc.testing  (kubeadmin / $$(crc console --credentials 2>/dev/null | grep kubeadmin | sed 's/.*-p //' | sed 's/ .*//'))"
	@echo "  Grafana:  https://$$(oc get route -n monitoring grafana-route -o jsonpath='{.spec.host}')  (root / secret)"
	@echo "  Jaeger:   https://$$(oc get route -n observability jaeger -o jsonpath='{.spec.host}')"
	@echo ""
	@echo "Toystore sample app:"
	@echo "  URL:      https://toystore.apps-crc.testing"
	@echo ""
	@echo "  Test authenticated request:"
	@echo "    curl -sk https://toystore.apps-crc.testing/toy -H 'Authorization: APIKEY ALICEKEYFORDEMO'"
	@echo ""
	@echo "  Test unauthenticated request (should return 401):"
	@echo "    curl -sk https://toystore.apps-crc.testing/toy"
	@echo ""
	@echo "  Test rate limiting (6 req/30s global limit — 7th should return 429):"
	@echo '    for i in $$(seq 1 7); do curl -sk -o /dev/null -w "%{http_code}\n" https://toystore.apps-crc.testing/toy -H "Authorization: APIKEY ALICEKEYFORDEMO"; done'
	@echo ""

.PHONY: crc-cleanup-sample
crc-cleanup-sample: ## Remove toystore sample app from CRC
	-kubectl delete -f examples/toystore/crc/route.yaml
	-kubectl delete -n $(TOYSTORE_NAMESPACE) -f examples/toystore/ratelimitpolicy_httproute.yaml
	-kubectl delete -n $(TOYSTORE_NAMESPACE) -f examples/toystore/crc/authpolicy.yaml
	-kubectl delete -n $(TOYSTORE_NAMESPACE) -f examples/toystore/crc/httproute.yaml
	-kubectl delete -n $(KUADRANT_NAMESPACE) -f examples/toystore/admin-key-secret.yaml
	-kubectl delete -n $(KUADRANT_NAMESPACE) -f examples/toystore/bob-api-key-secret.yaml
	-kubectl delete -n $(KUADRANT_NAMESPACE) -f examples/toystore/alice-api-key-secret.yaml
	-kubectl delete -n $(TOYSTORE_NAMESPACE) -f examples/toystore/toystore.yaml
	-kubectl delete namespace $(TOYSTORE_NAMESPACE)

.PHONY: crc-cleanup-observability
crc-cleanup-observability: ## Remove observability components from CRC
	-$(MAKE) uninstall-jaeger
	-kubectl delete -f config/observability/openshift/jaeger-route.yaml
	-kubectl delete -n monitoring -f config/observability/openshift/grafana/dashboards.yaml
	-kubectl delete -f config/observability/crc/grafana-datasource.yaml
	-kubectl delete -n monitoring -f config/observability/openshift/grafana/grafana.yaml
	-kubectl delete -f config/observability/crc/grafana-sa.yaml
	-kubectl delete -f config/observability/openshift/grafana/subscription.yaml
	-kubectl delete -f config/observability/openshift/kube-state-metrics.yaml
	-kubectl delete -f config/observability/prometheus/monitors/operators.yaml

.PHONY: crc-cleanup
crc-cleanup: ## Remove Kuadrant operator and CRDs from CRC (does not stop the VM)
	-$(MAKE) crc-cleanup-sample
	-$(MAKE) crc-cleanup-observability
	-$(MAKE) undeploy
	-$(MAKE) uninstall
