
##@ GitHub Actions

## Targets to help test GitHub Actions locally using act https://github.com/nektos/act

ACT = $(shell pwd)/bin/act
act: ## Download act locally if necessary.
	$(call go-get-tool,$(ACT),github.com/nektos/act@latest)

.PHONY: act-pull-request-jobs
act-pull-request-jobs: act kind ## Run pull request jobs.
	$(ACT) pull_request --privileged
	$(KIND) delete cluster --name kuadrant-test

.PHONY: act-test-unit-tests
act-test-unit-tests: act ## Run unit tests job.
	$(ACT) -j unit-tests

.PHONY: act-test-integration-tests
act-test-integration-tests: act kind ## Run integration tests job.
	$(ACT) -j integration-tests --privileged
	$(KIND) delete cluster --name kuadrant-test
