MKFILE_PATH := $(abspath $(lastword $(MAKEFILE_LIST)))
PROJECT_PATH := $(patsubst %/,%,$(dir $(MKFILE_PATH)))

# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.1

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# quay.io/kuadrant/kuadrant-controller-bundle:$VERSION and quay.io/kuadrant/kuadrant-controller-catalog:$VERSION.
IMAGE_TAG_BASE ?= quay.io/kuadrant/kuadrant-controller

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# Image URL to use all building/pushing image targets
DEFAULT_IMG ?= $(IMAGE_TAG_BASE):latest
IMG ?= $(DEFAULT_IMG)
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.22

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

KIND_CLUSTER_NAME = kuadrant-local
GOLANGCI-LINT = $(PROJECT_PATH)/bin/golangci-lint

KUADRANT_NAMESPACE=kuadrant-system

all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-30s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

test: test-unit test-integration ## Run all tests

test-integration: clean-cov generate fmt vet manifests envtest ## Run Integration tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" USE_EXISTING_CLUSTER=true go test ./... -coverprofile $(PROJECT_PATH)/cover.out -tags integration -ginkgo.v -ginkgo.progress -v -timeout 0

test-unit: clean-cov generate fmt vet ## Run Unit tests.
	go test ./... -coverprofile $(PROJECT_PATH)/cover.out -tags unit -v -timeout 0

clean-cov:
	rm -rf $(PROJECT_PATH)/cover.out

.PHONY: local-setup
local-setup: export IMG := $(IMAGE_TAG_BASE):dev
local-setup: ## Deploy locally kuadrant controller from the current code
	$(MAKE) local-env-setup
	$(MAKE) docker-build
	$(KIND) load docker-image $(IMG) --name $(KIND_CLUSTER_NAME)
	$(MAKE) deploy
	kubectl -n kuadrant-system wait --timeout=300s --for=condition=Available deployments --all
	@echo
	@echo "Now you can export the kuadrant gateway by doing:"
	@echo "kubectl port-forward --namespace ${KUADRANT_NAMESPACE} deployment/kuadrant-gateway 8080:8080 8443:8443"
	@echo "after that, you can curl -H \"Host: myhost.com\" localhost:8080"
	@echo "-- Linux only -- Ingress gateway is exported using nodePort service in port 9080"
	@echo "curl -H \"Host: myhost.com\" localhost:9080"
	@echo

.PHONY: local-cleanup
local-cleanup: ## Delete local cluster
	$(MAKE) kind-delete-cluster

# kuadrant is not deployed
.PHONY: local-env-setup
local-env-setup: ## Deploys all services and manifests required by kuadrant to run. Used to run kuadrant with "make run"
	$(MAKE) kind-delete-cluster
	$(MAKE) kind-create-cluster
	$(MAKE) gateway-api-install
	$(MAKE) istio-install
	$(MAKE) deploy-gateway
	$(MAKE) deploy-dependencies
	$(MAKE) install

.PHONY: run-lint
run-lint: $(GOLANGCI-LINT) ## Run lint tests
	$(GOLANGCI-LINT) run

##@ Build

build: generate fmt vet ## Build manager binary.
	go build -o bin/manager main.go

run: export LOG_LEVEL = debug
run: export LOG_MODE = development
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

docker-build: ## Build docker image with the manager.
	docker build -t ${IMG} .

docker-push: ## Push docker image with the manager.
	docker push ${IMG}

##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/deploy | kubectl apply -f -
	cd config/manager && $(KUSTOMIZE) edit set image controller=${DEFAULT_IMG}

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/deploy | kubectl delete -f -

deploy-dependencies: kustomize ## Deploy dependencies to the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/dependencies | kubectl apply -f -
	kubectl -n "$(KUADRANT_NAMESPACE)" wait --timeout=300s --for=condition=Available deployments --all
	kubectl apply -n "$(KUADRANT_NAMESPACE)" -f utils/local-deployment/authorino.yaml
	kubectl apply -n "$(KUADRANT_NAMESPACE)" -f utils/local-deployment/limitador.yaml

##@ Misc

## Miscellaneous Custom targets

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0)

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.5.2)

ENVTEST = $(shell pwd)/bin/setup-envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

$(GOLANGCI-LINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(PROJECT_PATH)/bin v1.49.0

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI-LINT) ## Download golangci-lint locally if necessary.

OPERATOR_SDK = $(shell pwd)/bin/operator-sdk
OPERATOR_SDK_VERSION = v1.22.0
operator-sdk: ## Download operator-sdk locally if necessary.
	./utils/install-operator-sdk.sh $(OPERATOR_SDK) $(OPERATOR_SDK_VERSION)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

# Include last to avoid changing MAKEFILE_LIST used above
include ./make/*.mk
