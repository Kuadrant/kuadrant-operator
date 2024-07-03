##@ Helm Charts

.PHONY: helm-build
helm-build: $(YQ) kustomize manifests ## Build the helm chart from kustomize manifests
	# Set desired operator image and related wasm shim image
	V="$(RELATED_IMAGE_WASMSHIM)" \
	$(YQ) eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_WASMSHIM").value) = strenv(V)' -i config/manager/manager.yaml
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	# Build the helm chart templates from kustomize manifests
	$(KUSTOMIZE) build config/helm > charts/kuadrant-operator/templates/manifests.yaml
	# Set the helm chart version and dependencies versions
	V="$(BUNDLE_VERSION)" $(YQ) -i e '.version = strenv(V)' charts/kuadrant-operator/Chart.yaml
	V="$(AUTHORINO_OPERATOR_BUNDLE_VERSION)" $(YQ) -i e '(.dependencies[] | select(.name == "authorino-operator").version) = strenv(V)' charts/kuadrant-operator/Chart.yaml
	V="$(LIMITADOR_OPERATOR_BUNDLE_VERSION)" $(YQ) -i e '(.dependencies[] | select(.name == "limitador-operator").version) = strenv(V)' charts/kuadrant-operator/Chart.yaml
	V="$(DNS_OPERATOR_BUNDLE_VERSION)" $(YQ) -i e '(.dependencies[] | select(.name == "dns-operator").version) = strenv(V)' charts/kuadrant-operator/Chart.yaml

.PHONY: helm-install
helm-install: $(HELM) ## Install the helm chart
	# Install the helm chart in the cluster
	$(HELM) install $(REPO) charts/$(REPO)

.PHONY: helm-uninstall
helm-uninstall: $(HELM) ## Uninstall the helm chart
	# Uninstall the helm chart from the cluster
	$(HELM) uninstall $(REPO)

.PHONY: helm-upgrade
helm-upgrade: $(HELM) ## Upgrade the helm chart
	# Upgrade the helm chart in the cluster
	$(HELM) upgrade $(REPO) charts/$(REPO)

.PHONY: helm-package
helm-package: $(HELM) ## Package the helm chart
	# Package the helm chart
	$(HELM) package charts/$(REPO)

.PHONY: helm-dependency-build
helm-dependency-build: $(HELM) ## Build the chart dependencies
	# Fetch and builds dependencies in Chart.yaml, updates the Chart.lock and downloads the charts .tgz
	$(HELM) dependency build charts/$(REPO)

# GitHub Token with permissions to upload to the release assets
HELM_WORKFLOWS_TOKEN ?= <YOUR-TOKEN>
# GitHub Release Asset Browser Download URL, it can be find in the output of the uploaded asset
BROWSER_DOWNLOAD_URL ?= <BROWSER-DOWNLOAD-URL>
# Github repo name for the helm charts repository
HELM_REPO_NAME ?= helm-charts

CHART_VERSION ?= $(BUNDLE_VERSION)

.PHONY: helm-sync-package-created
helm-sync-package-created: ## Sync the helm chart package to the helm-charts repo
	curl -L \
	  -X POST \
	  -H "Accept: application/vnd.github+json" \
	  -H "Authorization: Bearer $(HELM_WORKFLOWS_TOKEN)" \
	  -H "X-GitHub-Api-Version: 2022-11-28" \
	  https://api.github.com/repos/$(ORG)/$(HELM_REPO_NAME)/dispatches \
	  -d '{"event_type":"chart-created","client_payload":{"chart":"$(REPO)","version":"$(CHART_VERSION)", "browser_download_url": "$(BROWSER_DOWNLOAD_URL)"}}'

.PHONY: helm-sync-package-deleted
helm-sync-package-deleted: ## Sync the deleted helm chart package to the helm-charts repo
	curl -L \
	  -X POST \
	  -H "Accept: application/vnd.github+json" \
	  -H "Authorization: Bearer $(HELM_WORKFLOWS_TOKEN)" \
	  -H "X-GitHub-Api-Version: 2022-11-28" \
	  https://api.github.com/repos/$(ORG)/$(HELM_REPO_NAME)/dispatches \
	  -d '{"event_type":"chart-deleted","client_payload":{"chart":"$(REPO)","version":"$(CHART_VERSION)"}}'
