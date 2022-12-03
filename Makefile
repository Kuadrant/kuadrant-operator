MKFILE_PATH := $(abspath $(lastword $(MAKEFILE_LIST)))
PROJECT_PATH := $(patsubst %/,%,$(dir $(MKFILE_PATH)))

# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.0

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

# Address of the container registry
REGISTRY = quay.io

# Organization in container registry
ORG ?= kuadrant

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# quay.io/kuadrant/kuadrant-operator-bundle:$VERSION and quay.io/kuadrant/kuadrant-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= $(REGISTRY)/$(ORG)/kuadrant-operator

ifeq (0.0.0,$(VERSION))
IMAGE_TAG ?= latest
else
IMAGE_TAG ?= v$(VERSION)
endif

# kubebuilder-tools still doesn't support darwin/arm64. This is a workaround (https://github.com/kubernetes-sigs/controller-runtime/issues/1657)
ARCH_PARAM =
ifeq ($(shell uname -sm),Darwin arm64)
	ARCH_PARAM = --arch=amd64
endif

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:$(IMAGE_TAG)

# Image URL to use all building/pushing image targets
IMG ?= $(IMAGE_TAG_BASE):$(IMAGE_TAG)
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

# Kuadrant Namespace
KUADRANT_NAMESPACE ?= kuadrant-system

# Kuadrant component versions
## kuadrant controller
#ToDo Pin this version once we have an initial release of the controller
KUADRANT_CONTROLLER_VERSION ?= latest
ifeq (latest,$(KUADRANT_CONTROLLER_VERSION))
KUADRANT_CONTROLLER_GITREF = main
else
KUADRANT_CONTROLLER_GITREF = $(KUADRANT_CONTROLLER_VERSION)
endif
## authorino
#ToDo Pin this version once we have an initial release of authorino
AUTHORINO_OPERATOR_VERSION ?= latest
ifeq (latest,$(AUTHORINO_OPERATOR_VERSION))
AUTHORINO_OPERATOR_BUNDLE_VERSION = 0.0.0
AUTHORINO_OPERATOR_BUNDLE_IMG_TAG = latest
AUTHORINO_OPERATOR_GITREF = main
else
AUTHORINO_OPERATOR_BUNDLE_VERSION = ${AUTHORINO_OPERATOR_VERSION}
AUTHORINO_OPERATOR_BUNDLE_IMG_TAG = v$(AUTHORINO_OPERATOR_BUNDLE_VERSION)
AUTHORINO_OPERATOR_GITREF = v$(AUTHORINO_OPERATOR_BUNDLE_VERSION)
endif
AUTHORINO_OPERATOR_BUNDLE_IMG ?= quay.io/kuadrant/authorino-operator-bundle:$(AUTHORINO_OPERATOR_BUNDLE_IMG_TAG)
## limitador
#ToDo Pin this version once we have an initial release of limitador
LIMITADOR_OPERATOR_VERSION ?= latest
ifeq (latest,$(LIMITADOR_OPERATOR_VERSION))
LIMITADOR_OPERATOR_BUNDLE_VERSION = 0.0.0
LIMITADOR_OPERATOR_BUNDLE_IMG_TAG = latest
LIMITADOR_OPERATOR_GITREF = main
else
LIMITADOR_OPERATOR_BUNDLE_VERSION = $(LIMITADOR_OPERATOR_VERSION)
LIMITADOR_OPERATOR_BUNDLE_IMG_TAG = v$(LIMITADOR_OPERATOR_BUNDLE_VERSION)
LIMITADOR_OPERATOR_GITREF = v$(LIMITADOR_OPERATOR_BUNDLE_VERSION)
endif
LIMITADOR_OPERATOR_BUNDLE_IMG ?= quay.io/kuadrant/limitador-operator-bundle:$(LIMITADOR_OPERATOR_BUNDLE_IMG_TAG)

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

manifests: controller-gen dependencies-manifests ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./api/v1beta1" output:crd:artifacts:config=config/crd/bases

.PHONY: dependencies-manifests
dependencies-manifests: export AUTHORINO_OPERATOR_GITREF := $(AUTHORINO_OPERATOR_GITREF)
dependencies-manifests: export LIMITADOR_OPERATOR_GITREF := $(LIMITADOR_OPERATOR_GITREF)
dependencies-manifests: ## Update kuadrant dependencies manifests.
	envsubst \
        < config/dependencies/authorino/kustomization.template.yaml \
        > config/dependencies/authorino/kustomization.yaml
	envsubst \
        < config/dependencies/limitador/kustomization.template.yaml \
        > config/dependencies/limitador/kustomization.yaml

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

.PHONY: clean-cov
clean-cov: ## Remove coverage report
	rm -rf cover.out

.PHONY: test
test: test-unit test-integration ## Run all tests

test-integration: clean-cov generate fmt vet envtest ## Run Integration tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) $(ARCH_PARAM) use $(ENVTEST_K8S_VERSION) -p path)" USE_EXISTING_CLUSTER=true go test ./... -coverprofile $(PROJECT_PATH)/cover.out -tags integration -ginkgo.v -ginkgo.progress -v -timeout 0

test-unit: clean-cov generate fmt vet ## Run Unit tests.
	go test ./... -coverprofile $(PROJECT_PATH)/cover.out -tags unit -v -timeout 0

.PHONY: namespace
namespace: ## Creates a namespace where to deploy Kuadrant Operator
	kubectl create namespace $(KUADRANT_NAMESPACE)

.PHONY: local-setup
local-setup: export IMG := $(IMAGE_TAG_BASE):dev
local-setup: ## Deploy locally kuadrant operator from the current code
	$(MAKE) local-env-setup
	$(MAKE) docker-build
	$(KIND) load docker-image $(IMG) --name $(KIND_CLUSTER_NAME)
	$(MAKE) deploy
	kubectl -n $(KUADRANT_NAMESPACE) wait --timeout=300s --for=condition=Available deployments --all
	@echo
	@echo "Now you can export the kuadrant gateway by doing:"
	@echo "kubectl port-forward -n istio-system service/istio-ingressgateway 9080:80 &"
	@echo "after that, you can curl -H \"Host: myhost.com\" localhost:9080"
	@echo "-- Linux only -- Ingress gateway is exported using nodePort service in port 9080"
	@echo "curl -H \"Host: myhost.com\" localhost:9080"
	@echo

.PHONY: local-cleanup
local-cleanup: ## Delete local cluster
	$(MAKE) kind-delete-cluster

.PHONY: local-cluster-setup
local-cluster-setup: ## Sets up Kind cluster with GatewayAPI manifests and istio GW, nothing Kuadrant.
	$(MAKE) kind-delete-cluster
	$(MAKE) kind-create-cluster
	$(MAKE) namespace
	$(MAKE) gateway-api-install
	$(MAKE) istio-install
	$(MAKE) deploy-gateway

# kuadrant is not deployed
.PHONY: local-env-setup
local-env-setup: ## Deploys all services and manifests required by kuadrant to run. Used to run kuadrant with "make run"
	$(MAKE) local-cluster-setup
	$(MAKE) deploy-dependencies
	$(MAKE) install

.PHONY: test-env-setup
test-env-setup: ## Deploys all services and manifests required by kuadrant to run on CI.
	$(MAKE) namespace
	$(MAKE) gateway-api-install
	$(MAKE) istio-install
	$(MAKE) deploy-gateway
	$(MAKE) deploy-dependencies
	$(MAKE) install

.PHONY: local-olm-setup
local-olm-setup: ## Installs OLM and the Kuadrant operator catalog, then installs the operator with its dependencies.
	$(MAKE) local-cluster-setup
	$(MAKE) docker-build
	$(MAKE) install-olm
	$(MAKE) bundle
	$(MAKE) bundle-build
	$(MAKE) catalog-generate
	$(MAKE) catalog-custom-build
	$(MAKE) kind-load-catalog
	$(MAKE) kind-load-image
	$(MAKE) kind-load-bundle
	$(MAKE) deploy-olm

##@ Build

build: generate fmt vet ## Build manager binary.
	go build -o bin/manager main.go

run: export LOG_LEVEL = debug
run: export LOG_MODE = development
run: generate fmt vet ## Run a controller from your host.
	go run ./main.go

docker-build: ## Build docker image with the manager.
	docker build -t ${IMG} .

docker-push: ## Push docker image with the manager.
	docker push ${IMG}

kind-load-catalog: ## Load catalog image to local cluster
	$(KIND) load docker-image $(CATALOG_IMG) --name $(KIND_CLUSTER_NAME)

kind-load-image: ## Load image to local cluster
	$(KIND) load docker-image $(IMG) --name $(KIND_CLUSTER_NAME)

kind-load-bundle: ## Load image to local cluster
	$(KIND) load docker-image $(BUNDLE_IMG) --name $(KIND_CLUSTER_NAME)

##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/deploy | kubectl apply -f -
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMAGE_TAG_BASE}:latest

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/deploy | kubectl delete -f -

deploy-dependencies: kustomize dependencies-manifests ## Deploy dependencies to the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/dependencies | kubectl apply -f -
	kubectl -n "$(KUADRANT_NAMESPACE)" wait --timeout=300s --for=condition=Available deployments --all

.PHONY: install-olm
install-olm:
	$(OPERATOR_SDK) olm install

.PHONY: uninstall-olm
uninstall-olm:
	$(OPERATOR_SDK) olm uninstall

deploy-olm: ## Deploy operator to the K8s cluster specified in ~/.kube/config using OLM catalog image.
	$(KUSTOMIZE) build config/deploy/olm | kubectl apply -f -

undeploy-olm: ## Undeploy controller from the K8s cluster specified in ~/.kube/config using OLM catalog image.
	$(KUSTOMIZE) build config/deploy/olm | kubectl delete -f -

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0)

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.5.5)

ENVTEST = $(shell pwd)/bin/setup-envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

# go-get-tool will 'go install' any package $2 and install it to $1.
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

OPERATOR_SDK = $(shell pwd)/bin/operator-sdk
OPERATOR_SDK_VERSION = v1.22.0
operator-sdk: ## Download operator-sdk locally if necessary.
	./utils/install-operator-sdk.sh $(OPERATOR_SDK) $(OPERATOR_SDK_VERSION)

.PHONY: bundle-dependencies
bundle-dependencies: export AUTHORINO_OPERATOR_BUNDLE_VERSION := $(AUTHORINO_OPERATOR_BUNDLE_VERSION)
bundle-dependencies: export LIMITADOR_OPERATOR_BUNDLE_VERSION := $(LIMITADOR_OPERATOR_BUNDLE_VERSION)
bundle-dependencies: ## Generate bundle dependencies file.
	./utils/generate-dependencies-yaml.sh > bundle/metadata/dependencies.yaml

.PHONY: bundle
bundle: export IMAGE_TAG := $(IMAGE_TAG)
bundle: export BUNDLE_VERSION := $(VERSION)
bundle: manifests kustomize operator-sdk bundle-dependencies ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	envsubst \
		< config/manifests/bases/kuadrant-operator.clusterserviceversion.template.yaml \
		> config/manifests/bases/kuadrant-operator.clusterserviceversion.yaml
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.26.2/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG),$(LIMITADOR_OPERATOR_BUNDLE_IMG),$(AUTHORINO_OPERATOR_BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:$(IMAGE_TAG)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

PLATFORM_PARAM =
ifeq ($(shell uname -sm),Darwin arm64)
	PLATFORM_PARAM = --platform=linux/arm64
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

.PHONY: catalog-custom-build
catalog-custom-build: ## Build the bundle image.
	docker build $(PLATFORM_PARAM) -f catalog.Dockerfile -t $(CATALOG_IMG) .


.PHONY: catalog-generate
catalog-generate: opm ## Generate a catalog/index Dockerfile.
	$(OPM) index add --generate --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

##@ Code Style

GOLANGCI-LINT = $(PROJECT_PATH)/bin/golangci-lint

.PHONY: run-lint
run-lint: $(GOLANGCI-LINT) ## Run lint tests
	$(GOLANGCI-LINT) run

$(GOLANGCI-LINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(PROJECT_PATH)/bin v1.50.1

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI-LINT) ## Download golangci-lint locally if necessary.


# Include last to avoid changing MAKEFILE_LIST used above
include ./make/*.mk
