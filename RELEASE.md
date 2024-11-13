# How to release Kuadrant Operator

## Process

To release a version _“v0.W.Z”_ of Kuadrant Operator in GitHub and Quay.io, follow these steps:

1. Kuadrant dependencies need to be released first:
   * [Authorino Operator](https://github.com/Kuadrant/authorino-operator/blob/main/RELEASE.md).
   * [Limitador Operator](https://github.com/Kuadrant/limitador-operator/blob/main/RELEASE.md).
   * [DNS Operator](https://github.com/Kuadrant/dns-operator/blob/main/docs/RELEASE.md).
   * [WASM Shim](https://github.com/Kuadrant/wasm-shim/).
   * [Console Plugin](https://github.com/Kuadrant/kuadrant-console-plugin).

2. Run the GHA [Release operator](https://github.com/Kuadrant/kuadrant-operator/actions/workflows/release.yaml); make
   sure to fill all the fields:

    * Branch containing the release workflow file – default: `main`
    * Commit SHA or branch name of the operator to release – usually: `main`
    * Operator version to release (without prefix) – i.e. `0.W.Z`
    * Kuadrant dependencies (WASM Shim, Console Plugin, Authorino, Limitador and DNS operators) versions (without prefix) – i.e. `0.X.Y`
    * If the release is a prerelease

3. Verify that the build [release tag workflow](https://github.com/Kuadrant/kuadrant-operator/actions/workflows/build-images-for-tag-release.yaml) is triggered and completes for the new tag.

4. Verify the new version can be installed from the catalog image, see [Verify OLM Deployment](#verify-olm-deployment)

5. Release to the [community operator index catalogs](#community-operator-index-catalogs).

### Verify OLM Deployment

1. Deploy the OLM catalog image following the [Deploy kuadrant operator using OLM](/doc/development.md#deploy-kuadrant-operator-using-olm) and providing the generated catalog image. For example:
```sh
make deploy-catalog CATALOG_IMG=quay.io/kuadrant/kuadrant-operator-catalog:v1.0.0-rc4
```

2. Wait for deployment:
```sh
kubectl -n kuadrant-system wait --timeout=60s --for=condition=Available deployments --all
```

The output should be:

```
deployment.apps/authorino-operator condition met
deployment.apps/dns-operator-controller-manager condition met
deployment.apps/kuadrant-operator-controller-manager condition met
deployment.apps/limitador-operator-controller-manager condition met
deployment.apps/kuadrant-operator-controller-manager condition met
```

3. Check the logs:
```sh
kubectl -n kuadrant-system logs -f deployment/kuadrant-operator-controller-manager
```

4. Check the version of the components deployed:
```sh
kubectl -n kuadrant-system get deployment -o yaml | grep "image:"
```
The output should be something like:

```
image: quay.io/kuadrant/authorino-operator:v0.14.0
image: quay.io/kuadrant/dns-operator:v0.8.0
image: quay.io/kuadrant/kuadrant-operator:v1.0.0-rc4
image: quay.io/kuadrant/limitador-operator:v0.12.0
```

### Community Operator Index Catalogs

- [Operatorhub Community Operators](https://github.com/k8s-operatorhub/community-operators)
- [Openshift Community Operators](http://github.com/redhat-openshift-ecosystem/community-operators-prod)

Open a PR on each index catalog ([example](https://github.com/redhat-openshift-ecosystem/community-operators-prod/pull/1595) |
[docs](https://redhat-openshift-ecosystem.github.io/community-operators-prod/operator-release-process/)).

The usual steps are:

1. Start a new branch named `kuadrant-operator-v0.W.Z`

2. Create a new directory `operators/kuadrant-operator/0.W.Z` containing:

    * Copy the bundle files from `github.com/kuadrant/kuadrant-operator/tree/v0.W.Z/bundle`
    * Copy `github.com/kuadrant/kuadrant-operator/tree/v0.W.Z/bundle.Dockerfile` with the proper fix to the COPY commands
      (i.e. remove /bundle from the paths)
