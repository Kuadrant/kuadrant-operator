
##@ Verify

## Targets to verify actions that generate/modify code have been executed and output committed

.PHONY: verify-fmt
verify-fmt: fmt ## Verify fmt update.
	git diff --exit-code ./api ./internal/controller

.PHONY: verify-generate
verify-generate: generate ## Verify generate update.
	git diff --exit-code ./api ./internal/controller

.PHONY: verify-go-mod
verify-go-mod: ## Verify go.mod matches source code
	go mod tidy
	git diff --exit-code ./go.mod

.PHONY: verify-controller-manifests
verify-controller-manifests: manifests ## Verify controller-gen manifests update.
	git diff --exit-code ./config
	[ -z "$$(git ls-files --other --exclude-standard --directory --no-empty-directory ./config)" ]

.PHONY: verify-bundle
verify-bundle: bundle ## Verify bundle update.
	git diff --exit-code ./bundle
	[ -z "$$(git ls-files --other --exclude-standard --directory --no-empty-directory ./bundle)" ]

.PHONY: verify-helm-charts
verify-helm-charts: helm-build ## Verify helm charts update.
	git diff --exit-code ./charts
	[ -z "$$(git ls-files --other --exclude-standard --directory --no-empty-directory ./charts)" ]

.PHONY: verify-manifests ## Verify controller-gen, bundle and helm charts manifests.
verify-manifests: ## Verify manifests update.
	make verify-controller-manifests
	make verify-bundle
	make verify-helm-charts

.PHONY: verify-prepare-release ## Verify set of manifests based on release.yaml file.
verify-prepare-release: prepare-release
	git diff --exit-code .

