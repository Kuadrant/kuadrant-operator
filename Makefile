SHELL := /bin/bash

MKFILE_PATH := $(abspath $(lastword $(MAKEFILE_LIST)))
PROJECT_PATH := $(patsubst %/,%,$(dir $(MKFILE_PATH)))

# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.1

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "preview,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=preview,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="preview,fast,stable")
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

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= controller-bundle:$(VERSION)

# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

KUADRANT_NAMESPACE=kuadrant-system

all: manager

# Run all tests
test: test-unit test-integration

# Run e2e tests
ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test-integration: clean-cov generate fmt vet manifests
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.7.0/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); USE_EXISTING_CLUSTER=true go test ./... -coverprofile $(PROJECT_PATH)/cover.out -tags integration -ginkgo.v -ginkgo.progress -v -timeout 0

test-unit: clean-cov generate fmt vet manifests
	go test ./... -coverprofile $(PROJECT_PATH)/cover.out -tags unit -v -timeout 0

clean-cov:
	rm -rf $(PROJECT_PATH)/cover.out

# Build manager binary
manager: generate fmt vet
	go build -o bin/manager main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet manifests
	go run ./main.go --zap-devel

# Install CRDs into a cluster
install: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests kustomize
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

# UnDeploy controller from the configured Kubernetes cluster in ~/.kube/config
undeploy:
	$(KUSTOMIZE) build config/default | kubectl delete -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Build the docker image
docker-build:
	docker build -t ${IMG} .

# Push the docker image
docker-push:
	docker push ${IMG}

# Download controller-gen locally if necessary
CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen:
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

# Download kustomize locally if necessary
KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize:
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

# Generate bundle manifests and metadata, then validate generated files.
.PHONY: bundle
bundle: manifests kustomize
	operator-sdk generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	operator-sdk bundle validate ./bundle

# Build the bundle image.
.PHONY: bundle-build
bundle-build:
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

# Download kind locally if necessary
KIND = $(shell pwd)/bin/kind
kind:
	$(call go-get-tool,$(KIND),sigs.k8s.io/kind@v0.11.1)

# Download istioctl.
ISTIOCTL = $(shell pwd)/bin/istioctl
ISTIOCTLVERSION = 1.9.4
istioctl:
ifeq (,$(wildcard $(ISTIOCTL)))
	@{ \
	set -e ;\
	mkdir -p $(dir $(ISTIOCTL)) ;\
	curl -sSL https://raw.githubusercontent.com/istio/istio/master/release/downloadIstioCtl.sh | ISTIO_VERSION=$(ISTIOCTLVERSION) HOME=$(shell pwd)/bin/ sh - > /dev/null 2>&1;\
	mv $(shell pwd)/bin/.istioctl/bin/istioctl $(ISTIOCTL) ;\
	rm -r $(shell pwd)/bin/.istioctl ;\
	chmod +x $(ISTIOCTL) ;\
	}
endif

# Generates istio manifests with patches.
.PHONY: generate-istio-manifests
generate-istio-manifests: istioctl
	$(ISTIOCTL) manifest generate --set profile=minimal --set values.gateways.istio-ingressgateway.autoscaleEnabled=false --set values.pilot.autoscaleEnabled=false --set values.global.istioNamespace=$(KUADRANT_NAMESPACE) -f utils/local-deployment/patches/istio-externalProvider.yaml -o utils/local-deployment/istio-manifests

.PHONY: istio-manifest-update-test
istio-manifest-update-test: generate-istio-manifests
	git diff --exit-code ./utils/local-deployment/istio-manifests
	[ -z "$$(git ls-files --other --exclude-standard --directory --no-empty-directory ./utils/local-deployment/istio-manifests)" ]

.PHONY: local-setup
local-setup: local-cleanup local-setup-kind manifests kustomize generate
	./utils/local-deployment/local-setup.sh

# Deploys all services and manifests required by kuadrant to run
# kuadrant is not deployed
.PHONY: local-env-setup
local-env-setup: local-cleanup local-setup-kind deploy-kuadrant-deps generate install

.PHONY: deploy-kuadrant-deps
deploy-kuadrant-deps:
	./utils/local-deployment/deploy-kuadrant-deps.sh

KIND_CLUSTER_NAME = kuadrant-local

.PHONY: local-setup-kind
local-setup-kind: kind
	$(KIND) create cluster --name $(KIND_CLUSTER_NAME) --config utils/local-deployment/kind-cluster.yaml

.PHONY: local-cleanup
local-cleanup: kind
	$(KIND) delete cluster --name $(KIND_CLUSTER_NAME)

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
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.15.1/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

.PHONY: manifests-update-test
manifests-update-test: manifests
	git diff --exit-code ./config
	[ -z "$$(git ls-files --other --exclude-standard --directory --no-empty-directory ./config)" ]

GOLANGCI-LINT=$(PROJECT_PATH)/bin/golangci-lint
$(GOLANGCI-LINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(PROJECT_PATH)/bin v1.41.1

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI-LINT)

.PHONY: run-lint
run-lint: $(GOLANGCI-LINT)
	$(GOLANGCI-LINT) run

# Generates limitador manifests.
LIMITADOR_OPERATOR_VERSION=v0.2.0
LIMITADOR_OPERATOR_IMAGE=quay.io/3scale/limitador-operator:$(LIMITADOR_OPERATOR_VERSION)
.PHONY: generate-limitador-operator-manifests
generate-limitador-operator-manifests:
	$(eval TMP := $(shell mktemp -d))
	cd $(TMP); git clone --depth 1 --branch $(LIMITADOR_OPERATOR_VERSION) https://github.com/kuadrant/limitador-operator.git
	cd $(TMP)/limitador-operator; make kustomize
	cd $(TMP)/limitador-operator/config/manager; $(TMP)/limitador-operator/bin/kustomize edit set image controller=$(LIMITADOR_OPERATOR_IMAGE)
	cd $(TMP)/limitador-operator/config/default; $(TMP)/limitador-operator/bin/kustomize edit set namespace $(KUADRANT_NAMESPACE)
	cd $(TMP)/limitador-operator; bin/kustomize build config/default -o $(PROJECT_PATH)/utils/local-deployment/limitador-operator.yaml
	-rm -rf $(TMP)
