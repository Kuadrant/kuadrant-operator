# Development Guide

<!--ts-->
   * [Technology stack required for development](#technology-stack-required-for-development)
   * [Build](#build)
   * [Run locally](#run-locally)
   * [Deploy the operator in a deployment object](#deploy-the-operator-in-a-deployment-object)
   * [Deploy kuadrant operator using OLM](#deploy-kuadrant-operator-using-olm)
   * [Build custom OLM catalog](#build-custom-olm-catalog)
      * [Build kuadrant operator bundle image](#build-kuadrant-operator-bundle-image)
      * [Build custom catalog](#build-custom-catalog)
   * [Cleaning up](#cleaning-up)
   * [Run tests](#run-tests)
      * [Unit tests](#unittests)
      * [Integration tests](#integration-tests)
      * [All tests](#all-tests)
      * [Lint tests](#lint-tests)
   * [(Un)Install Kuadrant CRDs](#uninstall-kuadrant-crds)

<!-- Created by https://github.com/ekalinin/github-markdown-toc -->

<!--te-->

## Technology stack required for development

* [operator-sdk] version v1.28.1
* [kind] version v0.20.0
* [git][git_tool]
* [go] version 1.21+
* [kubernetes] version v1.19+
* [kubectl] version v1.19+

## Build

```sh
make
```

## Run locally

You need an active session open to a kubernetes cluster.

Optionally, run kind and deploy kuadrant deps

```sh
make local-env-setup
```

Then, run the operator locally

```sh
make run
```

## Deploy the operator in a deployment object

```sh
make local-setup
```

List of tasks done by the command above:

* Create local cluster using kind
* Build kuadrant docker image from the current working directory
* Deploy Kuadrant control plane (including istio, authorino and limitador)

TODO: customize with custom authorino and limitador git refs.
Make sure Makefile propagates variable to `deploy` target

## Deploy kuadrant operator using OLM

You can deploy kuadrant using OLM just running few commands.
No need to build any image. Kuadrant engineering team provides `latest` and
release version tagged images. They are available in
the [Quay.io/Kuadrant](https://quay.io/organization/kuadrant) image repository.

Create kind cluster

```sh
make kind-create-cluster
```

Deploy OLM system

```sh
make install-olm
```

Deploy kuadrant using OLM. The `make deploy-catalog` target accepts the following variables:

| **Makefile Variable** | **Description**                     | **Default value**                                   |
|-----------------------|-------------------------------------|-----------------------------------------------------|
| `CATALOG_IMG`         | Kuadrant operator catalog image URL | `quay.io/kuadrant/kuadrant-operator-catalog:latest` |

```sh
make deploy-catalog [CATALOG_IMG=quay.io/kuadrant/kuadrant-operator-catalog:latest]
```

## Build custom OLM catalog

If you want to deploy (using OLM) a custom kuadrant operator, you need to build your own catalog.
Furthermore, if you want to deploy a custom limitador or authorino operator, you also need
to build your own catalog. The kuadrant operator bundle includes the authorino or limtador operator
dependency version, hence using other than `latest` version requires a custom kuadrant operator
bundle and a custom catalog including the custom bundle.

### Build kuadrant operator bundle image

The `make bundle` target accepts the following variables:

| **Makefile Variable**           | **Description**               | **Default value**                                   | **Notes**                                                                                          |
|---------------------------------|-------------------------------|-----------------------------------------------------|----------------------------------------------------------------------------------------------------|
| `IMG`                           | Kuadrant operator image URL   | `quay.io/kuadrant/kuadrant-operator:latest`         | `TAG` var could be use to build this URL, defaults to _latest_  if not provided                    |
| `VERSION`                       | Bundle version                | `0.0.0`                                             |                                                                                                    |
| `LIMITADOR_OPERATOR_BUNDLE_IMG` | Limitador operator bundle URL | `quay.io/kuadrant/limitador-operator-bundle:latest` | `LIMITADOR_OPERATOR_VERSION` var could be used to build this, defaults to _latest_ if not provided |
| `AUTHORINO_OPERATOR_BUNDLE_IMG` | Authorino operator bundle URL | `quay.io/kuadrant/authorino-operator-bundle:latest` | `AUTHORINO_OPERATOR_VERSION` var could be used to build this, defaults to _latest_ if not provided |
| `RELATED_IMAGE_WASMSHIM`        | WASM shim image URL           | `oci://quay.io/kuadrant/wasm-shim:latest`           | `WASM_SHIM_VERSION` var could be used to build this, defaults to _latest_ if not provided          |

* Build the bundle manifests

```bash

make bundle [IMG=quay.io/kuadrant/kuadrant-operator:latest] \
            [VERSION=0.0.0] \
            [LIMITADOR_OPERATOR_BUNDLE_IMG=quay.io/kuadrant/limitador-operator-bundle:latest] \
            [AUTHORINO_OPERATOR_BUNDLE_IMG=quay.io/kuadrant/authorino-operator-bundle:latest] \
            [RELATED_IMAGE_WASMSHIM=oci://quay.io/kuadrant/wasm-shim:latest]
```

* Build the bundle image from the manifests

| **Makefile Variable** | **Description**                    | **Default value**                                  |
|-----------------------|------------------------------------|----------------------------------------------------|
| `BUNDLE_IMG`          | Kuadrant operator bundle image URL | `quay.io/kuadrant/kuadrant-operator-bundle:latest` |

```sh
make bundle-build [BUNDLE_IMG=quay.io/kuadrant/kuadrant-operator-bundle:latest]
```

* Push the bundle image to a registry

| **Makefile Variable** | **Description**                    | **Default value**                                  |
|-----------------------|------------------------------------|----------------------------------------------------|
| `BUNDLE_IMG`          | Kuadrant operator bundle image URL | `quay.io/kuadrant/kuadrant-operator-bundle:latest` |

```sh
make bundle-push [BUNDLE_IMG=quay.io/kuadrant/kuadrant-operator-bundle:latest]
```

Frequently, you may need to build custom kuadrant bundle with the default (`latest`) Limitador and
Authorino bundles. These are the example commands to build the manifests, build the bundle image
and push to the registry.

In the example, a new kuadrant operator bundle version `0.8.0` will be created that references
the kuadrant operator image `quay.io/kuadrant/kuadrant-operator:v0.5.0` and latest Limitador and
Authorino bundles.

```sh
# manifests
make bundle IMG=quay.io/kuadrant/kuadrant-operator:v0.5.0 VERSION=0.8.0

# bundle image
make bundle-build BUNDLE_IMG=quay.io/kuadrant/kuadrant-operator-bundle:my-bundle

# push bundle image
make bundle-push BUNDLE_IMG=quay.io/kuadrant/kuadrant-operator-bundle:my-bundle
```

### Build custom catalog

The catalog's format will be [File-based Catalog](https://olm.operatorframework.io/docs/reference/file-based-catalogs/).

Make sure all the required bundles are pushed to the registry. It is required by the `opm` tool.

The `make catalog` target accepts the following variables:

| **Makefile Variable**           | **Description**                    | **Default value**                                   |
|---------------------------------|------------------------------------|-----------------------------------------------------|
| `BUNDLE_IMG`                    | Kuadrant operator bundle image URL | `quay.io/kuadrant/kuadrant-operator-bundle:latest`  |
| `LIMITADOR_OPERATOR_BUNDLE_IMG` | Limitador operator bundle URL      | `quay.io/kuadrant/limitador-operator-bundle:latest` |
| `AUTHORINO_OPERATOR_BUNDLE_IMG` | Authorino operator bundle URL      | `quay.io/kuadrant/authorino-operator-bundle:latest` |

```sh
make catalog [BUNDLE_IMG=quay.io/kuadrant/kuadrant-operator-bundle:latest] \
            [LIMITADOR_OPERATOR_BUNDLE_IMG=quay.io/kuadrant/limitador-operator-bundle:latest] \
            [AUTHORINO_OPERATOR_BUNDLE_IMG=quay.io/kuadrant/authorino-operator-bundle:latest]
```

* Build the catalog image from the manifests

| **Makefile Variable** | **Description**                     | **Default value**                                   |
|-----------------------|-------------------------------------|-----------------------------------------------------|
| `CATALOG_IMG`         | Kuadrant operator catalog image URL | `quay.io/kuadrant/kuadrant-operator-catalog:latest` |

```sh
make catalog-build [CATALOG_IMG=quay.io/kuadrant/kuadrant-operator-catalog:latest]
```

* Push the catalog image to a registry

```sh
make catalog-push [CATALOG_IMG=quay.io/kuadrant/kuadrant-operator-bundle:latest]
```

You can try out your custom catalog image following the steps of the
[Deploy kuadrant operator using OLM](#deploy-kuadrant-operator-using-olm) section.

## Cleaning up

```sh
make local-cleanup
```

## Run tests

### Unittests

```sh
make test-unit
```

Optionally, add `TEST_NAME` makefile variable to run specific test

```sh
make test-unit TEST_NAME=TestLimitIndexEquals
```

or even subtest

```sh
make test-unit TEST_NAME=TestLimitIndexEquals/empty_indexes_are_equal
```

### Integration tests

You need an active session open to a kubernetes cluster.

Optionally, run kind and deploy kuadrant deps

```sh
make local-env-setup
```

Run integration tests

```sh
make test-integration
```

### All tests

You need an active session open to a kubernetes cluster.

Optionally, run kind and deploy kuadrant deps

```sh
make local-env-setup
```

Run all tests

```sh
make test
```

### Lint tests

```sh
make run-lint
```

## (Un)Install Kuadrant CRDs

You need an active session open to a kubernetes cluster.

Remove CRDs

```sh
make uninstall
```

[git_tool]:https://git-scm.com/downloads
[operator-sdk]:https://github.com/operator-framework/operator-sdk
[go]:https://golang.org/
[kind]:https://kind.sigs.k8s.io/
[kubernetes]:https://kubernetes.io/
[kubectl]:https://kubernetes.io/docs/tasks/tools/#kubectl
