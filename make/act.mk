
##@ GitHub Actions

## Targets to help test GitHub Actions locally using act https://github.com/nektos/act

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

.PHONY: act-test-verify-manifests
act-test-verify-manifests: act kind ## Run verify manifests job.
	$(ACT) -j verify-manifests
	$(KIND) delete cluster --name kuadrant-test

.PHONY: act-test-verify-bundle
act-test-verify-bundle: act kind ## Run verify bundle job.
	$(ACT) -j verify-bundle
	$(KIND) delete cluster --name kuadrant-test

.PHONY: act-test-verify-fmt
act-test-verify-fmt: act kind ## Run verify fmt job.
	$(ACT) -j verify-fmt
	$(KIND) delete cluster --name kuadrant-test
