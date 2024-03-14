# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

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

DEFAULT_IMAGE_TAG = latest
DEFAULT_REPLACES_VERSION = 0.0.0-alpha

REPLACES_VERSION ?= $(DEFAULT_REPLACES_VERSION)

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
UNIT_DIRS := ./pkg/... ./api/... ./controllers/...
INTEGRATION_TEST_SUITE_PATHS := ./controllers/...
INTEGRATION_COVER_PKGS := ./pkg/...,./controllers/...,./api/...

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Kuadrant Namespace
KUADRANT_NAMESPACE ?= kuadrant-system

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

OPERATOR_SDK = $(PROJECT_PATH)/bin/operator-sdk
OPERATOR_SDK_VERSION = v1.32.0
$(OPERATOR_SDK):
	./utils/install-operator-sdk.sh $(OPERATOR_SDK) $(OPERATOR_SDK_VERSION)

.PHONY: operator-sdk
operator-sdk: $(OPERATOR_SDK) ## Download operator-sdk locally if necessary.

CONTROLLER_GEN = $(PROJECT_PATH)/bin/controller-gen
$(CONTROLLER_GEN):
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN)  ## Download controller-gen locally if necessary.

KUSTOMIZE = $(PROJECT_PATH)/bin/kustomize
$(KUSTOMIZE):
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.5.5)

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.

YQ=$(PROJECT_PATH)/bin/yq
YQ_VERSION := v4.34.2
$(YQ):
	$(call go-install-tool,$(YQ),github.com/mikefarah/yq/v4@$(YQ_VERSION))

.PHONY: yq
yq: $(YQ) ## Download yq locally if necessary.

OPM = $(PROJECT_PATH)/bin/opm
OPM_VERSION = v1.26.2
$(OPM):
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/$(OPM_VERSION)/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}

.PHONY: opm
opm: $(OPM) ## Download opm locally if necessary.

KIND = $(PROJECT_PATH)/bin/kind
KIND_VERSION = v0.22.0
$(KIND):
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind@$(KIND_VERSION))

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.

ACT = $(PROJECT_PATH)/bin/act
$(ACT):
	$(call go-install-tool,$(ACT),github.com/nektos/act@latest)

.PHONY: act
act: $(ACT) ## Download act locally if necessary.

GINKGO = $(PROJECT_PATH)/bin/ginkgo
$(GINKGO):
	# In order to make sure the version of the ginkgo cli installed
	# is the same as the version of go.mod,
	# instead of calling go-install-tool,
	# running go install from the current module will pick version from current go.mod file.
	GOBIN=$(PROJECT_DIR)/bin go install github.com/onsi/ginkgo/v2/ginkgo

.PHONY: ginkgo
ginkgo: $(GINKGO) ## Download ginkgo locally if necessary.

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

define update-operator-dependencies
	V=`$(PROJECT_PATH)/utils/parse-bundle-version.sh $(OPM) $(YQ) $2` \
	$(YQ) eval '(.dependencies[] | select(.value.packageName == "$1").value.version) = strenv(V)' -i bundle/metadata/dependencies.yaml
endef

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd paths="./api/v1alpha1;./api/v1beta1;./api/v1beta2" output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=manager-role webhook paths="./..."

.PHONY: dependencies-manifests
dependencies-manifests: export AUTHORINO_OPERATOR_GITREF := $(AUTHORINO_OPERATOR_GITREF)
dependencies-manifests: export LIMITADOR_OPERATOR_GITREF := $(LIMITADOR_OPERATOR_GITREF)
dependencies-manifests: export DNS_OPERATOR_GITREF := $(DNS_OPERATOR_GITREF)
dependencies-manifests: ## Update kuadrant dependencies manifests.
	$(call patch-config,config/dependencies/authorino/kustomization.template.yaml,config/dependencies/authorino/kustomization.yaml)
	$(call patch-config,config/dependencies/limitador/kustomization.template.yaml,config/dependencies/limitador/kustomization.yaml)
	$(call patch-config,config/dependencies/dns/kustomization.template.yaml,config/dependencies/dns/kustomization.yaml)

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

.PHONY: test
test: test-unit test-integration ## Run all tests

test-integration: clean-cov generate fmt vet ginkgo ## Run Integration tests.
	mkdir -p $(PROJECT_PATH)/coverage/integration
#	Check `ginkgo help run` for command line options. For example to filtering tests.
	$(GINKGO) \
		--coverpkg $(INTEGRATION_COVER_PKGS) \
		--output-dir $(PROJECT_PATH)/coverage/integration \
		--coverprofile cover.out \
		-tags integration \
		$(INTEGRATION_TEST_SUITE_PATHS)

ifdef TEST_NAME
test-unit: TEST_PATTERN := --run $(TEST_NAME)
endif
test-unit: clean-cov generate fmt vet ## Run Unit tests.
	mkdir -p $(PROJECT_PATH)/coverage/unit
	go test $(UNIT_DIRS) -coverprofile $(PROJECT_PATH)/coverage/unit/cover.out -tags unit -v -timeout 0 $(TEST_PATTERN)

.PHONY: namespace
namespace: ## Creates a namespace where to deploy Kuadrant Operator
	kubectl create namespace $(KUADRANT_NAMESPACE)

.PHONY: local-deploy
local-deploy: ## Deploy Kuadrant Operator in the cluster pointed by KUBECONFIG
	$(MAKE) docker-build IMG=$(IMAGE_TAG_BASE):dev
	$(KIND) load docker-image $(IMAGE_TAG_BASE):dev --name $(KIND_CLUSTER_NAME)
	$(MAKE) deploy IMG=$(IMAGE_TAG_BASE):dev
	kubectl -n $(KUADRANT_NAMESPACE) wait --timeout=300s --for=condition=Available deployments --all
	@echo
	@echo "Now you can export the kuadrant gateway by doing:"
	@echo "kubectl port-forward -n istio-system service/istio-ingressgateway-istio 9080:80 &"
	@echo "export GATEWAY_URL=localhost:9080"
	@echo "after that, you can curl -H \"Host: myhost.com\" \$$GATEWAY_URL"
	@echo "-- Linux only -- Ingress gateway is exported using loadbalancer service in port 80"
	@echo "export INGRESS_HOST=\$$(kubectl get gtw istio-ingressgateway -n istio-system -o jsonpath='{.status.addresses[0].value}')"
	@echo "export INGRESS_PORT=\$$(kubectl get gtw istio-ingressgateway -n istio-system -o jsonpath='{.spec.listeners[?(@.name==\"http\")].port}')"
	@echo "export GATEWAY_URL=\$$INGRESS_HOST:\$$INGRESS_PORT"
	@echo "curl -H \"Host: myhost.com\" \$$GATEWAY_URL"
	@echo

.PHONY: local-setup
local-setup: $(KIND) ## Deploy locally kuadrant operator from the current code
	$(MAKE) local-env-setup
	$(MAKE) local-deploy

.PHONY: local-cleanup
local-cleanup: ## Delete local cluster
	$(MAKE) kind-delete-cluster

.PHONY: local-cluster-setup
local-cluster-setup: ## Sets up Kind cluster with GatewayAPI manifests and istio GW, nothing Kuadrant.
	$(MAKE) kind-delete-cluster
	$(MAKE) kind-create-cluster
	$(MAKE) deploy-metrics-server
	$(MAKE) namespace
	$(MAKE) gateway-api-install
	$(MAKE) install-metallb
	$(MAKE) istio-install
	$(MAKE) install-cert-manager
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
	$(MAKE) install-metallb
	$(MAKE) istio-install
	$(MAKE) install-cert-manager
	$(MAKE) deploy-gateway
	$(MAKE) deploy-dependencies
	$(MAKE) install

##@ Build

build: generate fmt vet ## Build manager binary.
	go build -o bin/manager main.go

run: export LOG_LEVEL = debug
run: export LOG_MODE = development
run: generate fmt vet ## Run a controller from your host.
	go run ./main.go

docker-build: ## Build docker image with the manager.
	docker build -t $(IMG) .  --load

docker-push: ## Push docker image with the manager.
	docker push $(IMG)

kind-load-image: ## Load image to local cluster
	$(KIND) load docker-image $(IMG) --name $(KIND_CLUSTER_NAME)

kind-load-bundle: ## Load image to local cluster
	$(KIND) load docker-image $(BUNDLE_IMG) --name $(KIND_CLUSTER_NAME)

##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	# Use server side apply, otherwise will hit into this issue https://medium.com/pareture/kubectl-install-crd-failed-annotations-too-long-2ebc91b40c7d
	$(KUSTOMIZE) build config/crd | kubectl apply --server-side -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: manifests dependencies-manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/deploy | kubectl apply --server-side -f -
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMAGE_TAG_BASE):latest

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/deploy | kubectl delete -f -

deploy-dependencies: kustomize dependencies-manifests ## Deploy dependencies to the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/dependencies | kubectl apply -f -
	kubectl -n "$(KUADRANT_NAMESPACE)" wait --timeout=300s --for=condition=Available deployments --all

.PHONY: install-metallb
install-metallb: kustomize yq ## Installs the metallb load balancer allowing use of an LoadBalancer type with a gateway
	$(KUSTOMIZE) build config/metallb | kubectl apply -f -
	kubectl -n metallb-system wait --for=condition=Available deployments controller --timeout=300s
	kubectl -n metallb-system wait --for=condition=ready pod --selector=app=metallb --timeout=60s
	./utils/docker-network-ipaddresspool.sh kind $(YQ) | kubectl apply -n metallb-system -f -

.PHONY: uninstall-metallb
uninstall-metallb: $(KUSTOMIZE)
	$(KUSTOMIZE) build config/metallb | kubectl delete -f -

.PHONY: install-olm
install-olm: $(OPERATOR_SDK)
	$(OPERATOR_SDK) olm install

.PHONY: uninstall-olm
uninstall-olm:
	$(OPERATOR_SDK) olm uninstall

deploy-catalog: $(KUSTOMIZE) $(YQ) ## Deploy operator to the K8s cluster specified in ~/.kube/config using OLM catalog image.
	V="$(CATALOG_IMG)" $(YQ) eval '.spec.image = strenv(V)' -i config/deploy/olm/catalogsource.yaml
	$(KUSTOMIZE) build config/deploy/olm | kubectl apply -f -

undeploy-catalog: $(KUSTOMIZE) ## Undeploy controller from the K8s cluster specified in ~/.kube/config using OLM catalog image.
	$(KUSTOMIZE) build config/deploy/olm | kubectl delete -f -


# go-install-tool will 'go install' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-install-tool
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

.PHONY: bundle
bundle: $(OPM) $(YQ) manifests dependencies-manifests kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q
	# Set desired operator image and related wasm shim image
	V="$(RELATED_IMAGE_WASMSHIM)" \
	$(YQ) eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_WASMSHIM").value) = strenv(V)' -i config/manager/manager.yaml
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	# Update CSV
	$(call update-csv-config,kuadrant-operator.v$(BUNDLE_VERSION),config/manifests/bases/kuadrant-operator.clusterserviceversion.yaml,.metadata.name)
	$(call update-csv-config,$(BUNDLE_VERSION),config/manifests/bases/kuadrant-operator.clusterserviceversion.yaml,.spec.version)
	$(call update-csv-config,$(IMG),config/manifests/bases/kuadrant-operator.clusterserviceversion.yaml,.metadata.annotations.containerImage)
	$(call update-csv-config,kuadrant-operator.v$(REPLACES_VERSION),config/manifests/bases/kuadrant-operator.clusterserviceversion.yaml,.spec.replaces)
	# Generate bundle
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle -q --overwrite --version $(BUNDLE_VERSION) $(BUNDLE_METADATA_OPTS)
	# Update operator dependencies
	# TODO(eguzki): run only if not default one. Avoid bundle parsing if version is known in advance
	$(call update-operator-dependencies,limitador-operator,$(LIMITADOR_OPERATOR_BUNDLE_IMG))
	$(call update-operator-dependencies,authorino-operator,$(AUTHORINO_OPERATOR_BUNDLE_IMG))
	$(call update-operator-dependencies,dns-operator,$(DNS_OPERATOR_BUNDLE_IMG))
	$(OPERATOR_SDK) bundle validate ./bundle
	$(MAKE) bundle-ignore-createdAt

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
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

##@ Code Style

GOLANGCI-LINT = $(PROJECT_PATH)/bin/golangci-lint

.PHONY: run-lint
run-lint: $(GOLANGCI-LINT) ## Run lint tests
	$(GOLANGCI-LINT) run

$(GOLANGCI-LINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(PROJECT_PATH)/bin v1.54.2

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI-LINT) ## Download golangci-lint locally if necessary.


# Include last to avoid changing MAKEFILE_LIST used above
include ./make/*.mk
