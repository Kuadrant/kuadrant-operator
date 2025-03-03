
##@ Verify

## Targets to verify actions that generate/modify code have been executed and output committed

.PHONY: verify-fmt
verify-fmt: fmt ## Verify fmt update.
	git diff --exit-code ./api ./controllers

.PHONY: verify-generate
verify-generate: generate ## Verify generate update.
	git diff --exit-code ./api ./controllers

.PHONY: verify-go-mod
verify-go-mod: ## Verify go.mod matches source code
	go mod tidy
	git diff --exit-code ./go.mod

.PHONY: verify-prepare-release
verify-prepare-release: prepare-release
	git diff --exit-code .

