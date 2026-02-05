# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

MKFILE_PATH := $(abspath $(lastword $(MAKEFILE_LIST)))
PROJECT_PATH := $(patsubst %/,%,$(dir $(MKFILE_PATH)))

OS = $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH := $(shell uname -m | tr '[:upper:]' '[:lower:]')
# Container Engine to be used for building image and with kind
CONTAINER_ENGINE ?= docker
ifeq (podman,$(CONTAINER_ENGINE))
	CONTAINER_ENGINE_EXTRA_FLAGS ?= --load
endif

# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.0

# CHANNEL define the catalog channel used in the catalog.
# - use the CHANNEL as arg of the catalog target (e.g make catalog CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export CHANNEL="stable")
CHANNEL ?= alpha

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
CHANNELS ?= alpha
BUNDLE_CHANNELS := --channels=$(CHANNELS)

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
DEFAULT_CHANNEL ?= alpha
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

DEFAULT_IMAGE_TAG = latest

# Semantic versioning (i.e. Major.Minor.Patch)
is_semantic_version = $(shell [[ $(1) =~ ^[0-9]+\.[0-9]+\.[0-9]+(-.+)?$$ ]] && echo "true")

# BUNDLE_VERSION defines the version for the kuadrant-operator bundle.
# If the version is not semantic, will use the default one
bundle_is_semantic := $(call is_semantic_version,$(VERSION))
ifeq (0.0.0,$(VERSION))
BUNDLE_VERSION = $(VERSION)
IMAGE_TAG = latest
else ifeq ($(bundle_is_semantic),true)
BUNDLE_VERSION = $(VERSION)
IMAGE_TAG = v$(VERSION)
else
BUNDLE_VERSION = 0.0.0
IMAGE_TAG ?= $(DEFAULT_IMAGE_TAG)
endif

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:$(IMAGE_TAG)

# Address of the container registry
REGISTRY = quay.io

# Organization in container registry
ORG ?= kuadrant

# Repo in the container registry
DEFAULT_REPO = kuadrant-operator
REPO ?= $(DEFAULT_REPO)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# quay.io/kuadrant/kuadrant-operator-bundle:$VERSION and quay.io/kuadrant/kuadrant-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= $(REGISTRY)/$(ORG)/kuadrant-operator

# kubebuilder-tools still doesn't support darwin/arm64. This is a workaround (https://github.com/kubernetes-sigs/controller-runtime/issues/1657)
ARCH_PARAM =
ifeq ($(shell uname -sm),Darwin arm64)
	ARCH_PARAM = --arch=amd64
endif

# Image URL to use all building/pushing image targets
IMG ?= $(IMAGE_TAG_BASE):$(IMAGE_TAG)

# Directories containing unit & integration test packages
UNIT_DIRS := ./pkg/... ./api/... ./internal/...

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# Kuadrant Namespace
KUADRANT_NAMESPACE ?= kuadrant-system
OPERATOR_NAMESPACE ?= $(KUADRANT_NAMESPACE)

# Kuadrant Service Account
KUADRANT_SA_NAME ?= kuadrant-operator-controller-manager

#Kuadrant Extensions
WITH_EXTENSIONS ?= true
EXTENSIONS_DIRECTORIES ?= $(shell ls -d $(PROJECT_PATH)/cmd/extensions/*/)

# Kuadrant component versions
## authorino
#ToDo Pin this version once we have an initial release of authorino
AUTHORINO_OPERATOR_VERSION ?= latest
authorino_bundle_is_semantic := $(call is_semantic_version,$(AUTHORINO_OPERATOR_VERSION))

ifeq (latest,$(AUTHORINO_OPERATOR_VERSION))
AUTHORINO_OPERATOR_BUNDLE_VERSION = 0.0.0
AUTHORINO_OPERATOR_BUNDLE_IMG_TAG = latest
AUTHORINO_OPERATOR_GITREF = main
else ifeq (true,$(authorino_bundle_is_semantic))
AUTHORINO_OPERATOR_BUNDLE_VERSION = $(AUTHORINO_OPERATOR_VERSION)
AUTHORINO_OPERATOR_BUNDLE_IMG_TAG = v$(AUTHORINO_OPERATOR_BUNDLE_VERSION)
AUTHORINO_OPERATOR_GITREF = v$(AUTHORINO_OPERATOR_BUNDLE_VERSION)
else
AUTHORINO_OPERATOR_BUNDLE_VERSION = $(AUTHORINO_OPERATOR_VERSION)
AUTHORINO_OPERATOR_BUNDLE_IMG_TAG = $(AUTHORINO_OPERATOR_BUNDLE_VERSION)
AUTHORINO_OPERATOR_GITREF = $(AUTHORINO_OPERATOR_BUNDLE_VERSION)
endif

AUTHORINO_OPERATOR_BUNDLE_IMG ?= quay.io/kuadrant/authorino-operator-bundle:$(AUTHORINO_OPERATOR_BUNDLE_IMG_TAG)
## limitador
#ToDo Pin this version once we have an initial release of limitador
LIMITADOR_OPERATOR_VERSION ?= latest
limitador_bundle_is_semantic := $(call is_semantic_version,$(LIMITADOR_OPERATOR_VERSION))
ifeq (latest,$(LIMITADOR_OPERATOR_VERSION))
LIMITADOR_OPERATOR_BUNDLE_VERSION = 0.0.0
LIMITADOR_OPERATOR_BUNDLE_IMG_TAG = latest
LIMITADOR_OPERATOR_GITREF = main
else ifeq (true,$(limitador_bundle_is_semantic))
LIMITADOR_OPERATOR_BUNDLE_VERSION = $(LIMITADOR_OPERATOR_VERSION)
LIMITADOR_OPERATOR_BUNDLE_IMG_TAG = v$(LIMITADOR_OPERATOR_BUNDLE_VERSION)
LIMITADOR_OPERATOR_GITREF = v$(LIMITADOR_OPERATOR_BUNDLE_VERSION)
else
LIMITADOR_OPERATOR_BUNDLE_VERSION = $(LIMITADOR_OPERATOR_VERSION)
LIMITADOR_OPERATOR_BUNDLE_IMG_TAG = $(LIMITADOR_OPERATOR_BUNDLE_VERSION)
LIMITADOR_OPERATOR_GITREF = $(LIMITADOR_OPERATOR_BUNDLE_VERSION)
endif
LIMITADOR_OPERATOR_BUNDLE_IMG ?= quay.io/kuadrant/limitador-operator-bundle:$(LIMITADOR_OPERATOR_BUNDLE_IMG_TAG)

## dns
DNS_OPERATOR_VERSION ?= latest

kuadrantdns_bundle_is_semantic := $(call is_semantic_version,$(DNS_OPERATOR_VERSION))
ifeq (latest,$(DNS_OPERATOR_VERSION))
DNS_OPERATOR_BUNDLE_VERSION = 0.0.0
DNS_OPERATOR_BUNDLE_IMG_TAG = latest
DNS_OPERATOR_GITREF = main
else ifeq (true,$(kuadrantdns_bundle_is_semantic))
DNS_OPERATOR_BUNDLE_VERSION = $(DNS_OPERATOR_VERSION)
DNS_OPERATOR_BUNDLE_IMG_TAG = v$(DNS_OPERATOR_BUNDLE_VERSION)
DNS_OPERATOR_GITREF = v$(DNS_OPERATOR_BUNDLE_VERSION)
else
DNS_OPERATOR_BUNDLE_VERSION = $(DNS_OPERATOR_VERSION)
DNS_OPERATOR_BUNDLE_IMG_TAG = $(DNS_OPERATOR_BUNDLE_VERSION)
DNS_OPERATOR_GITREF = $(DNS_OPERATOR_BUNDLE_VERSION)
endif
DNS_OPERATOR_BUNDLE_IMG ?= quay.io/kuadrant/dns-operator-bundle:$(DNS_OPERATOR_BUNDLE_IMG_TAG)

## wasm-shim
WASM_SHIM_VERSION ?= latest
shim_version_is_semantic := $(call is_semantic_version,$(WASM_SHIM_VERSION))

ifeq (true,$(shim_version_is_semantic))
RELATED_IMAGE_WASMSHIM ?= oci://quay.io/kuadrant/wasm-shim:v$(WASM_SHIM_VERSION)
else
RELATED_IMAGE_WASMSHIM ?= oci://quay.io/kuadrant/wasm-shim:$(WASM_SHIM_VERSION)
endif

## developer-portal-controller
DEVELOPERPORTAL_VERSION ?= latest
developerportal_version_is_semantic := $(call is_semantic_version,$(DEVELOPERPORTAL_VERSION))

ifeq (latest,$(DEVELOPERPORTAL_VERSION))
RELATED_IMAGE_DEVELOPERPORTAL ?= quay.io/kuadrant/developer-portal-controller:latest
DEVELOPERPORTAL_GITREF = main
else ifeq (true,$(developerportal_version_is_semantic))
RELATED_IMAGE_DEVELOPERPORTAL ?= quay.io/kuadrant/developer-portal-controller:v$(DEVELOPERPORTAL_VERSION)
DEVELOPERPORTAL_GITREF = v$(DEVELOPERPORTAL_VERSION)
else
RELATED_IMAGE_DEVELOPERPORTAL ?= quay.io/kuadrant/developer-portal-controller:$(DEVELOPERPORTAL_VERSION)
DEVELOPERPORTAL_GITREF = $(DEVELOPERPORTAL_VERSION)
endif

## console-plugin
CONSOLEPLUGIN_VERSION ?= latest
consoleplugin_version_is_semantic := $(call is_semantic_version,$(CONSOLEPLUGIN_VERSION))

ifeq (latest,$(CONSOLEPLUGIN_VERSION))
RELATED_IMAGE_CONSOLE_PLUGIN_LATEST ?= quay.io/kuadrant/console-plugin:latest
else ifeq (true,$(consoleplugin_version_is_semantic))
RELATED_IMAGE_CONSOLE_PLUGIN_LATEST ?= quay.io/kuadrant/console-plugin:v$(CONSOLEPLUGIN_VERSION)
else
RELATED_IMAGE_CONSOLE_PLUGIN_LATEST ?= quay.io/kuadrant/console-plugin:$(CONSOLEPLUGIN_VERSION)
endif

## gatewayapi-provider
GATEWAYAPI_PROVIDER ?= istio

EXTENSIONS_DIR ?= /extensions
EXTENSIONS_IMG ?= quay.io/kuadrant/extensions:dev

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

##@ Tools

## Tool Binaries
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
KUSTOMIZE ?= $(LOCALBIN)/kustomize
YQ ?= $(LOCALBIN)/yq
OPM ?= $(LOCALBIN)/opm
KIND ?= $(LOCALBIN)/kind
ACT ?= $(LOCALBIN)/act
GINKGO ?= $(LOCALBIN)/ginkgo
HELM ?= $(LOCALBIN)/helm
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint

## Tool Versions
OPERATOR_SDK_VERSION ?= v1.33.0
CONTROLLER_GEN_VERSION ?= v0.19.0
KUSTOMIZE_VERSION ?= v4.5.5
YQ_VERSION ?= v4.34.2
OPM_VERSION ?= v1.48.0
KIND_VERSION ?= v0.23.0
ACT_VERSION ?= latest
HELM_VERSION ?= v3.15.0
GOLANGCI_LINT_VERSION ?= v2.7.2

## Versioned Binaries
OPERATOR_SDK_V_BINARY := $(LOCALBIN)/operator-sdk-$(OPERATOR_SDK_VERSION)
CONTROLLER_GEN_V_BINARY := $(LOCALBIN)/controller-gen-$(CONTROLLER_GEN_VERSION)
KUSTOMIZE_V_BINARY := $(LOCALBIN)/kustomize-$(KUSTOMIZE_VERSION)
YQ_V_BINARY := $(LOCALBIN)/yq-$(YQ_VERSION)
OPM_V_BINARY := $(LOCALBIN)/opm-$(OPM_VERSION)
KIND_V_BINARY := $(LOCALBIN)/kind-$(KIND_VERSION)
ACT_V_BINARY := $(LOCALBIN)/act-$(ACT_VERSION)
HELM_V_BINARY := $(LOCALBIN)/helm-$(HELM_VERSION)
GOLANGCI_LINT_V_BINARY := $(LOCALBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)

.PHONY: operator-sdk
operator-sdk: $(OPERATOR_SDK_V_BINARY) ## Download operator-sdk locally if necessary.
$(OPERATOR_SDK_V_BINARY): $(LOCALBIN)
	@./utils/install-operator-sdk.sh $(OPERATOR_SDK)-$(OPERATOR_SDK_VERSION) $(OPERATOR_SDK_VERSION)
	@ln -sf $(shell basename $(OPERATOR_SDK))-$(OPERATOR_SDK_VERSION) $(OPERATOR_SDK)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN_V_BINARY) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN_V_BINARY): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_GEN_VERSION))

.PHONY: kustomize
kustomize: $(KUSTOMIZE_V_BINARY) ## Download kustomize locally if necessary.
$(KUSTOMIZE_V_BINARY): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4,$(KUSTOMIZE_VERSION))

.PHONY: yq
yq: $(YQ_V_BINARY) ## Download yq locally if necessary.
$(YQ_V_BINARY): $(LOCALBIN)
	$(call go-install-tool,$(YQ),github.com/mikefarah/yq/v4,$(YQ_VERSION))

.PHONY: opm
opm: $(OPM_V_BINARY) ## Download opm locally if necessary.
$(OPM_V_BINARY): $(LOCALBIN)
	$(call go-install-tool,$(OPM),github.com/operator-framework/operator-registry/cmd/opm,$(OPM_VERSION))

.PHONY: kind
kind: $(KIND_V_BINARY) ## Download kind locally if necessary.
$(KIND_V_BINARY): $(LOCALBIN)
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind,$(KIND_VERSION))

.PHONY: act
act: $(ACT_V_BINARY) ## Download act locally if necessary.
$(ACT_V_BINARY): $(LOCALBIN)
	$(call go-install-tool,$(ACT),github.com/nektos/act,$(ACT_VERSION))

.PHONY: ginkgo
ginkgo: $(GINKGO) ## Download ginkgo locally if necessary.
$(GINKGO): $(LOCALBIN) go.mod
	# In order to make sure the version of the ginkgo cli installed
	# is the same as the version of go.mod,
	# instead of calling go-install-tool,
	# running go install from the current module will pick version from current go.mod file.
	GOBIN=$(LOCALBIN) go install github.com/onsi/ginkgo/v2/ginkgo

.PHONY: helm
helm: $(HELM_V_BINARY) ## Download helm locally if necessary.
$(HELM_V_BINARY): $(LOCALBIN)
	$(call go-install-tool,$(HELM),helm.sh/helm/v3/cmd/helm,$(HELM_VERSION))

##@ Development
define patch-config
	envsubst \
		< $1 \
		> $2
endef

define update-csv-config
	V="$1" \
	$(YQ) eval '$(3) = strenv(V)' -i $2
endef

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
ifeq ($(WITH_EXTENSIONS),true)
	$(CONTROLLER_GEN) crd paths="./api/v1alpha1;./api/v1beta1;./api/v1;./cmd/extensions/..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=manager-role webhook paths="./internal/...;./cmd/extensions/..."
else
	$(CONTROLLER_GEN) crd paths="./api/v1alpha1;./api/v1beta1;./api/v1" output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=manager-role webhook paths="./internal/..."
endif

.PHONY: extensions-manifests
extensions-manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects for extensions.
	@for ext_dir in $(EXTENSIONS_DIRECTORIES); do \
		ext_name=$$(echo "$$ext_dir" | sed 's/.*\/\([^\/]*\)\/$$/\1/') ; \
		role_name="$$ext_name-manager-role" ;\
		echo "Generating manifests for extension $$ext_name"; \
		$(CONTROLLER_GEN) crd paths="$$ext_dir/api/..." output:crd:artifacts:config="$$ext_dir/config/crd/bases"; \
		$(CONTROLLER_GEN) rbac:roleName="$$role_name" webhook paths="$$ext_dir/..." output:rbac:artifacts:config="$$ext_dir/config/rbac"; \
	done


.PHONY: dependencies-manifests
dependencies-manifests: export AUTHORINO_OPERATOR_GITREF := $(AUTHORINO_OPERATOR_GITREF)
dependencies-manifests: export LIMITADOR_OPERATOR_GITREF := $(LIMITADOR_OPERATOR_GITREF)
dependencies-manifests: export DNS_OPERATOR_GITREF := $(DNS_OPERATOR_GITREF)
dependencies-manifests: export DEVELOPERPORTAL_GITREF := $(DEVELOPERPORTAL_GITREF)
dependencies-manifests: ## Update kuadrant dependencies manifests.
	$(call patch-config,config/dependencies/authorino/kustomization.template.yaml,config/dependencies/authorino/kustomization.yaml)
	$(call patch-config,config/dependencies/limitador/kustomization.template.yaml,config/dependencies/limitador/kustomization.yaml)
	$(call patch-config,config/dependencies/dns/kustomization.template.yaml,config/dependencies/dns/kustomization.yaml)
	$(call patch-config,config/dependencies/developer-portal/kustomization.template.yaml,config/dependencies/developer-portal/kustomization.yaml)

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

.PHONY: clean-cov
clean-cov: ## Remove coverage reports
	rm -rf $(PROJECT_PATH)/coverage

ifdef TEST_NAME
test-unit: TEST_PATTERN := --run $(TEST_NAME)
endif
ifdef VERBOSE
test-unit: VERBOSE_FLAG = -v
endif
test-unit: clean-cov generate fmt vet ## Run Unit tests.
	mkdir -p $(PROJECT_PATH)/coverage/unit
	go test $(UNIT_DIRS) -coverprofile $(PROJECT_PATH)/coverage/unit/cover.out -tags unit $(VERBOSE_FLAG) -timeout 0 $(TEST_PATTERN)

##@ Build

build: GIT_SHA=$(shell git rev-parse HEAD || echo "unknown")
build: DIRTY=$(shell $(PROJECT_PATH)/utils/check-git-dirty.sh || echo "unknown")
build: generate fmt vet ## Build manager binary.
	go build -ldflags "-X main.version=v$(VERSION) -X main.gitSHA=${GIT_SHA} -X main.dirty=${DIRTY}" -o bin/manager cmd/main.go

run: export LOG_LEVEL = debug
run: export LOG_MODE = development
run: export OPERATOR_NAMESPACE := $(OPERATOR_NAMESPACE)
run: GIT_SHA=$(shell git rev-parse HEAD || echo "unknown")
run: DIRTY=$(shell $(PROJECT_PATH)/utils/check-git-dirty.sh || echo "unknown")
run: generate fmt vet ## Run a controller from your host.
	go run -ldflags "-X main.version=v$(VERSION) -X main.gitSHA=${GIT_SHA} -X main.dirty=${DIRTY}" --race ./cmd/main.go

docker-build: GIT_SHA=$(shell git rev-parse HEAD || echo "unknown")
docker-build: DIRTY=$(shell $(PROJECT_PATH)/utils/check-git-dirty.sh || echo "unknown")
docker-build: ## Build docker image with the manager.
		$(CONTAINER_ENGINE) build \
		--build-arg QUAY_IMAGE_EXPIRY=$(QUAY_IMAGE_EXPIRY) \
		--build-arg GIT_SHA=$(GIT_SHA) \
		--build-arg DIRTY=$(DIRTY) \
		--build-arg VERSION=v$(VERSION) \
		--build-arg WITH_EXTENSIONS=$(WITH_EXTENSIONS) \
		$(CONTAINER_ENGINE_EXTRA_FLAGS) \
		-t $(IMG) .

docker-push: ## Push docker image with the manager.
	$(CONTAINER_ENGINE) push $(IMG)

kind-load-image: ## Load image to local cluster
	$(eval TMP_DIR := $(shell mktemp -d))
	$(CONTAINER_ENGINE) save -o $(TMP_DIR)/image.tar $(IMG) \
	   && KIND_EXPERIMENTAL_PROVIDER=$(CONTAINER_ENGINE) $(KIND) load image-archive $(TMP_DIR)/image.tar $(IMG) --name $(KIND_CLUSTER_NAME) ; \
	   EXITVAL=$$? ; \
	   rm -rf $(TMP_DIR) ;\
	   exit $${EXITVAL}

.PHONY: extensions-build
extensions-build: ## Build extensions docker image
	$(CONTAINER_ENGINE) build \
		--build-arg QUAY_IMAGE_EXPIRY=$(QUAY_IMAGE_EXPIRY) \
		$(CONTAINER_ENGINE_EXTRA_FLAGS) \
		-f extension.Dockerfile \
		-t $(EXTENSIONS_IMG) .


# go-install-tool will 'go install' any package with version.
# Parameters: $(1) base binary path, $(2) package, $(3) version
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(shell basename $(1))-$(3) $(1)
endef

.PHONY: bundle
bundle: opm yq manifests dependencies-manifests kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	@echo "Cleaning bundle manifests..."
	rm -rf bundle/manifests
	$(OPERATOR_SDK) generate kustomize manifests -q
	# Set desired Wasm-shim image
	V="$(RELATED_IMAGE_WASMSHIM)" \
	$(YQ) eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_WASMSHIM").value) = strenv(V)' -i config/manager/manager.yaml
	# Set desired developer-portal-controller image
	V="$(RELATED_IMAGE_DEVELOPERPORTAL)" \
	$(YQ) eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_DEVELOPERPORTAL").value) = strenv(V)' -i config/manager/manager.yaml
	# Set desired console-plugin image
	V="$(RELATED_IMAGE_CONSOLE_PLUGIN_LATEST)" \
	$(YQ) eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_CONSOLE_PLUGIN_LATEST").value) = strenv(V)' -i config/manager/manager.yaml
	# Set desired operator image
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	# Update CSV
	$(call update-csv-config,kuadrant-operator.v$(BUNDLE_VERSION),config/manifests/bases/kuadrant-operator.clusterserviceversion.yaml,.metadata.name)
	$(call update-csv-config,$(BUNDLE_VERSION),config/manifests/bases/kuadrant-operator.clusterserviceversion.yaml,.spec.version)
	$(call update-csv-config,$(IMG),config/manifests/bases/kuadrant-operator.clusterserviceversion.yaml,.metadata.annotations.containerImage)
	# Generate bundle
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle -q --overwrite --version $(BUNDLE_VERSION) $(BUNDLE_METADATA_OPTS) --extra-service-accounts=developer-portal-controller-manager
	$(MAKE) bundle-post-generate LIMITADOR_OPERATOR_BUNDLE_IMG=$(LIMITADOR_OPERATOR_BUNDLE_IMG) \
		AUTHORINO_OPERATOR_BUNDLE_IMG=$(AUTHORINO_OPERATOR_BUNDLE_IMG) \
		DNS_OPERATOR_BUNDLE_IMG=$(DNS_OPERATOR_BUNDLE_IMG)
	$(OPERATOR_SDK) bundle validate ./bundle
	$(MAKE) bundle-ignore-createdAt
	echo "$$QUAY_EXPIRY_TIME_LABEL" >> bundle.Dockerfile

.PHONY: bundle-post-generate
bundle-post-generate: OPENSHIFT_VERSIONS_ANNOTATION_KEY="com.redhat.openshift.versions"
# Supports Openshift v4.12+ (https://redhat-connect.gitbook.io/certified-operator-guide/ocp-deployment/operator-metadata/bundle-directory/managing-openshift-versions)
bundle-post-generate: OPENSHIFT_SUPPORTED_VERSIONS="v4.14"
bundle-post-generate: yq opm
	# Set Openshift version in bundle annotations
	$(YQ) -i '.annotations[$(OPENSHIFT_VERSIONS_ANNOTATION_KEY)] = $(OPENSHIFT_SUPPORTED_VERSIONS)' bundle/metadata/annotations.yaml
	$(YQ) -i '(.annotations[$(OPENSHIFT_VERSIONS_ANNOTATION_KEY)] | key) headComment = "Custom annotations"' bundle/metadata/annotations.yaml
	# Update operator dependencies
	PATH=$(PROJECT_PATH)/bin:$$PATH; \
			 $(PROJECT_PATH)/utils/update-operator-dependencies.sh limitador-operator $(LIMITADOR_OPERATOR_BUNDLE_IMG)
	PATH=$(PROJECT_PATH)/bin:$$PATH; \
			 $(PROJECT_PATH)/utils/update-operator-dependencies.sh authorino-operator $(AUTHORINO_OPERATOR_BUNDLE_IMG)
	PATH=$(PROJECT_PATH)/bin:$$PATH; \
			 $(PROJECT_PATH)/utils/update-operator-dependencies.sh dns-operator $(DNS_OPERATOR_BUNDLE_IMG)

.PHONY: bundle-ignore-createdAt
bundle-ignore-createdAt:
	# Since operator-sdk 1.26.0, `make bundle` changes the `createdAt` field from the bundle
	# even if it is patched:
	#   https://github.com/operator-framework/operator-sdk/pull/6136
	# This code checks if only the createdAt field. If is the only change, it is ignored.
	# Else, it will do nothing.
	# https://github.com/operator-framework/operator-sdk/issues/6285#issuecomment-1415350333
	# https://github.com/operator-framework/operator-sdk/issues/6285#issuecomment-1532150678
	git diff --quiet -I'^    createdAt: ' ./bundle && git checkout ./bundle || true

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	$(CONTAINER_ENGINE) build --build-arg QUAY_IMAGE_EXPIRY=$(QUAY_IMAGE_EXPIRY) -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

##@ Release

.PHONY: prepare-release
prepare-release: yq controller-gen opm kustomize operator-sdk ## Prepare the manifests for OLM and Helm Chart for a release.
	PATH=$(PROJECT_PATH)/bin:$$PATH; $(PROJECT_PATH)/utils/release/release.sh


.PHONY: bundle-operator-image-url
bundle-operator-image-url: yq ## Read operator image reference URL from the manifest bundle.
	@$(YQ) '.metadata.annotations.containerImage' bundle/manifests/kuadrant-operator.clusterserviceversion.yaml

.PHONY: read-release-version
read-release-version: ## Reads release version
	@echo "v$(VERSION)"

print-bundle-image: ## Print bundle image
	@echo $(BUNDLE_IMG)

print-operator-repo: ## Print operator repo
	@echo $(IMAGE_TAG_BASE)

print-operator-image: ## Print operator image
	@echo $(IMG)

.PHONY: update-catalogsource
update-catalogsource:
	@$(YQ) e -i '.spec.image = "${CATALOG_IMG}"' config/deploy/olm/catalogsource.yaml

##@ Code Style

.PHONY: run-lint
run-lint: golangci-lint ## Run lint tests
	$(GOLANGCI_LINT) run

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT_V_BINARY) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT_V_BINARY): $(LOCALBIN)
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(LOCALBIN) $(GOLANGCI_LINT_VERSION)
	@mv $(GOLANGCI_LINT) $(GOLANGCI_LINT)-$(GOLANGCI_LINT_VERSION)
	@ln -sf $(shell basename $(GOLANGCI_LINT))-$(GOLANGCI_LINT_VERSION) $(GOLANGCI_LINT)

##@ Pre-commit

# Set to specify which integration test environment to run:
# - bare-k8s: Basic Kubernetes without gateway provider
# - gatewayapi: GatewayAPI CRDs without gateway provider
# - integration-istio: General integration tests with istio provider only
# - integration-envoygateway: General integration tests with envoygateway provider only
# - istio: Full Istio environment
# - envoygateway: Full EnvoyGateway environment
# - all: Run all integration tests sequentially
# - (empty): Skip all integration tests
INTEGRATION_TEST_ENV ?=

# Integration test configurations using colon delimiters
INTEGRATION_CONFIGS := \
	bare-k8s:local-k8s-env-setup:test-bare-k8s-integration: \
	gatewayapi:local-gatewayapi-env-setup:test-gatewayapi-env-integration: \
	istio:local-env-setup:test-istio-env-integration:GATEWAYAPI_PROVIDER=istio \
	envoygateway:local-env-setup:test-envoygateway-env-integration:GATEWAYAPI_PROVIDER=envoygateway \
	integration-istio:local-env-setup:test-integration:GATEWAYAPI_PROVIDER=istio \
	integration-envoygateway:local-env-setup:test-integration:GATEWAYAPI_PROVIDER=envoygateway

# Extract valid environment names
VALID_INTEGRATION_ENVS := $(foreach config,$(INTEGRATION_CONFIGS),$(word 1,$(subst :, ,$(config))))

# Function to get config value for an environment
# Usage: $(call get_config,env_name,field_number)
get_config = $(word $(2),$(subst :, ,$(filter $(1):%,$(INTEGRATION_CONFIGS))))


define run_integration_test
$(if $(call get_config,$(1),2),
	@echo "  🔧 Running $(1) integration tests..."
	$(MAKE) $(call get_config,$(1),2) $(call get_config,$(1),4)
	$(MAKE) $(call get_config,$(1),3) $(call get_config,$(1),4)
	$(MAKE) local-cleanup
	@echo "  ✅ $(1) integration tests completed!"
,
	$(error Invalid INTEGRATION_TEST_ENV=$(1). Valid values: $(VALID_INTEGRATION_ENVS), "all" for all tests, or leave empty to skip integration tests)
)
endef

.PHONY: pre-commit
pre-commit: ## Run pre-commit checks including verification, linting, unit tests, and integration tests
	@echo "🚀 Running pre-commit checks..."
	@echo "1️⃣ Running verification checks..."
	$(MAKE) verify-all
	@echo "2️⃣ Running lint checks..."
	$(MAKE) run-lint
	@echo "3️⃣ Running unit tests..."
	$(MAKE) test-unit
ifeq ($(INTEGRATION_TEST_ENV),)
	@echo "4️⃣	Skipping integration tests (set INTEGRATION_TEST_ENV to run tests)..."
else ifeq ($(INTEGRATION_TEST_ENV),all)
	@echo "4️⃣	Running all integration tests sequentially..."
	@for env in $(VALID_INTEGRATION_ENVS); do \
		echo "📦	Testing $$env environment..."; \
		$(MAKE) pre-commit-single ENV=$$env; \
	done
else
	@echo "4️⃣	Running integration tests for environment(s): $(INTEGRATION_TEST_ENV)..."
	@for env in $(INTEGRATION_TEST_ENV); do \
		echo "📦	Testing $$env environment..."; \
		$(MAKE) pre-commit-single ENV=$$env; \
	done
endif
	@echo "🎉	All pre-commit checks completed successfully!"

.PHONY: pre-commit-single
pre-commit-single:
	$(call run_integration_test,$(ENV))

# Include last to avoid changing MAKEFILE_LIST used above
include ./make/*.mk
