##@ Helm Charts

# Chart name
CHART_NAME ?= kuadrant-operator
# Chart directory
CHART_DIRECTORY ?= charts/$(CHART_NAME)

.PHONY: helm-build
helm-build: $(YQ) kustomize manifests ## Build the helm chart from kustomize manifests
	# Set desired Wasm-shim image
	V="$(RELATED_IMAGE_WASMSHIM)" \
	$(YQ) eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_WASMSHIM").value) = strenv(V)' -i config/manager/manager.yaml
	# Set desired operator image
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	# Build the helm chart templates from kustomize manifests
	$(KUSTOMIZE) build config/helm > $(CHART_DIRECTORY)/templates/manifests.yaml
	# Set the helm chart version and dependencies versions
	V="$(BUNDLE_VERSION)" $(YQ) -i e '.version = strenv(V)' $(CHART_DIRECTORY)/Chart.yaml
	V="$(BUNDLE_VERSION)" $(YQ) -i e '.appVersion = strenv(V)' $(CHART_DIRECTORY)/Chart.yaml
	V="$(AUTHORINO_OPERATOR_BUNDLE_VERSION)" $(YQ) -i e '(.dependencies[] | select(.name == "authorino-operator").version) = strenv(V)' $(CHART_DIRECTORY)/Chart.yaml
	V="$(LIMITADOR_OPERATOR_BUNDLE_VERSION)" $(YQ) -i e '(.dependencies[] | select(.name == "limitador-operator").version) = strenv(V)' $(CHART_DIRECTORY)/Chart.yaml
	V="$(DNS_OPERATOR_BUNDLE_VERSION)" $(YQ) -i e '(.dependencies[] | select(.name == "dns-operator").version) = strenv(V)' $(CHART_DIRECTORY)/Chart.yaml

.PHONY: helm-install
helm-install: $(HELM) ## Install the helm chart
	# Install the helm chart in the cluster
	$(HELM) install $(CHART_NAME) $(CHART_DIRECTORY)

.PHONY: helm-uninstall
helm-uninstall: $(HELM) ## Uninstall the helm chart
	# Uninstall the helm chart from the cluster
	$(HELM) uninstall $(CHART_NAME)

.PHONY: helm-upgrade
helm-upgrade: $(HELM) ## Upgrade the helm chart
	# Upgrade the helm chart in the cluster
	$(HELM) upgrade $(CHART_NAME) $(CHART_DIRECTORY)

.PHONY: helm-package
helm-package: $(HELM) ## Package the helm chart
	# Package the helm chart
	$(HELM) package $(CHART_DIRECTORY)

# GPG_KEY_UID: substring of the desired key's uid, the name or email
GPG_KEY_UID ?= 'Kuadrant Development Team'
# The keyring should've been imported before running this target
.PHONY: helm-package-sign
helm-package-sign: $(HELM) ## Package the helm chart and GPG sign it
	# Package the helm chart and sign it
	$(HELM) package --sign --key "$(GPG_KEY_UID)" $(CHART_DIRECTORY)

.PHONY: helm-dependency-build
helm-dependency-build: $(HELM) ## Build the chart dependencies
	# Fetch and builds dependencies in Chart.yaml, updates the Chart.lock and downloads the charts .tgz
	$(HELM) dependency build $(CHART_DIRECTORY)

.PHONY: helm-add-kuadrant-repo
helm-add-kuadrant-repo: $(HELM) ## Add the Kuadrant charts repo and force update it
	$(HELM) repo add kuadrant https://kuadrant.io/helm-charts --force-update

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
	  -d '{"event_type":"chart-created","client_payload":{"chart":"$(CHART_NAME)","version":"$(CHART_VERSION)", "browser_download_url": "$(BROWSER_DOWNLOAD_URL)"}}'

.PHONY: helm-sync-package-deleted
helm-sync-package-deleted: ## Sync the deleted helm chart package to the helm-charts repo
	curl -L \
	  -X POST \
	  -H "Accept: application/vnd.github+json" \
	  -H "Authorization: Bearer $(HELM_WORKFLOWS_TOKEN)" \
	  -H "X-GitHub-Api-Version: 2022-11-28" \
	  https://api.github.com/repos/$(ORG)/$(HELM_REPO_NAME)/dispatches \
	  -d '{"event_type":"chart-deleted","client_payload":{"chart":"$(CHART_NAME)","version":"$(CHART_VERSION)"}}'

.PHONY: helm-print-chart-version
helm-print-chart-version: $(HELM) ## Reads local chart version
	@$(HELM) show chart charts/$(CHART_NAME) | grep -E "^version:" | awk '{print $$2}'

helm-print-installed-chart-version: $(YQ) $(HELM) ## Reads installed chart version
	@$(HELM) list --all-namespaces -o yaml | $(YQ) '(.[] | select(.name == "kuadrant") | .app_version)'
